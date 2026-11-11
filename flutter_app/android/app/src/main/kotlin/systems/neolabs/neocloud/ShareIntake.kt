package systems.neolabs.neocloud

import android.content.ContentResolver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodChannel
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.util.UUID
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/**
 * Files other apps share to NeoCloud (ACTION_SEND / ACTION_SEND_MULTIPLE).
 *
 * Only content:// streams from other apps are taken. Each one is copied to
 * cacheDir/share-intake/<random uuid>/<sanitized display name>; nothing the
 * sender supplies (display name, path, size) decides where a file lands. A
 * share bigger than [MAX_SHARE_BYTES] is refused as a whole. Dart gets the
 * copies as one batch on the event channel and deletes them after upload or
 * cancel; whatever is left from an earlier run is deleted on start.
 */
class ShareIntake(private val context: Context, messenger: BinaryMessenger) : EventChannel.StreamHandler {
    companion object {
        const val METHOD_CHANNEL = "systems.neolabs.neocloud/share"
        const val EVENT_CHANNEL = "systems.neolabs.neocloud/share/events"
        const val MAX_SHARE_BYTES = 2L * 1024 * 1024 * 1024
        const val MAX_NAME_BYTES = 255
        const val FALLBACK_NAME = "shared-file"

        /**
         * The last path segment of [raw] without control characters, NUL and
         * path separators, at most [MAX_NAME_BYTES] UTF-8 bytes. Empty, "."
         * and ".." become [FALLBACK_NAME].
         */
        fun sanitizeName(raw: String?): String {
            val base = (raw ?: "").substringAfterLast('/').substringAfterLast('\\')
            val cleaned = base.filter { it != '/' && it != '\\' && !Character.isISOControl(it) }.trim()
            val name = truncateUtf8(cleaned, MAX_NAME_BYTES)
            return if (name.isEmpty() || name == "." || name == "..") FALLBACK_NAME else name
        }

        private fun truncateUtf8(value: String, maxBytes: Int): String {
            if (value.toByteArray(Charsets.UTF_8).size <= maxBytes) return value
            val out = StringBuilder()
            var bytes = 0
            var index = 0
            while (index < value.length) {
                val codePoint = value.codePointAt(index)
                val chars = Character.charCount(codePoint)
                val size = value.substring(index, index + chars).toByteArray(Charsets.UTF_8).size
                if (bytes + size > maxBytes) break
                out.appendCodePoint(codePoint)
                bytes += size
                index += chars
            }
            return out.toString()
        }
    }

    private class TooLarge : IOException()

    private val root = File(context.cacheDir, "share-intake")
    private val executor: ExecutorService = Executors.newSingleThreadExecutor()
    private val main = Handler(Looper.getMainLooper())
    private val methods = MethodChannel(messenger, METHOD_CHANNEL)
    private val events = EventChannel(messenger, EVENT_CHANNEL)
    private var sink: EventChannel.EventSink? = null
    private val waiting = mutableListOf<Map<String, Any>>()

    init {
        methods.setMethodCallHandler { call, result ->
            when (call.method) {
                "intakeDir" -> result.success(root.canonicalPath)
                else -> result.notImplemented()
            }
        }
        events.setStreamHandler(this)
        // A new engine holds no shares yet: anything here is left over.
        executor.execute { root.deleteRecursively() }
    }

    fun dispose() {
        methods.setMethodCallHandler(null)
        events.setStreamHandler(null)
        sink = null
        executor.shutdownNow()
    }

    fun receive(intent: Intent?) {
        if (intent == null || (intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0) return
        val streams: List<Uri?> = when (intent.action) {
            Intent.ACTION_SEND -> listOf(streamOf(intent))
            Intent.ACTION_SEND_MULTIPLE -> streamsOf(intent)
            else -> return
        }
        if (streams.isEmpty() || streams.all { it == null }) return
        executor.execute {
            val batch = copyAll(streams)
            main.post { deliver(batch) }
        }
    }

    override fun onListen(arguments: Any?, eventSink: EventChannel.EventSink) {
        sink = eventSink
        waiting.forEach(eventSink::success)
        waiting.clear()
    }

    override fun onCancel(arguments: Any?) {
        sink = null
    }

    private fun deliver(batch: Map<String, Any>) {
        val target = sink
        if (target == null) waiting.add(batch) else target.success(batch)
    }

    private fun streamOf(intent: Intent): Uri? =
        if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
        }

    private fun streamsOf(intent: Intent): List<Uri?> =
        if (Build.VERSION.SDK_INT >= 33) {
            intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java).orEmpty()
        } else {
            @Suppress("DEPRECATION")
            intent.getParcelableArrayListExtra<Uri>(Intent.EXTRA_STREAM).orEmpty()
        }

    /** content:// from another app's provider; never file:// or our own provider. */
    private fun acceptable(uri: Uri?): Boolean {
        if (uri == null || uri.scheme != ContentResolver.SCHEME_CONTENT) return false
        val authority = uri.authority ?: return false
        val own = context.packageName
        return authority.split(';').none { it == own || it.startsWith("$own.") }
    }

    private fun copyAll(streams: List<Uri?>): Map<String, Any> {
        val files = mutableListOf<Map<String, Any>>()
        var rejected = 0
        var total = 0L
        for (uri in streams) {
            if (!acceptable(uri)) {
                rejected += 1
                continue
            }
            val dir = File(root, UUID.randomUUID().toString())
            try {
                val target = copyOne(uri!!, dir, MAX_SHARE_BYTES - total)
                total += target.length()
                files.add(mapOf("path" to target.canonicalPath, "name" to target.name, "size" to target.length()))
            } catch (error: TooLarge) {
                dir.deleteRecursively()
                files.forEach { File(it["path"] as String).parentFile?.deleteRecursively() }
                return mapOf("files" to emptyList<Any>(), "rejected" to streams.size, "tooLarge" to true)
            } catch (error: Exception) {
                dir.deleteRecursively()
                rejected += 1
            }
        }
        return mapOf("files" to files, "rejected" to rejected, "tooLarge" to false)
    }

    /** Copies [uri] into [dir]; throws [TooLarge] past [limit] bytes. */
    private fun copyOne(uri: Uri, dir: File, limit: Long): File {
        val resolver = context.contentResolver
        var name: String? = null
        var declared = -1L
        resolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { cursor ->
            if (cursor.moveToFirst()) {
                val nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
                val sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE)
                if (nameColumn >= 0 && !cursor.isNull(nameColumn)) name = cursor.getString(nameColumn)
                if (sizeColumn >= 0 && !cursor.isNull(sizeColumn)) declared = cursor.getLong(sizeColumn)
            }
        }
        if (declared > limit) throw TooLarge()
        val canonicalRoot = root.canonicalFile
        if (!dir.mkdirs()) throw IOException("intake folder")
        val canonicalDir = dir.canonicalFile
        val target = File(canonicalDir, sanitizeName(name))
        if (canonicalDir.parentFile != canonicalRoot || target.canonicalFile.parentFile != canonicalDir) {
            throw IOException("outside the intake folder")
        }
        if (!target.createNewFile()) throw IOException("exists")
        val input = resolver.openInputStream(uri) ?: throw IOException("no stream")
        input.use { source ->
            FileOutputStream(target).use { sink ->
                val buffer = ByteArray(64 * 1024)
                var copied = 0L
                while (true) {
                    val read = source.read(buffer)
                    if (read < 0) break
                    copied += read
                    if (copied > limit) throw TooLarge()
                    sink.write(buffer, 0, read)
                }
            }
        }
        return target
    }
}
