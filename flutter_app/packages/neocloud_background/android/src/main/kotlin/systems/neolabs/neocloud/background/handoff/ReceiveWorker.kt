package systems.neolabs.neocloud.background.handoff

import android.app.DownloadManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.SystemClock
import android.util.Log
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.Data
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequest
import androidx.work.OutOfQuotaPolicy
import androidx.work.WorkerParameters
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * The person's answer to a notification: Decline tells the server; Accept
 * downloads every file into Downloads/NeoCloud without the app being opened,
 * with a progress notification (as a data sync foreground service, which
 * WorkManager runs the work in), resumes after a dropped connection (a retry
 * continues from the part file), checks every size, then confirms to the
 * server and offers "Open". Failures end in a notification, never silently.
 */
class ReceiveWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    companion object {
        private const val TAG = "NeoCloudHandoff"
        private const val KEY_OFFER = "offer"
        private const val KEY_FROM = "from"
        private const val KEY_ACCEPT = "accept"
        private const val MAX_ATTEMPTS = 4
        private const val PROGRESS_INTERVAL_MS = 500L

        fun request(offerId: String, fromName: String, accept: Boolean): OneTimeWorkRequest =
            OneTimeWorkRequest.Builder(ReceiveWorker::class.java)
                .setInputData(Data.Builder().putString(KEY_OFFER, offerId).putString(KEY_FROM, fromName).putBoolean(KEY_ACCEPT, accept).build())
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 15, TimeUnit.SECONDS)
                .setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)
                .build()
    }

    private var lastProgress = 0L

    /** Who is sending: what the notification says, from the offer once it is known. */
    private var sender = inputData.getString(KEY_FROM).orEmpty()

    // Expedited work on Android 11 and older runs as a foreground service from the start.
    override suspend fun getForegroundInfo(): ForegroundInfo = foreground(0, 0, "")

    private fun foreground(done: Long, total: Long, fileName: String): ForegroundInfo {
        val offerId = inputData.getString(KEY_OFFER).orEmpty()
        val notification = OfferNotifier.progress(applicationContext, sender, fileName, done, total)
        val id = OfferNotifier.progressId(offerId)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(id, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(id, notification)
        }
    }

    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val app = applicationContext
        val offerId = inputData.getString(KEY_OFFER).orEmpty()
        val accepting = inputData.getBoolean(KEY_ACCEPT, false)
        if (!Offers.isSafeId(offerId)) return@withContext Result.failure()
        val settings = HandoffConfig.load(app)
        if (settings == null) {
            OfferNotifier.cancelOffer(app, offerId)
            return@withContext Result.failure()
        }
        val api = HandoffApi(settings.server, settings.token)
        try {
            if (accepting) receive(api, offerId) else decline(api, offerId)
            Result.success()
        } catch (error: HandoffApi.Failure) {
            if (error.unauthorized) {
                HandoffWork.signedOut(app, error.wipeRequested)
                Result.failure()
            } else if (error.permanent) {
                giveUp(offerId, silent = !accepting || error.status == 404 || error.status == 410)
            } else {
                retryOrGiveUp(offerId, accepting, error)
            }
        } catch (error: PartDownload.OversizeException) {
            giveUp(offerId)
        } catch (error: IOException) {
            retryOrGiveUp(offerId, accepting, error)
        }
    }

    private fun decline(api: HandoffApi, offerId: String) {
        OfferNotifier.cancelOffer(applicationContext, offerId)
        api.decline(offerId)
        NotifiedOffers.release(applicationContext, offerId)
    }

    private suspend fun receive(api: HandoffApi, offerId: String) {
        val app = applicationContext
        val offer = api.offer(offerId)
        if (offer == null || offer.status != "pending" && offer.status != "accepted") {
            // Cancelled, expired or answered elsewhere meanwhile.
            OfferNotifier.cancelOffer(app, offerId)
            return
        }
        if (offer.fromName.isNotEmpty()) sender = offer.fromName
        api.accept(offerId)
        OfferNotifier.cancelOffer(app, offerId)
        try {
            setForeground(foreground(0, offer.totalSize, offer.files.first().name))
        } catch (error: Exception) {
            // Android refused the foreground service: it goes on as an ordinary job.
            Log.i(TAG, "Receiving without a foreground service: ${error.javaClass.simpleName}")
        }
        val folder = File(File(app.cacheDir, "handoff"), offerId)
        val saver = DownloadsFolder(app)
        val ledger = SavedLedger(app, offerId)
        val saved = ArrayList<SavedFile>(offer.files.size)
        var finishedBytes = 0L
        for (file in offer.files) {
            val already = ledger.get(file.id)
            if (already != null) {
                saved.add(already)
                finishedBytes += file.size
                continue
            }
            val base = finishedBytes
            val part = PartDownload.fetch(
                api,
                offerId,
                file,
                PartDownload.partFile(folder, file.id),
                progress = { bytes -> report(file.name, base + bytes, offer.totalSize) },
                stopped = { isStopped },
            )
            val result = saver.save(file.name, part)
            ledger.put(file.id, result)
            saved.add(result)
            finishedBytes += file.size
        }
        api.complete(offerId)
        folder.deleteRecursively()
        ledger.clear()
        NotifiedOffers.release(app, offerId)
        OfferNotifier.received(app, offerId, saved.size, openIntent(app, saved))
        Log.i(TAG, "Received ${saved.size} file(s) of offer $offerId")
    }

    /** The progress notification, at most twice a second. */
    private fun report(fileName: String, done: Long, total: Long) {
        val now = SystemClock.elapsedRealtime()
        if (now - lastProgress < PROGRESS_INTERVAL_MS && done < total) return
        lastProgress = now
        try {
            setForegroundAsync(foreground(done, total, fileName))
        } catch (_: Exception) {
            // Not a foreground service: nothing to update.
        }
    }

    private fun retryOrGiveUp(offerId: String, accepting: Boolean, error: Exception): Result {
        if (isStopped) return Result.retry()
        Log.w(TAG, "Receiving offer $offerId failed (attempt $runAttemptCount)", error)
        return if (runAttemptCount + 1 < MAX_ATTEMPTS) Result.retry() else giveUp(offerId, silent = !accepting)
    }

    /** Ends the attempt for good: the partial files go and the person is told (unless the offer is simply gone). */
    private fun giveUp(offerId: String, silent: Boolean = false): Result {
        val app = applicationContext
        File(File(app.cacheDir, "handoff"), offerId).deleteRecursively()
        SavedLedger(app, offerId).clear()
        OfferNotifier.cancelOffer(app, offerId)
        if (!silent) OfferNotifier.failed(app, offerId, sender)
        return Result.failure()
    }

    /** Tapping opens the single file in its app, or the Downloads list. */
    private fun openIntent(context: Context, saved: List<SavedFile>): PendingIntent? {
        val single = saved.singleOrNull()
        val intent = if (single?.uri != null) {
            Intent(Intent.ACTION_VIEW).setDataAndType(single.uri, single.mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        } else {
            Intent(DownloadManager.ACTION_VIEW_DOWNLOADS)
        }
        return PendingIntent.getActivity(
            context,
            0,
            intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }
}
