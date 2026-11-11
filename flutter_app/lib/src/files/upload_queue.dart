import 'dart:async';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/foundation.dart';

import '../api/backend_client.dart';
import 'transfer_estimate.dart';

/// One file to send: its name, size and a way to read its bytes. Picked,
/// dropped and shared files all become one of these.
class UploadSource {
  const UploadSource({required this.name, required this.size, required this.openRead, this.folder = '', this.modifiedAt});

  final String name;
  final int size;
  final Stream<List<int>> Function() openRead;

  /// Folder path below the target ("Photos/2026") for files dropped as part
  /// of a folder; created on the server as needed.
  final String folder;

  /// The local file's modification time, when the platform tells it; shown
  /// in the conflict dialog.
  final DateTime? modifiedAt;
}

enum UploadStatus { waiting, uploading, finalizing, done, skipped, failed, cancelled }

/// Encrypts what goes into a vault folder before it leaves the device: the
/// name and the content (docs/clients.md §4).
abstract class UploadCipher {
  Future<String> encryptName(String name);

  Future<Uint8List> encryptContent(Uint8List plaintext);
}

/// What to do when a file of that name already exists in the folder.
enum ConflictChoice { replace, keepBoth, skip }

class ConflictDecision {
  const ConflictDecision(this.choice, {this.applyToAll = false});

  final ConflictChoice choice;
  final bool applyToAll;
}

/// A name clash the user is asked about: the incoming file and where it goes.
class UploadConflict {
  const UploadConflict({required this.source, required this.folderId, required this.remaining});

  final UploadSource source;

  /// The folder that already holds a file of that name.
  final String folderId;

  /// Uploads still waiting after this one, for "apply to all N".
  final int remaining;
}

class UploadTask {
  UploadTask({required this.source, required this.parentId, this.cipher, this.resumeUploadId, this.hidden = false});

  final UploadSource source;
  final String parentId;

  /// Set for uploads into a vault.
  final UploadCipher? cipher;

  /// Sent with [UploadQueue.send]: not in the list, fails fast offline.
  final bool hidden;

  /// A server-side resumable upload left by an interrupted session; its
  /// parts that already arrived are skipped.
  String? resumeUploadId;

  /// The resumable upload in progress, if the file goes in parts.
  String? uploadId;

  /// Where the file actually lands: [parentId], or the folder made for
  /// [UploadSource.folder] inside it.
  String? targetId;
  UploadStatus status = UploadStatus.waiting;
  int sentBytes = 0;
  String? error;

  /// Held by the user; a running upload stops at its next part.
  bool paused = false;

  /// The connection dropped; the current part is retried until it is back.
  bool waitingForConnection = false;

  bool _cancelRequested = false;
  Completer<void>? _resumed;

  /// A small file's bytes, kept so a retry after a name clash needn't read
  /// the source again (a picked file's stream can only be read once).
  Uint8List? _bytes;

  double get progress {
    if (status == UploadStatus.done) return 1;
    final size = source.size;
    return size <= 0 ? 0 : (sentBytes / size).clamp(0, 1).toDouble();
  }

  bool get finished =>
      status == UploadStatus.done || status == UploadStatus.skipped || status == UploadStatus.failed || status == UploadStatus.cancelled;

  /// Pausing works between parts, so only for uploads that go in parts.
  bool get canPause => !finished && status != UploadStatus.finalizing && (status == UploadStatus.waiting || source.size > singleRequestLimit);
}

/// Files above this go through the resumable API in parts; smaller ones are
/// sent in one request. Stays well below Cloudflare's 100 MB request limit.
const int singleRequestLimit = 50 * 1024 * 1024;
const int _parallelUploads = 3;

/// Sends files to the server, a few at a time, and keeps the list the upload
/// panel shows. Asks [resolveConflict] when a name is taken, unless the
/// account's setting already says what to do.
class UploadQueue extends ChangeNotifier {
  UploadQueue({required this.client, required this.onFileUploaded, DateTime Function()? clock}) : _clock = clock ?? DateTime.now;

  final BackendClient client;
  final DateTime Function() _clock;

  /// Called with the folder id after each file lands, so open views refresh.
  final void Function(String parentId) onFileUploaded;

  /// Set by the UI: shows the "already exists" dialog.
  Future<ConflictDecision?> Function(UploadConflict conflict)? resolveConflict;

