package systems.neolabs.neocloud

import android.content.Intent
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel

class MainActivity : FlutterActivity() {
    private var shareIntake: ShareIntake? = null

    // NeoCloud as the phone's gallery: opened as the gallery, or asked to
    // show a photo or video (GalleryIntents.kt).
    private var gallery: GalleryIntents? = null

    // neocloud://pair?server=…&code=… links (the web pairing page's "Open in
    // the app"). The one that started the app waits until Dart asks for it
    // (listenForAppLinks in lib/src/pairing/pairing_link.dart); later ones
    // are passed on as they arrive.
    private var links: MethodChannel? = null
    private var initialLink: String? = null
    private var dartListening = false

    // Runs inside super.onCreate, before the launch intent is looked at.
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        shareIntake?.dispose()
        shareIntake = ShareIntake(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
        gallery?.dispose()
        gallery = GalleryIntents(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
        dartListening = false
        links = MethodChannel(flutterEngine.dartExecutor.binaryMessenger, LINKS_CHANNEL).apply {
            setMethodCallHandler { call, result ->
                if (call.method == "initialLink") {
                    dartListening = true
                    result.success(initialLink)
                    initialLink = null
                } else {
                    result.notImplemented()
                }
            }
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // A recreated activity gets its old intent again; it was handled then.
        if (savedInstanceState == null) {
            shareIntake?.receive(intent)
            receiveLink(intent)
            gallery?.receive(intent, cold = true)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        shareIntake?.receive(intent)
        receiveLink(intent)
        gallery?.receive(intent, cold = false)
    }

    override fun onDestroy() {
        shareIntake?.dispose()
        shareIntake = null
        gallery?.dispose()
        gallery = null
        links?.setMethodCallHandler(null)
        links = null
        super.onDestroy()
    }

    private fun receiveLink(intent: Intent?) {
        if (intent == null || intent.action != Intent.ACTION_VIEW) return
        if ((intent.flags and Intent.FLAG_ACTIVITY_LAUNCHED_FROM_HISTORY) != 0) return
        val uri = intent.data ?: return
        if (uri.scheme != "neocloud" || uri.host != "pair") return
        val link = uri.toString()
        if (dartListening) links?.invokeMethod("link", link) else initialLink = link
    }

    private companion object {
        const val LINKS_CHANNEL = "systems.neolabs.neocloud/links"
    }
}
