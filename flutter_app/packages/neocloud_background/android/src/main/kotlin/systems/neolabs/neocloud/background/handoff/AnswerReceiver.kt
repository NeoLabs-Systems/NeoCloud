package systems.neolabs.neocloud.background.handoff

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** The Accept and Decline buttons of an offer's notification. */
class AnswerReceiver : BroadcastReceiver() {
    companion object {
        const val ACTION_ACCEPT = "systems.neolabs.neocloud.handoff.ACCEPT"
        const val ACTION_DECLINE = "systems.neolabs.neocloud.handoff.DECLINE"
        const val EXTRA_OFFER = "offer"
        const val EXTRA_FROM = "from"
    }

    override fun onReceive(context: Context, intent: Intent) {
        val offerId = intent.getStringExtra(EXTRA_OFFER) ?: return
        if (!Offers.isSafeId(offerId)) return
        val accept = when (intent.action) {
            ACTION_ACCEPT -> true
            ACTION_DECLINE -> false
            else -> return
        }
        // An action button doesn't dismiss its notification; the answer is on its way.
        OfferNotifier.cancelOffer(context, offerId)
        HandoffWork.answer(context, offerId, intent.getStringExtra(EXTRA_FROM).orEmpty(), accept)
    }
}
