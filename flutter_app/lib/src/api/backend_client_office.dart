part of 'backend_client.dart';

/// Document editing (docs/api.md, "Document editing").
extension BackendOfficeCalls on BackendClient {
  /// `{enabled, extensions}`: what the editor opens, if it is on.
  Future<Map<String, dynamic>> officeConfig() async => _map(await _request('GET', '/api/office/config'));

  /// `{editorUrl, accessToken, accessTokenTtl, canWrite}` for one file.
  Future<Map<String, dynamic>> officeEditSession(String nodeId) async =>
      _map(await _request('POST', '/api/files/$nodeId/edit-session', json: const <String, Object?>{}));

  /// Admin: `{enabled, url, hostUrl, status}`.
  Future<Map<String, dynamic>> adminOfficeConfig() async => _map(await _request('GET', '/api/admin/config/office'));

  Future<Map<String, dynamic>> adminSaveOfficeConfig(Map<String, Object?> body) async =>
      _map(await _request('PUT', '/api/admin/config/office', json: body));

  Future<Map<String, dynamic>> adminCheckOffice() async =>
      _map(await _request('POST', '/api/admin/config/office/check', json: const <String, Object?>{}));
}
