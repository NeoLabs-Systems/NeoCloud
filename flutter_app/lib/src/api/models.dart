/// Data the NeoCloud API returns, parsed at the boundary so the rest of the
/// app works with typed values. Field names follow docs/api.md.
library;

part 'models_ai.dart';
part 'models_features.dart';
part 'models_members.dart';
part 'models_qr_login.dart';

int _int(Object? value) => value is num ? value.toInt() : int.tryParse('$value') ?? 0;

String _string(Object? value) => value == null ? '' : '$value';

DateTime? _date(Object? value) {
  final text = value?.toString();
  if (text == null || text.isEmpty) return null;
  // SQLite "YYYY-MM-DD HH:MM:SS" is UTC.
  return DateTime.tryParse(text.contains('T') ? text : '${text.replaceFirst(' ', 'T')}Z')?.toLocal();
}

class StorageUsage {
  const StorageUsage({
    required this.usedBytes,
    required this.quotaBytes,
    required this.trashBytes,
    required this.versionBytes,
  });

  factory StorageUsage.fromJson(Map<String, dynamic> json) => StorageUsage(
    usedBytes: _int(json['usedBytes']),
    quotaBytes: _int(json['quotaBytes']),
    trashBytes: _int(json['trashBytes']),
    versionBytes: _int(json['versionBytes']),
  );

  final int usedBytes;

  /// -1 when unlimited.
  final int quotaBytes;
  final int trashBytes;
  final int versionBytes;

  bool get unlimited => quotaBytes < 0;

  double get fraction => unlimited || quotaBytes == 0 ? 0 : (usedBytes / quotaBytes).clamp(0, 1).toDouble();
}

class AccountUser {
  const AccountUser({
    required this.id,
    required this.username,
    required this.displayName,
    required this.email,
    required this.isAdmin,
    required this.storage,
  });

  factory AccountUser.fromJson(Map<String, dynamic> json) => AccountUser(
    id: _int(json['id']),
    username: _string(json['username']),
    displayName: json['displayName'] as String?,
    email: json['email'] as String?,
    isAdmin: json['isAdmin'] == true,
    storage: StorageUsage.fromJson((json['storage'] as Map?)?.cast<String, dynamic>() ?? const {}),
  );

  final int id;
  final String username;
  final String? displayName;
  final String? email;
  final bool isAdmin;
  final StorageUsage storage;

  String get label => (displayName?.trim().isNotEmpty ?? false) ? displayName!.trim() : username;
}

class FileNode {
  const FileNode({
    required this.id,
    required this.parentId,
    required this.type,
    required this.name,
    required this.size,
    required this.mime,
    required this.etag,
    required this.version,
    required this.modifiedAt,
    this.trashedAt,
    this.originalPath,
    this.vaultId,
    this.cipherName,
    this.favorite = false,
    this.shared = false,
    this.path,
    this.expiresAt,
    this.aiExcluded = false,
    this.aiTags = const <String>[],
    this.aiMeta,
  });

  factory FileNode.fromJson(Map<String, dynamic> json) => FileNode(
    id: _string(json['id']),
    parentId: json['parentId'] as String?,
    type: _string(json['type']),
    name: _string(json['name']),
    size: _int(json['size']),
    mime: json['mime'] as String?,
    etag: _string(json['etag']),
    version: _int(json['version']),
    modifiedAt: _date(json['modifiedAt']),
    trashedAt: _date(json['trashedAt']),
    originalPath: json['originalPath'] as String?,
    vaultId: json['vaultId'] as String?,
    favorite: json['favorite'] == true,
    shared: json['shared'] == true,
    path: json['path'] is String ? json['path'] as String : null,
    expiresAt: _date(json['expiresAt']),
    aiExcluded: json['aiExcluded'] == true,
    aiTags: _strings(json['aiTags']),
    aiMeta: json['aiMeta'] is Map ? (json['aiMeta'] as Map).cast<String, dynamic>() : null,
  );

  final String id;
  final String? parentId;
  final String type;
  final String name;
  final int size;
  final String? mime;
  final String etag;
  final int version;
  final DateTime? modifiedAt;
  final DateTime? trashedAt;
  final String? originalPath;

  /// Set on a vault folder and everything inside it (docs/clients.md §4).
  final String? vaultId;

