package systems.neolabs.neocloud

import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest
import java.util.UUID
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/**
 * NeoCloud as the phone's gallery app.
 *
 * - Opened as the gallery (ACTION_MAIN with CATEGORY_APP_GALLERY, directly
 *   or as the selector of a launcher intent): Dart opens the Photos tab.
 * - Asked to show a photo or video (ACTION_VIEW, or the camera app's
 *   REVIEW for the shot just taken) with a content:// URI: Dart shows it in
 *   a local viewer. The file is the phone's, not necessarily in NeoCloud.
 *
 * Dart only ever reads URIs that arrived this way. Events wait until Dart
 * asks for them ("start"), like the pairing links in MainActivity.
 */
class GalleryIntents(private val context: Context, messenger: BinaryMessenger) {
    companion object {
        const val CHANNEL = "systems.neolabs.neocloud/gallery"
        const val ACTION_REVIEW = "com.android.camera.action.REVIEW"
        const val MAX_READ_BYTES = 64L * 1024 * 1024
        const val MAX_COPY_BYTES = 4L * 1024 * 1024 * 1024
    }

    private val root = File(context.cacheDir, "gallery-view")
    private val executor: ExecutorService = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private val channel = MethodChannel(messenger, CHANNEL)
    private val waiting = mutableListOf<Map<String, Any?>>()
    private val allowed = mutableSetOf<String>()
    private var dartListening = false

    init {
        channel.setMethodCallHandler { call, result ->
            when (call.method) {
                "start" -> {
                    dartListening = true
                    result.success(ArrayList(waiting))
                    waiting.clear()
                }
                "read" -> withUri(call.argument<String>("uri"), result) { uri -> read(uri) }
                "hash" -> withUri(call.argument<String>("uri"), result) { uri -> hash(uri) }
                "copy" -> withUri(call.argument<String>("uri"), result) { uri -> copy(uri, call.argument<String>("name")) }
                else -> result.notImplemented()
            }
        }
        // A new engine holds no copies yet: anything here is left over.
        executor.execute { root.deleteRecursively() }
    }

    fun dispose() {
        channel.setMethodCallHandler(null)
        executor.shutdownNow()
    }

    /** [cold]: the intent the app was started with (closing the viewer leaves the app). */
    fun receive(intent: Intent?, cold: Boolean) {
        if (intent == null || (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0) return
        when (intent.action) {
            Intent.ACTION_MAIN -> {
                val gallery = intent.hasCategory(Intent.CATEGORY_APP_GALLERY) ||
                    intent.selector?.hasCategory(Intent.CATEGORY_APP_GALLERY) == true
                if (gallery) emit(mapOf("type" to "gallery"))
            }
            Intent.ACTION_VIEW, ACTION_REVIEW -> view(intent, cold)
        }
    }

    private fun view(intent: Intent, cold: Boolean) {
        val uri = intent.data ?: return
        if (!acceptable(uri)) return
        val resolver = context.contentResolver
        val mime = (intent.type ?: resolver.getType(uri) ?: return).lowercase()
        if (!mime.startsWith("image/") && !mime.startsWith("video/")) return
        var name: String? = null
        var size = -1L
        try {
            resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
                if (cursor.moveToFirst()) {
                    val nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                    val sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE)
                    if (nameColumn >= 0 && !cursor.isNull(nameColumn)) name = cursor.getString(nameColumn)
                    if (sizeColumn >= 0 && !cursor.isNull(sizeColumn)) size = cursor.getLong(sizeColumn)
                }
            }
        } catch (error: Exception) {
            // Some providers answer no columns; the name falls back below.
        }
        allowed.add(uri.toString())
        emit(mapOf(
            "type" to "view",
            "uri" to uri.toString(),
            "name" to ShareIntake.sanitizeName(name),
            "mime" to mime,
            "size" to size,
            "cold" to cold,
        ))
    }

    private fun emit(event: Map<String, Any?>) {
        if (dartListening) channel.invokeMethod("event", event) else waiting.add(event)
    }

    /** content:// from another app's provider; never file:// or our own provider. */
    private fun acceptable(uri: Uri): Boolean {
        if (uri.scheme != ContentResolver.SCHEME_CONTENT) return false
        val authority = uri.authority ?: return false
        val own = context.packageName
        return authority.split(';').none { it == own || it.startsWith("$own.") }
    }

    private fun withUri(raw: String?, result: MethodChannel.Result, work: (Uri) -> Any?) {
        if (raw == null || !allowed.contains(raw)) {
            result.error("NOT_ALLOWED", "Not a file NeoCloud was asked to open", null)
            return
        }
        executor.execute {
            try {
                val value = work(Uri.parse(raw))
                main.post { result.success(value) }
            } catch (error: Exception) {
                main.post { result.error("READ_FAILED", error.javaClass.simpleName, null) }
            }
        }
    }

    private fun read(uri: Uri): ByteArray {
        val input = context.contentResolver.openInputStream(uri) ?: throw IOException("no stream")
        return input.use { source ->
            val out = java.io.ByteArrayOutputStream()
            val buffer = ByteArray(64 * 1024)
            var total = 0L
            while (true) {
                val read = source.read(buffer)
                if (read < 0) break
                total += read
                if (total > MAX_READ_BYTES) throw IOException("too large")
                out.write(buffer, 0, read)
            }
            out.toByteArray()
        }
    }

    /** { sha256, size } of the content, for "is it in NeoCloud already?". */
    private fun hash(uri: Uri): Map<String, Any> {
        val digest = MessageDigest.getInstance("SHA-256")
        var size = 0L
        val input = context.contentResolver.openInputStream(uri) ?: throw IOException("no stream")
        input.use { source ->
            val buffer = ByteArray(256 * 1024)
            while (true) {
                val read = source.read(buffer)
                if (read < 0) break
                size += read
                digest.update(buffer, 0, read)
            }
        }
        val hex = digest.digest().joinToString("") { "%02x".format(it) }
        return mapOf("sha256" to hex, "size" to size)
    }

    /** Copies the content into cacheDir/gallery-view/<uuid>/<name> for the upload queue. */
    private fun copy(uri: Uri, rawName: String?): String {
        val dir = File(root, UUID.randomUUID().toString())
        if (!dir.mkdirs()) throw IOException("copy folder")
        val canonicalDir = dir.canonicalFile
        val target = File(canonicalDir, ShareIntake.sanitizeName(rawName))
        if (canonicalDir.parentFile != root.canonicalFile || target.canonicalFile.parentFile != canonicalDir) {
            throw IOException("outside the copy folder")
        }
        try {
            val input = context.contentResolver.openInputStream(uri) ?: throw IOException("no stream")
            input.use { source ->
                FileOutputStream(target).use { sink ->
                    val buffer = ByteArray(256 * 1024)
                    var copied = 0L
                    while (true) {
                        val read = source.read(buffer)
                        if (read < 0) break
                        copied += read
                        if (copied > MAX_COPY_BYTES) throw IOException("too large")
                        sink.write(buffer, 0, read)
                    }
                }
            }
        } catch (error: Exception) {
            dir.deleteRecursively()
            throw error
        }
        return target.canonicalPath
    }
}
