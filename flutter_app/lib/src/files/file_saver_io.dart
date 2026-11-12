import 'dart:io';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';
import 'package:http/http.dart' as http;

/// Asks where to save, then streams the download to that file with the
/// device token in [headers], so large files never sit in memory whole.
Future<void> startDownload(Uri uri, {String? fileName, Map<String, String> headers = const <String, String>{}}) async {
  final target = await FilePicker.platform.saveFile(fileName: fileName ?? 'NeoCloud.zip');
  if (target == null) return;
  final client = http.Client();
  try {
    final request = http.Request('GET', uri)..headers.addAll(headers);
    final response = await client.send(request);
    if (response.statusCode != 200) {
      await response.stream.drain<void>();
      throw HttpException('HTTP ${response.statusCode}', uri: uri);
    }
    await response.stream.pipe(File(target).openWrite());
  } finally {
    client.close();
  }
}

/// Asks where to save bytes the app already holds (an account export); the
/// system's save dialog writes them.
Future<void> saveBytes(Uint8List bytes, String fileName, {String mime = 'application/octet-stream'}) async {
  await FilePicker.platform.saveFile(fileName: fileName, bytes: bytes);
}
