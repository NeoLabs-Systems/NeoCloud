package systems.neolabs.neocloud.background.handoff

import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import android.webkit.MimeTypeMap
import java.io.File
import java.io.FileInputStream
import java.io.IOException

/** A file that was saved; [uri] is null where only the folder can be opened (Android 9 and older). */
data class SavedFile(val name: String, val mime: String, val uri: Uri?)

/**
 * Where received files end up: Downloads/NeoCloud, where the person finds
 * them in the Files app. Android 10+ saves through MediaStore, which needs no
 * storage permission. Older phones would need one just for this; there the
 * files go into NeoCloud's own Downloads folder (Android/data/…/files/Download)
 * instead. Names are sanitised again here, never overwrite another file
 * ("name (1).ext") and stay inside the folder.
 */
class DownloadsFolder(private val context: Context) {

    companion object {
        const val FOLDER = "NeoCloud"

        fun mimeOf(name: String): String {
            val ext = name.substringAfterLast('.', "").lowercase()
            return MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
        }
    }

    /** Moves the finished [part] into the folder under [announcedName]. */
    fun save(announcedName: String, part: File): SavedFile {
        val clean = SafeNames.safeName(announcedName)
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveWithMediaStore(clean, part) else saveToFile(clean, part)
    }

    private fun saveWithMediaStore(clean: String, part: File): SavedFile {
        val resolver = context.contentResolver
        val relative = "${Environment.DIRECTORY_DOWNLOADS}/$FOLDER/"
        val name = SafeNames.uniqueName(clean) { exists(relative, it) } ?: throw IOException("no free name for $clean")
        val mime = mimeOf(name)
        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, name)
            put(MediaStore.MediaColumns.MIME_TYPE, mime)
            put(MediaStore.MediaColumns.RELATIVE_PATH, relative)
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val uri = resolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, values) ?: throw IOException("could not create $name")
        try {
            val out = resolver.openOutputStream(uri, "w") ?: throw IOException("could not write $name")
            out.use { target -> FileInputStream(part).use { it.copyTo(target) } }
            resolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
        } catch (error: Exception) {
            resolver.delete(uri, null, null)
            throw error
        }
        part.delete()
        return SavedFile(name, mime, uri)
    }

    private fun exists(relative: String, name: String): Boolean {
        context.contentResolver.query(
            MediaStore.Downloads.EXTERNAL_CONTENT_URI,
            arrayOf(MediaStore.MediaColumns._ID),
            "${MediaStore.MediaColumns.DISPLAY_NAME} = ? AND ${MediaStore.MediaColumns.RELATIVE_PATH} = ?",
            arrayOf(name, relative),
            null,
        )?.use { return it.moveToFirst() }
        return false
    }

    private fun saveToFile(clean: String, part: File): SavedFile {
        val root = File(context.getExternalFilesDir(Environment.DIRECTORY_DOWNLOADS) ?: context.filesDir, FOLDER)
        if (!root.isDirectory && !root.mkdirs()) throw IOException("could not create the folder")
        val folder = root.canonicalFile
        // Created exclusively, so two sends racing for one name get different files.
        for (copy in 0 until 1000) {
            val name = SafeNames.uniqueName(clean) { File(folder, it).exists() } ?: break
            val target = File(folder, name)
            if (target.canonicalFile.parentFile != folder) throw IOException("the file name points outside the folder")
            if (!target.createNewFile()) continue
            try {
                part.inputStream().use { input -> target.outputStream().use { input.copyTo(it) } }
            } catch (error: Exception) {
                target.delete()
                throw error
            }
            part.delete()
            return SavedFile(name, mimeOf(name), null)
        }
        throw IOException("no free name for $clean")
    }
}