  /// "ask", "replace" or "rename", from the account settings.
  String defaultConflict = 'ask';

  final List<UploadTask> tasks = <UploadTask>[];
  final TransferEstimate estimate = TransferEstimate();
  ConflictChoice? _rememberedChoice;
  int _running = 0;

  /// Folders created for dropped folder trees, so each is made only once.
  final Map<String, Future<String>> _folders = <String, Future<String>>{};
  Future<void> _conflictPrompt = Future<void>.value();

  int get activeCount => tasks.where((task) => !task.finished).length;

  bool get hasTasks => tasks.isNotEmpty;

  /// Finished out of all files in the list ("12 of 40").
  int get finishedCount => tasks.where((task) => task.finished).length;

  int get failedCount => tasks.where((task) => task.status == UploadStatus.failed).length;

  bool get waitingForConnection => tasks.any((task) => task.waitingForConnection && !task.finished);

  bool get allPaused {
    final active = tasks.where((task) => !task.finished);
    return active.isNotEmpty && active.every((task) => task.paused);
  }

  /// Bytes of everything that is or will be sent, and how much of it is.
  int get totalBytes => _counted.fold(0, (sum, task) => sum + task.source.size);

  int get sentBytes => _counted.fold(0, (sum, task) => sum + (task.status == UploadStatus.done ? task.source.size : math.min(task.sentBytes, task.source.size)));

  Iterable<UploadTask> get _counted =>
      tasks.where((task) => task.status != UploadStatus.cancelled && task.status != UploadStatus.skipped && task.status != UploadStatus.failed);

  /// Queues [sources] for [parentId]; with a [cipher] they are encrypted
  /// first, for a vault folder. [resumeIds] maps a source to a resumable
  /// upload an earlier session left behind.
  void enqueue(String parentId, List<UploadSource> sources, {UploadCipher? cipher, Map<UploadSource, String> resumeIds = const {}}) {
    if (sources.isEmpty) return;
    if (activeCount == 0) {
      _rememberedChoice = null;
      _folders.clear();
      estimate.reset();
    }
    tasks.addAll(sources.map((source) => UploadTask(source: source, parentId: parentId, cipher: cipher, resumeUploadId: resumeIds[source])));
    notifyListeners();
    _pump();
  }

  /// Sends one file now, outside the visible list, and resolves when it is
  /// done or failed. Name clashes follow [defaultConflict] (camera uploads
  /// use "rename").
  Future<UploadTask> send(String parentId, UploadSource source) async {
    final task = UploadTask(source: source, parentId: parentId, hidden: true)..status = UploadStatus.uploading;
    await _run(task);
    return task;
  }

  void cancel(UploadTask task) {
    if (task.finished) return;
    task._cancelRequested = true;
    _wake(task);
    if (task.status == UploadStatus.waiting) {
      task.status = UploadStatus.cancelled;
      notifyListeners();
    }
  }

  void pause(UploadTask task) {
    if (!task.canPause || task.paused) return;
    task.paused = true;
    notifyListeners();
  }

  void resume(UploadTask task) {
    if (!task.paused) return;
    task.paused = false;
    _wake(task);
    notifyListeners();
    _pump();
  }

  void pauseAll() {
    for (final task in tasks) {
      if (task.canPause) task.paused = true;
    }
    notifyListeners();
  }

  void resumeAll() {
    for (final task in tasks) {
      task.paused = false;
      _wake(task);
    }
    notifyListeners();
    _pump();
  }

  void retry(UploadTask task) {
    if (task.status != UploadStatus.failed) return;
    task
      ..status = UploadStatus.waiting
      ..sentBytes = 0
      ..error = null
      .._cancelRequested = false;
    notifyListeners();
    _pump();
  }

  void clearFinished() {
    tasks.removeWhere((task) => task.finished);
    if (tasks.isEmpty) estimate.reset();
    notifyListeners();
  }

  @override
  void notifyListeners() {
    if (activeCount > 0) estimate.sample(sentBytes, totalBytes, _clock());
    super.notifyListeners();
  }

  void _wake(UploadTask task) {
    final resumed = task._resumed;
    task._resumed = null;
    if (resumed != null && !resumed.isCompleted) resumed.complete();
  }

