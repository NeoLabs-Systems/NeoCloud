import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../files/upload_queue.dart';
import 'gallery_launch.dart';
import 'gallery_launch_stub.dart';

// The Android side is GalleryIntents.kt next to MainActivity. iOS lets no
// app replace Photos, so nothing arrives there.

GalleryLaunch createGalleryLaunch() => Platform.isAndroid ? AndroidGalleryLaunch() : NoGalleryLaunch();

const MethodChannel _channel = MethodChannel('systems.neolabs.neocloud/gallery');

class AndroidGalleryLaunch implements GalleryLaunch {
  final ValueNotifier<int> _opens = ValueNotifier<int>(0);
  final ValueNotifier<LocalMedia?> _viewing = ValueNotifier<LocalMedia?>(null);
  bool _started = false;

  @override
  bool get supported => true;

  @override
  ValueListenable<int> get galleryOpens => _opens;

  @override
  ValueListenable<LocalMedia?> get viewing => _viewing;

  @override
  Future<void> start() async {
    if (_started) return;
    _started = true;
    _channel.setMethodCallHandler((call) async {
      if (call.method == 'event') _receive(call.arguments);
    });
    try {
      final waiting = await _channel.invokeListMethod<Object?>('start');
      for (final event in waiting ?? const <Object?>[]) {
        _receive(event);
      }
    } on PlatformException {
      // An older build without the channel.
    } on MissingPluginException {
      // Tests and engines without MainActivity.
    }
  }

  void _receive(Object? raw) {
    if (raw is! Map) return;
    switch (raw['type']) {
      case 'gallery':
        _opens.value += 1;
      case 'view':
        _viewing.value = LocalMedia(
          uri: '${raw['uri']}',
          name: '${raw['name'] ?? ''}',
          mime: '${raw['mime'] ?? ''}',
          size: (raw['size'] as num?)?.toInt() ?? -1,
          cold: raw['cold'] == true,
        );
    }
  }

  @override
  void doneViewing() => _viewing.value = null;

  @override
  Future<Uint8List?> read(LocalMedia media) async {
    try {
      return await _channel.invokeMethod<Uint8List>('read', <String, Object?>{'uri': media.uri});
    } on PlatformException {
      return null;
    }
  }

  @override
  Future<(String, int)?> fingerprint(LocalMedia media) async {
    try {
      final result = await _channel.invokeMapMethod<String, Object?>('hash', <String, Object?>{'uri': media.uri});
      final hash = result?['sha256'];
      final size = (result?['size'] as num?)?.toInt();
      return hash is String && size != null ? (hash, size) : null;
    } on PlatformException {
      return null;
    }
  }

  @override
  Future<UploadSource?> uploadSource(LocalMedia media) async {
    try {
      final path = await _channel.invokeMethod<String>('copy', <String, Object?>{'uri': media.uri, 'name': media.name});
      if (path == null) return null;
      final file = File(path);
      return UploadSource(name: file.uri.pathSegments.last, size: file.lengthSync(), openRead: file.openRead);
    } on PlatformException {
      return null;
    }
  }
}
