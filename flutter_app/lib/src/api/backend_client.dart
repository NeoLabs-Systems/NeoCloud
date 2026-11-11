import 'dart:convert';
import 'dart:typed_data';

import '../network/app_http_client.dart';
import '../network/app_http_client_factory.dart';
import 'models.dart';

part 'backend_client_features.dart';
part 'backend_client_members.dart';

/// An error the server answered with. [code] is the stable machine code from
/// the API (e.g. NAME_EXISTS); [message] is readable text for the user.
class ApiException implements Exception {
  const ApiException(this.statusCode, this.message, {this.code});

  final int statusCode;
  final String message;
  final String? code;

  @override
  String toString() => message;
}

/// Every call NeoCloud's UI makes, in one place. Paths and bodies follow
/// docs/api.md in the repository root.
class BackendClient {
  BackendClient({required this.baseUrl}) : _http = createAppHttpClient();

  /// Empty means "same origin as the page" (the web app under /app). The
  /// mobile apps set it to the server they are connected to.
  String baseUrl;
  final AppHttpClient _http;

  /// The device token (`nc_…`) native apps sign in with; null on the web,
  /// which uses the session cookie.
  String? bearerToken;

  Map<String, String> get _authHeaders {
    final token = bearerToken;
    return token == null ? const <String, String>{} : <String, String>{'Authorization': 'Bearer $token'};
  }

  /// Called on any 401 so the app can return to the sign-in screen.
  void Function()? onSessionExpired;

  Uri uri(String path, [Map<String, String>? query]) {
    final base = baseUrl.isEmpty ? Uri.base.origin : baseUrl;
    return Uri.parse('$base$path').replace(queryParameters: query == null || query.isEmpty ? null : query);
  }

  Future<dynamic> _request(
    String method,
    String path, {
    Map<String, String>? query,
    Object? json,
    Uint8List? bytes,
    Map<String, String>? headers,
    bool expireOn401 = true,
  }) async {
    final HttpResponseData response;
    try {
      response = await _http.send(
        method,
        uri(path, query),
        headers: <String, String>{
          'Accept': 'application/json',
          ..._authHeaders,
          if (json != null) 'Content-Type': 'application/json',
          if (bytes != null) 'Content-Type': 'application/octet-stream',
          ...?headers,
        },
        body: json != null ? jsonEncode(json) : bytes,
      );
    } catch (_) {
      throw const ApiException(0, '', code: 'NETWORK');
    }
    dynamic decoded;
    if (response.body.isNotEmpty) {
      try {
        decoded = jsonDecode(response.body);
      } catch (_) {
        decoded = null;
      }
    }
    if (response.statusCode >= 200 && response.statusCode < 300) return decoded;
    if (response.statusCode == 401 && expireOn401) onSessionExpired?.call();
    final body = decoded is Map ? decoded : const <String, dynamic>{};
    throw ApiException(
      response.statusCode,
      body['error']?.toString() ?? 'HTTP ${response.statusCode}',
      code: body['code']?.toString(),
    );
  }

  Map<String, dynamic> _map(dynamic value) => (value as Map).cast<String, dynamic>();

  // Auth

  Future<Map<String, dynamic>> authStatus() async => _map(await _request('GET', '/api/auth/status', expireOn401: false));

  Future<Map<String, dynamic>> login(String username, String password) async =>
      _map(await _request('POST', '/api/auth/login', json: {'username': username, 'password': password}, expireOn401: false));

  Future<Map<String, dynamic>> loginTwoFactor(String code) async =>
      _map(await _request('POST', '/api/auth/login/2fa', json: {'code': code}, expireOn401: false));

  Future<Map<String, dynamic>> register({
    required String username,
    required String email,
    required String password,
  }) async => _map(await _request('POST', '/api/auth/register', json: {
    'username': username,
    'email': email,
    'password': password,
  }, expireOn401: false));

  /// Security key sign-in: options for `navigator.credentials.get`, then the
  /// assertion back. Without [username] any discoverable key (passkey) works.
  Future<Map<String, dynamic>> beginSecurityKeyLogin({String? username}) async => _map(await _request(
    'POST',
    '/api/auth/webauthn/login/options',
    json: {if (username != null && username.isNotEmpty) 'username': username},
    expireOn401: false,
  ));

  Future<Map<String, dynamic>> completeSecurityKeyLogin(Map<String, dynamic> response) async =>
      _map(await _request('POST', '/api/auth/webauthn/login/verify', json: {'response': response}, expireOn401: false));

