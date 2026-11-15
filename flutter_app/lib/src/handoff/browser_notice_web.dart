import 'dart:js_interop';

import 'package:web/web.dart' as web;

import 'browser_notice.dart';

BrowserNotice createBrowserNotice() => const _WebNotice();

class _WebNotice implements BrowserNotice {
  const _WebNotice();

  @override
  bool get supported => globalContext.hasProperty('Notification'.toJS).toDart;

  @override
  bool get granted => supported && web.Notification.permission == 'granted';

  @override
  Future<bool> request() async {
    if (!supported) return false;
    final answer = await web.Notification.requestPermission().toDart;
    return answer.toDart == 'granted';
  }

  @override
  void show(
    String title,
    String body, {
    required String tag,
    required void Function() onClick,
  }) {
    if (!granted) return;
    // The tag makes the browser replace a notification instead of stacking one per push.
    final notification = web.Notification(
      title,
      web.NotificationOptions(body: body, tag: tag),
    );
    notification.onclick = ((web.Event _) {
      web.window.focus();
      notification.close();
      onClick();
    }).toJS;
  }
}
