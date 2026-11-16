package systems.neolabs.neocloud.background.handoff

/**
 * File names from another device, made safe to create as a plain file in the
 * Downloads folder: a port of the desktop receiver's safe.go
 * (clients/lite/internal/handoff). The server sanitises names too; the
 * receiver never relies on that. Pure, so it can be tested without Android.
 */
object SafeNames {
    private const val MAX_NAME_BYTES = 200
    private const val MAX_EXTENSION_BYTES = 20
    private const val MAX_COPIES = 1000

    // Android's file systems refuse these as FAT does.
    private const val REFUSED = ":*?\"<>|"
    private val RESERVED = Regex("^(con|prn|aux|nul|com[0-9]|lpt[0-9])$", RegexOption.IGNORE_CASE)

    /** No path parts, no control or bidirectional characters, nothing FAT refuses. */
    fun safeName(name: String): String {
        val kept = StringBuilder()
        var index = 0
        while (index < name.length) {
            val cp = name.codePointAt(index)
            index += Character.charCount(cp)
            when {
                cp == '/'.code || cp == '\\'.code || (cp < 0x80 && REFUSED.indexOf(cp.toChar()) >= 0) -> kept.append('_')
                isDropped(cp) -> Unit
                else -> kept.appendCodePoint(cp)
            }
        }
        val clean = kept.toString().trim().trimEnd('.', ' ').trimStart('.')
        if (clean.isEmpty()) return "file"
        var stem = clean
        var ext = ""
        val dot = clean.lastIndexOf('.')
        if (dot > 0) {
            stem = clean.substring(0, dot)
            ext = clean.substring(dot)
        }
        if (utf8Length(ext) > MAX_EXTENSION_BYTES) ext = truncate(ext, MAX_EXTENSION_BYTES)
        stem = truncate(stem, MAX_NAME_BYTES - utf8Length(ext))
        if (RESERVED.matches(stem)) stem = "_$stem"
        return stem + ext
    }

    /**
     * [clean] (already [safeName]d), or "stem (1).ext", "stem (2).ext", … for
     * the first one [taken] doesn't know. Null when a thousand are taken.
     */
    fun uniqueName(clean: String, taken: (String) -> Boolean): String? {
        var stem = clean
        var ext = ""
        val dot = clean.lastIndexOf('.')
        if (dot > 0) {
            stem = clean.substring(0, dot)
            ext = clean.substring(dot)
        }
        for (copy in 0 until MAX_COPIES) {
            val candidate = if (copy == 0) clean else "$stem ($copy)$ext"
            if (!taken(candidate)) return candidate
        }
        return null
    }

    private fun isDropped(cp: Int): Boolean =
        Character.getType(cp) == Character.CONTROL.toInt() ||
            cp == 0x061c ||
            cp in 0x200b..0x200f ||
            cp in 0x202a..0x202e ||
            cp in 0x2066..0x2069 ||
            cp == 0xfeff

    private fun utf8Length(text: String): Int = text.toByteArray(Charsets.UTF_8).size

    /** Cuts [text] to at most [bytes] UTF-8 bytes without splitting a character. */
    private fun truncate(text: String, bytes: Int): String {
        var end = text.length
        while (end > 0 && utf8Length(text.substring(0, end)) > bytes) {
            end -= Character.charCount(text.codePointBefore(end))
        }
        return text.substring(0, end)
    }
}
