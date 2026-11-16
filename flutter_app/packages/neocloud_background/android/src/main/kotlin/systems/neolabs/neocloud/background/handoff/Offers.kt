package systems.neolabs.neocloud.background.handoff

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

data class OfferFile(val id: Long, val name: String, val size: Long)

/** A send addressed to this device, as the server reports it (docs/handoff.md). */
data class Offer(val id: String, val status: String, val fromName: String, val files: List<OfferFile>) {
    val pending: Boolean get() = status == "pending"
    val totalSize: Long get() = files.sumOf { it.size }

    /** The file names for the notification's expanded text: a few, then how many more. */
    fun nameList(shown: Int = 4): String {
        val names = files.take(shown).joinToString("\n") { it.name }
        return if (files.size > shown) "$names\n+${files.size - shown}" else names
    }
}

/** Reading the server's JSON; anything that doesn't look like an offer is dropped. */
object Offers {
    // Offer ids become folder names and notification tags: only what the server issues.
    private val SAFE_ID = Regex("^[A-Za-z0-9_-]{1,64}$")

    fun isSafeId(id: String): Boolean = SAFE_ID.matches(id)

    fun parse(json: JSONObject?): Offer? {
        json ?: return null
        val id = json.optString("id")
        val files = json.optJSONArray("files") ?: return null
        if (!isSafeId(id) || files.length() == 0) return null
        val parsed = ArrayList<OfferFile>(files.length())
        for (index in 0 until files.length()) {
            val file = files.optJSONObject(index) ?: return null
            val size = file.optLong("size", -1)
            if (!file.has("id") || size < 0) return null
            parsed.add(OfferFile(file.getLong("id"), file.optString("name", "file"), size))
        }
        val from = json.optJSONObject("from")
        return Offer(id, json.optString("status"), from?.optString("name").orEmpty(), parsed)
    }

    /** An event's data: the offer itself, or `{ offer }` for `handoff:updated`. */
    fun parseEvent(data: String): Offer? = try {
        val json = JSONObject(data)
        parse(json.optJSONObject("offer") ?: json)
    } catch (_: JSONException) {
        null
    }

    /** `GET /api/handoff/offers`: what waits for this device. */
    fun parseInbox(body: String): List<Offer> {
        val inbox: JSONArray = try {
            JSONObject(body).optJSONArray("inbox") ?: return emptyList()
        } catch (_: JSONException) {
            return emptyList()
        }
        return (0 until inbox.length()).mapNotNull { parse(inbox.optJSONObject(it)) }
    }
}
