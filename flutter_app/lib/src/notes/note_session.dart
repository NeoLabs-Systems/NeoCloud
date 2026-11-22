import 'dart:async';

import 'package:flutter/foundation.dart';

import '../api/backend_client.dart';
import 'note_models.dart';
import 'note_store.dart';

enum NoteSaveState { saved, unsaved, saving, failed, conflict }

/// One open note: autosave a moment after typing stops, one save at a
/// time, each sent with the revision it was based on so a change made
/// elsewhere is never overwritten without asking.
class NoteSession extends ChangeNotifier {
  NoteSession(this.store, Note note, {this.delay = const Duration(milliseconds: 900)}) : _note = note;

  /// The server, or the app's vault crypto for an encrypted notebook.
  final NoteStore store;
  final Duration delay;

  Note _note;
  Note get note => _note;

  NoteSaveState _state = NoteSaveState.saved;
  NoteSaveState get state => _state;

  /// The last save's error, for the status line.
  Object? error;

  String? _pendingBody;
  String? _pendingTitle;
  Timer? _timer;
  Future<void> _queue = Future<void>.value();
  bool _disposed = false;

  bool get hasPending => _pendingBody != null || _pendingTitle != null;

  /// The text as typed, saved after [delay].
  void edit({String? body, String? title}) {
    if (body != null && body != _note.body) _pendingBody = body;
    if (title != null && title.trim().isNotEmpty && title.trim() != _note.title) _pendingTitle = title.trim();
    if (!hasPending) return;
    _setState(NoteSaveState.unsaved);
    _timer?.cancel();
    _timer = Timer(delay, () => unawaited(flush()));
  }

  /// Saves what is waiting now (before closing the note).
  Future<void> flush() {
    _timer?.cancel();
    return _enqueue(() async {
      final body = _pendingBody;
      final title = _pendingTitle;
      if (body == null && title == null) return;
      _pendingBody = null;
      _pendingTitle = null;
      await _send(() => store.update(_note.id, NoteChange(body: body, title: title), revision: _note.revision), restore: () {
        _pendingBody ??= body;
        _pendingTitle ??= title;
      });
    });
  }

  /// Pin, archive, color, tags or notebook: saved right away, after any typing.
  Future<void> setProperties({bool? pinned, bool? archived, NoteColor? color, List<String>? tags, String? notebook}) async {
    await flush();
    await _enqueue(() => _send(() => store.update(
      _note.id,
      NoteChange(pinned: pinned, archived: archived, color: color, tags: tags, notebook: notebook),
      revision: _note.revision,
    )));
  }

  /// After a conflict: saves this text over the newer version (which stays
  /// in the version history).
  Future<void> overwrite() => _enqueue(() async {
    final latest = await store.load(_note.id);
    _note = _note.withRevision(latest.revision);
    _setState(NoteSaveState.unsaved);
  }).then((_) => flush());

  /// After a conflict: drops this text and takes the newer version.
  Future<void> reload() => _enqueue(() async {
    _pendingBody = null;
    _pendingTitle = null;
    _note = await store.load(_note.id);
    _setState(NoteSaveState.saved);
  });

  Future<void> _send(Future<Note> Function() call, {VoidCallback? restore}) async {
    _setState(NoteSaveState.saving);
    try {
      final saved = await call();
      // Typing that arrived while saving keeps its own text.
      _note = saved;
      error = null;
      _setState(hasPending ? NoteSaveState.unsaved : NoteSaveState.saved);
    } on ApiException catch (failure) {
      restore?.call();
      error = failure;
      _setState(failure.code == 'CONFLICT' ? NoteSaveState.conflict : NoteSaveState.failed);
    } catch (failure) {
      restore?.call();
      error = failure;
      _setState(NoteSaveState.failed);
    }
  }

  Future<void> _enqueue(Future<void> Function() step) {
    final next = _queue.then((_) => step());
    _queue = next.catchError((Object _) {});
    return next;
  }

  void _setState(NoteSaveState value) {
    _state = value;
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    super.dispose();
  }
}