  /// Inside a vault: the encrypted name the server stores, while [name] is
  /// the decrypted one. Replacing the file must reuse it.
  final String? cipherName;

  /// Starred by the account.
  final bool favorite;

  /// Shared by the account with other accounts on the server (the owner's
  /// view only; members never see it).
  final bool shared;

  /// Search, recent, storage and favorites results: the folder the node is
  /// in, e.g. "/Photos/2026" ("/" for the root).
  final String? path;

  /// In the trash: when the server deletes it for good.
  final DateTime? expiresAt;

  /// A folder its owner excluded from AI; it covers everything below it.
  final bool aiExcluded;

  /// Tags the account accepted from AI suggestions.
  final List<String> aiTags;

  /// What AI read from the file's current content (date, issuer, subject,
  /// type, amount, currency, caption, `confidence`, `model`); null when it
  /// hasn't looked yet or the content changed since.
  final Map<String, dynamic>? aiMeta;

  bool get isFolder => type == 'folder';

  bool get inVault => vaultId != null;

  /// The same node under its decrypted name; [mime] is then guessed from it.
  FileNode withName(String plainName, {String? mime}) => FileNode(
    id: id,
    parentId: parentId,
    type: type,
    name: plainName,
    size: size,
    mime: mime,
    etag: etag,
    version: version,
    modifiedAt: modifiedAt,
    trashedAt: trashedAt,
    originalPath: originalPath,
    vaultId: vaultId,
    cipherName: name,
    favorite: favorite,
    shared: shared,
    path: path,
    expiresAt: expiresAt,
    aiExcluded: aiExcluded,
    aiTags: aiTags,
    aiMeta: aiMeta,
  );

  /// This node after a rename, move, star or AI exclusion, applied before the
  /// server answers.
  FileNode copyWith({String? name, String? parentId, bool? favorite, bool? aiExcluded}) => FileNode(
    id: id,
    parentId: parentId ?? this.parentId,
    type: type,
    name: name ?? this.name,
    size: size,
    mime: mime,
    etag: etag,
    version: version,
    modifiedAt: modifiedAt,
    trashedAt: trashedAt,
    originalPath: originalPath,
    vaultId: vaultId,
    cipherName: cipherName,
    favorite: favorite ?? this.favorite,
    shared: shared,
    path: path,
    expiresAt: expiresAt,
    aiExcluded: aiExcluded ?? this.aiExcluded,
    aiTags: aiTags,
    aiMeta: aiMeta,
  );

  /// This node after an upload replaced its content, keeping the decrypted
  /// name of a vault file.
  FileNode updatedFrom(FileNode stored) => cipherName == null ? stored : stored.withName(name, mime: mime);
}

class PathSegment {
  const PathSegment({required this.id, required this.name});

  final String id;
  final String name;
}

class FolderListing {
  const FolderListing({required this.folder, required this.path, required this.items, this.nextCursor});

  factory FolderListing.fromJson(Map<String, dynamic> json) => FolderListing(
    folder: FileNode.fromJson((json['folder'] as Map).cast<String, dynamic>()),
    path: ((json['path'] as List?) ?? const [])
        .map((e) => (e as Map).cast<String, dynamic>())
        .map((e) => PathSegment(id: _string(e['id']), name: _string(e['name'])))
        .toList(),
    items: _nodes(json['items']),
    nextCursor: json['nextCursor'] as String?,
  );

  final FileNode folder;
  final List<PathSegment> path;
  final List<FileNode> items;

  /// Fetches the next page; null when this was the last one.
  final String? nextCursor;
}

List<FileNode> _nodes(Object? raw) => ((raw as List?) ?? const [])
    .map((e) => FileNode.fromJson((e as Map).cast<String, dynamic>()))
    .toList();

List<FileNode> nodesFromJson(Object? raw) => _nodes(raw);

class FileVersion {
  const FileVersion({required this.version, required this.size, required this.createdAt});

  factory FileVersion.fromJson(Map<String, dynamic> json) => FileVersion(
    version: _int(json['version']),
    size: _int(json['size']),
    createdAt: _date(json['createdAt']),
  );

  final int version;
  final int size;
  final DateTime? createdAt;
}