  Future<void> logout() async {
    await _request('POST', '/api/auth/logout', expireOn401: false);
    _http.clearSession();
  }

  Future<AccountUser> me() async => AccountUser.fromJson(_map(_map(await _request('GET', '/api/auth/me'))['user']));

  // Files

  /// One page of a folder; [FolderListing.nextCursor] fetches the next.
  Future<FolderListing> listFolder(String folderId, {String sort = 'name', String order = 'asc', int? limit, String? cursor}) async =>
      FolderListing.fromJson(_map(await _request('GET', '/api/folders/$folderId/children', query: {
        'sort': sort,
        'order': order,
        if (limit != null) 'limit': '$limit',
        'cursor': ?cursor,
      })));

  Future<FileNode> createFolder(String parentId, String name) async =>
      FileNode.fromJson(_map(await _request('POST', '/api/folders', json: {'parentId': parentId, 'name': name})));

  Future<FileNode> updateNode(String id, {String? name, String? parentId}) async => FileNode.fromJson(_map(await _request(
    'PATCH',
    '/api/nodes/$id',
    json: {'name': ?name, 'parentId': ?parentId},
  )));

  Future<FileNode> copyNode(String id, {String? parentId, String? name}) async =>
      FileNode.fromJson(_map(await _request('POST', '/api/nodes/$id/copy', json: {'parentId': ?parentId, 'name': ?name})));

  Future<void> trashNode(String id) => _request('DELETE', '/api/nodes/$id');

  /// Name search with optional filters; results carry their folder path.
  Future<List<FileNode>> search(
    String query, {
    String? type,
    DateTime? after,
    DateTime? before,
    int? minSize,
    int? maxSize,
    String? parentId,
    int? limit,
  }) async => nodesFromJson(_map(await _request('GET', '/api/search', query: {
    'q': query,
    'type': ?type,
    'after': ?after?.toUtc().toIso8601String(),
    'before': ?before?.toUtc().toIso8601String(),
    if (minSize != null) 'minSize': '$minSize',
    if (maxSize != null) 'maxSize': '$maxSize',
    'parentId': ?parentId,
    if (limit != null) 'limit': '$limit',
  }))['items']);

  /// Recent files, by when they were last opened (default) or `modified`.
  Future<List<FileNode>> recent({String? by}) async =>
      nodesFromJson(_map(await _request('GET', '/api/recent', query: by == null ? null : {'by': by}))['items']);

  Future<void> setFavorite(String id, bool favorite) => _request(favorite ? 'PUT' : 'DELETE', '/api/nodes/$id/favorite');

  Future<List<FileNode>> favorites() async => nodesFromJson(_map(await _request('GET', '/api/favorites'))['items']);

  /// Records that a file was opened or downloaded, for Quick Access.
  Future<void> markOpened(String id) => _request('POST', '/api/nodes/$id/opened');

  Future<StorageBreakdown> storageBreakdown() async => StorageBreakdown.fromJson(_map(await _request('GET', '/api/storage/breakdown')));

  Future<DuplicateReport> storageDuplicates() async => DuplicateReport.fromJson(_map(await _request('GET', '/api/storage/duplicates')));

  Future<List<FileNode>> trash() async => nodesFromJson(_map(await _request('GET', '/api/trash'))['items']);

  /// The change journal after [cursor]: `{changes, cursor, hasMore}`. The web
  /// app polls it when its socket is down; the mobile apps always do. A null
  /// [cursor] asks for the head only (`cursor=latest`), no changes.
  Future<Map<String, dynamic>> syncChanges(int? cursor, {int limit = 5000}) async =>
      _map(await _request('GET', '/api/sync/changes', query: {'cursor': cursor == null ? 'latest' : '$cursor', 'limit': '$limit'}));

  Future<void> restoreFromTrash(String id) => _request('POST', '/api/trash/$id/restore');

  Future<void> deleteForever(String id) => _request('DELETE', '/api/trash/$id');

  Future<void> emptyTrash() => _request('DELETE', '/api/trash');

  Future<List<FileVersion>> versions(String fileId) async => ((_map(await _request('GET', '/api/files/$fileId/versions'))['versions'] as List?) ?? const [])
      .map((e) => FileVersion.fromJson(_map(e)))
      .toList();

  Future<void> restoreVersion(String fileId, int version) => _request('POST', '/api/files/$fileId/versions/$version/restore');

