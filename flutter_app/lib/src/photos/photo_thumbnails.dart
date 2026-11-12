import 'dart:async';
import 'dart:collection';
import 'dart:typed_data';

import 'thumbnail_disk_stub.dart' if (dart.library.io) 'thumbnail_disk_io.dart' as disk;

/// Fetches one grid thumbnail; null when the server makes none.
typedef ThumbnailFetch = Future<Uint8List?> Function(String id, String etag);

/// Where thumbnails outlive the app: a cache folder on phones; the web has
/// the browser's HTTP cache (the URLs are cached for good by etag).
abstract class ThumbnailStore {
  Future<Uint8List?> read(String key);

  Future<void> write(String key, Uint8List bytes);
}

ThumbnailStore? createThumbnailStore(String account) => disk.createThumbnailStore(account);

/// The photo grid's thumbnails: a memory cache (by bytes), the store below
/// it, and at most [maxParallel] downloads at a time. What is on screen
/// goes first, newest request first, so a fling ends with the photos where
/// it stopped; prefetches ahead of the scroll wait behind them, and a tile
/// that scrolled away before its turn is dropped.
class PhotoThumbnailCache {
  PhotoThumbnailCache({required this.fetch, this.store, this.maxBytes = 48 * 1024 * 1024, this.maxParallel = 6});

  final ThumbnailFetch fetch;
  final ThumbnailStore? store;
  final int maxBytes;
  final int maxParallel;

  final LinkedHashMap<String, Uint8List> _memory = LinkedHashMap<String, Uint8List>();
  int _bytes = 0;
  final Map<String, _Job> _jobs = <String, _Job>{};
  final List<_Job> _visible = <_Job>[];
  final Queue<_Job> _ahead = Queue<_Job>();
  int _running = 0;

  /// Files with no thumbnail (videos, unreadable images): not asked again.
  final Set<String> _none = <String>{};

  static String keyOf(String id, String etag) => '$id-$etag';

  /// The thumbnail if it is in memory: tiles show it in their first frame.
  Uint8List? peek(String id, String etag) {
    final key = keyOf(id, etag);
    final bytes = _memory.remove(key);
    if (bytes != null) _memory[key] = bytes;
    return bytes;
  }

  bool hasNone(String id, String etag) => _none.contains(keyOf(id, etag));

  /// The thumbnail for a tile on screen. Call [release] when the tile goes.
  Future<Uint8List?> load(String id, String etag) => _request(id, etag, visible: true);

  /// Loads a thumbnail just ahead of the scroll, behind what is on screen.
  void prefetch(String id, String etag) {
    final key = keyOf(id, etag);
    if (_memory.containsKey(key) || _none.contains(key) || _jobs.containsKey(key)) return;
    unawaited(_request(id, etag, visible: false));
  }

  /// A tile left the screen: its download is dropped if it hasn't started.
  void release(String id, String etag) {
    final job = _jobs[keyOf(id, etag)];
    if (job == null || job.started) return;
    job.wanted -= 1;
    if (job.wanted > 0 || !job.visible) return;
    _visible.remove(job);
    _jobs.remove(job.key);
    job.completer.complete(null);
  }

  Future<Uint8List?> _request(String id, String etag, {required bool visible}) {
    final key = keyOf(id, etag);
    final cached = peek(id, etag);
    if (cached != null) return Future<Uint8List?>.value(cached);
    if (_none.contains(key)) return Future<Uint8List?>.value();
    final existing = _jobs[key];
    if (existing != null) {
      if (visible) {
        existing.wanted += 1;
        if (!existing.visible && !existing.started) {
          _ahead.remove(existing);
          existing.visible = true;
          _visible.add(existing);
        }
      }
      return existing.completer.future;
    }
    final job = _Job(key, id, etag, visible: visible);
    _jobs[key] = job;
    if (visible) {
      _visible.add(job);
    } else {
      _ahead.add(job);
    }
    _pump();
    return job.completer.future;
  }

  void _pump() {
    while (_running < maxParallel && (_visible.isNotEmpty || _ahead.isNotEmpty)) {
      final job = _visible.isNotEmpty ? _visible.removeLast() : _ahead.removeFirst();
      job.started = true;
      _running += 1;
      unawaited(_run(job));
    }
  }

  Future<void> _run(_Job job) async {
    Uint8List? bytes;
    try {
      bytes = await store?.read(job.key);
      if (bytes == null) {
        bytes = await fetch(job.id, job.etag);
        if (bytes == null) {
          _none.add(job.key);
        } else {
          unawaited(store?.write(job.key, bytes));
        }
      }
      if (bytes != null) _remember(job.key, bytes);
    } catch (_) {
      bytes = null;
    } finally {
      _running -= 1;
      _jobs.remove(job.key);
      job.completer.complete(bytes);
      _pump();
    }
  }

  void _remember(String key, Uint8List bytes) {
    final old = _memory.remove(key);
    if (old != null) _bytes -= old.length;
    _memory[key] = bytes;
    _bytes += bytes.length;
    while (_bytes > maxBytes && _memory.isNotEmpty) {
      final oldest = _memory.keys.first;
      _bytes -= _memory.remove(oldest)!.length;
    }
  }

  /// Forgets everything in memory (another account signed in).
  void clear() {
    _memory.clear();
    _bytes = 0;
    _none.clear();
  }
}

class _Job {
  _Job(this.key, this.id, this.etag, {required this.visible}) : wanted = visible ? 1 : 0;

  final String key;
  final String id;
  final String etag;
  bool visible;
  bool started = false;
  int wanted;
  final Completer<Uint8List?> completer = Completer<Uint8List?>();
}
