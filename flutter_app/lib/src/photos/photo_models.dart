import 'package:flutter/foundation.dart';

import '../api/models.dart';

/// The timeline's first section: photos whose file the server hasn't read
/// yet, so their date is still a guess. Never filed under a day meanwhile.
const String photoSortingKey = 'sorting';

/// The timeline's last section: photos with no date of their own (only
/// when they were uploaded is known).
const String photoUnknownKey = 'unknown';

/// Whether [key] is one of the two sections that aren't a date.
bool isPhotoSpecialKey(String key) => key == photoSortingKey || key == photoUnknownKey;

/// One photo or video of the timeline (`GET /api/photos`).
@immutable
class PhotoItem {
  const PhotoItem({
    required this.id,
    required this.takenAt,
    required this.kind,
    required this.name,
    required this.etag,
    this.width,
    this.height,
    this.durationSeconds,
    this.parentId,
    this.mime,
    this.size = 0,
    this.favorite = false,
    this.dateSource = 'exif',
    this.provisional = false,
    this.dateUnknown = false,
  });

  factory PhotoItem.fromJson(Map<String, dynamic> json) => PhotoItem(
    id: '${json['id']}',
    takenAt: '${json['takenAt']}',
    kind: json['kind'] == 'video' ? PhotoKind.video : PhotoKind.photo,
    name: '${json['name'] ?? ''}',
    etag: '${json['etag'] ?? ''}',
    width: (json['width'] as num?)?.toInt(),
    height: (json['height'] as num?)?.toInt(),
    durationSeconds: (json['durationSeconds'] as num?)?.toDouble(),
    parentId: json['parentId'] as String?,
    mime: json['mime'] as String?,
    size: (json['size'] as num?)?.toInt() ?? 0,
    favorite: json['favorite'] == true,
    dateSource: '${json['dateSource'] ?? 'exif'}',
    provisional: json['provisional'] == true,
    dateUnknown: json['dateUnknown'] == true,
  );

  final String id;

  /// The wall-clock time it was taken, `YYYY-MM-DDTHH:MM:SS`.
  final String takenAt;
  final PhotoKind kind;
  final String name;
  final String etag;
  final int? width;
  final int? height;
  final double? durationSeconds;
  final String? parentId;
  final String? mime;
  final int size;
  final bool favorite;

  /// Where [takenAt] came from: exif, client, filename, mtime or upload.
  final String dateSource;

  /// The file wasn't read yet: [takenAt] may still change.
  final bool provisional;

  /// Only the upload time is known.
  final bool dateUnknown;

  bool get isVideo => kind == PhotoKind.video;

  /// `YYYY-MM-DD`: the day the photo belongs to.
  String get day => takenAt.length >= 10 ? takenAt.substring(0, 10) : takenAt;

  DateTime? get takenDate => DateTime.tryParse(takenAt);

  /// The timeline section it belongs to: its [day], or [photoSortingKey] /
  /// [photoUnknownKey] while its date is a guess or unknown.
  String get section => provisional ? photoSortingKey : (dateUnknown ? photoUnknownKey : day);

  PhotoItem copyWith({bool? favorite}) => PhotoItem(
    id: id,
    takenAt: takenAt,
    kind: kind,
    name: name,
    etag: etag,
    width: width,
    height: height,
    durationSeconds: durationSeconds,
    parentId: parentId,
    mime: mime,
    size: size,
    favorite: favorite ?? this.favorite,
    dateSource: dateSource,
    provisional: provisional,
    dateUnknown: dateUnknown,
  );

  /// The file behind it, for the file actions (move, share, download …).
  FileNode toFileNode() => FileNode(
    id: id,
    parentId: parentId,
    type: 'file',
    name: name,
    size: size,
    mime: mime,
    etag: etag,
    version: 0,
    modifiedAt: takenDate,
    favorite: favorite,
  );
}

enum PhotoKind { photo, video }

/// How many photos a day holds (`GET /api/photos/summary`).
@immutable
class PhotoDayCount {
  const PhotoDayCount(this.date, this.count);

  /// `YYYY-MM-DD`, or [photoSortingKey] / [photoUnknownKey].
  final String date;
  final int count;
}

@immutable
class PhotoSummary {
  const PhotoSummary({required this.days, this.pending = 0, this.unknown = 0});

  /// The server's days, with [photoSortingKey] in front ([pending] photos)
  /// and [photoUnknownKey] at the end ([unknown]): the sections in the
  /// order `GET /api/photos` lists them, so the counts and the grid agree.
  factory PhotoSummary.fromJson(Map<String, dynamic> json) {
    final pending = (json['pending'] as num?)?.toInt() ?? 0;
    final unknown = (json['unknown'] as num?)?.toInt() ?? 0;
    return PhotoSummary(
      days: <PhotoDayCount>[
        if (pending > 0) PhotoDayCount(photoSortingKey, pending),
        for (final raw in (json['days'] as List?) ?? const <Object?>[])
          if (raw is Map) PhotoDayCount('${raw['date']}', (raw['count'] as num?)?.toInt() ?? 0),
        if (unknown > 0) PhotoDayCount(photoUnknownKey, unknown),
      ],
      pending: pending,
      unknown: unknown,
    );
  }

  static const PhotoSummary empty = PhotoSummary(days: <PhotoDayCount>[]);

  /// The sections, top to bottom: sorting, the days newest first, unknown.
  final List<PhotoDayCount> days;

  /// Photos whose date is still a guess (not read yet).
  final int pending;

  /// Photos with no date of their own.
  final int unknown;

  int get total => days.fold(0, (sum, day) => sum + day.count);
}

@immutable
class PhotoPage {
  const PhotoPage({required this.items, this.nextCursor});

  factory PhotoPage.fromJson(Map<String, dynamic> json) => PhotoPage(
    items: <PhotoItem>[
      for (final raw in (json['items'] as List?) ?? const <Object?>[])
        if (raw is Map) PhotoItem.fromJson(raw.cast<String, dynamic>()),
    ],
    nextCursor: json['nextCursor'] as String?,
  );

  final List<PhotoItem> items;
  final String? nextCursor;
}