  Future<Uint8List> _bytes(String path, [Map<String, String>? query]) async {
    final HttpResponseData response;
    try {
      response = await _http.send('GET', uri(path, query), headers: _authHeaders);
    } catch (_) {
      throw const ApiException(0, '', code: 'NETWORK');
    }
    if (response.statusCode == 200) return response.bodyBytes;
    if (response.statusCode == 401) onSessionExpired?.call();
    String? code;
    var message = 'HTTP ${response.statusCode}';
    try {
      final body = jsonDecode(response.body);
      if (body is Map) {
        code = body['code']?.toString();
        message = body['error']?.toString() ?? message;
      }
    } catch (_) {
      // Not JSON.
    }
    throw ApiException(response.statusCode, message, code: code);
  }

  Future<Uint8List> fileBytes(String fileId) => _bytes('/api/files/$fileId/content');

  /// A small WebP thumbnail of an image, or null when the server can't make one.
  Future<Uint8List?> thumbnailBytes(String fileId, {int size = 256}) async {
    try {
      return await _bytes('/api/files/$fileId/thumbnail', {'size': '$size'});
    } on ApiException catch (error) {
      if (error.code == 'PREVIEW_UNAVAILABLE') return null;
      rethrow;
    }
  }

  /// The PDF rendition of an Office document.
  Future<Uint8List> previewPdfBytes(String fileId) => _bytes('/api/files/$fileId/preview');

  Future<Map<String, dynamic>> capabilities() async => _map(await _request('GET', '/api/capabilities'));

  /// Inline content URL, for players that stream with Range requests.
  Uri contentUri(String fileId) => uri('/api/files/$fileId/content');

  /// What a native media player must send to be signed in; the browser sends
  /// the session cookie by itself.
  Map<String, String> get mediaHeaders {
    final cookie = _http.sessionCookie;
    return <String, String>{..._authHeaders, 'Cookie': ?cookie};
  }

  Future<FileNode> ensureFolderPath(String parentId, String path) async =>
      FileNode.fromJson(_map(await _request('POST', '/api/folders', json: {'parentId': parentId, 'path': path})));

  Uri downloadUri(String fileId) => uri('/api/files/$fileId/content', {'download': '1'});

  Uri zipUri(List<String> ids) => uri('/api/download/zip', {'ids': ids.join(',')});

  // Uploads

  /// One-request upload. [ifMatch] (with `replace`) makes the server refuse
  /// with `412 ETAG_MISMATCH` when the file changed since it was read.
  Future<FileNode> uploadSmall(String parentId, String name, Uint8List bytes, {required String conflict, String? ifMatch}) async =>
      FileNode.fromJson(_map(await _request(
        'PUT',
        '/api/upload',
        query: {'parentId': parentId, 'name': name, 'conflict': conflict},
        bytes: bytes,
        headers: ifMatch == null ? null : {'If-Match': '"$ifMatch"'},
      )));

  Future<Map<String, dynamic>> createUpload(String parentId, String name, int size, {required String conflict}) async =>
      _map(await _request('POST', '/api/uploads', json: {'parentId': parentId, 'name': name, 'size': size, 'conflict': conflict}));

  Future<void> uploadPart(String uploadId, int index, Uint8List bytes) =>
      _request('PUT', '/api/uploads/$uploadId/parts/$index', bytes: bytes);

  Future<FileNode> completeUpload(String uploadId) async =>
      FileNode.fromJson(_map(await _request('POST', '/api/uploads/$uploadId/complete', json: const {})));

  Future<void> cancelUpload(String uploadId) => _request('DELETE', '/api/uploads/$uploadId');

  /// A resumable upload's state: `partSize`, `partCount`, `receivedParts`.
  Future<Map<String, dynamic>> uploadStatus(String uploadId) async => _map(await _request('GET', '/api/uploads/$uploadId'));

  // Account & settings

  Future<Map<String, dynamic>> account() async => _map(await _request('GET', '/api/account'));

  Future<StorageUsage> usage() async => StorageUsage.fromJson(_map(await _request('GET', '/api/account/usage')));

  Future<void> setDisplayName(String name) => _request('PUT', '/api/account/display-name', json: {'displayName': name});

  Future<void> changePassword(String current, String next) =>
      _request('PUT', '/api/account/password', json: {'currentPassword': current, 'newPassword': next});

  Future<List<SecurityKeyItem>> securityKeys() async => _securityKeys(_map(await _request('GET', '/api/account/security-keys'))['securityKeys']);

  Future<Map<String, dynamic>> beginSecurityKeyRegistration() async =>
      _map(await _request('POST', '/api/account/security-keys/register/options'));

