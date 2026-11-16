package systems.neolabs.neocloud.background.handoff

import android.content.Context
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.IOException

/**
 * The battery-friendly fallback: about every fifteen minutes it asks the
 * server for offers that wait for this device and tells the person about any
 * it hasn't mentioned yet. It does nothing while the service holds the live
 * connection (which brings offers at once), and tries to bring the service
 * back where Android lets it start from here (with the battery exemption).
 */
class PollWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val app = applicationContext
        val settings = HandoffConfig.load(app) ?: return@withContext Result.success()
        if (HandoffService.running) return@withContext Result.success()
        HandoffService.start(app)
        try {
            val waiting = HandoffApi(settings.server, settings.token).inbox().filter { it.pending }
            // Offers answered elsewhere meanwhile take their notification with them.
            for (known in NotifiedOffers.ids(app) - waiting.map { it.id }.toSet()) {
                OfferNotifier.cancelOffer(app, known)
                NotifiedOffers.release(app, known)
            }
            for (offer in waiting) OfferNotifier.offered(app, offer)
        } catch (error: HandoffApi.Failure) {
            if (error.unauthorized) HandoffWork.signedOut(app, error.wipeRequested)
        } catch (error: IOException) {
            Log.i("NeoCloudHandoff", "Offline: looking again later")
        }
        Result.success()
    }
}
