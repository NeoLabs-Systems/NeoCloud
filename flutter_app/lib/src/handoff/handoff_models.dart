import 'package:flutter/foundation.dart';

/// A device of the account that can be sent to (`GET /api/handoff/devices`).
@immutable
class HandoffDevice {
  const HandoffDevice({
    required this.id,
    required this.name,
    required this.platform,
    required this.kind,
    required this.canReceive,
    required this.online,
    required this.current,
  });

  factory HandoffDevice.fromJson(Map<String, dynamic> json) => HandoffDevice(
    id: '${json['id']}',
    name: '${json['name'] ?? ''}',
    platform: '${json['platform'] ?? ''}',
    kind: '${json['kind'] ?? 'app'}',
    canReceive: json['canReceive'] != false,
    online: json['online'] == true,
    current: json['current'] == true,
  );

  final String id;
  final String name;

  /// ios, android, windows, macos, linux or empty (a browser of unknown system).
  final String platform;

  /// phone, desktop, browser or app.
  final String kind;
  final bool canReceive;
  final bool online;

  /// This device itself: never a target.
  final bool current;

  /// Devices that can be chosen right now: online, accepting, not this one.
  bool get isTarget => online && canReceive && !current;
}

@immutable
class HandoffFile {
  const HandoffFile({required this.id, required this.name, required this.size});

  factory HandoffFile.fromJson(Map<String, dynamic> json) => HandoffFile(
    id: (json['id'] as num).toInt(),
    name: '${json['name']}',
    size: (json['size'] as num).toInt(),
  );

  final int id;
  final String name;
  final int size;
}

/// What a send is up to, from the viewer's side.
enum HandoffStatus {
  uploading,
  pending,
  accepted,
  delivered,
  declined,
  cancelled,
  expired,
}

HandoffStatus handoffStatusFrom(Object? value) {
  for (final status in HandoffStatus.values) {
    if (status.name == value) return status;
  }
  return HandoffStatus.expired;
}

/// A send (`role` says whether this device sent it or receives it).
@immutable
class HandoffOffer {
  const HandoffOffer({
    required this.id,
    required this.receiving,
    required this.status,
    required this.fromName,
    required this.fromPlatform,
    required this.toName,
    required this.files,
    required this.totalSize,
  });

  factory HandoffOffer.fromJson(Map<String, dynamic> json) {
    final from =
        (json['from'] as Map?)?.cast<String, dynamic>() ??
        const <String, dynamic>{};
    final to =
        (json['to'] as Map?)?.cast<String, dynamic>() ??
        const <String, dynamic>{};
    return HandoffOffer(
      id: '${json['id']}',
      receiving: json['role'] == 'receiver',
      status: handoffStatusFrom(json['status']),
      fromName: '${from['name'] ?? ''}',
      fromPlatform: '${from['platform'] ?? ''}',
      toName: '${to['name'] ?? ''}',
      files: <HandoffFile>[
        for (final file in (json['files'] as List?) ?? const <Object?>[])
          HandoffFile.fromJson((file as Map).cast<String, dynamic>()),
      ],
      totalSize: (json['totalSize'] as num?)?.toInt() ?? 0,
    );
  }

  final String id;
  final bool receiving;
  final HandoffStatus status;
  final String fromName;
  final String fromPlatform;
  final String toName;
  final List<HandoffFile> files;
  final int totalSize;

  /// Nothing more will happen to it.
  bool get finished =>
      status == HandoffStatus.delivered ||
      status == HandoffStatus.declined ||
      status == HandoffStatus.cancelled ||
      status == HandoffStatus.expired;
}
