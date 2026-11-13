package systems.neolabs.neocloud.background

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.util.Log
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import io.flutter.plugin.common.MethodChannel
import java.util.concurrent.atomic.AtomicInteger

/**
 * Keeps the app process, and with it the Flutter engine running the upload,
 * alive while files are transferred: camera upload and the upload queue.
 * Ported from NeoAgent's KeepAliveService, with work of its own: a progress
 * notification with a Pause action and a partial wakelock. It runs only
 * while an owner holds it and stops with the last one.
 *
 * It is a data sync service. Android 15 allows those six hours a day; when
 * they run out, [onTimeout] asks the owners to stop (camera upload saves
 * its ledger and books a continuation, which goes on as ordinary background
 * work) and the service ends. Background passes don't use it: they run in a
 * [CameraUploadWorker], which shows the same notification.
 */
class TransferService : Service() {

    /** What the notification shows; set by the latest start or update. */
    data class Progress(
        val channelName: String,
        val title: String,
        val body: String,
        val pauseLabel: String,
        val current: Int,
        val total: Int,
    )

    companion object {
        private const val TAG = "NeoCloudTransfer"

        const val ACTION_PAUSE = "systems.neolabs.neocloud.background.PAUSE"

        // Renewed with every progress update; a stuck engine can't hold the
        // CPU awake for longer than this.
        private const val WAKELOCK_TIMEOUT_MS = 10 * 60 * 1000L

        /** Owner name → the channel of the engine that holds the service. */
        private val owners = LinkedHashMap<String, MethodChannel>()

        @Volatile
        private var progress = Progress("NeoCloud", "NeoCloud", "", "Pause", 0, 0)

        @Volatile
        private var instance: TransferService? = null

        /**
         * [CameraUploadWorker]s running as a foreground service of their own
         * (WorkManager's) with this service's notification: owners in their
         * engines [join] instead of starting this service.
         */
        private val foregroundWorkers = AtomicInteger()

        val workerForeground: Boolean get() = foregroundWorkers.get() > 0

        fun workerForegroundStarted() {
            foregroundWorkers.incrementAndGet()
        }

        fun workerForegroundEnded() {
            foregroundWorkers.decrementAndGet()
        }

        /** False when Android refused to start the service from the background. */
        fun start(context: Context, owner: String, channel: MethodChannel, next: Progress): Boolean {
            synchronized(owners) { owners[owner] = channel }
            progress = next
            instance?.let {
                it.refresh()
                return true
            }
            return try {
                ContextCompat.startForegroundService(context, Intent(context, TransferService::class.java))
                true
            } catch (error: Exception) {
                // Started from the background without a battery exemption, or
                // the data sync allowance is used up: the work goes on without.
                Log.w(TAG, "Transfer service unavailable", error)
                synchronized(owners) { owners.remove(owner) }
                false
            }
        }

        /** An owner in a [CameraUploadWorker]'s engine: shown by the worker, paused through here. */
        fun join(context: Context, owner: String, channel: MethodChannel, next: Progress) {
            synchronized(owners) { owners[owner] = channel }
            progress = next
            if (workerForeground) TransferNotification.show(context, next)
        }

        fun update(context: Context, next: Progress) {
            progress = next
            val running = instance
            if (running != null) {
                running.refresh()
            } else if (workerForeground) {
                TransferNotification.show(context, next)
            }
        }

        fun stop(context: Context, owner: String) {
            val empty = synchronized(owners) {
                owners.remove(owner)
                owners.isEmpty()
            }
            // Not started yet: onStartCommand stops it right after
            // startForeground, which Android insists on.
            if (empty && instance != null) context.stopService(Intent(context, TransferService::class.java))
        }

        /** An engine went away; whatever it held is let go. */
        fun forget(context: Context, channel: MethodChannel) {
            val empty = synchronized(owners) {
                owners.entries.removeAll { it.value === channel }
                owners.isEmpty()
            }
            if (empty && instance != null) context.stopService(Intent(context, TransferService::class.java))
        }
    }

    private var wakeLock: PowerManager.WakeLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_PAUSE) {
            askOwnersToStop("pause")
            return START_NOT_STICKY
        }
        instance = this
        try {
            ServiceCompat.startForeground(this, TransferNotification.ID, TransferNotification.build(this, progress), serviceType())
        } catch (error: Exception) {
            Log.w(TAG, "Foreground service refused", error)
            synchronized(owners) { owners.clear() }
            stopSelf()
            return START_NOT_STICKY
        }
        if (synchronized(owners) { owners.isEmpty() }) {
            // Every owner let go while the service was starting.
            stopSelf()
            return START_NOT_STICKY
        }
        holdWakeLock()
        // A restarted service would have no upload to keep alive: the
        // engine died with the process. The next trigger starts a new pass.
        return START_NOT_STICKY
    }

    /** Android 15+: the daily data sync time is used up. */
    override fun onTimeout(startId: Int, fgsType: Int) {
        Log.i(TAG, "Data sync time limit reached")
        askOwnersToStop("timeout")
    }

    override fun onDestroy() {
        instance = null
        wakeLock?.let { if (it.isHeld) it.release() }
        wakeLock = null
        super.onDestroy()
    }

    fun refresh() {
        holdWakeLock()
        TransferNotification.show(this, progress)
    }

    /** Tells every owner to stop and ends the service right away, as Android requires on a timeout. */
    private fun askOwnersToStop(reason: String) {
        val held = synchronized(owners) {
            val copy = owners.toMap()
            owners.clear()
            copy
        }
        val main = Handler(Looper.getMainLooper())
        for ((owner, channel) in held) {
            main.post {
                try {
                    channel.invokeMethod("stopRequested", mapOf("owner" to owner, "reason" to reason))
                } catch (error: Exception) {
                    Log.w(TAG, "Could not reach the engine of $owner", error)
                }
            }
        }
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    private fun holdWakeLock() {
        val lock = wakeLock ?: (getSystemService(Context.POWER_SERVICE) as PowerManager)
            .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "NeoCloud:transfer")
            .apply { setReferenceCounted(false) }
            .also { wakeLock = it }
        lock.acquire(WAKELOCK_TIMEOUT_MS)
    }

    private fun serviceType(): Int =
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0
}
