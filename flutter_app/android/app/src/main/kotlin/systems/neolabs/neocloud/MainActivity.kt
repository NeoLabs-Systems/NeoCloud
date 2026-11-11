package systems.neolabs.neocloud

import android.content.Intent
import android.os.Bundle
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine

class MainActivity : FlutterActivity() {
    private var shareIntake: ShareIntake? = null

    // Runs inside super.onCreate, before the launch intent is looked at.
    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)
        shareIntake?.dispose()
        shareIntake = ShareIntake(applicationContext, flutterEngine.dartExecutor.binaryMessenger)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // A recreated activity gets its old intent again; it was handled then.
        if (savedInstanceState == null) shareIntake?.receive(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        shareIntake?.receive(intent)
    }

    override fun onDestroy() {
        shareIntake?.dispose()
        shareIntake = null
        super.onDestroy()
    }
}
