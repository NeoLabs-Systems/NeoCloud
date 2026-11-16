import 'package:neocloud_background/neocloud_background.dart';

/// What the native side reports about receiving files in the background.
class HandoffBackgroundStatus {
  const HandoffBackgroundStatus({
    this.enabled = false,
    this.serviceRunning = false,
    this.wipeRequested = false,
  });

  factory HandoffBackgroundStatus.fromMap(Map<String, Object?> raw) =>
      HandoffBackgroundStatus(
        enabled: raw['enabled'] == true,
        serviceRunning: raw['serviceRunning'] == true,
        wipeRequested: raw['wipeRequested'] == true,
      );

  /// Configured natively (a token is stored).
  final bool enabled;

  /// The foreground service holds the live connection. Off, only the
  /// 15-minute check looks for waiting files.
  final bool serviceRunning;

  /// The server asked this device to erase itself while only the background
  /// receiver was talking to it; the app has not acted on it yet.
  final bool wipeRequested;
}

/// The platform side of [HandoffBackground]: Android's foreground service and
/// WorkManager check in neocloud_background. A fake in tests.
abstract class HandoffBackgroundPlatform {
  /// Only Android can keep a connection open for a closed app; iOS would
  /// need Apple's push service, which a self-hosted server doesn't have.
  bool get supported;

  /// True when the service runs.
  Future<bool> configure({
    required String server,
    required String token,
    required Map<String, String> texts,
  });

  Future<void> disable();

  Future<HandoffBackgroundStatus> status();

  Future<bool> takeWipeRequest();

  Future<void> dismissOffer(String offerId);
}

/// [NeoCloudBackground]'s Android implementation.
class NeoCloudHandoffPlatform implements HandoffBackgroundPlatform {
  const NeoCloudHandoffPlatform();

  @override
  bool get supported => NeoCloudBackground.supportsHandoff;

  @override
  Future<bool> configure({
    required String server,
    required String token,
    required Map<String, String> texts,
  }) => NeoCloudBackground.configureHandoff(
    server: server,
    token: token,
    texts: texts,
  );

  @override
  Future<void> disable() => NeoCloudBackground.disableHandoff();

  @override
  Future<HandoffBackgroundStatus> status() async =>
      HandoffBackgroundStatus.fromMap(await NeoCloudBackground.handoffStatus());

  @override
  Future<bool> takeWipeRequest() => NeoCloudBackground.takeHandoffWipeRequest();

  @override
  Future<void> dismissOffer(String offerId) =>
      NeoCloudBackground.dismissHandoffOffer(offerId);
}
