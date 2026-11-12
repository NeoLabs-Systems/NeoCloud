import 'dart:async';

import 'package:flutter/foundation.dart';

import 'photo_layout.dart';
import 'photo_models.dart';

/// Where the timeline's data comes from: the server, or a fake in tests.
abstract class PhotoSource {
  Future<PhotoSummary> summary();

  Future<PhotoPage> page({String? cursor, String? day, int limit});
}

/// The Photos timeline as the grid needs it: the day counts up front, then
/// the photos themselves loaded in pages around wherever the grid is
/// looking (a fling or a jump with the scrubber loads that place, not
/// everything above it). Lives as long as the app, so leaving the tab and
/// coming back keeps both the photos and the scroll position.
class PhotoTimeline extends ChangeNotifier {
  PhotoTimeline(this.source, {this.pageSize = 200});

  final PhotoSource source;
  final int pageSize;

  PhotoSummary _summary = PhotoSummary.empty;
  PhotoSummary get summary => _summary;

  List<PhotoItem?> _items = const <PhotoItem?>[];
  List<int> _dayStarts = const <int>[];

  /// Where a page that ended in the middle of a day continues.
  final Map<int, String> _continuations = <int, String>{};
  final Set<int> _loading = <int>{};

  /// Bumped on every reload; answers for an older one are dropped.
  int _generation = 0;

  bool _loaded = false;
  bool _refreshing = false;
  Object? _error;
  String? _owner;

  /// The first summary arrived (the grid can be laid out).
  bool get loaded => _loaded;
  bool get refreshing => _refreshing;
  Object? get error => _error;
  int get total => _items.length;

  /// Where the grid was left: kept across tab switches.
  double savedOffset = 0;
  PhotoZoom zoom = PhotoZoom.day;

  /// Starts over for another account (or server); the same one keeps all.
  void attach(String owner) {
    if (_owner == owner) return;
    _owner = owner;
    _generation += 1;
    _summary = PhotoSummary.empty;
    _items = const <PhotoItem?>[];
    _dayStarts = const <int>[];
    _continuations.clear();
    _loading.clear();
    _loaded = false;
    _error = null;
    savedOffset = 0;
    zoom = PhotoZoom.day;
    notifyListeners();
  }

  PhotoItem? itemAt(int index) => index >= 0 && index < _items.length ? _items[index] : null;

  /// The index of the photo [id] among those loaded, or -1.
  int indexOf(String id) => _items.indexWhere((item) => item?.id == id);

  /// Loads the day counts again. The loaded photos stay when nothing moved
  /// (same days, same counts), unless [reloadItems]: pull to refresh.
  Future<void> refresh({bool reloadItems = false}) async {
    if (_refreshing) return;
    _refreshing = true;
    notifyListeners();
    try {
      final next = await source.summary();
      final same = _loaded && listEquals(_dayKeys(next), _dayKeys(_summary));
      _summary = next;
      _error = null;
      if (!same || reloadItems) _resetItems();
      _loaded = true;
    } catch (error) {
      _error = error;
    } finally {
      _refreshing = false;
      notifyListeners();
    }
  }

  static List<String> _dayKeys(PhotoSummary summary) => <String>[for (final day in summary.days) '${day.date}:${day.count}'];

  void _resetItems() {
    _generation += 1;
    final starts = <int>[];
    var total = 0;
    for (final day in _summary.days) {
      starts.add(total);
      total += day.count;
    }
    _dayStarts = starts;
    _items = List<PhotoItem?>.filled(total, null);
    _continuations.clear();
    _loading.clear();
  }

  int _dayOf(int index) {
    var low = 0;
    var high = _dayStarts.length - 1;
    while (low < high) {
      final middle = (low + high + 1) >> 1;
      if (_dayStarts[middle] <= index) {
        low = middle;
      } else {
        high = middle - 1;
      }
    }
    return low;
  }

  /// Makes sure photo [index] is loaded or on its way.
  void ensureLoaded(int index) {
    if (index < 0 || index >= _items.length || _items[index] != null) return;
    final day = _dayOf(index);
    final dayStart = _dayStarts[day];
    // Continue the page before it when it stopped inside this day.
    var start = index;
    while (start > dayStart && _items[start - 1] == null) {
      start -= 1;
    }
    final cursor = start > dayStart ? _continuations[start] : null;
    if (cursor == null) start = dayStart;
    if (!_loading.add(start)) return;
    unawaited(_fetch(start, cursor == null ? _summary.days[day].date : null, cursor, index));
  }

  Future<void> _fetch(int start, String? day, String? cursor, int wanted) async {
    final generation = _generation;
    var position = start;
    var nextCursor = cursor;
    var nextDay = day;
    try {
      do {
        final page = await source.page(cursor: nextCursor, day: nextDay, limit: pageSize);
        if (generation != _generation) return;
        nextDay = null;
        nextCursor = page.nextCursor;
        for (final item in page.items) {
          if (position >= _items.length) break;
          // The counts moved since the summary (an upload, a delete): the
          // places are off, so start over from fresh counts.
          if (item.day != _summary.days[_dayOf(position)].date) {
            unawaited(refresh(reloadItems: true));
            return;
          }
          _items[position] = item;
          position += 1;
        }
        if (nextCursor != null && position < _items.length) _continuations[position] = nextCursor;
        notifyListeners();
      } while (position <= wanted && nextCursor != null && position < _items.length);
    } catch (error) {
      if (generation == _generation) {
        _error = error;
        notifyListeners();
      }
    } finally {
      if (generation == _generation) _loading.remove(start);
    }
  }

  /// Marks or unmarks photos as favorites in place.
  void setFavorite(Set<String> ids, bool favorite) {
    var changed = false;
    for (var index = 0; index < _items.length; index += 1) {
      final item = _items[index];
      if (item != null && ids.contains(item.id)) {
        _items[index] = item.copyWith(favorite: favorite);
        changed = true;
      }
    }
    if (changed) notifyListeners();
  }
}
