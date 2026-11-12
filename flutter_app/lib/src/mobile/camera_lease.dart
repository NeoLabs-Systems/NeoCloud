import 'dart:convert';
import 'dart:io';
import 'dart:math';

// Only one camera upload pass at a time, across everything that can start
// one: the app on screen, WorkManager's hourly task and new-photo trigger on
// Android, the BGTasks on iOS. Those run in separate isolates of one process
// (sometimes separate processes), so neither a Dart flag nor an fcntl file
// lock (per process) can tell them apart. A lease file can:
//
//   - it is created with O_EXCL, which only one caller wins;
//   - it names its owner (a random id per pass) and when the owner was last
//     alive; the running pass renews it before every file and every minute
//     while one file takes long, so a pass of hours keeps it;
//   - one not renewed for [CameraLease.staleAfter] is from a pass that died
//     (the process was killed, the app suspended): the next one takes it
//     over by replacing the file atomically;
//   - after taking it, a caller waits [CameraLease.settle] and reads it
//     again, so of two that took it at the same moment only the last writer
//     goes on. A pass that finds itself no longer the owner stops before its
//     next file.

class CameraLease {
  CameraLease(
    this.file, {
    this.staleAfter = const Duration(minutes: 5),
    this.settle = const Duration(milliseconds: 300),
    DateTime Function()? clock,
    String? owner,
  }) : _clock = clock ?? DateTime.now,
       owner = owner ?? _randomOwner();

  final File file;
  final Duration staleAfter;
  final Duration settle;
  final String owner;
  final DateTime Function() _clock;
  int _writes = 0;

  static String _randomOwner() {
    final random = Random.secure();
    return List<String>.generate(16, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0')).join();
  }

  /// Takes the lease. False when another pass holds a fresh one.
  Future<bool> acquire() async {
    await file.parent.create(recursive: true);
    try {
      await file.create(exclusive: true);
      await file.writeAsString(_encode(), flush: true);
    } on FileSystemException {
      final held = await _read();
      if (held != null && held.owner != owner && !_stale(held)) return false;
      await _write();
    }
    if (settle > Duration.zero) await Future<void>.delayed(settle);
    return (await _read())?.owner == owner;
  }

  /// Marks the pass alive. False when the lease was taken over meanwhile;
  /// the pass must then stop.
  Future<bool> renew() async {
    final held = await _read();
    if (held?.owner != owner) return false;
    await _write();
    return true;
  }

  /// Gives the lease up, if still held.
  Future<void> release() async {
    final held = await _read();
    if (held?.owner != owner) return;
    try {
      await file.delete();
    } on FileSystemException {
      // Already gone.
    }
  }

  bool _stale(_Held held) => _clock().difference(held.alive) >= staleAfter;

  String _encode() => jsonEncode(<String, String>{'owner': owner, 'alive': _clock().toUtc().toIso8601String()});

  /// Replaces the file atomically: write beside it, then rename over it.
  Future<void> _write() async {
    // Own name per write: a renewal from the timer may overlap one from
    // the pass.
    final temp = File('${file.path}.$owner.${_writes++}.tmp');
    await temp.writeAsString(_encode(), flush: true);
    await temp.rename(file.path);
  }

  /// The holder, or null when there is no lease. A file just created and
  /// not yet written counts as fresh, from its modification time.
  Future<_Held?> _read() async {
    try {
      final text = await file.readAsString();
      try {
        final json = (jsonDecode(text) as Map).cast<String, dynamic>();
        final alive = DateTime.tryParse(json['alive'] as String? ?? '');
        if (alive != null) return _Held(json['owner'] as String? ?? '', alive);
      } catch (_) {
        // Empty while being written, or damaged: judged by its age.
      }
      return _Held('', await file.lastModified());
    } on FileSystemException {
      return null;
    }
  }
}

class _Held {
  const _Held(this.owner, this.alive);

  final String owner;
  final DateTime alive;
}