  Future<void> _waitWhilePaused(UploadTask task) async {
    while (task.paused && !task._cancelRequested) {
      await (task._resumed ??= Completer<void>()).future;
    }
    if (task._cancelRequested) throw const _Cancelled();
  }

  void _pump() {
    while (_running < _parallelUploads) {
      final next = tasks.where((task) => task.status == UploadStatus.waiting && !task.paused).firstOrNull;
      if (next == null) return;
      _running += 1;
      next.status = UploadStatus.uploading;
      notifyListeners();
      unawaited(_run(next).whenComplete(() {
        _running -= 1;
        notifyListeners();
        _pump();
      }));
    }
  }

  String? _modeFor(ConflictChoice? choice) {
    switch (choice) {
      case ConflictChoice.replace:
        return 'replace';
      case ConflictChoice.keepBoth:
        return 'rename';
      case ConflictChoice.skip:
      case null:
        return null;
    }
  }

  String get _initialMode {
    final remembered = _modeFor(_rememberedChoice);
    if (remembered != null) return remembered;
    if (defaultConflict == 'replace' || defaultConflict == 'rename') return defaultConflict;
    return 'fail';
  }

  /// One dialog at a time, even with several uploads hitting a clash at once.
  Future<ConflictChoice> _askConflict(UploadTask task) {
    final completer = Completer<ConflictChoice>();
    _conflictPrompt = _conflictPrompt.then((_) async {
      final remembered = _rememberedChoice;
      if (remembered != null) {
        completer.complete(remembered);
        return;
      }
      final remaining = tasks.where((other) => !other.finished && other != task).length;
      final decision = await resolveConflict?.call(UploadConflict(source: task.source, folderId: task.targetId!, remaining: remaining));
      final choice = decision?.choice ?? ConflictChoice.skip;
      if (decision?.applyToAll ?? false) _rememberedChoice = choice;
      completer.complete(choice);
    });
    return completer.future;
  }

  Future<String> _targetFolder(UploadTask task) {
    final folder = task.source.folder;
    if (folder.isEmpty) return Future<String>.value(task.parentId);
    return _folders.putIfAbsent('${task.parentId}/$folder', () async {
      final cipher = task.cipher;
      var path = folder;
      if (cipher != null) {
        final segments = <String>[for (final segment in folder.split('/')) await cipher.encryptName(segment)];
        path = segments.join('/');
      }
      return (await client.ensureFolderPath(task.parentId, path)).id;
    });
  }

  Future<void> _run(UploadTask task) async {
    var mode = _initialMode;
    try {
      task.targetId ??= await _retrying(task, () => _targetFolder(task));
      while (true) {
        try {
          if (task.cipher != null) {
            await _sendEncrypted(task);
          } else if (task.source.size <= singleRequestLimit) {
            await _sendSmall(task, mode);
          } else {
            await _sendInParts(task, mode);
          }
          if (task._cancelRequested) {
            task.status = UploadStatus.cancelled;
            return;
          }
          task
            ..status = UploadStatus.done
            ..sentBytes = task.source.size
            ..uploadId = null
            .._bytes = null;
          onFileUploaded(task.targetId!);
          return;
        } on ApiException catch (error) {
          if (error.code != 'NAME_EXISTS' || mode != 'fail') rethrow;
          final choice = await _askConflict(task);
          final next = _modeFor(choice);
          if (next == null) {
            task.status = UploadStatus.skipped;
            return;
          }
          mode = next;
        }
      }
    } on _Cancelled {
      task.status = UploadStatus.cancelled;
      final uploadId = task.uploadId;
      if (uploadId != null) unawaited(client.cancelUpload(uploadId).catchError((Object _) {}));
    } catch (error) {
      task
        ..status = UploadStatus.failed
        ..error = error is ApiException && error.message.isNotEmpty && !error.message.startsWith('HTTP ') ? error.message : null;
    } finally {
      task.waitingForConnection = false;
    }
  }

  static bool _transient(ApiException error) =>
      error.code == 'NETWORK' || error.statusCode == 502 || error.statusCode == 503 || error.statusCode == 504;

