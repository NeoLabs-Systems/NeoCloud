part of 'backend_client.dart';

/// The calls for QR pairing, WebDAV app passwords, imports and full-text
/// search (docs/clients.md §5–8), kept beside [BackendClient] so that file
/// stays readable.
extension BackendFeatureCalls on BackendClient {
  // QR pairing (§5)

  Future<PairingTicket> startPairing() async =>
      PairingTicket.fromJson(_map(await _request('POST', '/api/account/devices/pairing', json: const <String, Object?>{})));

  Future<PairingStatus> pairingStatus(String code) async =>
      PairingStatus.fromJson(_map(await _request('GET', '/api/account/devices/pairing/${Uri.encodeComponent(code)}')));

  // WebDAV (§6)

  /// An app password: a device token with platform `webdav`, shown once.
  Future<String> createAppPassword(String name) async =>
      _map(await _request('POST', '/api/account/devices', json: {'name': name, 'platform': 'webdav'}))['token'].toString();

  /// Where WebDAV apps connect for [username]: the Nextcloud-compatible
  /// mount, which apps that know Nextcloud recognise.
  Uri webDavUri(String username) => uri('/remote.php/dav/files/${Uri.encodeComponent(username)}/');

  // Imports (§7)

  Future<ImportProbe> testImport(Map<String, Object?> body) async =>
      ImportProbe.fromJson(_map(await _request('POST', '/api/imports/test', json: body)));

  Future<ImportJob> startImport(Map<String, Object?> body) async {
    final json = _map(await _request('POST', '/api/imports', json: body));
    return ImportJob.fromJson(json['job'] is Map ? _map(json['job']) : json);
  }

  Future<List<ImportJob>> imports() async =>
      ((_map(await _request('GET', '/api/imports'))['items'] as List?) ?? const []).map((e) => ImportJob.fromJson(_map(e))).toList();

  Future<void> cancelImport(String id) => _request('POST', '/api/imports/${Uri.encodeComponent(id)}/cancel', json: const <String, Object?>{});

  Future<void> pauseImport(String id) => _request('POST', '/api/imports/${Uri.encodeComponent(id)}/pause', json: const <String, Object?>{});

  /// Continues a paused, failed or cancelled job. A job that ended no
  /// longer holds the password: the server answers `PASSWORD_REQUIRED`
  /// until it comes again as [password].
  Future<void> resumeImport(String id, {String? password}) =>
      _request('POST', '/api/imports/${Uri.encodeComponent(id)}/resume', json: <String, Object?>{'password': ?password});

  Future<void> deleteImport(String id) => _request('DELETE', '/api/imports/${Uri.encodeComponent(id)}');

  // Full-text search (§8)

  /// Like [BackendClient.search], with each hit's match kind and snippet.
  /// [content] asks for content matches too.
  Future<List<SearchHit>> searchHits(
    String query, {
    required bool content,
    String? type,
    DateTime? after,
    int? minSize,
    String? parentId,
    int? limit,
  }) async {
    final body = _map(await _request('GET', '/api/search', query: {
      'q': query,
      if (content) 'content': '1',
      'type': ?type,
      'after': ?after?.toUtc().toIso8601String(),
      if (minSize != null) 'minSize': '$minSize',
      'parentId': ?parentId,
      if (limit != null) 'limit': '$limit',
    }));
    return ((body['items'] as List?) ?? const []).map((e) => SearchHit.fromJson(_map(e))).toList();
  }

  Future<SearchIndexStatus> adminSearchIndex() async => SearchIndexStatus.fromJson(_map(await _request('GET', '/api/admin/search-index')));

  Future<void> adminRebuildSearchIndex() => _request('POST', '/api/admin/search-index/rebuild', json: const <String, Object?>{});

  /// Turns full-text search on or off for the whole server.
  Future<void> adminSetFullTextSearch(bool enabled) => _request('PUT', '/api/admin/config', json: {'fullTextSearch': enabled});
}
