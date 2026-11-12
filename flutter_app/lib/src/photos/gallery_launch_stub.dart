import 'package:flutter/foundation.dart';

import '../files/upload_queue.dart';
import 'gallery_launch.dart';

GalleryLaunch createGalleryLaunch() => NoGalleryLaunch();

/// The web and iOS: no app can be the gallery there.
class NoGalleryLaunch implements GalleryLaunch {
  final ValueNotifier<int> _opens = ValueNotifier<int>(0);
  final ValueNotifier<LocalMedia?> _viewing = ValueNotifier<LocalMedia?>(null);

  @override
  bool get supported => false;

  @override
  ValueListenable<int> get galleryOpens => _opens;

  @override
  ValueListenable<LocalMedia?> get viewing => _viewing;

  @override
  Future<void> start() async {}

  @override
  void doneViewing() {}

  @override
  Future<Uint8List?> read(LocalMedia media) async => null;

  @override
  Future<(String, int)?> fingerprint(LocalMedia media) async => null;

  @override
  Future<UploadSource?> uploadSource(LocalMedia media) async => null;
}
