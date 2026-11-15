part of 'backend_client.dart';

/// The calls for cloud storage the account connected itself (docs/api.md,
/// "Cloud storage"). Paths are relative to a connection's root.
extension BackendExternalStorageCalls on BackendClient {
  static const String _base = '/api/external-storage';

  Future<List<ExternalMount>> externalMounts() async =>
      ((_map(await _request('GET', _base))['items'] as List?) ?? const <Object?>[]).map((e) => ExternalMount.fromJson(_map(e))).toList();

  Future<ExternalTest> testExternalMount(Map<String, Object?> body) async =>
      ExternalTest.fromJson(_map(await _request('POST', '$_base/test', json: body)));

  Future<ExternalMount> connectExternalMount(Map<String, Object?> body) async =>
      ExternalMount.fromJson(_map(await _request('POST', _base, json: body)));

  Future<ExternalMount> updateExternalMount(String id, Map<String, Object?> body) async =>
      ExternalMount.fromJson(_map(await _request('PATCH', '$_base/${Uri.encodeComponent(id)}', json: body)));

  Future<void> disconnectExternalMount(String id) => _request('DELETE', '$_base/${Uri.encodeComponent(id)}');

  Future<ExternalListing> listExternal(String id, String path) async =>
      ExternalListing.fromJson(_map(await _request('GET', '$_base/${Uri.encodeComponent(id)}/list', query: {'path': path})));

  /// Where a file can be opened or downloaded from (the browser or a player
  /// carries the session itself).
  Uri externalFileUri(String id, String path, {bool download = false}) =>
      uri('$_base/${Uri.encodeComponent(id)}/file', {'path': path, if (download) 'download': '1'});

  Future<void> uploadExternal(String id, String path, Uint8List bytes, {String conflict = 'rename'}) =>
      _request('PUT', '$_base/${Uri.encodeComponent(id)}/file', query: {'path': path, 'conflict': conflict}, bytes: bytes);

  Future<void> createExternalFolder(String id, String path) =>
      _request('POST', '$_base/${Uri.encodeComponent(id)}/folder', json: {'path': path});

  Future<void> moveExternal(String id, String from, String to) =>
      _request('POST', '$_base/${Uri.encodeComponent(id)}/move', json: {'from': from, 'to': to});

  Future<void> deleteExternal(String id, String path) =>
      _request('DELETE', '$_base/${Uri.encodeComponent(id)}/entry', query: {'path': path});

  Future<void> copyToExternal(String id, {required String nodeId, required String folder}) =>
      _request('POST', '$_base/${Uri.encodeComponent(id)}/copy-in', json: {'nodeId': nodeId, 'folder': folder});

  Future<void> copyFromExternal(String id, {required String path, required String parentId}) =>
      _request('POST', '$_base/${Uri.encodeComponent(id)}/copy-out', json: {'path': path, 'parentId': parentId});
}
