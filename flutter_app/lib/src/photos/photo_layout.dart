import 'dart:math' as math;

import 'package:flutter/foundation.dart';

import 'photo_models.dart';

/// How dense the grid is; pinching steps between them.
enum PhotoZoom {
  /// Big tiles, a header per day.
  day,

  /// Small tiles, a header per month.
  month,

  /// Tiny tiles, a header per year.
  year,
}

extension PhotoZoomSteps on PhotoZoom {
  /// One step denser (pinching in), or null at the densest.
  PhotoZoom? get denser => switch (this) {
    PhotoZoom.day => PhotoZoom.month,
    PhotoZoom.month => PhotoZoom.year,
    PhotoZoom.year => null,
  };

  /// One step bigger (spreading out), or null at the biggest.
  PhotoZoom? get bigger => switch (this) {
    PhotoZoom.day => null,
    PhotoZoom.month => PhotoZoom.day,
    PhotoZoom.year => PhotoZoom.month,
  };

  /// The section a day belongs to at this zoom: the day, its month or year.
  String sectionKey(String day) => isPhotoSpecialKey(day) ? day : switch (this) {
    PhotoZoom.day => day,
    PhotoZoom.month => day.length >= 7 ? day.substring(0, 7) : day,
    PhotoZoom.year => day.length >= 4 ? day.substring(0, 4) : day,
  };
}

/// Columns for [zoom] at [width] logical pixels: 4 / 5 / 8 on a phone of
/// about 400 points, more on wider screens.
int photoColumns(PhotoZoom zoom, double width) {
  final (minimum, tile) = switch (zoom) {
    PhotoZoom.day => (3, 100.0),
    PhotoZoom.month => (5, 72.0),
    PhotoZoom.year => (7, 46.0),
  };
  if (!width.isFinite || width <= 0) return minimum;
  return math.max(minimum, (width / tile).floor());
}

/// A run of photos under one header: a day, a month or a year.
@immutable
class PhotoSection {
  const PhotoSection({required this.key, required this.firstItem, required this.count, required this.headerRow});

  /// `YYYY-MM-DD`, `YYYY-MM` or `YYYY`.
  final String key;

  /// Index of its first photo in the whole timeline.
  final int firstItem;
  final int count;

  /// Index of its header in [PhotoGridLayout.rows].
  final int headerRow;
}

/// One line of the grid: a section header, or up to `columns` tiles.
@immutable
class PhotoRow {
  const PhotoRow({required this.section, required this.offset, required this.extent, this.firstItem = 0, this.itemCount = 0});

  final int section;
  final double offset;
  final double extent;

  /// For a tile row: the first photo's index and how many it shows. A
  /// header has none.
  final int firstItem;
  final int itemCount;

  bool get isHeader => itemCount == 0;
}

/// Where every header and tile sits, worked out from the day counts alone:
/// the grid knows its full height and each photo's place before a single
/// photo is loaded, so scrolling never jumps and the scrubber can go
/// anywhere at once.
class PhotoGridLayout {
  PhotoGridLayout._({
    required this.zoom,
    required this.columns,
    required this.tileExtent,
    required this.spacing,
    required this.headerExtent,
    required this.sections,
    required this.rows,
    required this.totalItems,
  });

  factory PhotoGridLayout.build(
    List<PhotoDayCount> days, {
    required PhotoZoom zoom,
    required double width,
    double spacing = 2,
    double? headerExtent,
  }) {
    final columns = photoColumns(zoom, width);
    final tile = width.isFinite && width > 0 ? (width - spacing * (columns - 1)) / columns : 100.0;
    final header = headerExtent ?? (zoom == PhotoZoom.day ? 48.0 : 56.0);
    final sections = <PhotoSection>[];
    final rows = <PhotoRow>[];
    var offset = 0.0;
    var item = 0;
    var index = 0;
    while (index < days.length) {
      final key = zoom.sectionKey(days[index].date);
      var count = 0;
      while (index < days.length && zoom.sectionKey(days[index].date) == key) {
        count += days[index].count;
        index += 1;
      }
      if (count == 0) continue;
      final section = sections.length;
      sections.add(PhotoSection(key: key, firstItem: item, count: count, headerRow: rows.length));
      rows.add(PhotoRow(section: section, offset: offset, extent: header));
      offset += header;
      for (var start = 0; start < count; start += columns) {
        rows.add(PhotoRow(
          section: section,
          offset: offset,
          extent: tile + spacing,
          firstItem: item + start,
          itemCount: math.min(columns, count - start),
        ));
        offset += tile + spacing;
      }
      item += count;
    }
    return PhotoGridLayout._(
      zoom: zoom,
      columns: columns,
      tileExtent: tile,
      spacing: spacing,
      headerExtent: header,
      sections: List<PhotoSection>.unmodifiable(sections),
      rows: List<PhotoRow>.unmodifiable(rows),
      totalItems: item,
    );
  }

  final PhotoZoom zoom;
  final int columns;
  final double tileExtent;
  final double spacing;
  final double headerExtent;
  final List<PhotoSection> sections;
  final List<PhotoRow> rows;
  final int totalItems;

  double get totalExtent => rows.isEmpty ? 0 : rows.last.offset + rows.last.extent;

  /// The row at scroll [offset] (clamped to the grid).
  int rowAtOffset(double offset) {
    if (rows.isEmpty) return 0;
    var low = 0;
    var high = rows.length - 1;
    while (low < high) {
      final middle = (low + high + 1) >> 1;
      if (rows[middle].offset <= offset) {
        low = middle;
      } else {
        high = middle - 1;
      }
    }
    return low;
  }

  /// The section holding photo [item].
  int sectionOfItem(int item) {
    if (sections.isEmpty) return 0;
    var low = 0;
    var high = sections.length - 1;
    while (low < high) {
      final middle = (low + high + 1) >> 1;
      if (sections[middle].firstItem <= item) {
        low = middle;
      } else {
        high = middle - 1;
      }
    }
    return low;
  }

  /// The tile row showing photo [item].
  int rowOfItem(int item) {
    if (sections.isEmpty) return 0;
    final section = sections[sectionOfItem(item)];
    return section.headerRow + 1 + (item - section.firstItem) ~/ columns;
  }

  /// The first photo shown at or below scroll [offset].
  int itemAtOffset(double offset) {
    if (rows.isEmpty) return 0;
    var row = rowAtOffset(offset);
    if (rows[row].isHeader && row + 1 < rows.length) row += 1;
    return rows[row].firstItem;
  }

  /// Photo at [position] (relative to the grid's top, scroll offset
  /// included), or null over a header or a gap.
  int? itemAt(double dx, double dy) {
    if (rows.isEmpty || dy < 0 || dy >= totalExtent) return null;
    final row = rows[rowAtOffset(dy)];
    if (row.isHeader) return null;
    final column = (dx / (tileExtent + spacing)).floor().clamp(0, columns - 1);
    return column < row.itemCount ? row.firstItem + column : null;
  }

  /// The first row of each year, newest first: the scrubber's marks.
  List<(String year, double offset)> yearMarks() {
    final marks = <(String, double)>[];
    for (final section in sections) {
      final year = section.key.substring(0, math.min(4, section.key.length));
      if (marks.isEmpty || marks.last.$1 != year) marks.add((year, rows[section.headerRow].offset));
    }
    return marks;
  }
}
