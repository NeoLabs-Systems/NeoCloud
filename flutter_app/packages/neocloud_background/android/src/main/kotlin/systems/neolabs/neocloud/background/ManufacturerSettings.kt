package systems.neolabs.neocloud.background

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Settings
import android.util.Log

/**
 * The autostart and battery screens manufacturers add on top of Android,
 * where their own task killers are switched off (see dontkillmyapp.com).
 * None of them is public API: each is tried in turn, and the app's info
 * page is the fallback. Only the phone's own manufacturer has any of these
 * packages, so the list needs no matching by brand; the steps to follow
 * there are chosen on the Dart side (background_reliability.dart).
 */
object ManufacturerSettings {
    private const val TAG = "NeoCloudOemSettings"

    private val candidates = listOf(
        // Xiaomi, Redmi, POCO (MIUI / HyperOS): autostart, then battery saver.
        ComponentName("com.miui.securitycenter", "com.miui.permcenter.autostart.AutoStartManagementActivity"),
        ComponentName("com.miui.powerkeeper", "com.miui.powerkeeper.ui.HiddenAppsConfigActivity"),
        // Huawei and Honor: app launch.
        ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
        ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.optimize.process.ProtectActivity"),
        ComponentName("com.huawei.systemmanager", "com.huawei.systemmanager.appcontrol.activity.StartupAppControlActivity"),
        ComponentName("com.hihonor.systemmanager", "com.hihonor.systemmanager.startupmgr.ui.StartupNormalAppListActivity"),
        // Oppo, Realme, OnePlus (ColorOS / OxygenOS): startup manager.
        ComponentName("com.coloros.safecenter", "com.coloros.safecenter.permission.startup.StartupAppListActivity"),
        ComponentName("com.coloros.safecenter", "com.coloros.safecenter.startupapp.StartupAppListActivity"),
        ComponentName("com.oppo.safe", "com.oppo.safe.permission.startup.StartupAppListActivity"),
        ComponentName("com.oneplus.security", "com.oneplus.security.chainlaunch.view.ChainLaunchAppListActivity"),
        // Vivo and iQOO: background start-up.
        ComponentName("com.vivo.permissionmanager", "com.vivo.permissionmanager.activity.BgStartUpManagerActivity"),
        ComponentName("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.BgStartUpManager"),
        ComponentName("com.iqoo.secure", "com.iqoo.secure.ui.phoneoptimize.AddWhiteListActivity"),
        // Samsung: Device care › Battery (background usage limits).
        ComponentName("com.samsung.android.lool", "com.samsung.android.sm.battery.ui.BatteryActivity"),
        ComponentName("com.samsung.android.sm", "com.samsung.android.sm.ui.battery.BatteryActivity"),
        // Asus: auto-start manager.
        ComponentName("com.asus.mobilemanager", "com.asus.mobilemanager.autostart.AutoStartActivity"),
        ComponentName("com.asus.mobilemanager", "com.asus.mobilemanager.powersaver.PowerSaverSettings"),
    )

    /** One of the screens exists on this phone. */
    fun available(context: Context): Boolean = candidates.any { resolves(context, it) }

    /** Opens the first screen that works; false when the app's info page opened instead. */
    fun open(context: Context): Boolean {
        for (component in candidates) {
            if (!resolves(context, component)) continue
            try {
                context.startActivity(
                    Intent()
                        .setComponent(component)
                        // Xiaomi's battery saver screen reads these.
                        .putExtra("package_name", context.packageName)
                        .putExtra("package_label", context.applicationInfo.loadLabel(context.packageManager))
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                )
                return true
            } catch (error: Exception) {
                // Not exported on this firmware version: try the next one.
                Log.i(TAG, "Could not open $component", error)
            }
        }
        context.startActivity(
            Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.parse("package:${context.packageName}"))
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
        )
        return false
    }

    private fun resolves(context: Context, component: ComponentName): Boolean =
        context.packageManager.resolveActivity(Intent().setComponent(component), 0) != null
}
