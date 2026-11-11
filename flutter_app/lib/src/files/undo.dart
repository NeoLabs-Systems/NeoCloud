import 'dart:async';

import 'package:flutter/foundation.dart';

/// What the undo bar shows: a short message and, for reversible actions,
/// what Undo does.
class UndoEntry {
  UndoEntry({required this.message, this.onUndo});

  final String message;
  final Future<void> Function()? onUndo;
}

/// The one floating "3 items moved to trash · Undo" bar. A new entry replaces
/// the old one; it hides after [duration], and hovering it pauses the clock.
class UndoCenter extends ChangeNotifier {
  UndoCenter({this.duration = const Duration(seconds: 10), DateTime Function()? clock}) : _clock = clock ?? DateTime.now;

  final Duration duration;
  final DateTime Function() _clock;

  UndoEntry? _current;
  Timer? _timer;
  Duration _remaining = Duration.zero;
  DateTime? _startedAt;

  UndoEntry? get current => _current;

  bool get isPaused => _current != null && _timer == null;

  void show(UndoEntry entry) {
    _current = entry;
    _remaining = duration;
    _start();
    notifyListeners();
  }

  /// Stops the clock while the pointer is over the bar.
  void pause() {
    final timer = _timer;
    if (_current == null || timer == null) return;
    timer.cancel();
    _timer = null;
    _remaining -= _clock().difference(_startedAt!);
    if (_remaining < const Duration(seconds: 2)) _remaining = const Duration(seconds: 2);
  }

  void resume() {
    if (_current == null || _timer != null) return;
    _start();
  }

  /// Runs the entry's undo and hides the bar.
  Future<void> undo() async {
    final entry = _current;
    dismiss();
    await entry?.onUndo?.call();
  }

  void dismiss() {
    _timer?.cancel();
    _timer = null;
    if (_current == null) return;
    _current = null;
    notifyListeners();
  }

  void _start() {
    _timer?.cancel();
    _startedAt = _clock();
    _timer = Timer(_remaining, dismiss);
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }
}
