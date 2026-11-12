part of 'backend_client.dart';

/// Sharing with other accounts on the server (docs/api.md, "Sharing with
/// accounts"). Members are added by an exact email address or username only;
/// the server offers no way to look accounts up, and neither does the app.
extension BackendMemberCalls on BackendClient {
  Future<List<ShareMember>> members(String nodeId) async =>
      ((_map(await _request('GET', '/api/nodes/$nodeId/members'))['members'] as List?) ?? const <dynamic>[])
          .map((item) => ShareMember.fromJson((item as Map).cast<String, dynamic>()))
          .toList();

  /// Shares [nodeId] with the account [recipient] names. An unknown name
  /// answers 404 RECIPIENT_NOT_FOUND, the same as for any account that can't
  /// receive it.
  Future<ShareMember> addMember(String nodeId, String recipient, MemberPermission permission) async => ShareMember.fromJson(
    _map(await _request('POST', '/api/nodes/$nodeId/members', json: {'recipient': recipient, 'permission': permission.name})),
  );

  Future<ShareMember> updateMember(String nodeId, int memberId, MemberPermission permission) async => ShareMember.fromJson(
    _map(await _request('PATCH', '/api/nodes/$nodeId/members/$memberId', json: {'permission': permission.name})),
  );

  Future<void> removeMember(String nodeId, int memberId) => _request('DELETE', '/api/nodes/$nodeId/members/$memberId');

  Future<List<SharedWithMe>> sharedWithMe() async =>
      ((_map(await _request('GET', '/api/shared-with-me'))['shares'] as List?) ?? const <dynamic>[])
          .map((item) => SharedWithMe.fromJson((item as Map).cast<String, dynamic>()))
          .toList();

  Future<void> leaveShare(int shareId) => _request('DELETE', '/api/shared-with-me/$shareId');
}
