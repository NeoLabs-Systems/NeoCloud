import '../api/backend_client.dart';
import 'photo_models.dart';
import 'photo_thumbnails.dart';
import 'photo_timeline.dart';

/// The server as a [PhotoSource].
class ServerPhotoSource implements PhotoSource {
  const ServerPhotoSource(this.client);

  final BackendClient client;

  @override
  Future<PhotoSummary> summary() => client.photoSummary();

  @override
  Future<PhotoPage> page({String? cursor, String? day, int limit = 200}) => client.photos(cursor: cursor, day: day, limit: limit);
}

/// Everything the Photos tab keeps while the app runs: the timeline (with
/// where it was scrolled to) and the thumbnails, for the signed-in account.
/// [source] and [fetchThumbnail] replace the server in tests.
class PhotoLibrary {
  PhotoLibrary(BackendClient client, {PhotoSource? source, ThumbnailFetch? fetchThumbnail, this.keepOnDisk = true})
    : timeline = PhotoTimeline(source ?? ServerPhotoSource(client)),
      _fetch = fetchThumbnail ?? client.photoThumbnail {
    _thumbnails = PhotoThumbnailCache(fetch: _fetch);
  }

  final PhotoTimeline timeline;
  final ThumbnailFetch _fetch;

  /// Thumbnails also go to the app's cache folder (phones).
  final bool keepOnDisk;
  late PhotoThumbnailCache _thumbnails;
  String? _owner;

  PhotoThumbnailCache get thumbnails => _thumbnails;

  /// Keeps everything for the same account on the same server; starts over
  /// (memory and the thumbnail folder) for another.
  void attach(String owner) {
    if (_owner == owner) return;
    _owner = owner;
    _thumbnails = PhotoThumbnailCache(fetch: _fetch, store: keepOnDisk ? createThumbnailStore(owner) : null);
    timeline.attach(owner);
  }
}
