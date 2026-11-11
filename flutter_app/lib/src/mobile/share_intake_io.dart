import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../files/upload_queue.dart';
import 'share_intake.dart';
import 'share_intake_stub.dart';

// The Android side is ShareIntake.kt next to MainActivity. iOS has no share
// extension yet, so nothing arrives there.

ShareIntake createShareIntake() => Platform.isAndroid ? AndroidShareIntake() : NoShareIntake();

const MethodChannel _methods = MethodChannel('systems.neolabs.neocloud/share');
const EventChannel _events = EventChannel('systems.neolabs.neocloud/share/events');

/// [path] as a regular file directly inside a folder of [intakeRoot]
/// (`<intakeRoot>/<one folder>/<name>`), resolved; null for anything else,
/// symlinks included.
File? intakeFile(String intakeRoot, String path) {
  try {
    final root = Directory(intakeRoot).resolveSymbolicLinksSync();
    if (FileSystemEntity.typeSync(path, followLinks: false) != FileSystemEntityType.file) return null;
    final resolved = File(path).resolveSymbolicLinksSync();
    final folder = File(resolved).parent;
    if (folder.parent.path != root || resolved != path) return null;
    if (FileSystemEntity.typeSync(folder.path, followLinks: false) != FileSystemEntityType.directory) return null;
    return File(resolved);
  } on FileSystemException {
    return null;
  }
}

/// Turns one event of the Android channel into a batch, keeping only files
/// inside [intakeRoot].
SharedBatch sharedBatchFrom(Object? event, String intakeRoot) {
  final map = event is Map ? event : const <Object?, Object?>{};
  final files = <UploadSource>[];
  var rejected = (map['rejected'] as num?)?.toInt() ?? 0;
  for (final raw in (map['files'] as List?) ?? const <Object?>[]) {
    final item = raw is Map ? raw : const <Object?, Object?>{};
    final file = intakeFile(intakeRoot, '${item['path']}');
    if (file == null) {
      rejected += 1;
      continue;
    }
    try {
      files.add(IntakeSource(file));
    } on FileSystemException {
      rejected += 1;
    }
  }
  return SharedBatch(files: files, rejected: rejected, tooLarge: map['tooLarge'] == true);
}

/// An intake copy; [file] is what [ShareIntake.release] deletes.
class IntakeSource extends UploadSource {
  IntakeSource(this.file) : super(name: file.uri.pathSegments.last, size: file.lengthSync(), openRead: file.openRead);

  final File file;
}

class AndroidShareIntake implements ShareIntake {
  final ValueNotifier<List<SharedBatch>> _pending = ValueNotifier<List<SharedBatch>>(const <SharedBatch>[]);
  StreamSubscription<Object?>? _subscription;
  String? _root;

  @override
  ValueListenable<List<SharedBatch>> get pending => _pending;

  @override
  Future<void> start() async {
    if (_subscription != null) return;
    final root = await _methods.invokeMethod<String>('intakeDir');
    if (root == null) return;
    _root = root;
    _subscription = _events.receiveBroadcastStream().listen(_receive, onError: (Object _) {});
  }

  void _receive(Object? event) {
    final root = _root;
    if (root == null) return;
    final batch = sharedBatchFrom(event, root);
    if (batch.files.isEmpty && batch.rejected == 0 && !batch.tooLarge) return;
    _pending.value = <SharedBatch>[..._pending.value, batch];
  }

  @override
  void remove(SharedBatch batch) {
    _pending.value = <SharedBatch>[for (final other in _pending.value) if (!identical(other, batch)) other];
  }

  @override
  Future<void> release(Iterable<UploadSource> files) async {
    final root = _root;
    if (root == null) return;
    for (final source in files) {
      if (source is! IntakeSource) continue;
      final file = intakeFile(root, source.file.path);
      if (file == null) continue;
      try {
        await file.parent.delete(recursive: true);
      } on FileSystemException {
        // Gone already, or the next start clears it.
      }
    }
  }
}
