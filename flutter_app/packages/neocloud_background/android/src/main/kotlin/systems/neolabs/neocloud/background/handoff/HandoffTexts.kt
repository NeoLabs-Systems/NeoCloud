package systems.neolabs.neocloud.background.handoff

import org.json.JSONObject

/**
 * The words of the notifications, in the app's language: Dart passes them
 * when it configures receiving, since the native side runs without an engine
 * and doesn't know which language the person chose. `{name}` and `{count}`
 * are filled in here. English stands in for anything missing.
 */
class HandoffTexts(private val values: Map<String, String>) {
    val channelOffers get() = text("channelOffers", "Incoming files")
    val channelService get() = text("channelService", "Ready to receive")
    val channelTransfers get() = text("channelTransfers", "Receiving files")
    val serviceTitle get() = text("serviceTitle", "NeoCloud is ready to receive files")
    val accept get() = text("accept", "Accept")
    val decline get() = text("decline", "Decline")
    val open get() = text("open", "Open")
    val savedIn get() = text("savedIn", "Saved in Downloads/NeoCloud")

    fun offer(name: String, count: Int): String =
        if (count == 1) fill("offerOne", "{name} wants to send you a file", name, count)
        else fill("offerMany", "{name} wants to send you {count} files", name, count)

    fun receiving(name: String) = fill("receiving", "Receiving from {name}", name, 0)

    fun received(count: Int): String =
        if (count == 1) fill("receivedOne", "File received", "", count)
        else fill("receivedMany", "{count} files received", "", count)

    fun failedTitle() = text("failedTitle", "Couldn’t receive the files")
    fun failedBody(name: String) = fill("failedBody", "Ask {name} to send them again.", name, 0)

    private fun text(key: String, fallback: String) = values[key]?.takeIf { it.isNotBlank() } ?: fallback

    private fun fill(key: String, fallback: String, name: String, count: Int) =
        text(key, fallback).replace("{name}", name).replace("{count}", count.toString())

    fun toJson(): String = JSONObject(values).toString()

    companion object {
        fun fromJson(json: String?): HandoffTexts = try {
            val parsed = JSONObject(json ?: "{}")
            HandoffTexts(parsed.keys().asSequence().associateWith { parsed.optString(it) })
        } catch (_: Exception) {
            HandoffTexts(emptyMap())
        }
    }
}
