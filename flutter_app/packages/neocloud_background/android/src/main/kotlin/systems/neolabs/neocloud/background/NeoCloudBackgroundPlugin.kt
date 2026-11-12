package systems.neolabs.neocloud.background

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.Uri
import android.os.Build
import android.os.PowerManager
import android.provider.Settings
import androidx.core.app.ActivityCompat
import androidx.core.app.NotificationManagerCompat
import io.flutter.embedding.engine.plugins.FlutterPlugin
import io.flutter.embedding.engine.plugins.activity.ActivityAware
import io.flutter.embedding.engine.plugins.activity.ActivityPluginBinding
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.PluginRegistry

/**
 * The `neocloud/background` channel: the transfer service, the new-photo
 * trigger, and the settings that decide whether Android lets NeoCloud run
 * in the background (battery optimisation, manufacturer screens,
 * notifications, Data Saver). Registered in every engine, including the
 * headless one of a background pass.
 */
class NeoCloudBackgroundPlugin :
    FlutterPlugin,
    MethodChannel.MethodCallHandler,
    ActivityAware,
    PluginRegistry.RequestPermissionsResultListener {

    companion object {
        private const val CHANNEL = "neocloud/background"
        private const val REQUEST_NOTIFICATIONS = 5107
        private const val PREFERENCES = "neocloud_background"
        private const val KEY_ASKED_NOTIFICATIONS = "notifications.asked"
    }

    private lateinit var context: Context
    private var channel: MethodChannel? = null
    private var activity: ActivityPluginBinding? = null
    private var pendingNotifications: MethodChannel.Result? = null

    override fun onAttachedToEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        context = binding.applicationContext
        channel = MethodChannel(binding.binaryMessenger, CHANNEL).also { it.setMethodCallHandler(this) }
    }

    override fun onDetachedFromEngine(binding: FlutterPlugin.FlutterPluginBinding) {
        channel?.let { TransferService.forget(context, it) }
        channel?.setMethodCallHandler(null)
        channel = null
    }

    override fun onAttachedToActivity(binding: ActivityPluginBinding) {
        activity = binding
        binding.addRequestPermissionsResultListener(this)
    }

    override fun onDetachedFromActivityForConfigChanges() = onDetachedFromActivity()

    override fun onReattachedToActivityForConfigChanges(binding: ActivityPluginBinding) = onAttachedToActivity(binding)

    override fun onDetachedFromActivity() {
        activity?.removeRequestPermissionsResultListener(this)
        activity = null
    }

    override fun onMethodCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "status" -> result.success(status())
            "isUnmetered" -> result.success(isUnmetered())
            "startTransfer" -> {
                val owner = call.argument<String>("owner") ?: "transfer"
                val held = channel
                result.success(held != null && TransferService.start(context, owner, held, progress(call)))
            }
            "updateTransfer" -> {
                TransferService.update(progress(call))
                result.success(null)
            }
            "stopTransfer" -> {
                TransferService.stop(context, call.argument<String>("owner") ?: "transfer")
                result.success(null)
            }
            "requestIgnoreBatteryOptimizations" -> {
                // The explanation came first, in the app; this is the
                // system's yes/no dialog.
                openSettings(
                    Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS, packageUri()),
                    Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS),
                )
                result.success(null)
            }
            "openManufacturerSettings" -> result.success(ManufacturerSettings.open(context))
            "openBackgroundDataSettings" -> {
                openSettings(Intent(Settings.ACTION_IGNORE_BACKGROUND_DATA_RESTRICTIONS_SETTINGS, packageUri()))
                result.success(null)
            }
            "requestNotifications" -> requestNotifications(result)
            "openNotificationSettings" -> {
                openNotificationSettings()
                result.success(null)
            }
            "openAppSettings" -> {
                openSettings(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, packageUri()))
                result.success(null)
            }
            "armMediaTrigger" -> {
                MediaTrigger.arm(
                    context,
                    call.argument<Boolean>("unmeteredOnly") != false,
                    call.argument<Boolean>("chargingOnly") == true,
                )
                result.success(null)
            }
            "disarmMediaTrigger" -> {
                MediaTrigger.disarm(context)
                result.success(null)
            }
            else -> result.notImplemented()
        }
    }

    private fun status(): Map<String, Any?> {
        val power = context.getSystemService(Context.POWER_SERVICE) as PowerManager
        val connectivity = context.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        return mapOf(
            "platform" to "android",
            "sdkInt" to Build.VERSION.SDK_INT,
            "manufacturer" to Build.MANUFACTURER,
            "brand" to Build.BRAND,
            "ignoringBatteryOptimizations" to power.isIgnoringBatteryOptimizations(context.packageName),
            "notificationsEnabled" to NotificationManagerCompat.from(context).areNotificationsEnabled(),
            // Data Saver is on and NeoCloud is not exempt from it.
            "backgroundDataRestricted" to
                (connectivity.restrictBackgroundStatus == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED),
            "manufacturerSettingsAvailable" to ManufacturerSettings.available(context),
        )
    }

    private fun progress(call: MethodCall) = TransferService.Progress(
        channelName = call.argument<String>("channelName") ?: "NeoCloud",
        title = call.argument<String>("title") ?: "NeoCloud",
        body = call.argument<String>("body") ?: "",
        pauseLabel = call.argument<String>("pauseLabel") ?: "Pause",
        current = call.argument<Int>("current") ?: 0,
        total = call.argument<Int>("total") ?: 0,
    )

    /**
     * Android 13+ asks once or twice; after that only the settings can turn
     * notifications on, so those open instead.
     */
    private fun requestNotifications(result: MethodChannel.Result) {
        if (NotificationManagerCompat.from(context).areNotificationsEnabled()) {
            result.success(true)
            return
        }
        val host = activity?.activity
        val preferences = context.getSharedPreferences(PREFERENCES, Context.MODE_PRIVATE)
        val askedBefore = preferences.getBoolean(KEY_ASKED_NOTIFICATIONS, false)
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || host == null || pendingNotifications != null ||
            (askedBefore && !ActivityCompat.shouldShowRequestPermissionRationale(host, Manifest.permission.POST_NOTIFICATIONS))
        ) {
            openNotificationSettings()
            result.success(false)
            return
        }
        preferences.edit().putBoolean(KEY_ASKED_NOTIFICATIONS, true).apply()
        pendingNotifications = result
        ActivityCompat.requestPermissions(host, arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFICATIONS)
    }

    override fun onRequestPermissionsResult(requestCode: Int, permissions: Array<out String>, grantResults: IntArray): Boolean {
        if (requestCode != REQUEST_NOTIFICATIONS) return false
        val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
        pendingNotifications?.success(granted)
        pendingNotifications = null
        return true
    }

    private fun openNotificationSettings() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            openSettings(
                Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName),
            )
        } else {
            openSettings(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, packageUri()))
        }
    }

    private fun packageUri(): Uri = Uri.parse("package:${context.packageName}")

    /** Tries [intent], then [fallback], then the app's info page, like NeoAgent's CallBridge. */
    private fun openSettings(intent: Intent, fallback: Intent? = null) {
        val attempts = listOfNotNull(
            intent,
            fallback,
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, packageUri()),
        )
        for (attempt in attempts) {
            try {
                context.startActivity(attempt.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                return
            } catch (_: Exception) {
                // Not on this phone: try the next.
            }
        }
    }

    /**
     * Whether the active network is connected and not metered: what
     * "Wi-Fi only" means, and the same test WorkManager's UNMETERED
     * constraint uses. Null when it can't tell.
     */
    private fun isUnmetered(): Boolean? {
        val manager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager ?: return null
        val network = manager.activeNetwork ?: return false
        val capabilities = manager.getNetworkCapabilities(network) ?: return false
        if (!capabilities.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_INTERNET)) return false
        return !manager.isActiveNetworkMetered
    }
}