class UserSettings {
  const UserSettings({
    this.language,
    this.theme = 'system',
    this.fileView = 'list',
    this.sortField = 'name',
    this.sortOrder = 'asc',
    this.confirmDelete = true,
    this.uploadConflict = 'ask',
    this.fullTextSearch = true,
    this.aiOptIn = false,
    this.aiModels = const <String, String>{},
    this.aiFeatures = const <String, bool>{},
    this.aiNamingTemplate = '',
    this.folderViews = const <String, Object?>{},
  });

  factory UserSettings.fromJson(Map<String, dynamic> json) => UserSettings(
    language: json['language'] as String?,
    theme: _string(json['theme'] ?? 'system'),
    fileView: _string(json['fileView'] ?? 'list'),
    sortField: _string(json['sortField'] ?? 'name'),
    sortOrder: _string(json['sortOrder'] ?? 'asc'),
    confirmDelete: json['confirmDelete'] != false,
    uploadConflict: _string(json['uploadConflict'] ?? 'ask'),
    fullTextSearch: json['fullTextSearch'] != false,
    aiOptIn: json['ai_opt_in'] == true,
    aiModels: <String, String>{
      for (final entry in json.entries)
        if (entry.key.startsWith('ai_model_') && entry.value is String) entry.key: entry.value as String,
    },
    aiFeatures: _aiFeatures(json['ai_features']),
    aiNamingTemplate: _string(json['ai_naming_template']),
    folderViews: json['folder_views'] is Map ? (json['folder_views'] as Map).cast<String, Object?>() : const <String, Object?>{},
  );

  final String? language;
  final String theme;
  final String fileView;
  final String sortField;
  final String sortOrder;
  final bool confirmDelete;
  final String uploadConflict;

  /// Whether the server may index this account's files for content search
  /// (when the admin turned full-text search on).
  final bool fullTextSearch;

  /// Whether this account uses AI features (`ai_opt_in`).
  final bool aiOptIn;

  /// The model per kind of work, by setting key (`ai_model_text` …):
  /// `auto` or `provider::model`. Missing keys mean `auto`.
  final Map<String, String> aiModels;

  /// Which suggestions are on (`ai_features`), by feature name, as stored;
  /// [aiFeatureOn] adds the server's defaults.
  final Map<String, bool> aiFeatures;

  /// Whether an AI feature is on: the stored switch, else the server's
  /// default ([aiFeatureDefaults]).
  bool aiFeatureOn(String feature) => aiFeatures[feature] ?? aiFeatureDefaults[feature] ?? false;

  /// How suggested names are built (`ai_naming_template`); empty for the
  /// server's default.
  final String aiNamingTemplate;

  /// Sort and view per folder and page (`folder_views`), as stored; read
  /// through [FolderViews] (src/files/folder_views.dart).
  final Map<String, Object?> folderViews;
}

class SessionInfo {
  const SessionInfo({required this.id, required this.current, required this.userAgent, required this.ipAddress, required this.lastSeenAt});

  factory SessionInfo.fromJson(Map<String, dynamic> json) => SessionInfo(
    id: _int(json['id']),
    current: json['current'] == true,
    userAgent: _string(json['userAgent']),
    ipAddress: json['ipAddress'] as String?,
    lastSeenAt: _date(json['lastSeenAt']),
  );

  final int id;
  final bool current;
  final String userAgent;
  final String? ipAddress;
  final DateTime? lastSeenAt;
}

/// A security key or passkey on the account (`/api/account/security-keys`).
class SecurityKeyItem {
  const SecurityKeyItem({required this.id, required this.label, required this.createdAt, required this.lastUsedAt, required this.backedUp});

  factory SecurityKeyItem.fromJson(Map<String, dynamic> json) => SecurityKeyItem(
    id: _int(json['id']),
    label: _string(json['label']),
    createdAt: _date(json['createdAt']),
    lastUsedAt: _date(json['lastUsedAt']),
    backedUp: json['backedUp'] == true,
  );

  final int id;
  final String label;
  final DateTime? createdAt;
  final DateTime? lastUsedAt;

  /// Synced by its provider (a passkey in a password manager or iCloud).
  final bool backedUp;
}

class DeviceInfo {
  const DeviceInfo({required this.id, required this.name, required this.platform, required this.lastSeenAt, this.current = false});

