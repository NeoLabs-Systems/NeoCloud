package systems.neolabs.neocloud.background.handoff

import android.content.Context
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/**
 * The `handoff*` calls of the `neocloud/background` channel (see
 * lib/src/handoff/background in the app). The plugin hands every call it
 * doesn't know to [handle].
 */
object HandoffBridge {
    /** True when [call] was one of ours. */
    fun handle(context: Context, call: MethodCall, result: MethodChannel.Result): Boolean {
        when (call.method) {
            "handoffConfigure" -> {
                val server = call.argument<String>("server")
                val token = call.argument<String>("token")
                if (server.isNullOrBlank() || token.isNullOrBlank()) {
                    result.error("ARGUMENTS", "server and token are needed", null)
                } else {
                    val texts = call.argument<Map<String, Any?>>("texts").orEmpty()
                    HandoffConfig.save(
                        context,
                        server,
                        token,
                        HandoffTexts(texts.mapNotNull { (key, value) -> (value as? String)?.let { key to it } }.toMap()),
                    )
                    // Whether the live service runs; the 15-minute check is booked either way.
                    result.success(HandoffWork.apply(context))
                }
            }
            "handoffDisable" -> {
                HandoffWork.disable(context)
                result.success(null)
            }
            "handoffStatus" -> result.success(
                mapOf(
                    "enabled" to HandoffWork.enabled(context),
                    "serviceRunning" to HandoffService.running,
                    "wipeRequested" to HandoffConfig.wipeRequested(context),
                ),
            )
            // The app asked itself about an offer (its dialog is up): the notification is redundant.
            "handoffDismiss" -> {
                call.argument<String>("offerId")?.let { OfferNotifier.cancelOffer(context, it) }
                result.success(null)
            }
            "handoffTakeWipeRequest" -> result.success(HandoffConfig.takeWipeRequest(context))
            else -> return false
        }
        return true
    }
}
