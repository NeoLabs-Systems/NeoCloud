import 'browser_notice.dart';

BrowserNotice createBrowserNotice() => const _NoNotice();

class _NoNotice implements BrowserNotice {
  const _NoNotice();

  @override
  bool get supported => false;

  @override
  bool get granted => false;

  @override
  Future<bool> request() async => false;

  @override
  void show(
    String title,
    String body, {
    required String tag,
    required void Function() onClick,
  }) {}
}
