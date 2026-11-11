import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../api/backend_client.dart';
import 'camera_backup.dart';
import 'camera_upload_stub.dart' if (dart.library.io) 'camera_upload_io.dart' as platform;

export 'camera_backup.dart'
    show CameraCounts, CameraRecord, CameraRecordStatus, CameraReason, CameraScope, CameraStatus, CameraStatusKind, CameraProgress;

/// What camera upload does; kept in SharedPreferences so the background
/// task can read it too. Bound to the account it was turned on for
/// ([account]): it runs only while that account on
/// that server is signed in.
@immutable
class CameraUploadSettings {
  const CameraUploadSettings({
    this.enabled = false,
    this.account,
    this.folderId,
    this.folderName = '',
    this.wifiOnly = true,
    this.includeVideos = false,
    this.chargingOnly = false,
    this.videosWifiOnly = true,
    this.scope = CameraScope.everything,
    this.enabledAt,
  });

  factory CameraUploadSettings.decode(String? raw) {
    if (raw == null || raw.isEmpty) return const CameraUploadSettings();
    try {
      final json = (jsonDecode(raw) as Map).cast<String, dynamic>();
      return CameraUploadSettings(
        enabled: json['enabled'] == true,
        account: json['account'] as String?,
        folderId: json['folderId'] as String?,
        folderName: json['folderName'] as String? ?? '',
        wifiOnly: json['wifiOnly'] != false,
        includeVideos: json['includeVideos'] == true,
        chargingOnly: json['chargingOnly'] == true,
        videosWifiOnly: json['videosWifiOnly'] != false,
        // Settings from before the choice existed only uploaded new photos.
        scope: json['scope'] == 'everything' ? CameraScope.everything : CameraScope.newOnly,
        enabledAt: DateTime.tryParse(json['enabledAt'] as String? ?? ''),
      );
    } catch (_) {
      return const CameraUploadSettings();
    }
  }

  final bool enabled;

  /// `<server>#<account id>` it was turned on for; null before that.
  final String? account;
  final String? folderId;
  final String folderName;
  final bool wifiOnly;
  final bool includeVideos;
  final bool chargingOnly;

  /// Photos may use mobile data while videos wait for Wi-Fi.
  final bool videosWifiOnly;
  final CameraScope scope;

  /// With [CameraScope.newOnly], photos taken before this stay out.
  final DateTime? enabledAt;

  /// On, and for [accountKey].
  bool activeFor(String? accountKey) => enabled && accountKey != null && account == accountKey;

  /// The same choices for another account: off, no folder, nothing done.
  CameraUploadSettings rebound(String? accountKey) => CameraUploadSettings(
    account: accountKey,
    wifiOnly: wifiOnly,
    includeVideos: includeVideos,
    chargingOnly: chargingOnly,
    videosWifiOnly: videosWifiOnly,
    scope: scope,
  );

  CameraUploadSettings copyWith({
    bool? enabled,
    String? account,
    String? folderId,
    String? folderName,
    bool? wifiOnly,
    bool? includeVideos,
    bool? chargingOnly,
    bool? videosWifiOnly,
    CameraScope? scope,
    DateTime? enabledAt,
  }) => CameraUploadSettings(
    enabled: enabled ?? this.enabled,
    account: account ?? this.account,
    folderId: folderId ?? this.folderId,
    folderName: folderName ?? this.folderName,
    wifiOnly: wifiOnly ?? this.wifiOnly,
    includeVideos: includeVideos ?? this.includeVideos,
    chargingOnly: chargingOnly ?? this.chargingOnly,
    videosWifiOnly: videosWifiOnly ?? this.videosWifiOnly,
    scope: scope ?? this.scope,
    enabledAt: enabledAt ?? this.enabledAt,
  );

  String encode() => jsonEncode(<String, Object?>{
    'enabled': enabled,
    'account': account,
    'folderId': folderId,
    'folderName': folderName,
    'wifiOnly': wifiOnly,
    'includeVideos': includeVideos,
    'chargingOnly': chargingOnly,
    'videosWifiOnly': videosWifiOnly,
    'scope': scope == CameraScope.everything ? 'everything' : 'new',
    'enabledAt': enabledAt?.toIso8601String(),
  });
}

enum CameraAccess { granted, limited, denied }

/// How much the library holds, for "Everything (N photos, X GB)". [bytes]
/// is an estimate from a sample, null when nothing could be measured.
class CameraLibrarySize {
  const CameraLibrarySize({required this.count, this.bytes});

  final int count;
  final int? bytes;
}

/// Backs up the phone's photos (and videos) to a folder while the app is
/// open, on every resume and from a periodic background task.
abstract class CameraUploads extends ChangeNotifier {
  /// True on Android and iOS only.
  bool get supported;

  CameraUploadSettings get settings;

  bool get running;

  /// The running pass, null when idle.
  CameraProgress? get progress;

  CameraStatus get status;

  CameraCounts get counts;

  /// Records of files still in the library, newest first.
  List<CameraRecord> records(CameraRecordStatus? status);

  DateTime? get lastRun;

  /// Only some photos are visible to the app (iOS "Selected photos",
  /// Android 14 partial access).
  bool get limitedAccess;

  /// The upload log, oldest line first.
  List<String> get log;

  Future<void> load();

  /// [accountKey] is signed in. Settings and ledger of another account (or
  /// of none) are dropped; if camera upload was on, [needsConfirmation].
  Future<void> signIn(String accountKey);

  /// Signed out: camera upload goes off and its background task is
  /// cancelled. It stays off until turned on again.
  Future<void> signOut();

  /// Camera upload was on before a sign-out or a change of server or
  /// account; the app asks once whether to turn it on again.
  bool get needsConfirmation;

  Future<void> dismissConfirmation();

  Future<CameraAccess> requestAccess();

  Future<void> openSystemSettings();

  Future<CameraLibrarySize> librarySize({required bool includeVideos});

  /// Saves [next] for the signed-in account and schedules or cancels the
  /// background task.
  Future<void> update(CameraUploadSettings next);

  /// One pass now, with the app's signed-in client. [manual] when the user
  /// asked for it.
  Future<void> runNow(BackendClient client, {bool manual = false});

  /// Failed files get their automatic retries back.
  Future<void> retryFailed();
}

CameraUploads createCameraUploads() => platform.createCameraUploads();
