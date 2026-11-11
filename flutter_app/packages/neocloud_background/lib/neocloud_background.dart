import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Why the platform wants a transfer to stop: the user tapped Pause in the
/// notification, or Android's daily data sync allowance (Android 15) or
/// iOS's background time ran out.
enum BackgroundStopReason { pause, timeout }

/// The native side of NeoCloud's background work, ported from NeoAgent's
/// background keep-alive.
///
/// Android: a data sync foreground service with a low-importance progress
/// notification (and a Pause action) and a partial wakelock, both only
/// while files are transferred; a WorkManager job that fires when a photo
/// or video is added; battery optimisation, manufacturer and notification
/// settings. iOS: extra time to finish a transfer when the app leaves the
/// screen, and the Background App Refresh, Low Power Mode and notification
/// status.
///
/// Several owners (camera upload, the upload queue) can hold the service at
/// once; it stops when the last one lets go.
class NeoCloudBackground {
  NeoCloudBackground._();

  static const MethodChannel _channel = MethodChannel('neocloud/background');

  static final Map<String, void Function(BackgroundStopReason reason)> _onStop =
      <String, void Function(BackgroundStopReason reason)>{};
  static bool _listening = false;

  static bool get supported =>
      !kIsWeb && (defaultTargetPlatform == TargetPlatform.android || defaultTargetPlatform == TargetPlatform.iOS);

  static bool get _android => !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

  static void _listen() {
    if (_listening) return;
    _listening = true;
    _channel.setMethodCallHandler((call) async {
      if (call.method != 'stopRequested') return null;
      final args = (call.arguments as Map?)?.cast<String, Object?>() ?? const <String, Object?>{};
      final reason = args['reason'] == 'pause' ? BackgroundStopReason.pause : BackgroundStopReason.timeout;
      final owner = args['owner'] as String?;
      final handlers = owner == null ? _onStop.values.toList() : <void Function(BackgroundStopReason)>[?_onStop[owner]];
      for (final handler in handlers) {
        handler(reason);
      }
      return null;
    });
  }

  static Future<T?> _invoke<T>(String method, [Map<String, Object?>? arguments]) async {
    if (!supported) return null;
    try {
      return await _channel.invokeMethod<T>(method, arguments);
    } on PlatformException catch (error) {
      debugPrint('neocloud/background $method: ${error.message}');
      return null;
    } on MissingPluginException {
      return null;
    }
  }

  /// What the platform reports right now; see the native `status` handlers
  /// for the keys. Empty off Android and iOS.
  static Future<Map<String, Object?>> status() async {
    final raw = await _invoke<Map<Object?, Object?>>('status');
    return raw?.cast<String, Object?>() ?? const <String, Object?>{};
  }

  /// [owner] has files to transfer. Android starts (or joins) the
  /// foreground service and shows [title] and [body] with a progress bar of
  /// [current] of [total]; iOS asks for time to finish when the app leaves
  /// the screen. [onStop] runs when the platform wants the transfer to end.
  /// False when Android refused the service (started from the background
  /// without an exemption, or the daily allowance is used up): the work
  /// goes on without it.
  static Future<bool> startTransfer({
    required String owner,
    required String channelName,
    required String title,
    required String body,
    required String pauseLabel,
    int current = 0,
    int total = 0,
    void Function(BackgroundStopReason reason)? onStop,
  }) async {
    if (!supported) return false;
    _listen();
    if (onStop != null) _onStop[owner] = onStop;
    final started = await _invoke<bool>('startTransfer', <String, Object?>{
      'owner': owner,
      'channelName': channelName,
      'title': title,
      'body': body,
      'pauseLabel': pauseLabel,
      'current': current,
      'total': total,
    });
    return started ?? false;
  }

  static Future<void> updateTransfer({
    required String owner,
    required String title,
    required String body,
    int current = 0,
    int total = 0,
  }) => _invoke<void>('updateTransfer', <String, Object?>{
    'owner': owner,
    'title': title,
    'body': body,
    'current': current,
    'total': total,
  });

  static Future<void> stopTransfer(String owner) async {
    _onStop.remove(owner);
    await _invoke<void>('stopTransfer', <String, Object?>{'owner': owner});
  }

  /// Android: the system dialog that exempts NeoCloud from battery
  /// optimisation, or the settings list where it can't be shown.
  static Future<void> requestIgnoreBatteryOptimizations() => _invoke<void>('requestIgnoreBatteryOptimizations');

  /// Android: the manufacturer's own autostart or battery screen. False
  /// when none was found and the app's info page opened instead.
  static Future<bool> openManufacturerSettings() async => await _invoke<bool>('openManufacturerSettings') ?? false;

  /// Android: Data Saver's "unrestricted data" switch for NeoCloud.
  static Future<void> openBackgroundDataSettings() => _invoke<void>('openBackgroundDataSettings');

  /// Asks for permission to post notifications. Where the system no longer
  /// asks (denied before), the notification settings open instead.
  static Future<bool> requestNotifications() async => await _invoke<bool>('requestNotifications') ?? false;

  static Future<void> openNotificationSettings() => _invoke<void>('openNotificationSettings');

  /// The app's page in the system settings.
  static Future<void> openAppSettings() => _invoke<void>('openAppSettings');

  /// Android: run a background pass soon after a photo or video is added,
  /// with the same network and charging conditions as the periodic task.
  /// Also restored after a reboot or an app update.
  static Future<void> armMediaTrigger({required bool unmeteredOnly, required bool chargingOnly}) async {
    if (!_android) return;
    await _invoke<void>('armMediaTrigger', <String, Object?>{
      'unmeteredOnly': unmeteredOnly,
      'chargingOnly': chargingOnly,
    });
  }

  static Future<void> disarmMediaTrigger() async {
    if (!_android) return;
    await _invoke<void>('disarmMediaTrigger');
  }
}
