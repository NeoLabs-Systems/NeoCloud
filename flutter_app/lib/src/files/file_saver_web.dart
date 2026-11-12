import 'dart:js_interop';
import 'dart:typed_data';

import 'package:web/web.dart' as web;

/// Lets the browser download the file: it sends the session cookie itself
/// and streams to disk, so large files never pass through app memory.
/// [headers] are not needed here; the browser carries the cookie.
Future<void> startDownload(Uri uri, {String? fileName, Map<String, String> headers = const <String, String>{}}) async {
  final anchor = web.HTMLAnchorElement()
    ..href = uri.toString()
    ..download = fileName ?? ''
    ..style.display = 'none';
  web.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
}

/// Saves bytes the app already holds (an account export) as a download.
Future<void> saveBytes(Uint8List bytes, String fileName, {String mime = 'application/octet-stream'}) async {
  final blob = web.Blob(<JSAny>[bytes.toJS].toJS, web.BlobPropertyBag(type: mime));
  final url = web.URL.createObjectURL(blob);
  final anchor = web.HTMLAnchorElement()
    ..href = url
    ..download = fileName
    ..style.display = 'none';
  web.document.body?.append(anchor);
  anchor.click();
  anchor.remove();
  web.URL.revokeObjectURL(url);
}
