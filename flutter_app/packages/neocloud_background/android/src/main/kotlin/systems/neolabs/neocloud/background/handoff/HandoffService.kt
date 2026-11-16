package systems.neolabs.neocloud.background.handoff

import android.app.Application
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import java.io.IOException
import java.net.HttpURLConnection
import kotlin.math.min

/**
 * Keeps this device online for "Send to device" while NeoCloud is closed: a
 * foreground service with a minimal, silent notification that holds the
 * server-sent event stream (`GET /api/handoff/events`) with the stored device
 * token, reconnects with backoff, and hands each offer that arrives to
 * [OfferNotifier]. Only runs while receiving in the background is on.
 *
 * Foreground service type: `remoteMessaging` from Android 14, which is made
 * for passing messages and media between a person's devices, has no time
 * limit (data sync gets six hours a day on Android 15) and may start after
 * a reboot (data sync may not). Android 10 to 13 have no such type and use
 * `dataSync`; older versions have none.
 *
 * Doze stops background network access until the screen is on or the app
 * is exempt from battery optimisation (the permission sheet asks for it);
 * [PollWorker] looks every ~15 minutes meanwhile.
 */
class HandoffService : Service() {

    companion object {
        private const val TAG = "NeoCloudHandoff"
        private const val MAX_BACKOFF_SECONDS = 60L

        @Volatile
        var running = false
            private set

        /** False when Android refuses to start a foreground service from here. */
        fun start(context: Context): Boolean = try {
            ContextCompat.startForegroundService(context, Intent(context, HandoffService::class.java))
            true
        } catch (error: Exception) {
            Log.i(TAG, "The receiving service can't start now: ${error.javaClass.simpleName}")
            false
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, HandoffService::class.java))
        }

        /** Seconds to wait before reconnecting after [attempt] failures: 1, 2, 4 … 60. */
        fun backoffSeconds(attempt: Int): Long = min(MAX_BACKOFF_SECONDS, 1L shl min(attempt, 6))
    }

    @Volatile
    private var stopping = false
    private var loop: Thread? = null

    @Volatile
    private var connection: HttpURLConnection? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        AppVisibility.register(applicationContext as Application)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        try {
            ServiceCompat.startForeground(this, OfferNotifier.SERVICE_ID, OfferNotifier.serviceNotification(this), serviceType())
        } catch (error: Exception) {
            Log.w(TAG, "Foreground service refused", error)
            stopSelf()
            return START_NOT_STICKY
        }
        if (HandoffConfig.load(this) == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        running = true
        if (loop?.isAlive != true) {
            stopping = false
            loop = Thread(::listen, "neocloud-handoff").also { it.start() }
        }
        // Restarted by the system after it was killed; the settings are stored.
        return START_STICKY
    }

    override fun onDestroy() {
        running = false
        stopping = true
        connection?.disconnect()
        loop?.interrupt()
        super.onDestroy()
    }

    /** Android 15's time limit for data sync services (Android 14+ uses remoteMessaging, which has none). */
    override fun onTimeout(startId: Int, fgsType: Int) {
        Log.i(TAG, "Foreground service time limit reached; the poll takes over")
        stopSelf()
    }

    private fun serviceType(): Int = when {
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE -> ServiceInfo.FOREGROUND_SERVICE_TYPE_REMOTE_MESSAGING
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q -> ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
        else -> 0
    }

    private fun listen() {
        var attempt = 0
        while (!stopping) {
            val settings = HandoffConfig.load(this)
            if (settings == null) {
                stopSelf()
                return
            }
            try {
                val live = HandoffApi(settings.server, settings.token).openEvents()
                connection = live
                val parser = SseParser(::onEvent)
                live.inputStream.bufferedReader(Charsets.UTF_8).use { reader ->
                    // Connected: the next failure starts the backoff over.
                    attempt = 0
                    while (!stopping) parser.addLine(reader.readLine() ?: break)
                }
            } catch (error: HandoffApi.Failure) {
                if (error.unauthorized) {
                    // Signed out, revoked or wiped: stop for good, remember a wipe for the app.
                    HandoffWork.signedOut(this, error.wipeRequested)
                    return
                }
            } catch (error: IOException) {
                // Offline, the server restarted or went quiet: reconnect.
            } finally {
                connection?.disconnect()
                connection = null
            }
            if (stopping) return
            try {
                Thread.sleep(backoffSeconds(attempt++) * 1000)
            } catch (_: InterruptedException) {
                return
            }
        }
    }

    private fun onEvent(event: String, data: String) {
        val offer = Offers.parseEvent(data) ?: return
        when (event) {
            "handoff:offer" -> OfferNotifier.offered(this, offer)
            "handoff:updated" -> if (!offer.pending) {
                // Answered here, by another way, cancelled or expired: its notification is stale.
                OfferNotifier.cancelOffer(this, offer.id)
                NotifiedOffers.release(this, offer.id)
            }
        }
    }
}
