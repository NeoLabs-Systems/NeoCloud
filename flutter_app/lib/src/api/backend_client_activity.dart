part of 'backend_client.dart';

/// The activity feed and comments on files (docs/api.md).
extension BackendActivityCalls on BackendClient {
  /// One page of the feed, newest first. [nodeId] limits it to a file or
  /// folder; [cursor] is the previous page's `nextCursor`.
  Future<ActivityPage> activity({ActivityScope scope = ActivityScope.all, String? nodeId, int? cursor}) async => ActivityPage.fromJson(
    _map(await _request('GET', '/api/activity', query: <String, String>{
      'scope': scope.name,
      'nodeId': ?nodeId,
      'cursor': ?cursor?.toString(),
    })),
  );

  Future<CommentPage> comments(String nodeId, {int? cursor}) async => CommentPage.fromJson(
    _map(await _request('GET', '/api/files/$nodeId/comments', query: <String, String>{'cursor': ?cursor?.toString()})),
  );

  Future<NodeComment> addComment(String nodeId, String body) async =>
      NodeComment.fromJson(_map(await _request('POST', '/api/files/$nodeId/comments', json: <String, Object?>{'body': body})));

  Future<NodeComment> editComment(String nodeId, int commentId, String body) async => NodeComment.fromJson(
    _map(await _request('PATCH', '/api/files/$nodeId/comments/$commentId', json: <String, Object?>{'body': body})),
  );

  Future<void> deleteComment(String nodeId, int commentId) => _request('DELETE', '/api/files/$nodeId/comments/$commentId');
}
