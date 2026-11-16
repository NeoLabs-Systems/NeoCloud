package systems.neolabs.neocloud.background.handoff

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** What the background receiver needs to talk to the server. */
data class HandoffSettings(val server: String, val token: String, val texts: HandoffTexts)

/**
 * "Receive files in the background" as Dart configured it. Present means on:
 * the server address, the device token (sealed with a key in the Android
 * Keystore, so it is never plain text on disk) and the notification texts.
 * Removed on sign-out, when the token is revoked or the device is wiped.
 */
object HandoffConfig {
    private const val PREFERENCES = "neocloud_handoff"
    private const val KEY_SERVER = "server"
    private const val KEY_TOKEN = "token"
    private const val KEY_TEXTS = "texts"
    private const val KEY_WIPE = "wipe.requested"

    private fun preferences(context: Context) =
        context.applicationContext.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)

    fun save(context: Context, server: String, token: String, texts: HandoffTexts) {
        preferences(context).edit()
            .putString(KEY_SERVER, server.trimEnd('/'))
            .putString(KEY_TOKEN, TokenVault.seal(token))
            .putString(KEY_TEXTS, texts.toJson())
            .apply()
    }

    /** Null when receiving is off, or the sealed token can't be opened any more. */
    fun load(context: Context): HandoffSettings? {
        val stored = preferences(context)
        val server = stored.getString(KEY_SERVER, null) ?: return null
        val token = stored.getString(KEY_TOKEN, null)?.let(TokenVault::open) ?: return null
        return HandoffSettings(server, token, HandoffTexts.fromJson(stored.getString(KEY_TEXTS, null)))
    }

    /** Texts for notifications that outlive the configuration (a download finishing after sign-out). */
    fun texts(context: Context): HandoffTexts = HandoffTexts.fromJson(preferences(context).getString(KEY_TEXTS, null))

    fun clear(context: Context) {
        preferences(context).edit().remove(KEY_SERVER).remove(KEY_TOKEN).apply()
    }

    /**
     * The server answered 401 DEVICE_WIPE_REQUESTED to the background
     * receiver, which it does once. The app never sees that answer, so it is
     * kept here until the app is opened ([takeWipeRequest]).
     */
    fun markWipeRequested(context: Context) {
        preferences(context).edit().putBoolean(KEY_WIPE, true).apply()
    }

    fun wipeRequested(context: Context): Boolean = preferences(context).getBoolean(KEY_WIPE, false)

    fun takeWipeRequest(context: Context): Boolean {
        val requested = wipeRequested(context)
        if (requested) preferences(context).edit().remove(KEY_WIPE).apply()
        return requested
    }
}

/** AES-GCM with a non-exportable Keystore key. */
internal object TokenVault {
    private const val ALIAS = "neocloud.handoff.token"
    private const val PROVIDER = "AndroidKeyStore"
    private const val TRANSFORMATION = "AES/GCM/NoPadding"

    private fun key(): SecretKey {
        val store = KeyStore.getInstance(PROVIDER).apply { load(null) }
        (store.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, PROVIDER)
        generator.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .build(),
        )
        return generator.generateKey()
    }

    fun seal(plain: String): String {
        val cipher = Cipher.getInstance(TRANSFORMATION).apply { init(Cipher.ENCRYPT_MODE, key()) }
        val sealed = cipher.doFinal(plain.toByteArray(Charsets.UTF_8))
        return Base64.encodeToString(cipher.iv, Base64.NO_WRAP) + ":" + Base64.encodeToString(sealed, Base64.NO_WRAP)
    }

    /** Null when it can't be opened (the key is gone, e.g. after a restore onto another phone). */
    fun open(sealed: String): String? = try {
        val (iv, data) = sealed.split(":", limit = 2)
        val cipher = Cipher.getInstance(TRANSFORMATION).apply {
            init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, Base64.decode(iv, Base64.NO_WRAP)))
        }
        String(cipher.doFinal(Base64.decode(data, Base64.NO_WRAP)), Charsets.UTF_8)
    } catch (_: Exception) {
        null
    }
}