  Future<List<SecurityKeyItem>> completeSecurityKeyRegistration(Map<String, dynamic> response, String label) async => _securityKeys(
    _map(await _request('POST', '/api/account/security-keys/register/verify', json: {'response': response, 'label': label}))['credentials'],
  );

  Future<List<SecurityKeyItem>> renameSecurityKey(int id, String label) async =>
      _securityKeys(_map(await _request('PUT', '/api/account/security-keys/$id', json: {'label': label}))['credentials']);

  Future<List<SecurityKeyItem>> deleteSecurityKey(int id) async =>
      _securityKeys(_map(await _request('DELETE', '/api/account/security-keys/$id'))['credentials']);

  List<SecurityKeyItem> _securityKeys(Object? rows) =>
      ((rows as List?) ?? const []).map((row) => SecurityKeyItem.fromJson(_map(row))).toList();

  Future<Map<String, dynamic>> beginTwoFactor(String password) async =>
      _map(await _request('POST', '/api/account/2fa/setup', json: {'currentPassword': password}));

  Future<List<String>> enableTwoFactor(String code) async =>
      ((_map(await _request('POST', '/api/account/2fa/enable', json: {'code': code}))['recoveryCodes'] as List?) ?? const [])
          .map((e) => '$e')
          .toList();

  Future<void> disableTwoFactor(String password, String code) =>
      _request('POST', '/api/account/2fa/disable', json: {'currentPassword': password, 'code': code});

  Future<List<SessionInfo>> sessions() async => ((_map(await _request('GET', '/api/account/sessions'))['sessions'] as List?) ?? const [])
      .map((e) => SessionInfo.fromJson(_map(e)))
      .toList();

  Future<void> revokeSession(int id) => _request('DELETE', '/api/account/sessions/$id');

  Future<List<DeviceInfo>> devices() async => ((_map(await _request('GET', '/api/account/devices'))['devices'] as List?) ?? const [])
      .map((e) => DeviceInfo.fromJson(_map(e)))
      .toList();

  Future<String> createDevice(String name) async =>
      _map(await _request('POST', '/api/account/devices', json: {'name': name}))['token'].toString();

  Future<void> revokeDevice(int id) => _request('DELETE', '/api/account/devices/$id');

  /// The device waiting for approval under [userCode] (docs/clients.md §1).
  Future<DeviceAuthorizationRequest> deviceAuthorization(String userCode) async => DeviceAuthorizationRequest.fromJson(
    _map(await _request('GET', '/api/account/devices/authorize/${Uri.encodeComponent(userCode)}')),
  );

  Future<void> answerDeviceAuthorization(String userCode, {required bool approve}) =>
      _request('POST', '/api/account/devices/authorize/${Uri.encodeComponent(userCode)}', json: {'approve': approve});

  /// What a bearer token belongs to: the device, its account and the quota.
  Future<Map<String, dynamic>> deviceMe() async => _map(await _request('GET', '/api/devices/me'));

  // Share links

  /// The account's links; with [nodeId], only that node's active ones.
  Future<List<ShareLink>> shares({String? nodeId}) async => ((_map(await _request(
    'GET',
    '/api/shares',
    query: nodeId == null ? null : {'nodeId': nodeId},
  ))['shares'] as List?) ?? const [])
      .map((e) => ShareLink.fromJson(_map(e)))
      .toList();

  Future<ShareLink> createShare(
    String nodeId, {
    DateTime? expiresAt,
    String? password,
    ShareMode mode = ShareMode.view,
    int? maxDownloads,
    String? note,
  }) async {
    final body = _map(await _request('POST', '/api/shares', json: {
      'nodeId': nodeId,
      'expiresAt': ?expiresAt?.toUtc().toIso8601String(),
      'password': ?password,
      'mode': mode.apiName,
      'maxDownloads': ?maxDownloads,
      'note': ?note,
    }));
    return ShareLink.fromJson(body['share'] is Map ? _map(body['share']) : body);
  }

  /// Changes a link and keeps its URL. [patch] holds only what changes;
  /// a null `expiresAt`, `password` or `maxDownloads` clears it.
  Future<ShareLink> updateShare(String id, Map<String, Object?> patch) async {
    final body = _map(await _request('PATCH', '/api/shares/$id', json: patch));
    return ShareLink.fromJson(body['share'] is Map ? _map(body['share']) : body);
  }

  Future<void> deleteShare(String id) => _request('DELETE', '/api/shares/$id');

