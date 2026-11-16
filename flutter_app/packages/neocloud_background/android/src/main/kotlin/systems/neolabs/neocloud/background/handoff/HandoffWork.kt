package systems.neolabs.neocloud.background.handoff

import android.content.Context
import android.util.Log
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkManager
import java.util.concurrent.TimeUnit

/**
 * Switching background receiving on and off, and what runs for it:
 *
 *   - [HandoffService]: the live connection that brings offers at once;
 *   - [PollWorker]: every ~15 minutes, looks for waiting offers when the
 *     service isn't running (Android stopped it, or it may not start);
 *   - [ReceiveWorker]: one answer (accept and download, or decline).
 */
object HandoffWork {
    private const val TAG = "NeoCloudHandoff"
    private const val POLL_WORK = "systems.neolabs.neocloud.handoff.poll"
    private const val POLL_MINUTES = 15L

    /** Receiving is on and the device token is known. */
    fun enabled(context: Context): Boolean = HandoffConfig.load(context) != null

    /**
     * Books the fallback poll and tries to start the service. From the app,
     * or after a boot, Android lets it start; from background work it may
     * not (then the poll alone carries on).
     */
    fun apply(context: Context): Boolean {
        val app = context.applicationContext
        if (!enabled(app)) {
            disable(app)
            return false
        }
        try {
            val request = PeriodicWorkRequest.Builder(PollWorker::class.java, POLL_MINUTES, TimeUnit.MINUTES)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .build()
            WorkManager.getInstance(app).enqueueUniquePeriodicWork(POLL_WORK, ExistingPeriodicWorkPolicy.UPDATE, request)
        } catch (error: Exception) {
            Log.w(TAG, "Could not book the check for waiting files", error)
        }
        return HandoffService.running || HandoffService.start(app)
    }

    /** After a reboot or an app update. */
    fun restore(context: Context) {
        apply(context)
    }

    /** Off: no service, no poll, nothing on screen; the token is forgotten. */
    fun disable(context: Context) {
        val app = context.applicationContext
        HandoffConfig.clear(app)
        HandoffService.stop(app)
        try {
            WorkManager.getInstance(app).cancelUniqueWork(POLL_WORK)
        } catch (error: Exception) {
            Log.w(TAG, "Could not cancel the check for waiting files", error)
        }
        OfferNotifier.cancelAll(app)
        NotifiedOffers.clear(app)
    }

    /** The server refused the token: signed out, revoked or wiped. */
    fun signedOut(context: Context, wipeRequested: Boolean) {
        if (wipeRequested) HandoffConfig.markWipeRequested(context)
        disable(context)
    }

    /** The person answered a notification. */
    fun answer(context: Context, offerId: String, fromName: String, accept: Boolean) {
        val app = context.applicationContext
        WorkManager.getInstance(app).enqueueUniqueWork(
            "systems.neolabs.neocloud.handoff.answer.$offerId",
            ExistingWorkPolicy.KEEP,
            ReceiveWorker.request(offerId, fromName, accept),
        )
    }
}
