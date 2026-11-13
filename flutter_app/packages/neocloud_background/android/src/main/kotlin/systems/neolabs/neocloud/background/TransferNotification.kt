package systems.neolabs.neocloud.background

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat

/**
 * The one transfer notification: progress, and a Pause action that asks the
 * owners to stop. Shown by [TransferService] for passes in the app and by
 * [CameraUploadWorker] for passes in the background, under the same id, so
 * progress updates reach whichever of them is running.
 */
object TransferNotification {
    const val ID = 5106

    // Low importance: no sound or peek, but the progress shows in the shade
    // and the status bar.
    private const val CHANNEL_ID = "neocloud_transfers"

    fun show(context: Context, progress: TransferService.Progress) {
        context.getSystemService(NotificationManager::class.java)?.notify(ID, build(context, progress))
    }

    fun build(context: Context, progress: TransferService.Progress): Notification {
        val manager = context.getSystemService(NotificationManager::class.java)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && manager != null) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, progress.channelName, NotificationManager.IMPORTANCE_LOW).apply {
                    setShowBadge(false)
                    setSound(null, null)
                    enableVibration(false)
                },
            )
        }
        val launch = context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP)
        val open = launch?.let {
            PendingIntent.getActivity(context, 0, it, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
        }
        val pause = PendingIntent.getService(
            context,
            1,
            Intent(context, TransferService::class.java).setAction(TransferService.ACTION_PAUSE),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.neocloud_stat_upload)
            .setContentTitle(progress.title)
            .setContentText(progress.body)
            .setContentIntent(open)
            .setProgress(progress.total, progress.current, progress.total <= 0)
            .addAction(0, progress.pauseLabel, pause)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }
}