  factory DeviceInfo.fromJson(Map<String, dynamic> json) => DeviceInfo(
    id: _int(json['id']),
    name: _string(json['name']),
    platform: _string(json['platform']),
    lastSeenAt: _date(json['lastSeenAt']),
    current: json['current'] == true,
  );

  final int id;
  final String name;
  final String platform;
  final DateTime? lastSeenAt;

  /// The phone asking: it disconnects instead of revoking itself.
  final bool current;
}

class AdminUser {
  const AdminUser({
    required this.id,
    required this.username,
    required this.email,
    required this.isAdmin,
    required this.disabled,
    required this.usedBytes,
    required this.quotaBytes,
    required this.quotaMode,
    required this.fileCount,
    required this.lastLogin,
    this.aiMonthlyTokens,
  });

  factory AdminUser.fromJson(Map<String, dynamic> json) => AdminUser(
    id: _int(json['id']),
    username: _string(json['username']),
    email: json['email'] as String?,
    isAdmin: json['isAdmin'] == true,
    disabled: json['disabled'] == true,
    usedBytes: _int(json['usedBytes']),
    quotaBytes: _int(json['quotaBytes']),
    quotaMode: _string(json['quotaMode']),
    fileCount: _int(json['fileCount']),
    lastLogin: _date(json['lastLogin']),
    aiMonthlyTokens: json['aiMonthlyTokens'] is num ? (json['aiMonthlyTokens'] as num).toInt() : null,
  );

  final int id;
  final String username;
  final String? email;
  final bool isAdmin;
  final bool disabled;
  final int usedBytes;
  final int quotaBytes;
  final String quotaMode;
  final int fileCount;
  final DateTime? lastLogin;

  /// The account's own monthly AI token budget: null follows the server
  /// default, 0 is no limit.
  final int? aiMonthlyTokens;
}

/// Who a share link lets in (docs/research/roadmap.md, share modes).
enum ShareMode {
  /// View and download.
  view,

  /// View, download and add files (folders only).
  viewUpload,

  /// A file request: add files, see nothing (folders only).
  uploadOnly;

  String get apiName => switch (this) {
    ShareMode.view => 'view',
    ShareMode.viewUpload => 'view_upload',
    ShareMode.uploadOnly => 'upload_only',
  };

  static ShareMode fromApi(Object? value, {bool allowUpload = false}) => switch (value) {
    'view_upload' => ShareMode.viewUpload,
    'upload_only' => ShareMode.uploadOnly,
    'view' => ShareMode.view,
    _ => allowUpload ? ShareMode.viewUpload : ShareMode.view,
  };
}

/// A public link to a file or folder (docs/clients.md §3).
class ShareLink {
  const ShareLink({
    required this.id,
    required this.url,
    required this.nodeId,
    required this.nodeName,
    required this.nodeType,
    required this.expiresAt,
    required this.hasPassword,
    required this.createdAt,
    this.mode = ShareMode.view,
    this.maxDownloads,
    this.downloadCount = 0,
    this.note,
    this.uploadedBytes = 0,
    this.expiredFlag = false,
    this.exhaustedFlag = false,
  });

