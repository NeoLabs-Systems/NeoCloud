// Models for QR pairing, imports and full-text search (docs/clients.md §5,
// §7, §8), parsed with models.dart's helpers.
part of 'models.dart';

/// A code the web app shows as a QR for a phone to redeem.
class PairingTicket {
  const PairingTicket({required this.code, required this.expiresAt, required this.server, required this.qrPayload});

  factory PairingTicket.fromJson(Map<String, dynamic> json) => PairingTicket(
    code: _string(json['code']),
    expiresAt: _date(json['expiresAt']) ?? DateTime.now().add(const Duration(minutes: 5)),
    server: _string(json['server']),
    qrPayload: _string(json['qrPayload']),
  );

  final String code;
  final DateTime expiresAt;
  final String server;

  /// `neocloud://pair?server=…&code=…`
  final String qrPayload;
}

enum PairingState { pending, used, expired }

/// Where a pairing code stands; [deviceName] once a phone redeemed it.
class PairingStatus {
  const PairingStatus({required this.state, this.deviceName, this.devicePlatform});

  factory PairingStatus.fromJson(Map<String, dynamic> json) {
    final device = (json['device'] as Map?)?.cast<String, dynamic>();
    return PairingStatus(
      state: PairingState.values.where((state) => state.name == json['status']).firstOrNull ?? PairingState.pending,
      deviceName: device == null ? null : _string(device['name']),
      devicePlatform: device == null ? null : _string(device['platform']),
    );
  }

  final PairingState state;
  final String? deviceName;
  final String? devicePlatform;
}

/// What "Test connection" found on the other server.
class ImportProbe {
  const ImportProbe({required this.ok, required this.files, required this.bytes, this.error});

  factory ImportProbe.fromJson(Map<String, dynamic> json) => ImportProbe(
    ok: json['ok'] == true,
    files: _int(json['files']),
    bytes: _int(json['bytes']),
    error: json['error']?.toString(),
  );

  final bool ok;
  final int files;
  final int bytes;
  final String? error;
}

enum ImportStatus { queued, scanning, running, paused, done, failed, cancelled }

class ImportError {
  const ImportError({required this.path, required this.message});

  final String path;
  final String message;
}

/// An import job and its progress.
class ImportJob {
  const ImportJob({
    required this.id,
    required this.source,
    required this.url,
    required this.username,
    required this.sourcePath,
    required this.targetFolderId,
    required this.targetPath,
    required this.status,
    required this.totalFiles,
    required this.totalBytes,
    required this.doneFiles,
    required this.doneBytes,
    required this.skippedFiles,
    required this.failedFiles,
    required this.errors,
    this.startedAt,
    this.finishedAt,
    this.createdAt,
  });

  factory ImportJob.fromJson(Map<String, dynamic> json) => ImportJob(
    id: _string(json['id']),
    source: _string(json['source']),
    url: _string(json['url']),
    username: _string(json['username']),
    sourcePath: _string(json['sourcePath'] ?? '/'),
    targetFolderId: json['targetFolderId']?.toString(),
    targetPath: _string(json['targetPath']),
    status: ImportStatus.values.where((status) => status.name == json['status']).firstOrNull ?? ImportStatus.queued,
    totalFiles: _int(json['totalFiles']),
    totalBytes: _int(json['totalBytes']),
    doneFiles: _int(json['doneFiles']),
    doneBytes: _int(json['doneBytes']),
    skippedFiles: _int(json['skippedFiles']),
    failedFiles: _int(json['failedFiles']),
    errors: <ImportError>[
      for (final entry in (json['errors'] as List?) ?? const <Object?>[])
        if (entry is Map) ImportError(path: _string(entry['path']), message: _string(entry['message'])),
    ],
    startedAt: _date(json['startedAt']),
    finishedAt: _date(json['finishedAt']),
    createdAt: _date(json['createdAt']),
  );

  final String id;
  final String source;
  final String url;
  final String username;
  final String sourcePath;
  final String? targetFolderId;
  final String targetPath;
  final ImportStatus status;
  final int totalFiles;
  final int totalBytes;
  final int doneFiles;
  final int doneBytes;
  final int skippedFiles;
  final int failedFiles;

  /// The last 50 files that failed.
  final List<ImportError> errors;
  final DateTime? startedAt;
  final DateTime? finishedAt;
  final DateTime? createdAt;

  /// Still moving: the page polls while any job is.
  bool get active => status == ImportStatus.queued || status == ImportStatus.scanning || status == ImportStatus.running;

  bool get finished => status == ImportStatus.done || status == ImportStatus.failed || status == ImportStatus.cancelled;
}

/// A search result: the node, why it matched and, for content matches, a
/// snippet with U+0002/U+0003 around the matched terms.
class SearchHit {
  const SearchHit({required this.node, this.match = 'name', this.snippet});

  factory SearchHit.fromJson(Map<String, dynamic> json) => SearchHit(
    node: FileNode.fromJson(json),
    match: _string(json['match'] ?? 'name'),
    snippet: json['snippet'] is String && (json['snippet'] as String).isNotEmpty ? json['snippet'] as String : null,
  );

  final FileNode node;

  /// `name`, `content` or `both`.
  final String match;
  final String? snippet;

  bool get matchedContent => match == 'content' || match == 'both';
}

/// The admin's view of the full-text index.
class SearchIndexStatus {
  const SearchIndexStatus({
    required this.enabled,
    required this.indexed,
    required this.pending,
    required this.failed,
    required this.bytes,
    required this.pdftotext,
    required this.office,
  });

  factory SearchIndexStatus.fromJson(Map<String, dynamic> json) {
    final available = (json['available'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    return SearchIndexStatus(
      enabled: json['enabled'] == true,
      indexed: _int(json['indexed']),
      pending: _int(json['pending']),
      failed: _int(json['failed']),
      bytes: _int(json['bytes']),
      pdftotext: available['pdftotext'] == true,
      office: available['office'] == true,
    );
  }

  final bool enabled;
  final int indexed;
  final int pending;
  final int failed;
  final int bytes;
  final bool pdftotext;
  final bool office;
}
