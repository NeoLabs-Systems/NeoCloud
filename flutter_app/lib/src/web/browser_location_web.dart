import 'package:web/web.dart' as web;

/// Swaps the address bar to [path] without a reload, e.g. from
/// `/app/device?code=…` back to `/app/` once the device is handled.
void replaceBrowserPath(String path) {
  web.window.history.replaceState(null, '', path);
}
