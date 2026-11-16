part of 'main.dart';

// "Receive files in the background" (Android): the words the native
// notifications use, turning it on with the permission sheet, and the
// one-time offer of it. The receiver itself is src/handoff/background and the
// Kotlin in packages/neocloud_background (handoff/).

/// What the native notifications say, in the app's language. `{name}` and
/// `{count}` are filled in there: the native side runs without the app.
Map<String, String> handoffBackgroundTexts() => <String, String>{
  'channelOffers': appStrings.handoffChannelOffers,
  'channelService': appStrings.handoffChannelService,
  'channelTransfers': appStrings.handoffChannelTransfers,
  'serviceTitle': appStrings.handoffServiceTitle,
  'offerOne': appStrings.handoffBackgroundOfferOne('{name}'),
  'offerMany': appStrings.handoffBackgroundOfferMany('{name}', '{count}'),
  'accept': appStrings.handoffAccept,
  'decline': appStrings.handoffDecline,
  'receiving': appStrings.handoffReceivingFrom('{name}'),
  'receivedOne': appStrings.handoffReceivedOne,
  'receivedMany': appStrings.handoffReceivedMany('{count}'),
  'savedIn': appStrings.handoffSavedIn,
  'open': appStrings.handoffOpen,
  'failedTitle': appStrings.handoffReceiveFailedTitle,
  'failedBody': appStrings.handoffReceiveFailedBody('{name}'),
};

/// What the permission sheet checks when receiving files is involved: always
/// receiving, plus the camera upload items while camera upload is on.
BackgroundFeatures _receiveFeatures(NeoCloudController controller) =>
    BackgroundFeatures(
      camera: controller.cameraUploads.settings.enabled,
      receive: true,
      receiveOn: controller.handoff.background.enabled,
    );

/// Switches receiving in the background on, then opens the permission sheet
/// if something it needs isn't granted yet (notifications, battery).
Future<void> turnOnBackgroundReceiving(
  BuildContext context,
  NeoCloudController controller,
) async {
  final background = controller.handoff.background;
  await background.setEnabled(true);
  if (!context.mounted) return;
  await showBackgroundReliabilitySheet(
    context,
    controller.cameraUploads,
    onlyIfOpen: true,
    features: _receiveFeatures(controller),
    handoff: background,
  );
}

/// Once, the first time the person sends or accepts files on a phone: files
/// can also arrive while the app is closed, if they allow it.
Future<void> offerBackgroundReceiving(
  BuildContext context,
  NeoCloudController controller,
) async {
  final background = controller.handoff.background;
  if (!background.hintDue) return;
  await background.markHintShown();
  if (!context.mounted) return;
  final turnOn = await showAdaptiveSheet<bool>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      icon: const Icon(Icons.cloud_download_outlined),
      title: Text(appStrings.handoffBackgroundHintTitle),
      content: Text(appStrings.handoffBackgroundHintBody),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: Text(appStrings.backgroundNotNow),
        ),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: Text(appStrings.backgroundTurnOn),
        ),
      ],
    ),
  );
  if (turnOn == true && context.mounted) {
    await turnOnBackgroundReceiving(context, controller);
  }
}
