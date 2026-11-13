package systems.neolabs.neocloud.background

import android.content.Context
import android.provider.MediaStore
import android.util.Log
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.OutOfQuotaPolicy
import androidx.work.PeriodicWorkRequest
import androidx.work.WorkInfo
import androidx.work.WorkManager
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

/**
 * When camera upload runs in the background on Android: every request is a
 * [CameraUploadWorker], which runs the pass in an engine of its own and as
 * a foreground service where Android allows it.
 *
 *   - periodic: about every hour, the fallback for everything else;
 *   - media: soon after a photo or video is added (MediaStore content-URI
 *     triggers). A content trigger fires once, so the worker re-arms it;
 *   - continue: right away (expedited where possible) when a pass stopped
 *     with files left: the system's time limit, an engine that went away
 *     mid-pass, Wi-Fi to come back. A first backup goes on in steps instead
 *     of waiting for the next trigger.
 *
 * The conditions are kept here so a reboot or an app update can restore it
 * all without the app being opened.
 */
object CameraWork {
    private const val TAG = "NeoCloudCameraWork"

    private const val PERIODIC_WORK = "systems.neolabs.neocloud.cameraUpload.periodic"
    private const val MEDIA_WORK = "systems.neolabs.neocloud.cameraUpload.media"
    private const val CONTINUE_WORK = "systems.neolabs.neocloud.cameraUpload.continue"

    // Scheduled by earlier versions: workmanager_android's periodic task and
    // the after-reboot run.
    private val OLD_WORK = listOf("systems.neolabs.neocloud.cameraUpload", "systems.neolabs.neocloud.cameraUpload.restore")

    const val TAG_PERIODIC = "neocloud.camera.periodic"
    const val TAG_MEDIA = "neocloud.camera.media"
    const val TAG_CONTINUE = "neocloud.camera.continue"

    private const val PREFERENCES = "neocloud_background"
    private const val KEY_ARMED = "media.armed"
    private const val KEY_UNMETERED = "media.unmetered"
    private const val KEY_CHARGING = "media.charging"
    private const val KEY_CALLBACK = "camera.callback"
    private const val KEY_CHANNEL = "camera.channelName"
    private const val KEY_TITLE = "camera.title"
    private const val KEY_PAUSE = "camera.pauseLabel"

    /** A worker that finds another pass running looks again after this. */
    const val BUSY_RETRY_MINUTES = 15L

    // camera_lease.dart's file and how long it stays fresh unrenewed.
    private const val LEASE_STALE_MS = 5 * 60 * 1000L

    // A lease this young may be one a pass is still taking: Dart writes it,
    // waits camera_lease.dart's settle time, then reports the pass started.
    private const val LEASE_TAKING_MS = 15_000L

    private val executor = Executors.newSingleThreadExecutor()

    /** Passes running in this process's engines (every pass runs in this process). */
    private val runningPasses = AtomicInteger(0)

    fun passStarted() {
        runningPasses.incrementAndGet()
    }

    fun passEnded() {
        runningPasses.updateAndGet { if (it > 0) it - 1 else 0 }
    }

    private fun leaseFile(context: Context) = File(File(context.filesDir, "camera"), "pass.lease")

    /**
     * A pass holds a lease it renewed within the last five minutes, and it
     * runs in this process. A fresh lease no engine here holds is left by
     * a pass whose process died or that failed to let go: it is removed, so
     * the next pass needn't wait up to [BUSY_RETRY_MINUTES] for it.
     */
    fun passAlive(context: Context): Boolean {
        val lease = leaseFile(context)
        if (!lease.exists()) return false
        val age = System.currentTimeMillis() - lease.lastModified()
        if (age >= LEASE_STALE_MS) return false
        if (runningPasses.get() > 0 || age < LEASE_TAKING_MS) return true
        Log.i(TAG, "Removing the lease of a pass that is no longer running")
        if (!lease.delete()) Log.w(TAG, "Could not remove a dead pass's lease")
        return false
    }

    /** The Dart entry point and the notification texts a background pass starts with. */
    data class Entry(val callback: Long, val channelName: String, val title: String, val pauseLabel: String)

