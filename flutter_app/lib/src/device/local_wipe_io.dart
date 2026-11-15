import 'dart:io';

import 'package:path_provider/path_provider.dart';

/// Deletes NeoCloud's own folders on disk: the camera backup ledger (app
/// support) and the photo thumbnails (cache). Nothing else is touched, and
/// a folder that is not there, or cannot be removed, is not an error.
Future<void> eraseLocalFiles() async {
  final separator = Platform.pathSeparator;
  final folders = <String>[];
  try {
    folders.add('${(await getApplicationSupportDirectory()).path}${separator}camera');
    folders.add('${(await getApplicationCacheDirectory()).path}${separator}photo-thumbs');
  } on Object {
    return;
  }
  for (final path in folders) {
    try {
      final folder = Directory(path);
      if (await folder.exists()) await folder.delete(recursive: true);
    } on FileSystemException {
      // Left behind; the next sign-in replaces what it finds there.
    }
  }
}
