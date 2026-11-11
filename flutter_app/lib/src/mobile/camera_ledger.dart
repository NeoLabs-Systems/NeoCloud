import 'dart:convert';

// Camera backup's persisted state: one record per library asset, the
// upload log, and the status line derived from them. The pass that changes
// them is camera_backup.dart.

enum CameraRecordStatus { pending, done, failed }

/// Why a pending record is still waiting.
abstract final class CameraReason {
  static const String icloud = 'icloud';
  static const String wifi = 'wifi';
  static const String notAvailable = 'notAvailable';
  static const String alreadyOnServer = 'alreadyOnServer';
}

/// Automatic retries of a failing file; after that it waits for "Retry".
const int cameraMaxAttempts = 5;

/// Consecutive failures after which a pass stops: something is wrong
/// beyond one file.
const int cameraFailureStreakLimit = 5;

const int _logLimit = 1000;

class CameraRecord {
  CameraRecord({
    required this.assetId,
    this.name = '',
    this.size = 0,
    this.modified,
    this.created,
    this.isVideo = false,
    this.sha256,
    this.status = CameraRecordStatus.pending,
    this.reason,
    this.attempts = 0,
    this.doneAt,
    this.present = true,
  });

  factory CameraRecord.fromJson(Map<String, dynamic> json) => CameraRecord(
    assetId: json['id'] as String,
    name: json['name'] as String? ?? '',
    size: (json['size'] as num?)?.toInt() ?? 0,
    modified: DateTime.tryParse(json['modified'] as String? ?? ''),
    created: DateTime.tryParse(json['created'] as String? ?? ''),
    isVideo: json['video'] == true,
    sha256: json['sha256'] as String?,
    status: CameraRecordStatus.values.firstWhere((s) => s.name == json['status'], orElse: () => CameraRecordStatus.pending),
    reason: json['reason'] as String?,
    attempts: (json['attempts'] as num?)?.toInt() ?? 0,
    doneAt: DateTime.tryParse(json['doneAt'] as String? ?? ''),
    present: json['present'] != false,
  );

  final String assetId;
  String name;
  int size;
  DateTime? modified;
  DateTime? created;
  bool isVideo;
  String? sha256;
  CameraRecordStatus status;

  /// Failure text for failed records, a [CameraReason] for waiting ones.
  String? reason;
  int attempts;
  DateTime? doneAt;

  /// Still in the photo library (and in scope) at the last pass.
  bool present;

  Map<String, dynamic> toJson() => <String, dynamic>{
    'id': assetId,
    'name': name,
    'size': size,
    'modified': modified?.toIso8601String(),
    'created': created?.toIso8601String(),
    'video': isVideo,
    'sha256': sha256,
    'status': status.name,
    'reason': reason,
    'attempts': attempts,
    'doneAt': doneAt?.toIso8601String(),
    'present': present,
  };
}

/// Everything camera backup remembers between passes.
class CameraLedger {
  CameraLedger({this.account, Map<String, CameraRecord>? records, List<String>? log, this.lastRun, this.lastSuccess, this.lastBlock})
    : records = records ?? <String, CameraRecord>{},
      log = log ?? <String>[];

  factory CameraLedger.fromJson(Map<String, dynamic> json) => CameraLedger(
    account: json['account'] as String?,
    records: <String, CameraRecord>{
      for (final raw in (json['records'] as List?) ?? const [])
        (raw as Map)['id'] as String: CameraRecord.fromJson(raw.cast<String, dynamic>()),
    },
    log: <String>[for (final line in (json['log'] as List?) ?? const []) '$line'],
    lastRun: DateTime.tryParse(json['lastRun'] as String? ?? ''),
    lastSuccess: DateTime.tryParse(json['lastSuccess'] as String? ?? ''),
    lastBlock: CameraBlock.values.where((b) => b.name == json['lastBlock']).firstOrNull,
  );

  /// The account (`<server>#<account id>`) the records are about.
  String? account;
  final Map<String, CameraRecord> records;
  final List<String> log;
  DateTime? lastRun;

  /// The last pass that got through every file it could.
  DateTime? lastSuccess;

  /// What stopped the last pass, if anything.
  CameraBlock? lastBlock;

  Iterable<CameraRecord> get present => records.values.where((record) => record.present);

  CameraCounts get counts {
    var done = 0;
    var pending = 0;
    var failed = 0;
    var icloud = 0;
    var total = 0;
    for (final record in present) {
      total += 1;
      switch (record.status) {
        case CameraRecordStatus.done:
          done += 1;
        case CameraRecordStatus.failed:
          failed += 1;
        case CameraRecordStatus.pending:
          pending += 1;
          if (record.reason == CameraReason.icloud) icloud += 1;
      }
    }
    return CameraCounts(onPhone: total, backedUp: done, pending: pending, failed: failed, waitingOnCloud: icloud);
  }

