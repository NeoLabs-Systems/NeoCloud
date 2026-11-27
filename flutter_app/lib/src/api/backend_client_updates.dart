part of 'backend_client.dart';

/// Server updates, administrators only. Every call answers the update status.
extension BackendUpdateCalls on BackendClient {
  Future<Map<String, dynamic>> adminUpdateStatus() async =>
      _map(await _request('GET', '/api/admin/update'));

  Future<Map<String, dynamic>> adminCheckForUpdate() async => _map(
    await _request('POST', '/api/admin/update/check', json: const {}),
  );

  Future<Map<String, dynamic>> adminStartUpdate() async =>
      _map(await _request('POST', '/api/admin/update', json: const {}));

  Future<Map<String, dynamic>> adminSetUpdateChannel(String channel) async =>
      _map(
        await _request(
          'PUT',
          '/api/admin/update/channel',
          json: {'channel': channel},
        ),
      );
}
