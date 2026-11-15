import 'browser_notice_stub.dart'
    if (dart.library.js_interop) 'browser_notice_web.dart'
    as platform;

/// Browser notifications for incoming sends. Only the web app has them; the
/// permission is asked for from a user gesture (a settings switch), never
/// on its own.
abstract class BrowserNotice {
  /// The browser can show notifications at all.
  bool get supported;

  /// The person said yes (and has not revoked it in the browser).
  bool get granted;

  /// Asks the browser for permission; true when granted. Call from a tap.
  Future<bool> request();

  /// Shows a notification; [onClick] runs when the person clicks it.
  void show(
    String title,
    String body, {
    required String tag,
    required void Function() onClick,
  });
}

BrowserNotice createBrowserNotice() => platform.createBrowserNotice();
