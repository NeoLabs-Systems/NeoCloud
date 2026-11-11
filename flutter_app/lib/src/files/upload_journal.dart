import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'unload_guard.dart';
import 'upload_queue.dart';

/// An upload a closed or reloaded page left unfinished. The file itself is
/// gone with the page; picking it again continues where it stopped.
class InterruptedUpload {
  const InterruptedUpload({required this.name, required this.size, required this.parentId, this.folder = '', this.uploadId});

  factory InterruptedUpload.fromJson(Map<String, dynamic> json) => InterruptedUpload(
    name: '${json['name'] ?? ''}',
    size: (json['size'] as num?)?.toInt() ?? 0,
    parentId: '${json['parentId'] ?? 'root'}',
    folder: '${json['folder'] ?? ''}',
    uploadId: json['uploadId'] as String?,
  );

  final String name;
  final int size;
  final String parentId;
  final String folder;
  final String? uploadId;

  Map<String, Object?> toJson() => <String, Object?>{'name': name, 'size': size, 'parentId': parentId, 'folder': folder, 'uploadId': uploadId};

  bool matches(String fileName, int fileSize) => fileName == name && fileSize == size;
}

/// Keeps the upload list's metadata on the device while uploads run, so a
/// reload can offer to resume them, and asks before the tab closes while
/// anything is still being sent. Vault uploads aren't kept: they need the
/// unlocked vault, which a reload locks.
class UploadJournal extends ChangeNotifier {
  UploadJournal(this._preferences, this._queue) {
    interrupted = _read();
    _queue.addListener(_onQueueChanged);
  }

  static const String _key = 'uploads.journal';

  final SharedPreferences _preferences;
  final UploadQueue _queue;
  String _written = '';

  /// What the last session left unfinished.
  late List<InterruptedUpload> interrupted;

  List<InterruptedUpload> _read() {
    try {
      final raw = _preferences.getString(_key);
      if (raw == null || raw.isEmpty) return <InterruptedUpload>[];
      return <InterruptedUpload>[
        for (final entry in jsonDecode(raw) as List) InterruptedUpload.fromJson((entry as Map).cast<String, dynamic>()),
      ];
    } catch (_) {
      return <InterruptedUpload>[];
    }
  }

  void _onQueueChanged() {
    setUnloadGuard(_queue.activeCount > 0);
    _write();
  }

  void _write() {
    final entries = <Map<String, Object?>>[
      for (final entry in interrupted) entry.toJson(),
      for (final task in _queue.tasks)
        if (!task.finished && !task.hidden && task.cipher == null)
          InterruptedUpload(
            name: task.source.name,
            size: task.source.size,
            parentId: task.parentId,
            folder: task.source.folder,
            uploadId: task.uploadId,
          ).toJson(),
    ];
    final encoded = entries.isEmpty ? '' : jsonEncode(entries);
    if (encoded == _written) return;
    _written = encoded;
    if (encoded.isEmpty) {
      _preferences.remove(_key);
    } else {
      _preferences.setString(_key, encoded);
    }
  }

  /// Queues the re-picked [sources] that match interrupted uploads, each into
  /// its original folder, and returns how many matched.
  int resume(List<UploadSource> sources) {
    var matched = 0;
    final remaining = List<InterruptedUpload>.of(interrupted);
    for (final source in sources) {
      final entry = remaining.where((entry) => entry.matches(source.name, source.size)).firstOrNull;
      if (entry == null) continue;
      remaining.remove(entry);
      matched += 1;
      final placed = UploadSource(name: source.name, size: source.size, openRead: source.openRead, folder: entry.folder, modifiedAt: source.modifiedAt);
      final uploadId = entry.uploadId;
      _queue.enqueue(entry.parentId, <UploadSource>[placed], resumeIds: uploadId == null ? const {} : <UploadSource, String>{placed: uploadId});
    }
    interrupted = remaining;
    _write();
    notifyListeners();
    return matched;
  }

  void dismiss() {
    interrupted = <InterruptedUpload>[];
    _write();
    notifyListeners();
  }

  @override
  void dispose() {
    _queue.removeListener(_onQueueChanged);
    super.dispose();
  }
}
