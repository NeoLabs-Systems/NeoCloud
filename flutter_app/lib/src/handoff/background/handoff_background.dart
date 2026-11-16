import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../api/backend_client.dart';
import 'handoff_background_platform.dart';

/// "Receive files in the background" (docs/handoff.md): the person's choice
/// to have this phone keep listening for Send to device offers while the app
/// is closed, and the hand-over of what the Android side needs to do that
/// without the app (server, device token, notification texts).
///
/// It is on only while the person turned it on, the "Let my other devices
/// send files here" switch is on and a device token is signed in; any other
/// state tells the native side to stop and forget the token.
class HandoffBackground extends ChangeNotifier {
  HandoffBackground({
    required this.client,
    SharedPreferences? preferences,
    HandoffBackgroundPlatform? platform,
    Map<String, String> Function()? texts,
    bool Function()? receiving,
  }) : _preferences = preferences,
       _platform = platform ?? const NeoCloudHandoffPlatform(),
       _texts = texts ?? _noTexts,
       _receiving = receiving ?? _always;

  static const String enabledKey = 'handoff_background_enabled';
  static const String hintShownKey = 'handoff_background_hint_shown';

  static Map<String, String> _noTexts() => const <String, String>{};
  static bool _always() => true;

  final BackendClient client;
  final SharedPreferences? _preferences;
  final HandoffBackgroundPlatform _platform;
  bool Function() _receiving;
  Map<String, String> Function() _texts;

  bool _serviceRunning = false;

  /// The notification texts in the app's current language; set once the UI
  /// (and with it the language) exists.
  set texts(Map<String, String> Function() value) => _texts = value;

  /// Whether this device accepts files (the controller's switch); set by it.
  set receiving(bool Function() value) => _receiving = value;

  /// Android only.
  bool get supported => _platform.supported;

  /// The person turned it on. Whether it can run also depends on the
  /// receiving switch and the sign-in; see [active].
  bool get enabled => supported && (_preferences?.getBool(enabledKey) ?? false);

  /// The native receiver is configured: switched on, accepting files, signed in.
  bool get active => enabled && _receiving() && client.bearerToken != null;

  /// The live connection runs (otherwise only the 15-minute check looks).
  bool get serviceRunning => _serviceRunning;

  /// The one-time offer to turn it on was shown already.
  bool get hintShown => _preferences?.getBool(hintShownKey) ?? false;

  /// Whether the one-time offer is due: it can be turned on here, isn't, and
  /// wasn't offered before.
  bool get hintDue => supported && !enabled && !hintShown && _receiving();

  Future<void> markHintShown() async {
    await _preferences?.setBool(hintShownKey, true);
  }

  Future<void> setEnabled(bool value) async {
    await _preferences?.setBool(enabledKey, value);
    await sync();
  }

  /// Brings the native receiver in line with the settings; called after any
  /// of them changes and when a session starts.
  Future<void> sync() async {
    if (!supported) return;
    final token = client.bearerToken;
    if (active && token != null) {
      try {
        _serviceRunning = await _platform.configure(
          server: client.baseUrl.replaceAll(RegExp(r'/+$'), ''),
          token: token,
          texts: _texts(),
        );
      } catch (_) {
        _serviceRunning = false;
      }
    } else {
      _serviceRunning = false;
      await _platform.disable();
    }
    notifyListeners();
  }

  /// Signed out (or the session ended): the native side forgets the token and
  /// stops. The person's choice stays for the next sign-in.
  Future<void> suspend() async {
    if (!supported) return;
    _serviceRunning = false;
    await _platform.disable();
    notifyListeners();
  }

  /// The native receiver was told by the server to erase this device (it
  /// answers that once, so the app never sees it). True once: the caller
  /// erases what the app holds.
  Future<bool> takeWipeRequest() async =>
      supported && await _platform.takeWipeRequest();

  /// The app asks about [offerId] itself: its notification is redundant.
  void dismissOffer(String offerId) {
    if (supported) unawaited(_platform.dismissOffer(offerId));
  }
}