  // Vaults

  Future<FileNode> createVault({
    required String parentId,
    required String name,
    required String salt,
    required int iterations,
    required String wrappedKey,
  }) async {
    final body = _map(await _request('POST', '/api/vaults', json: {
      'parentId': parentId,
      'name': name,
      'salt': salt,
      'iterations': iterations,
      'wrappedKey': wrappedKey,
    }));
    return FileNode.fromJson(body['folder'] is Map ? _map(body['folder']) : body);
  }

  Future<VaultKeyInfo> vault(String vaultId) async => VaultKeyInfo.fromJson(_map(await _request('GET', '/api/vaults/$vaultId')));

  /// Stores the vault key wrapped anew (new passphrase or more iterations).
  Future<void> updateVaultKey(String vaultId, {required String salt, required int iterations, required String wrappedKey}) =>
      _request('PUT', '/api/vaults/$vaultId/key', json: {'salt': salt, 'iterations': iterations, 'wrappedKey': wrappedKey});

  Future<UserSettings> settings() async => UserSettings.fromJson(_map(_map(await _request('GET', '/api/settings'))['settings']));

  Future<UserSettings> updateSettings(Map<String, Object?> patch) async =>
      UserSettings.fromJson(_map(_map(await _request('PUT', '/api/settings', json: patch))['settings']));

  // Admin

  Future<List<AdminUser>> adminUsers([String? query]) async => ((_map(await _request(
    'GET',
    '/api/admin/users',
    query: query == null || query.isEmpty ? null : {'q': query},
  ))['users'] as List?) ?? const [])
      .map((e) => AdminUser.fromJson(_map(e)))
      .toList();

  Future<void> adminSetQuota(int userId, String mode, {String? bytes}) =>
      _request('PUT', '/api/admin/users/$userId/quota', json: {'mode': mode, 'bytes': ?bytes});

  Future<void> adminSetAdmin(int userId, bool isAdmin) => _request('PUT', '/api/admin/users/$userId/admin', json: {'isAdmin': isAdmin});

  Future<void> adminResetPassword(int userId, String password) =>
      _request('POST', '/api/admin/users/$userId/password', json: {'password': password});

  Future<void> adminDeleteUser(int userId) => _request('DELETE', '/api/admin/users/$userId');

  Future<Map<String, dynamic>> adminStorageConfig() async => _map(await _request('GET', '/api/admin/config/storage'));

  Future<Map<String, dynamic>> adminUpdateStorageConfig(Map<String, Object?> body) async =>
      _map(await _request('PUT', '/api/admin/config/storage', json: body));

  Future<void> adminRecalculate() => _request('POST', '/api/admin/storage/recalculate');

  Future<bool> adminAllowSignup() async => _map(await _request('GET', '/api/admin/access'))['allowSignup'] == true;

  Future<void> adminSetAllowSignup(bool allow) => _request('PUT', '/api/admin/access/signup', json: {'allowSignup': allow});

  Future<Map<String, dynamic>> adminHealth() async => _map(await _request('GET', '/api/admin/health'));

  Future<List<Map<String, dynamic>>> adminLogs() async =>
      ((_map(await _request('GET', '/api/admin/logs'))['logs'] as List?) ?? const []).map(_map).toList();

  Future<Map<String, dynamic>> adminBackups() async => _map(await _request('GET', '/api/admin/backups'));

  Future<void> adminCreateBackupTarget(Map<String, Object?> body) => _request('POST', '/api/admin/backups/targets', json: body);

  Future<void> adminUpdateBackupTarget(int id, Map<String, Object?> body) =>
      _request('PUT', '/api/admin/backups/targets/$id', json: body);

  Future<void> adminDeleteBackupTarget(int id) => _request('DELETE', '/api/admin/backups/targets/$id');

  Future<void> adminTestBackupTarget(int id) => _request('POST', '/api/admin/backups/targets/$id/test');

  Future<void> adminRunBackup(int id) => _request('POST', '/api/admin/backups/targets/$id/run');

  /// Restores the target's latest snapshot to a scratch location and checks
  /// it; the result shows as the target's `lastVerification`.
  Future<Map<String, dynamic>> adminVerifyBackup(int id) async =>
      _map(await _request('POST', '/api/admin/backups/targets/$id/verify', json: const {}));

  Future<String> adminMasterKey(String password) async =>
      _map(await _request('POST', '/api/admin/keys/master', json: {'password': password}))['masterKey'].toString();
}
