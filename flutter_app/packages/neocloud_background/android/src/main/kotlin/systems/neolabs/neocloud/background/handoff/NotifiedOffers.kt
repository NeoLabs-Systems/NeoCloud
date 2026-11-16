package systems.neolabs.neocloud.background.handoff

import android.content.Context

/**
 * The offers the person was already told about, so a service that restarts
 * (the server sends every waiting offer again on each connect) or the
 * fifteen-minute check never shows one twice. Entries older than an offer
 * can live (24 h, plus a margin) are dropped.
 */
object NotifiedOffers {
    private const val PREFERENCES = "neocloud_handoff_notified"
    private const val KEEP_MS = 3 * 24 * 60 * 60 * 1000L

    private fun preferences(context: Context) =
        context.applicationContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)

    /** True once per offer id: the caller shows the notification only then. */
    @Synchronized
    fun claim(context: Context, id: String, now: Long = System.currentTimeMillis()): Boolean {
        val stored = preferences(context)
        val known = stored.all.filterValues { it is Long && now - it < KEEP_MS }
        if (id in known) return false
        val edit = stored.edit()
        for (old in stored.all.keys - known.keys) edit.remove(old)
        edit.putLong(id, now).apply()
        return true
    }

    /** The offer was answered or is gone; it can't arrive again. */
    @Synchronized
    fun release(context: Context, id: String) {
        preferences(context).edit().remove(id).apply()
    }

    @Synchronized
    fun clear(context: Context) {
        preferences(context).edit().clear().apply()
    }

    fun ids(context: Context): Set<String> = preferences(context).all.keys.toSet()
}
