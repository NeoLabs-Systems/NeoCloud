package systems.neolabs.neocloud.background.handoff

import android.content.Context
import android.net.Uri

/**
 * Which files of an offer are already in Downloads, so a download that is
 * retried after a failure on the third of five files doesn't save the first
 * two again ("name (1).ext").
 */
class SavedLedger(context: Context, private val offerId: String) {
    private val preferences = context.applicationContext.getSharedPreferences("neocloud_handoff_saved", Context.MODE_PRIVATE)

    private fun key(fileId: Long) = "$offerId.$fileId"

    fun get(fileId: Long): SavedFile? {
        val (name, mime, uri) = (preferences.getString(key(fileId), null) ?: return null).split("\n", limit = 3).let {
            if (it.size < 3) return null else it
        }
        return SavedFile(name, mime, uri.takeIf { it.isNotEmpty() }?.let(Uri::parse))
    }

    fun put(fileId: Long, saved: SavedFile) {
        preferences.edit().putString(key(fileId), "${saved.name}\n${saved.mime}\n${saved.uri ?: ""}").apply()
    }

    fun clear() {
        val edit = preferences.edit()
        for (stored in preferences.all.keys) if (stored.startsWith("$offerId.")) edit.remove(stored)
        edit.apply()
    }
}
