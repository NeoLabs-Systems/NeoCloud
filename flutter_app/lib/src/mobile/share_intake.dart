import 'package:flutter/foundation.dart';

import '../files/upload_queue.dart';
import 'share_intake_stub.dart' if (dart.library.io) 'share_intake_io.dart' as platform;

/// One share from another app (the system share sheet): the files as the
/// Android app copied them into its private intake folder.
@immutable
class SharedBatch {
  const SharedBatch({required this.files, this.rejected = 0, this.tooLarge = false});

  final List<UploadSource> files;

  /// Items that could not be taken (not a content:// stream, unreadable).
  final int rejected;

  /// The share was over the size limit and nothing of it was kept.
  final bool tooLarge;
}

/// Files other apps share to NeoCloud. Each share is its own batch; the app
/// asks for a folder per batch and uploads exactly that batch.
abstract class ShareIntake {
  /// Shares not yet answered, oldest first.
  ValueListenable<List<SharedBatch>> get pending;

  Future<void> start();

  /// Takes [batch] off [pending] once the user answered its prompt.
  void remove(SharedBatch batch);

  /// Deletes the intake copies of [files] (after upload or cancel).
  Future<void> release(Iterable<UploadSource> files);
}

ShareIntake createShareIntake() => platform.createShareIntake();

/// Deletes the intake copies of [batch]'s files as their uploads end:
/// done, skipped or cancelled, or taken off the list. Failed uploads keep
/// theirs so "Retry" still works.
void releaseWhenUploaded(ShareIntake intake, UploadQueue queue, SharedBatch batch) {
  final open = batch.files.toSet();
  void check() {
    final finished = <UploadSource>[
      for (final source in open)
        if (!queue.tasks.any((task) => identical(task.source, source) && (!task.finished || task.status == UploadStatus.failed)))
          source,
    ];
    if (finished.isEmpty) return;
    open.removeAll(finished);
    if (open.isEmpty) queue.removeListener(check);
    intake.release(finished);
  }

  queue.addListener(check);
  check();
}
