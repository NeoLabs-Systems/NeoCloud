package systems.neolabs.neocloud.background.handoff

import java.io.File
import java.io.FileOutputStream
import java.io.IOException

/**
 * One file of an accepted offer, downloaded into a ".ncpart" file that moves
 * on only when exactly the announced number of bytes arrived, as the desktop
 * receiver's download.go does: a broken download never looks finished. An
 * interrupted transfer continues from the part with a Range request; a
 * server that ignores the range makes it start over.
 */
object PartDownload {
    private const val BUFFER = 64 * 1024

    /** The part of [file] of [offerId] lives in [folder] until it is complete. */
    fun partFile(folder: File, fileId: Long) = File(folder, "$fileId.ncpart")

    /**
     * Fetches [file] into [part] and returns it once it has exactly
     * [OfferFile.size] bytes. [progress] gets the bytes in the part so far;
     * [stopped] is asked between chunks.
     */
    fun fetch(api: HandoffApi, offerId: String, file: OfferFile, part: File, progress: (Long) -> Unit, stopped: () -> Boolean): File {
        part.parentFile?.mkdirs()
        var offset = if (part.exists()) part.length() else 0L
        if (offset > file.size) {
            part.delete()
            offset = 0
        }
        if (offset == file.size) {
            if (!part.exists()) part.createNewFile()
            progress(offset)
            return part
        }
        val connection = api.openFile(offerId, file.id, offset)
        try {
            val append = offset > 0 && connection.responseCode == 206 &&
                resumesAt(connection.getHeaderField("Content-Range"), offset)
            if (!append) offset = 0
            FileOutputStream(part, append).use { out ->
                connection.inputStream.use { input ->
                    val buffer = ByteArray(BUFFER)
                    while (true) {
                        if (stopped()) throw IOException("stopped")
                        val read = input.read(buffer)
                        if (read < 0) break
                        // Never accept more than the declared size.
                        if (offset + read > file.size) throw OversizeException(file.name)
                        out.write(buffer, 0, read)
                        offset += read
                        progress(offset)
                    }
                    out.fd.sync()
                }
            }
        } catch (error: OversizeException) {
            part.delete()
            throw error
        } finally {
            connection.disconnect()
        }
        if (part.length() != file.size) {
            // Cut short without an error: the next attempt continues from here.
            throw IOException("received ${part.length()} of ${file.size} bytes")
        }
        return part
    }

    /** A 206 resumes only when its Content-Range starts exactly at [offset]. */
    fun resumesAt(contentRange: String?, offset: Long): Boolean =
        contentRange != null && contentRange.startsWith("bytes $offset-")

    class OversizeException(name: String) : IOException("$name is larger than announced")
}
