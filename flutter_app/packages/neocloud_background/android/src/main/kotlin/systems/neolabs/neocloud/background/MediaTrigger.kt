package systems.neolabs.neocloud.background

import android.content.Context
import android.provider.MediaStore
import android.util.Log
import androidx.work.Constraints
import androidx.work.Data
import androidx.work.ExistingWorkPolicy
import androidx.work.ListenableWorker
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.WorkInfo
import androidx.work.WorkManager
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * Runs a camera upload pass soon after a photo or video is added: a
 * WorkManager job with MediaStore content-URI triggers. It enqueues
 * workmanager_android's own worker, so the pass is the same Dart entry point
 * (cameraUploadDispatcher) as the periodic task, which stays the fallback.
 *
 * A content-URI job fires once; the pass re-arms it when it ends. Its
 * conditions are kept here so a reboot or an app update can restore it.
 */
object MediaTrigger {
    private const val TAG = "NeoCloudMediaTrigger"

    // Task names the Dart dispatcher sees; it treats every task alike.
    private const val MEDIA_WORK = "systems.neolabs.neocloud.cameraUpload.media"
    private const val RESTORE_WORK = "systems.neolabs.neocloud.cameraUpload.restore"

    // workmanager_android's worker and the input key it reads the task name
    // from (BackgroundWorker.DART_TASK_KEY). Looked up by name so this
    // plugin does not compile against another plugin's classes.
    private const val WORKER_CLASS = "dev.fluttercommunity.workmanager.BackgroundWorker"
    private const val DART_TASK_KEY = "dev.fluttercommunity.workmanager.DART_TASK"

    private const val PREFERENCES = "neocloud_background"
    private const val KEY_ARMED = "media.armed"
    private const val KEY_UNMETERED = "media.unmetered"
    private const val KEY_CHARGING = "media.charging"

    private val executor = Executors.newSingleThreadExecutor()

    fun arm(context: Context, unmeteredOnly: Boolean, chargingOnly: Boolean) {
        val app = context.applicationContext
        app.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).edit()
            .putBoolean(KEY_ARMED, true)
            .putBoolean(KEY_UNMETERED, unmeteredOnly)
            .putBoolean(KEY_CHARGING, chargingOnly)
            .apply()
        executor.execute { enqueue(app, unmeteredOnly, chargingOnly) }
    }

    fun disarm(context: Context) {
        val app = context.applicationContext
        app.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE).edit().putBoolean(KEY_ARMED, false).apply()
        val work = WorkManager.getInstance(app)
        work.cancelUniqueWork(MEDIA_WORK)
        work.cancelUniqueWork(RESTORE_WORK)
    }

    /**
     * After a reboot or an app update: re-arm, and run one pass as soon as
     * there is a network, for photos taken meanwhile. A worker rather than
     * the transfer service: Android 15 forbids starting a data sync service
     * from BOOT_COMPLETED.
     */
    fun restore(context: Context) {
        val app = context.applicationContext
        val preferences = app.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
        if (!preferences.getBoolean(KEY_ARMED, false)) return
        val unmetered = preferences.getBoolean(KEY_UNMETERED, true)
        val charging = preferences.getBoolean(KEY_CHARGING, false)
        val worker = workerClass() ?: return
        val work = WorkManager.getInstance(app)
        work.enqueueUniqueWork(MEDIA_WORK, ExistingWorkPolicy.REPLACE, mediaRequest(worker, unmetered, charging))
        work.enqueueUniqueWork(
            RESTORE_WORK,
            ExistingWorkPolicy.REPLACE,
            OneTimeWorkRequest.Builder(worker)
                .setConstraints(conditions(unmetered, charging).build())
                .setInitialDelay(1, TimeUnit.MINUTES)
                .setInputData(task(RESTORE_WORK))
                .build(),
        )
    }

    private fun enqueue(context: Context, unmeteredOnly: Boolean, chargingOnly: Boolean) {
        val worker = workerClass() ?: return
        try {
            val work = WorkManager.getInstance(context)
            val states = work.getWorkInfosForUniqueWork(MEDIA_WORK).get().map { it.state }
            val running = WorkInfo.State.RUNNING in states
            val waiting = WorkInfo.State.ENQUEUED in states || WorkInfo.State.BLOCKED in states
            // Re-armed from inside the job's own pass: queue the next one
            // behind it; replacing would cancel the running pass.
            val policy = when {
                running && waiting -> return
                running -> ExistingWorkPolicy.APPEND_OR_REPLACE
                else -> ExistingWorkPolicy.REPLACE
            }
            work.enqueueUniqueWork(MEDIA_WORK, policy, mediaRequest(worker, unmeteredOnly, chargingOnly))
        } catch (error: Exception) {
            Log.w(TAG, "Could not arm the new-photo trigger", error)
        }
    }

    private fun mediaRequest(
        worker: Class<out ListenableWorker>,
        unmeteredOnly: Boolean,
        chargingOnly: Boolean,
    ): OneTimeWorkRequest =
        OneTimeWorkRequest.Builder(worker)
            .setConstraints(
                conditions(unmeteredOnly, chargingOnly)
                    .addContentUriTrigger(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, true)
                    .addContentUriTrigger(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, true)
                    // A burst of photos is one pass.
                    .setTriggerContentUpdateDelay(30, TimeUnit.SECONDS)
                    .setTriggerContentMaxDelay(2, TimeUnit.MINUTES)
                    .build(),
            )
            .setInputData(task(MEDIA_WORK))
            .build()

    private fun conditions(unmeteredOnly: Boolean, chargingOnly: Boolean): Constraints.Builder =
        Constraints.Builder()
            .setRequiredNetworkType(if (unmeteredOnly) NetworkType.UNMETERED else NetworkType.CONNECTED)
            .setRequiresCharging(chargingOnly)

    private fun task(name: String): Data = Data.Builder().putString(DART_TASK_KEY, name).build()

    @Suppress("UNCHECKED_CAST")
    private fun workerClass(): Class<out ListenableWorker>? =
        try {
            Class.forName(WORKER_CLASS) as Class<out ListenableWorker>
        } catch (error: ClassNotFoundException) {
            Log.w(TAG, "workmanager_android's worker is missing", error)
            null
        }
}
