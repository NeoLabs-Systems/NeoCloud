package systems.neolabs.neocloud.background.handoff

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import systems.neolabs.neocloud.background.R

/**
 * Everything the background receiver tells the person: the persistent "ready
 * to receive" notification, the heads-up "Anna's laptop wants to send you 2
 * files" with Accept and Decline, progress, and what became of the files.
 * Offers are tagged with their id, so each has exactly one notification.
 */
object OfferNotifier {
    private const val CHANNEL_OFFERS = "neocloud_handoff_offers"
    private const val CHANNEL_SERVICE = "neocloud_handoff_service"
    private const val CHANNEL_TRANSFERS = "neocloud_handoff_transfers"

    const val SERVICE_ID = 5201
    private const val OFFER_ID = 5202
    private const val RESULT_ID = 5203
    private const val PROGRESS_BASE = 5300
    private const val PROGRESS_SPAN = 500

    /** The id of the foreground notification of the download of [offerId]. */
    fun progressId(offerId: String): Int = PROGRESS_BASE + (offerId.hashCode() and 0x7fffffff) % PROGRESS_SPAN

    private fun channels(context: Context, texts: HandoffTexts) {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_OFFERS, texts.channelOffers, NotificationManager.IMPORTANCE_HIGH),
        )
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_SERVICE, texts.channelService, NotificationManager.IMPORTANCE_MIN).apply {
                setShowBadge(false)
            },
        )
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_TRANSFERS, texts.channelTransfers, NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            },
        )
    }

    private fun immutable() = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE

    /** Opens the app; it shows its own Accept or Decline dialog for what waits. */
    private fun openApp(context: Context): PendingIntent? {
        val launch = context.packageManager.getLaunchIntentForPackage(context.packageName)
            ?.addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP) ?: return null
        return PendingIntent.getActivity(context, 0, launch, immutable())
    }

    private fun answer(context: Context, offer: Offer, action: String): PendingIntent {
        val intent = Intent(context, AnswerReceiver::class.java)
            .setAction(action)
            // The data keeps each offer's intents apart; extras don't count for PendingIntent equality.
            .setData(Uri.parse("neocloud-handoff://offer/${offer.id}/$action"))
            .putExtra(AnswerReceiver.EXTRA_OFFER, offer.id)
            .putExtra(AnswerReceiver.EXTRA_FROM, offer.fromName)
        return PendingIntent.getBroadcast(context, offer.id.hashCode(), intent, immutable())
    }

    fun serviceNotification(context: Context): Notification {
        val texts = HandoffConfig.texts(context)
        channels(context, texts)
        return NotificationCompat.Builder(context, CHANNEL_SERVICE)
            .setSmallIcon(R.drawable.neocloud_stat_download)
            .setContentTitle(texts.serviceTitle)
            .setContentIntent(openApp(context))
            .setOngoing(true)
            .setSilent(true)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    /**
     * Tells the person about [offer], once, and only while the app is closed
     * or in the background (open, it asks in its own dialog). False when
     * nothing was shown: notifications are off, or it was shown before.
     */
    fun offered(context: Context, offer: Offer): Boolean {
        if (!offer.pending || AppVisibility.inForeground) return false
        val manager = NotificationManagerCompat.from(context)
        if (!manager.areNotificationsEnabled() || !NotifiedOffers.claim(context, offer.id)) return false
        val texts = HandoffConfig.texts(context)
        channels(context, texts)
        val title = texts.offer(offer.fromName, offer.files.size)
        val notification = NotificationCompat.Builder(context, CHANNEL_OFFERS)
            .setSmallIcon(R.drawable.neocloud_stat_download)
            .setContentTitle(title)
            .setContentText(offer.nameList(1))
            .setStyle(NotificationCompat.BigTextStyle().bigText(offer.nameList()))
            .setContentIntent(openApp(context))
            .addAction(0, texts.accept, answer(context, offer, AnswerReceiver.ACTION_ACCEPT))
            .addAction(0, texts.decline, answer(context, offer, AnswerReceiver.ACTION_DECLINE))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_MESSAGE)
            .setDefaults(Notification.DEFAULT_ALL)
            .setAutoCancel(true)
            // An offer can be accepted for 24 hours.
            .setTimeoutAfter(24 * 60 * 60 * 1000L)
            .build()
        return notify(manager, offer.id, OFFER_ID, notification)
    }

    fun cancelOffer(context: Context, offerId: String) {
        NotificationManagerCompat.from(context).cancel(offerId, OFFER_ID)
    }

    /** Every "wants to send you" notification: the app is open and asks itself. */
    fun cancelOffers(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val shown = try {
            manager.activeNotifications
        } catch (_: Exception) {
            return
        }
        for (entry in shown) if (entry.id == OFFER_ID && entry.tag != null) manager.cancel(entry.tag, OFFER_ID)
    }

    /** The ongoing notification of a download; [total] 0 means unknown. */
    fun progress(context: Context, fromName: String, fileName: String, done: Long, total: Long): Notification {
        val texts = HandoffConfig.texts(context)
        channels(context, texts)
        val scale = if (total > Int.MAX_VALUE) 1000 else total.toInt()
        val value = if (total > Int.MAX_VALUE) (done * 1000 / total).toInt() else done.toInt()
        return NotificationCompat.Builder(context, CHANNEL_TRANSFERS)
            .setSmallIcon(R.drawable.neocloud_stat_download)
            .setContentTitle(texts.receiving(fromName))
            .setContentText(fileName)
            .setContentIntent(openApp(context))
            .setProgress(scale, value, total <= 0)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .build()
    }

    /** The files are in Downloads/NeoCloud; tapping opens them ([open]). */
    fun received(context: Context, offerId: String, count: Int, open: PendingIntent?) {
        val texts = HandoffConfig.texts(context)
        channels(context, texts)
        val builder = NotificationCompat.Builder(context, CHANNEL_TRANSFERS)
            .setSmallIcon(R.drawable.neocloud_stat_download)
            .setContentTitle(texts.received(count))
            .setContentText(texts.savedIn)
            .setContentIntent(open ?: openApp(context))
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
        if (open != null) builder.addAction(0, texts.open, open)
        notify(NotificationManagerCompat.from(context), offerId, RESULT_ID, builder.build())
    }

    fun failed(context: Context, offerId: String, fromName: String) {
        val texts = HandoffConfig.texts(context)
        channels(context, texts)
        val notification = NotificationCompat.Builder(context, CHANNEL_TRANSFERS)
            .setSmallIcon(R.drawable.neocloud_stat_download)
            .setContentTitle(texts.failedTitle())
            .setContentText(texts.failedBody(fromName))
            .setContentIntent(openApp(context))
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_ERROR)
            .build()
        notify(NotificationManagerCompat.from(context), offerId, RESULT_ID, notification)
    }

    /** Signed out, revoked or wiped: nothing of the receiver stays on screen. */
    fun cancelAll(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val shown = try {
            manager.activeNotifications
        } catch (_: Exception) {
            return
        }
        for (entry in shown) {
            if (entry.id == OFFER_ID || entry.id == RESULT_ID) manager.cancel(entry.tag, entry.id)
        }
    }

    private fun notify(manager: NotificationManagerCompat, tag: String, id: Int, notification: Notification): Boolean = try {
        manager.notify(tag, id, notification)
        true
    } catch (_: SecurityException) {
        // POST_NOTIFICATIONS was taken away meanwhile.
        false
    }
}
