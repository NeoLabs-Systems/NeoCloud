import 'dart:typed_data';

import 'package:url_launcher/url_launcher.dart';

/// Opens a download URL with the system, for platforms without their own
/// saver.
Future<void> startDownload(Uri uri, {String? fileName, Map<String, String> headers = const <String, String>{}}) async {
  await launchUrl(uri, mode: LaunchMode.externalApplication);
}

/// Saves bytes the app already holds (an account export). Platforms without
/// their own saver can't.
Future<void> saveBytes(Uint8List bytes, String fileName, {String mime = 'application/octet-stream'}) async {
  throw UnsupportedError('Saving files is not supported on this platform.');
}
