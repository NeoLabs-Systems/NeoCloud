import 'package:flutter/foundation.dart';

import '../files/upload_queue.dart';
import 'gallery_launch_stub.dart' if (dart.library.io) 'gallery_launch_io.dart' as platform;

/// A photo or video of the phone the system asked NeoCloud to show: it is
/// a local file, not necessarily in NeoCloud.
@immutable
class LocalMedia {
  const LocalMedia({required this.uri, required this.name, required this.mime, this.size = -1, this.cold = false});

  final String uri;
  final String name;
  final String mime;

  /// -1 when the other app didn't say.
  final int size;

  /// The app was started just for this: closing the viewer leaves the app.
  final bool cold;

  bool get isVideo => mime.startsWith('video/');
}

/// NeoCloud as the phone's gallery app (Android): the system opens it as
/// the gallery ([galleryOpens] ticks; the app shows Photos) or to show one
/// photo or video ([viewing]). Elsewhere nothing ever arrives.
abstract class GalleryLaunch {
  bool get supported;

  ValueListenable<int> get galleryOpens;

  ValueListenable<LocalMedia?> get viewing;

  Future<void> start();

  /// The viewer for [viewing] closed.
  void doneViewing();

  /// The whole file, for showing a photo; null when it can't be read.
  Future<Uint8List?> read(LocalMedia media);

  /// `(sha256, size)` of the content, for "is it backed up?".
  Future<(String, int)?> fingerprint(LocalMedia media);

  /// A copy the upload queue can send.
  Future<UploadSource?> uploadSource(LocalMedia media);
}

GalleryLaunch createGalleryLaunch() => platform.createGalleryLaunch();
