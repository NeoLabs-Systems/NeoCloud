package systems.neolabs.neocloud.background.handoff

import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** The `/api/handoff` routes the background receiver uses (docs/api.md), with the device token. */
class HandoffApi(private val server: String, private val token: String) {

    /** The server answered with an error status. */
    class Failure(val status: Int, val code: String) : IOException("HTTP $status $code") {
        /** The token is dead: signed out, revoked or wiped. */
        val unauthorized: Boolean get() = status == 401

        /** The owner asked this device to erase itself; the server says so once. */
        val wipeRequested: Boolean get() = status == 401 && code == WIPE_CODE

        /** Asking again won't help: the offer is gone, closed or expired. */
        val permanent: Boolean get() = status in 400..499 && status != 408 && status != 429
    }

    companion object {
        const val WIPE_CODE = "DEVICE_WIPE_REQUESTED"
        private const val CONNECT_TIMEOUT_MS = 15_000

        // The event stream's heartbeat comes every 25 s: silence for longer is a dead connection.
        private const val STREAM_READ_TIMEOUT_MS = 70_000
        private const val READ_TIMEOUT_MS = 30_000
    }

    /** The live connection that makes this device online; the caller reads and disconnects it. */
    fun openEvents(): HttpURLConnection =
        connect("GET", "/api/handoff/events?receive=1", STREAM_READ_TIMEOUT_MS).also {
            it.setRequestProperty("Accept", "text/event-stream")
            requireSuccess(it)
        }

    fun inbox(): List<Offer> = Offers.parseInbox(text("GET", "/api/handoff/offers"))

    fun offer(id: String): Offer? = try {
        Offers.parse(JSONObject(text("GET", "/api/handoff/offers/$id")))
    } catch (_: org.json.JSONException) {
        null
    }

    fun accept(id: String) {
        text("POST", "/api/handoff/offers/$id/accept")
    }

    fun decline(id: String) {
        text("POST", "/api/handoff/offers/$id/decline")
    }

    fun complete(id: String) {
        text("POST", "/api/handoff/offers/$id/complete")
    }

    /** A file of an accepted offer, from [offset] on; the caller reads and disconnects it. */
    fun openFile(offerId: String, fileId: Long, offset: Long): HttpURLConnection =
        connect("GET", "/api/handoff/offers/$offerId/files/$fileId", READ_TIMEOUT_MS).also {
            if (offset > 0) it.setRequestProperty("Range", "bytes=$offset-")
            requireSuccess(it)
        }

    private fun text(method: String, path: String): String {
        val connection = connect(method, path, READ_TIMEOUT_MS)
        try {
            if (method == "POST") {
                connection.doOutput = true
                connection.setFixedLengthStreamingMode(0)
            }
            requireSuccess(connection)
            return connection.inputStream.bufferedReader(Charsets.UTF_8).use { it.readText() }
        } finally {
            connection.disconnect()
        }
    }

    private fun connect(method: String, path: String, readTimeout: Int): HttpURLConnection {
        val connection = URL(server + path).openConnection() as HttpURLConnection
        connection.requestMethod = method
        connection.connectTimeout = CONNECT_TIMEOUT_MS
        connection.readTimeout = readTimeout
        connection.setRequestProperty("Authorization", "Bearer $token")
        connection.setRequestProperty("User-Agent", "NeoCloud-Android-Handoff")
        return connection
    }

    /** Throws [Failure] (after reading the server's error code) unless the status is a success. */
    private fun requireSuccess(connection: HttpURLConnection) {
        val status = connection.responseCode
        if (status in 200..299) return
        val code = try {
            JSONObject(connection.errorStream?.bufferedReader(Charsets.UTF_8)?.use { it.readText() } ?: "{}").optString("code")
        } catch (_: Exception) {
            ""
        }
        connection.disconnect()
        throw Failure(status, code)
    }
}
