part of 'backend_client.dart';

/// The Photos timeline (docs/api.md, Photos).
extension BackendPhotoCalls on BackendClient {
  /// A page of photos, newest first: after [cursor], or from the newest
  /// photo of [day] (`YYYY-MM-DD`, or `unknown` for the photos without a date).
  Future<PhotoPage> photos({String? cursor, String? day, int limit = 200}) async => PhotoPage.fromJson(_map(await _request(
    'GET',
    '/api/photos',
    query: <String, String>{'cursor': ?cursor, 'day': ?day, 'limit': '$limit'},
  )));

  Future<PhotoSummary> photoSummary() async => PhotoSummary.fromJson(_map(await _request('GET', '/api/photos/summary')));

  /// The grid thumbnail: a square WebP, or null when the server makes none.
  /// The URL names the content (`v=<etag>`), so caches may keep it for good.
  Future<Uint8List?> photoThumbnail(String id, String etag, {bool large = false}) async {
    try {
      return await _bytes('/api/files/$id/thumbnail', <String, String>{'size': large ? '1024' : 'square', 'v': etag});
    } on ApiException catch (error) {
      if (error.code == 'PREVIEW_UNAVAILABLE' || error.statusCode == 404) return null;
      rethrow;
    }
  }
}