  factory ShareLink.fromJson(Map<String, dynamic> json) {
    final node = (json['node'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    final note = json['note'] as String?;
    return ShareLink(
      id: _string(json['id']),
      url: _string(json['url']),
      nodeId: _string(json['nodeId'] ?? node['id']),
      nodeName: _string(json['nodeName'] ?? json['name'] ?? node['name']),
      nodeType: _string(json['nodeType'] ?? json['type'] ?? node['type']),
      expiresAt: _date(json['expiresAt']),
      hasPassword: json['hasPassword'] == true || json['passwordSet'] == true || json['protected'] == true,
      mode: ShareMode.fromApi(json['mode'], allowUpload: json['allowUpload'] == true),
      maxDownloads: json['maxDownloads'] == null ? null : _int(json['maxDownloads']),
      downloadCount: _int(json['downloadCount']),
      note: note == null || note.trim().isEmpty ? null : note,
      uploadedBytes: _int(json['uploadedBytes']),
      createdAt: _date(json['createdAt']),
      expiredFlag: json['expired'] == true,
      exhaustedFlag: json['exhausted'] == true,
    );
  }

  final String id;
  final String url;
  final String nodeId;
  final String nodeName;
  final String nodeType;
  final DateTime? expiresAt;
  final bool hasPassword;
  final ShareMode mode;

  /// Downloads allowed in total; null for no limit, 1 for a one-time link.
  final int? maxDownloads;
  final int downloadCount;

  /// Shown to visitors on the share page.
  final String? note;
  final DateTime? createdAt;

  /// What visitors uploaded through the link so far.
  final int uploadedBytes;
  final bool expiredFlag;
  final bool exhaustedFlag;

  bool get allowUpload => mode != ShareMode.view;

  bool get isFolder => nodeType == 'folder';

  /// The download limit is used up (the server answers 410 SHARE_EXHAUSTED).
  bool get exhausted => exhaustedFlag || (maxDownloads != null && downloadCount >= maxDownloads!);

  bool get expired => expiredFlag || (expiresAt != null && expiresAt!.isBefore(DateTime.now()));

  /// Still opens for visitors.
  bool get active => !expired && !exhausted;
}

/// What unlocking a vault needs: the passphrase-wrapped key and its KDF salt.
class VaultKeyInfo {
  const VaultKeyInfo({required this.id, required this.folderId, required this.salt, required this.iterations, required this.wrappedKey});

  factory VaultKeyInfo.fromJson(Map<String, dynamic> json) => VaultKeyInfo(
    id: _string(json['id']),
    folderId: _string(json['folderId']),
    salt: _string(json['salt']),
    iterations: _int(json['iterations']),
    wrappedKey: _string(json['wrappedKey']),
  );

  final String id;
  final String folderId;
  final String salt;
  final int iterations;
  final String wrappedKey;
}

/// A device waiting for the account's approval (docs/clients.md §1).
class DeviceAuthorizationRequest {
  const DeviceAuthorizationRequest({required this.name, required this.platform, required this.createdAt, required this.expiresAt});

  factory DeviceAuthorizationRequest.fromJson(Map<String, dynamic> json) => DeviceAuthorizationRequest(
    name: _string(json['name']),
    platform: _string(json['platform']),
    createdAt: _date(json['createdAt']),
    expiresAt: _date(json['expiresAt']),
  );

  final String name;
  final String platform;
  final DateTime? createdAt;
  final DateTime? expiresAt;
}

/// What takes the account's space (GET /api/storage/breakdown).
class StorageBreakdown {
  const StorageBreakdown({
    required this.total,
    required this.files,
    required this.trash,
    required this.versions,
    required this.byType,
    required this.largest,
  });

  factory StorageBreakdown.fromJson(Map<String, dynamic> json) => StorageBreakdown(
    total: _int(json['total']),
    files: _int(json['files']),
    trash: _int(json['trash']),
    versions: _int(json['versions']),
    byType: ((json['byType'] as List?) ?? const [])
        .map((e) => (e as Map).cast<String, dynamic>())
        .map((e) => StorageTypeUsage(type: _string(e['type']), bytes: _int(e['bytes']), count: _int(e['count'])))
        .toList(),
    largest: _nodes(json['largest']),
  );

  final int total;
  final int files;
  final int trash;
  final int versions;
  final List<StorageTypeUsage> byType;
  final List<FileNode> largest;
}

class StorageTypeUsage {
  const StorageTypeUsage({required this.type, required this.bytes, required this.count});

  final String type;
  final int bytes;
  final int count;
}

/// Files with identical content (GET /api/storage/duplicates).
class DuplicateReport {
  const DuplicateReport({required this.groups, required this.reclaimable});

  factory DuplicateReport.fromJson(Map<String, dynamic> json) => DuplicateReport(
    groups: ((json['groups'] as List?) ?? const [])
        .map((e) => (e as Map).cast<String, dynamic>())
        .map((e) => DuplicateGroup(sha256: _string(e['sha256']), size: _int(e['size']), nodes: _nodes(e['nodes'])))
        .toList(),
    reclaimable: _int(json['reclaimable']),
  );

  final List<DuplicateGroup> groups;
  final int reclaimable;
}

class DuplicateGroup {
  const DuplicateGroup({required this.sha256, required this.size, required this.nodes});

  final String sha256;
  final int size;
  final List<FileNode> nodes;
}
