package systems.neolabs.neocloud.background.handoff

import android.app.Activity
import android.app.Application
import android.os.Bundle

/**
 * Whether NeoCloud is on screen. With the app open the in-app dialog asks
 * about an incoming send, so no notification is shown; and when the app
 * comes to the front, the notifications that ask the same question go away.
 * Registered by the plugin in every engine and by the receiving service.
 */
object AppVisibility {
    private var started = 0
    private var registered = false

    val inForeground: Boolean @Synchronized get() = started > 0

    @Synchronized
    fun register(application: Application) {
        if (registered) return
        registered = true
        application.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityStarted(activity: Activity) {
                val first = synchronized(this@AppVisibility) { ++started == 1 }
                if (first) OfferNotifier.cancelOffers(application)
            }

            override fun onActivityStopped(activity: Activity) {
                synchronized(this@AppVisibility) { if (started > 0) started-- }
            }

            override fun onActivityCreated(activity: Activity, savedInstanceState: Bundle?) = Unit
            override fun onActivityResumed(activity: Activity) = Unit
            override fun onActivityPaused(activity: Activity) = Unit
            override fun onActivitySaveInstanceState(activity: Activity, outState: Bundle) = Unit
            override fun onActivityDestroyed(activity: Activity) = Unit
        })
    }
}
