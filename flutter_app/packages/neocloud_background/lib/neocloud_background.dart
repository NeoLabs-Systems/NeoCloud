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
/// while files are transferred from the app; camera upload's background
/// work, which runs its passes in WorkManager's foreground service;
/// battery optimisation, manufacturer and notification settings. iOS: extra time to finish a transfer when the app leaves the
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

  /// Whether the active network is connected and unmetered (Android), the
  /// test "Wi-Fi only" needs. Null where the platform can't tell.
  static Future<bool?> isUnmetered() => _invoke<bool>('isUnmetered');

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

  /// Android: camera upload is on. Books the background passes natively
  /// (hourly, soon after a photo or video is added, and continuations of a
  /// pass that stopped with files left), each run by [callback]'s Dart entry
  /// point in an engine of its own, as a foreground service where Android
  /// allows it, under [channelName] with [title] until progress shows.
  /// Restored after a reboot or an app update.
  static Future<void> configureCameraWork({
    required int callback,
    required bool unmeteredOnly,
    required bool chargingOnly,
    required String channelName,
    required String title,
    required String pauseLabel,
  }) async {
    if (!_android) return;
    await _invoke<void>('configureCameraWork', <String, Object?>{
      'callback': callback,
      'unmeteredOnly': unmeteredOnly,
      'chargingOnly': chargingOnly,
      'channelName': channelName,
      'title': title,
      'pauseLabel': pauseLabel,
    });
  }

  static Future<void> disableCameraWork() async {
    if (!_android) return;
    await _invoke<void>('disableCameraWork');
  }

  /// Android: a pass soon (after [delay]) for files a stopped pass left,
  /// under the same network and charging conditions.
  static Future<void> continueCameraWork({Duration delay = Duration.zero}) async {
    if (!_android) return;
    await _invoke<void>('continueCameraWork', <String, Object?>{'delayMinutes': delay.inMinutes});
  }

  /// A camera pass holds the lease in this engine; should the engine die
  /// before [cameraPassEnded], Android frees the lease and books the next
  /// pass.
  static Future<void> cameraPassStarted() async {
    if (!_android) return;
    await _invoke<void>('cameraPassStarted');
  }

  static Future<void> cameraPassEnded() async {
    if (!_android) return;
    await _invoke<void>('cameraPassEnded');
  }

  /// Android: removes a camera pass lease that no engine of this process
  /// holds any more (its pass died or failed to let go), so a new pass
  /// needn't wait for it to go stale.
  static Future<void> clearDeadCameraLease() async {
    if (!_android) return;
    await _invoke<void>('clearDeadCameraLease');
  }

  /// Android: camera upload's access to the photos, "full", "limited" or
  /// "denied", in any engine (photo_manager needs an activity to tell).
  /// Null elsewhere or when it can't tell.
  static Future<String?> photoAccess() async {
    if (!_android) return null;
    return _invoke<String>('photoAccess');
  }

  /// Android: whether photos are read with their GPS position
  /// (ACCESS_MEDIA_LOCATION). Null elsewhere or when it can't tell.
  static Future<bool?> photoLocationGranted() async {
    if (!_android) return null;
    return _invoke<bool>('photoLocationGranted');
  }

  /// The background entry point's pass is over; its worker ends.
  static Future<void> cameraWorkerDone() async {
    if (!_android) return;
    await _invoke<void>('cameraWorkerDone');
  }

  /// [onStop] runs when the platform wants [owner]'s work to end, whether
  /// or not it holds the transfer service yet: Pause, a time limit, or the
  /// background worker being stopped.
  static void watchStop(String owner, void Function(BackgroundStopReason reason) onStop) {
    if (!supported) return;
    _listen();
    _onStop[owner] = onStop;
  }
}