  void addLog(String line, DateTime at) {
    log.add('${at.toIso8601String().substring(0, 19).replaceFirst('T', ' ')}  $line');
    if (log.length > _logLimit) log.removeRange(0, log.length - _logLimit);
  }

  Map<String, dynamic> toJson() => <String, dynamic>{
    'account': account,
    'records': <Map<String, dynamic>>[for (final record in records.values) record.toJson()],
    'log': log,
    'lastRun': lastRun?.toIso8601String(),
    'lastSuccess': lastSuccess?.toIso8601String(),
    'lastBlock': lastBlock?.name,
  };

  String encode() => jsonEncode(toJson());

  static CameraLedger decode(String? raw) {
    if (raw == null || raw.isEmpty) return CameraLedger();
    try {
      return CameraLedger.fromJson((jsonDecode(raw) as Map).cast<String, dynamic>());
    } catch (_) {
      return CameraLedger();
    }
  }
}

/// Numbers that add up: onPhone = backedUp + pending + failed.
class CameraCounts {
  const CameraCounts({this.onPhone = 0, this.backedUp = 0, this.pending = 0, this.failed = 0, this.waitingOnCloud = 0});

  final int onPhone;
  final int backedUp;
  final int pending;
  final int failed;

  /// Part of [pending]: originals still in iCloud.
  final int waitingOnCloud;
}

/// Why nothing is being uploaded right now. [paused]: the user tapped Pause
/// in the notification. [interrupted]: the system ended the background time
/// (Android 15's data sync limit, iOS's background time); the next pass
/// resumes.
enum CameraBlock {
  noFolder,
  noAccess,
  waitingWifi,
  waitingCharger,
  signedOut,
  serverUnreachable,
  serverFull,
  tooManyFailures,
  paused,
  interrupted,
}

/// Progress of the running pass: [current] of [total] files, and the iCloud
/// download of the current one (null when it is local).
class CameraProgress {
  const CameraProgress({required this.current, required this.total, this.name = '', this.cloud});

  final int current;
  final int total;
  final String name;
  final double? cloud;
}

enum CameraStatusKind {
  off,
  upToDate,
  uploading,
  pending,
  waitingWifi,
  waitingCharger,
  limitedAccess,
  noAccess,
  noFolder,
  serverUnreachable,
  signedOut,
  serverFull,
  failed,
  paused,
}

/// The one line the status shows, with the numbers it needs.
class CameraStatus {
  const CameraStatus(this.kind, {this.current = 0, this.total = 0, this.count = 0});

  final CameraStatusKind kind;

  /// Uploading [current] of [total].
  final int current;
  final int total;

  /// Failed or pending files, for those kinds.
  final int count;
}

CameraStatus cameraStatus({required bool enabled, required CameraLedger ledger, CameraProgress? progress, bool limitedAccess = false}) {
  if (!enabled) return const CameraStatus(CameraStatusKind.off);
  if (progress != null) return CameraStatus(CameraStatusKind.uploading, current: progress.current, total: progress.total);
  final counts = ledger.counts;
  switch (ledger.lastBlock) {
    case CameraBlock.noFolder:
      return const CameraStatus(CameraStatusKind.noFolder);
    case CameraBlock.noAccess:
      return const CameraStatus(CameraStatusKind.noAccess);
    case CameraBlock.waitingWifi:
      return CameraStatus(CameraStatusKind.waitingWifi, count: counts.pending);
    case CameraBlock.waitingCharger:
      return CameraStatus(CameraStatusKind.waitingCharger, count: counts.pending);
    case CameraBlock.signedOut:
      return const CameraStatus(CameraStatusKind.signedOut);
    case CameraBlock.serverUnreachable:
      return const CameraStatus(CameraStatusKind.serverUnreachable);
    case CameraBlock.serverFull:
      return const CameraStatus(CameraStatusKind.serverFull);
    case CameraBlock.paused:
      return CameraStatus(CameraStatusKind.paused, count: counts.pending);
    case CameraBlock.tooManyFailures:
    case CameraBlock.interrupted:
    case null:
      break;
  }
  if (counts.failed > 0) return CameraStatus(CameraStatusKind.failed, count: counts.failed);
  if (limitedAccess) return const CameraStatus(CameraStatusKind.limitedAccess);
  if (counts.pending > 0) return CameraStatus(CameraStatusKind.pending, count: counts.pending);
  return const CameraStatus(CameraStatusKind.upToDate);
}