    private fun preferences(context: Context) = context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)

    fun enabled(context: Context): Boolean = preferences(context).getBoolean(KEY_ARMED, false)

    fun entry(context: Context): Entry? {
        val stored = preferences(context)
        val callback = stored.getLong(KEY_CALLBACK, 0L)
        if (callback == 0L) return null
        return Entry(
            callback,
            stored.getString(KEY_CHANNEL, null) ?: "NeoCloud",
            stored.getString(KEY_TITLE, null) ?: "NeoCloud",
            stored.getString(KEY_PAUSE, null) ?: "Pause",
        )
    }

    /** Camera upload is on: books the periodic pass and the new-photo trigger. */
    fun configure(context: Context, unmeteredOnly: Boolean, chargingOnly: Boolean, entry: Entry) {
        val app = context.applicationContext
        preferences(app).edit()
            .putBoolean(KEY_ARMED, true)
            .putBoolean(KEY_UNMETERED, unmeteredOnly)
            .putBoolean(KEY_CHARGING, chargingOnly)
            .putLong(KEY_CALLBACK, entry.callback)
            .putString(KEY_CHANNEL, entry.channelName)
            .putString(KEY_TITLE, entry.title)
            .putString(KEY_PAUSE, entry.pauseLabel)
            .apply()
        executor.execute {
            try {
                val work = WorkManager.getInstance(app)
                OLD_WORK.forEach(work::cancelUniqueWork)
                // UPDATE keeps the schedule and takes new conditions.
                work.enqueueUniquePeriodicWork(PERIODIC_WORK, ExistingPeriodicWorkPolicy.UPDATE, periodicRequest(app))
                armMedia(app)
            } catch (error: Exception) {
                Log.w(TAG, "Could not schedule camera upload", error)
            }
        }
    }

    fun disable(context: Context) {
        val app = context.applicationContext
        preferences(app).edit().putBoolean(KEY_ARMED, false).apply()
        val work = WorkManager.getInstance(app)
        (OLD_WORK + listOf(PERIODIC_WORK, MEDIA_WORK, CONTINUE_WORK)).forEach(work::cancelUniqueWork)
    }

    /**
     * The new-photo trigger, booked again. From inside the trigger's own
     * pass the next one queues behind it; replacing would cancel that pass.
     */
    fun armMedia(context: Context) {
        if (!enabled(context)) return
        enqueueBehindRunning(context, MEDIA_WORK, mediaRequest(context))
    }

    /** A pass soon, for files a stopped pass left. [delayMinutes] holds it back that long. */
    fun continueSoon(context: Context, delayMinutes: Long = 0) {
        if (!enabled(context)) return
        executor.execute { enqueueBehindRunning(context.applicationContext, CONTINUE_WORK, continueRequest(context, delayMinutes)) }
    }

    /**
     * After a reboot or an app update: everything booked again, and one pass
     * once there is a network, for photos taken meanwhile. A worker rather
     * than the transfer service: Android 15 forbids starting a data sync
     * service from BOOT_COMPLETED.
     */
    fun restore(context: Context) {
        val app = context.applicationContext
        if (!enabled(app)) return
        executor.execute {
            try {
                val work = WorkManager.getInstance(app)
                OLD_WORK.forEach(work::cancelUniqueWork)
                work.enqueueUniquePeriodicWork(PERIODIC_WORK, ExistingPeriodicWorkPolicy.UPDATE, periodicRequest(app))
                work.enqueueUniqueWork(MEDIA_WORK, ExistingWorkPolicy.REPLACE, mediaRequest(app))
                work.enqueueUniqueWork(CONTINUE_WORK, ExistingWorkPolicy.REPLACE, continueRequest(app, 1))
            } catch (error: Exception) {
                Log.w(TAG, "Could not restore camera upload", error)
            }
        }
    }

    /**
     * An engine died in the middle of a pass (the app swiped away, a worker
     * stopped by the system): its lease is let go so the next pass needn't
     * wait for it to go stale, and that pass is booked.
     */
    fun recoverDeadPass(context: Context) {
        val lease = leaseFile(context)
        if (lease.exists() && !lease.delete()) Log.w(TAG, "Could not remove the lease of a dead pass")
        continueSoon(context)
    }

    private fun enqueueBehindRunning(context: Context, name: String, request: OneTimeWorkRequest) {
        try {
            val work = WorkManager.getInstance(context)
            val states = work.getWorkInfosForUniqueWork(name).get().map { it.state }
            val running = WorkInfo.State.RUNNING in states
            val waiting = WorkInfo.State.ENQUEUED in states || WorkInfo.State.BLOCKED in states
            val policy = when {
                running && waiting -> return
                running -> ExistingWorkPolicy.APPEND_OR_REPLACE
                waiting && name == CONTINUE_WORK -> return
                else -> ExistingWorkPolicy.REPLACE
            }
            work.enqueueUniqueWork(name, policy, request)
        } catch (error: Exception) {
            Log.w(TAG, "Could not book $name", error)
        }
    }

    private fun conditions(context: Context): Constraints.Builder {
        val stored = preferences(context)
        return Constraints.Builder()
            .setRequiredNetworkType(if (stored.getBoolean(KEY_UNMETERED, true)) NetworkType.UNMETERED else NetworkType.CONNECTED)
            .setRequiresCharging(stored.getBoolean(KEY_CHARGING, false))
    }

    private fun periodicRequest(context: Context): PeriodicWorkRequest =
        PeriodicWorkRequest.Builder(CameraUploadWorker::class.java, 1, TimeUnit.HOURS)
            .setConstraints(conditions(context).build())
            .addTag(TAG_PERIODIC)
            .build()

    private fun mediaRequest(context: Context): OneTimeWorkRequest =
        OneTimeWorkRequest.Builder(CameraUploadWorker::class.java)
            .setConstraints(
                conditions(context)
                    .addContentUriTrigger(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, true)
                    .addContentUriTrigger(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, true)
                    // A burst of photos is one pass.
                    .setTriggerContentUpdateDelay(30, TimeUnit.SECONDS)
                    .setTriggerContentMaxDelay(2, TimeUnit.MINUTES)
                    .build(),
            )
            .addTag(TAG_MEDIA)
            .build()

    private fun continueRequest(context: Context, delayMinutes: Long): OneTimeWorkRequest {
        val builder = OneTimeWorkRequest.Builder(CameraUploadWorker::class.java)
            .setConstraints(conditions(context).build())
            .addTag(TAG_CONTINUE)
        if (delayMinutes > 0) {
            builder.setInitialDelay(delayMinutes, TimeUnit.MINUTES)
        } else if (!preferences(context).getBoolean(KEY_CHARGING, false)) {
            // Expedited work allows no charging condition and no delay.
            builder.setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
        }
        return builder.build()
    }
}
