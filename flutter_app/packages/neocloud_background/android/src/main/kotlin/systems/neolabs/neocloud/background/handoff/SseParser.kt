package systems.neolabs.neocloud.background.handoff

/**
 * Server-sent events, a line at a time (the Dart side's SseParser in
 * lib/src/handoff/handoff_stream.dart does the same): comment lines (the
 * server's heartbeat) are skipped, an event with no data is dropped.
 */
class SseParser(private val onEvent: (event: String, data: String) -> Unit) {
    private var name = ""
    private val data = ArrayList<String>()

    fun addLine(line: String) {
        when {
            line.isEmpty() -> {
                if (name.isNotEmpty() && data.isNotEmpty()) onEvent(name, data.joinToString("\n"))
                name = ""
                data.clear()
            }
            line.startsWith(":") || line.startsWith("retry:") -> Unit
            line.startsWith("event:") -> name = line.substring(6).trim()
            line.startsWith("data:") -> data.add(line.substring(5).removePrefix(" "))
        }
    }
}
