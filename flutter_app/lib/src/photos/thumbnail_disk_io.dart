import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';
import 'package:path_provider/path_provider.dart';

import 'photo_thumbnails.dart';

/// Thumbnails in the app's own cache folder (the system may clear it), one
/// folder per account and server: switching accounts deletes the others.
/// Kept to [_maxBytes]; the oldest go first.
ThumbnailStore? createThumbnailStore(String account) => _DiskThumbnailStore(sha1.convert(account.codeUnits).toString());

const int _maxBytes = 300 * 1024 * 1024;

class _DiskThumbnailStore implements ThumbnailStore {
  _DiskThumbnailStore(this._account);

  final String _account;
  Future<Directory?>? _folder;

  Future<Directory?> _open() => _folder ??= () async {
    try {
      final root = Directory('${(await getApplicationCacheDirectory()).path}/photo-thumbs');
      final folder = Directory('${root.path}/$_account');
      await folder.create(recursive: true);
      await for (final entry in root.list()) {
        if (entry is Directory && entry.path != folder.path) await entry.delete(recursive: true);
      }
      await _trim(folder);
      return folder;
    } on FileSystemException {
      return null;
    }
  }();

  Future<void> _trim(Directory folder) async {
    final files = <File, FileStat>{};
    var total = 0;
    await for (final entry in folder.list()) {
      if (entry is! File) continue;
      final stat = await entry.stat();
      files[entry] = stat;
      total += stat.size;
    }
    if (total <= _maxBytes) return;
    final oldest = files.entries.toList()..sort((a, b) => a.value.modified.compareTo(b.value.modified));
    for (final entry in oldest) {
      if (total <= _maxBytes * 0.8) break;
      total -= entry.value.size;
      await entry.key.delete();
    }
  }

  String _name(String key) => sha1.convert(key.codeUnits).toString();

  @override
  Future<Uint8List?> read(String key) async {
    final folder = await _open();
    if (folder == null) return null;
    final file = File('${folder.path}/${_name(key)}');
    try {
      return await file.readAsBytes();
    } on FileSystemException {
      return null;
    }
  }

  @override
  Future<void> write(String key, Uint8List bytes) async {
    final folder = await _open();
    if (folder == null) return;
    try {
      final file = File('${folder.path}/${_name(key)}.part');
      await file.writeAsBytes(bytes, flush: false);
      await file.rename('${folder.path}/${_name(key)}');
    } on FileSystemException {
      // A full disk only costs a download next time.
    }
  }
}
