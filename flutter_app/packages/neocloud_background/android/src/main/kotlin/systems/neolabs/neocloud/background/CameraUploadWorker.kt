package systems.neolabs.neocloud.background

import android.content.Context
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.work.CoroutineWorker
import androidx.work.ForegroundInfo
import androidx.work.WorkerParameters
import io.flutter.FlutterInjector
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.embedding.engine.dart.DartExecutor
import io.flutter.view.FlutterCallbackInformation
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.coroutines.resume

/**
 * One background camera upload pass (see [CameraWork] for when).
 *
 * workmanager_android's worker runs as an ordinary job, which Android stops
 * after ten minutes (and less often than hourly in the rarer standby
 * buckets), destroying the engine mid-file. A first backup of thousands of
 * photos never got through that way. This worker hosts the Dart pass
 * (cameraBackgroundMain) in an engine of its own and, once the pass has
 * files to send, runs as a foreground service (data sync, WorkManager's
 * own), which has no such limit. Where Android refuses the foreground service (no battery exemption,
 * the daily data sync time used up) it runs as an ordinary job, and the
 * pass books a continuation when it is stopped, so the backup goes on in
 * steps.
 *
 * The new-photo trigger is booked again here, natively, however the pass
 * ended: the Dart side may not get the chance.
 */
class CameraUploadWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    companion object {
        private const val TAG = "NeoCloudCameraWorker"

        // How long a stopped pass gets to save its ledger before its engine
        // goes.
        private const val STOP_GRACE_MS = 5_000L
    }

    private val promoted = AtomicBoolean(false)
    private val foreground = AtomicBoolean(false)

    // Expedited work (a continuation) on Android 11 and older runs as a
    // foreground service from the start.
    override suspend fun getForegroundInfo(): ForegroundInfo {
        val entry = CameraWork.entry(applicationContext)
        return foregroundInfo(
            TransferService.Progress(
                channelName = entry?.channelName ?: "NeoCloud",
                title = entry?.title ?: "NeoCloud",
                body = "",
                pauseLabel = entry?.pauseLabel ?: "Pause",
                current = 0,
                total = 0,
            ),
        )
    }

    /**
     * The pass has files to send: from now on the worker runs as a
     * foreground service, which Android doesn't stop after ten minutes. Not
     * before, so an hourly look that finds nothing new shows nothing. Where
     * Android refuses, the pass goes on as an ordinary job.
     */
    private fun promote(progress: TransferService.Progress) {
        if (promoted.getAndSet(true)) return
        val future = setForegroundAsync(foregroundInfo(progress))
        future.addListener({
            try {
                future.get()
                if (!isStopped && !foreground.getAndSet(true)) TransferService.workerForegroundStarted()
                Log.i(TAG, "Running as a foreground service")
            } catch (error: Exception) {
                // ForegroundServiceStartNotAllowedException and its kin.
                Log.i(TAG, "Running as an ordinary job: ${error.cause?.javaClass?.simpleName ?: error.javaClass.simpleName}")
            }
        }, Runnable::run)
    }

    override suspend fun doWork(): Result {
        val app = applicationContext
        if (!CameraWork.enabled(app)) return Result.success()
        // The app has not been opened since this version was installed.
        val entry = CameraWork.entry(app) ?: return Result.success()
        if (CameraWork.passAlive(app)) {
            // Another pass (the app's, or another worker's) is going; it
            // started before whatever woke this one, so look again later.
            Log.i(TAG, "Another pass is running; trying again later")
            CameraWork.continueSoon(app, CameraWork.BUSY_RETRY_MINUTES)
            if (CameraWork.TAG_MEDIA in tags) CameraWork.armMedia(app)
            return Result.success()
        }
        try {
            Log.i(TAG, "Pass started (${tags.firstOrNull { it.startsWith("neocloud.") }})")
            withContext(Dispatchers.Main) { runPass(app, entry.callback) }
            Log.i(TAG, "Pass ended")
        } finally {
            if (foreground.getAndSet(false)) TransferService.workerForegroundEnded()
            if (isStopped) {
                val reason = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) stopReason else -1
                Log.i(TAG, "Pass stopped by the system, reason $reason")
            }
            if (CameraWork.TAG_MEDIA in tags) CameraWork.armMedia(app)
        }
        return Result.success()
    }

    private fun foregroundInfo(progress: TransferService.Progress): ForegroundInfo {
        val notification = TransferNotification.build(applicationContext, progress)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(TransferNotification.ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(TransferNotification.ID, notification)
        }
    }

    /**
     * Starts an engine on [callback] (cameraBackgroundMain) and waits until
     * the pass says it is done. Stopped meanwhile, the pass is asked to stop
     * and its engine destroyed after [STOP_GRACE_MS]; the plugin then frees
     * the lease of a pass that didn't end in time and books the next one.
     */
    private suspend fun runPass(context: Context, callback: Long) {
        val main = Handler(Looper.getMainLooper())
        var engine: FlutterEngine? = null
        var plugin: NeoCloudBackgroundPlugin? = null
        var destroyed = false
        fun destroy() {
            if (destroyed) return
            destroyed = true
            engine?.destroy()
        }
        suspendCancellableCoroutine { continuation ->
            val loader = FlutterInjector.instance().flutterLoader()
            if (!loader.initialized()) loader.startInitialization(context)
            loader.ensureInitializationCompleteAsync(context, null, main) {
                if (!continuation.isActive) return@ensureInitializationCompleteAsync
                val info = FlutterCallbackInformation.lookupCallbackInformation(callback)
                if (info == null) {
                    Log.w(TAG, "The background entry point is unknown; open the app once")
                    continuation.resume(Unit)
                    return@ensureInitializationCompleteAsync
                }
                val started = FlutterEngine(context)
                engine = started
                plugin = (started.plugins.get(NeoCloudBackgroundPlugin::class.java) as? NeoCloudBackgroundPlugin)?.also {
                    it.onWorkerTransfer = ::promote
                    it.onWorkerDone = {
                        main.post { destroy() }
                        if (continuation.isActive) continuation.resume(Unit)
                    }
                }
                started.dartExecutor.executeDartCallback(
                    DartExecutor.DartCallback(context.assets, loader.findAppBundlePath(), info),
                )
            }
            continuation.invokeOnCancellation {
                main.post {
                    if (engine == null || destroyed) return@post
                    plugin?.requestStop("timeout")
                    main.postDelayed({ destroy() }, STOP_GRACE_MS)
                }
            }
        }
    }
}
