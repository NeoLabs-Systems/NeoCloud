import 'package:flutter/foundation.dart';

import '../api/models.dart';

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

  bool get isVideo => kind == PhotoKind.video;

  /// `YYYY-MM-DD`: the day the photo belongs to.
  String get day => takenAt.length >= 10 ? takenAt.substring(0, 10) : takenAt;

  DateTime? get takenDate => DateTime.tryParse(takenAt);

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

  /// `YYYY-MM-DD`.
  final String date;
  final int count;
}

@immutable
class PhotoSummary {
  const PhotoSummary({required this.days, this.pending = 0});

  factory PhotoSummary.fromJson(Map<String, dynamic> json) => PhotoSummary(
    days: <PhotoDayCount>[
      for (final raw in (json['days'] as List?) ?? const <Object?>[])
        if (raw is Map) PhotoDayCount('${raw['date']}', (raw['count'] as num?)?.toInt() ?? 0),
    ],
    pending: (json['pending'] as num?)?.toInt() ?? 0,
  );

  static const PhotoSummary empty = PhotoSummary(days: <PhotoDayCount>[]);

  /// Newest first.
  final List<PhotoDayCount> days;

  /// Photos whose date is still a guess (not read yet).
  final int pending;

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