  /// Runs [send]; a dropped connection marks the task "waiting for
  /// connection" and tries again with growing pauses until it is back.
  Future<T> _retrying<T>(UploadTask task, Future<T> Function() send) async {
    var delay = const Duration(seconds: 2);
    while (true) {
      await _waitWhilePaused(task);
      try {
        final result = await send();
        if (task.waitingForConnection) {
          task.waitingForConnection = false;
          notifyListeners();
        }
        return result;
      } on ApiException catch (error) {
        if (task.hidden || !_transient(error)) rethrow;
        task.waitingForConnection = true;
        notifyListeners();
        await Future<void>.delayed(delay);
        delay = Duration(seconds: math.min(delay.inSeconds * 2, 30));
      }
    }
  }

  Future<Uint8List> _readAll(UploadSource source) async {
    final builder = BytesBuilder(copy: false);
    await for (final chunk in source.openRead()) {
      builder.add(chunk);
    }
    return builder.takeBytes();
  }

  Future<void> _sendSmall(UploadTask task, String mode) async {
    final bytes = task._bytes ??= await _readAll(task.source);
    if (task._cancelRequested) throw const _Cancelled();
    await _retrying(task, () => client.uploadSmall(task.targetId!, task.source.name, bytes, conflict: mode));
  }

  /// Vault upload: the whole file is encrypted in memory, then sent in one
  /// request or, when large, through the resumable API. Encrypted names never
  /// clash, so there is no conflict to resolve.
  Future<void> _sendEncrypted(UploadTask task) async {
    final cipher = task.cipher!;
    final plaintext = await _readAll(task.source);
    if (task._cancelRequested) throw const _Cancelled();
    final name = await cipher.encryptName(task.source.name);
    final data = await cipher.encryptContent(plaintext);
    if (data.length <= singleRequestLimit) {
      await _retrying(task, () => client.uploadSmall(task.targetId!, name, data, conflict: 'rename'));
      return;
    }
    await _sendInParts(task, 'rename', name: name, size: data.length, read: () => Stream<List<int>>.value(data));
  }

  /// A resumable upload: the one an earlier session left (its arrived parts
  /// are skipped) or a new one.
  Future<(Map<String, dynamic>, Set<int>)> _session(UploadTask task, String mode, String name, int size) async {
    final resumeId = task.resumeUploadId;
    task.resumeUploadId = null;
    if (resumeId != null) {
      try {
        final status = await client.uploadStatus(resumeId);
        final received = <int>{
          for (final part in (status['receivedParts'] as List?) ?? const <Object?>[])
            if (part is num) part.toInt(),
        };
        return (<String, dynamic>{...status, 'id': resumeId}, received);
      } catch (_) {
        // Expired or gone: start over.
      }
    }
    return (await _retrying(task, () => client.createUpload(task.targetId!, name, size, conflict: mode)), <int>{});
  }

  /// Reads the file once, cutting it into the server's part size, and sends
  /// each part as soon as it is complete. Pausing holds the read between
  /// parts; a dropped connection retries the part.
  Future<void> _sendInParts(UploadTask task, String mode, {String? name, int? size, Stream<List<int>> Function()? read}) async {
    final (session, received) = await _session(task, mode, name ?? task.source.name, size ?? task.source.size);
    final uploadId = session['id'].toString();
    task.uploadId = uploadId;
    final partSize = (session['partSize'] as num).toInt();
    var index = 0;
    var buffer = BytesBuilder(copy: false);
    Future<void> flush() async {
      final bytes = buffer.takeBytes();
      if (!received.contains(index)) {
        await _retrying(task, () => client.uploadPart(uploadId, index, bytes));
      }
      task.sentBytes += bytes.length;
      notifyListeners();
      index += 1;
      buffer = BytesBuilder(copy: false);
    }

    try {
      await for (final chunk in (read ?? task.source.openRead)()) {
        var offset = 0;
        while (offset < chunk.length) {
          final take = math.min(partSize - buffer.length, chunk.length - offset);
          buffer.add(chunk.sublist(offset, offset + take));
          offset += take;
          if (buffer.length == partSize) await flush();
        }
      }
      if (buffer.length > 0 || index == 0) await flush();
      task.status = UploadStatus.finalizing;
      notifyListeners();
      await _retrying(task, () => client.completeUpload(uploadId));
    } on _Cancelled {
      rethrow;
    } catch (error) {
      unawaited(client.cancelUpload(uploadId).catchError((Object _) {}));
      task.uploadId = null;
      rethrow;
    }
  }
}

class _Cancelled implements Exception {
  const _Cancelled();
}
