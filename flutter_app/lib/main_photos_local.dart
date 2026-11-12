part of 'main.dart';

// A photo or video of the phone itself, opened in NeoCloud as the phone's
// gallery (Android's "Open with"). It is a local file: it shows without an
// account, and "Back up to NeoCloud" sends it to the camera backup folder
// unless NeoCloud already has the same content.

enum _LocalBackup { signedOut, checking, notBackedUp, backingUp, backedUp, failed }

Future<void> showLocalMediaViewer(BuildContext context, NeoCloudController controller, GalleryLaunch gallery, LocalMedia media) {
  return Navigator.of(context).push(MaterialPageRoute<void>(
    builder: (_) => _LocalMediaViewer(controller: controller, gallery: gallery, media: media),
  ));
}

class _LocalMediaViewer extends StatefulWidget {
  const _LocalMediaViewer({required this.controller, required this.gallery, required this.media});

  final NeoCloudController controller;
  final GalleryLaunch gallery;
  final LocalMedia media;

  @override
  State<_LocalMediaViewer> createState() => _LocalMediaViewerState();
}

class _LocalMediaViewerState extends State<_LocalMediaViewer> {
  late final Future<Uint8List?> _photo = widget.media.isVideo ? Future<Uint8List?>.value() : widget.gallery.read(widget.media);
  VideoPlayerController? _video;
  bool _videoFailed = false;
  _LocalBackup _backup = _LocalBackup.checking;
  (String, int)? _fingerprint;
  UploadSource? _source;

  NeoCloudController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    if (widget.media.isVideo) {
      final player = VideoPlayerController.contentUri(Uri.parse(widget.media.uri));
      _video = player;
      player.addListener(_tick);
      player.initialize().then((_) {
        if (mounted) unawaited(player.play());
      }).catchError((Object _) {
        if (mounted) setState(() => _videoFailed = true);
      });
    }
    controller.uploads.addListener(_uploadsChanged);
    unawaited(_check());
  }

  @override
  void dispose() {
    controller.uploads.removeListener(_uploadsChanged);
    _video?.removeListener(_tick);
    unawaited(_video?.dispose());
    super.dispose();
  }

  void _tick() {
    if (mounted) setState(() {});
  }

  /// Whether NeoCloud has the same content anywhere already.
  Future<void> _check() async {
    if (!controller.isAuthenticated) {
      setState(() => _backup = _LocalBackup.signedOut);
      return;
    }
    final fingerprint = _fingerprint ?? await widget.gallery.fingerprint(widget.media);
    if (!mounted) return;
    _fingerprint = fingerprint;
    if (fingerprint == null) {
      setState(() => _backup = _LocalBackup.notBackedUp);
      return;
    }
    try {
      final exists = await controller.client.uploadContentExists('root', fingerprint.$1, fingerprint.$2);
      if (mounted) setState(() => _backup = exists ? _LocalBackup.backedUp : _LocalBackup.notBackedUp);
    } catch (_) {
      if (mounted) setState(() => _backup = _LocalBackup.notBackedUp);
    }
  }

  Future<void> _backUp() async {
    final camera = controller.cameraUploads.settings;
    var folderId = camera.folderId;
    if (folderId == null) {
      final picked = await showFolderPicker(context, controller, title: appStrings.photosLocalBackUp, confirmLabel: appStrings.uploadHere, hideVaults: true);
      if (picked == null || !mounted) return;
      folderId = picked.$1;
    }
    setState(() => _backup = _LocalBackup.backingUp);
    final source = await widget.gallery.uploadSource(widget.media);
    if (!mounted) return;
    if (source == null) {
      setState(() => _backup = _LocalBackup.failed);
      return;
    }
    _source = source;
    controller.uploads.enqueue(folderId, <UploadSource>[source]);
  }

  void _uploadsChanged() {
    final source = _source;
    if (source == null || !mounted) return;
    final task = controller.uploads.tasks.where((task) => identical(task.source, source)).firstOrNull;
    if (task == null || !task.finished) return;
    _source = null;
    setState(() => _backup = task.status == UploadStatus.failed || task.status == UploadStatus.cancelled ? _LocalBackup.failed : _LocalBackup.backedUp);
    if (task.status != UploadStatus.failed) controller.notifyFilesChanged();
  }

  void _close() {
    widget.gallery.doneViewing();
    if (widget.media.cold) {
      // Started just for this file: back to the app that asked.
      unawaited(SystemNavigator.pop());
      return;
    }
    Navigator.of(context).maybePop();
  }

  Widget _content() {
    final video = _video;
    if (video != null) {
      if (_videoFailed || video.value.hasError) {
        return Center(child: Text(appStrings.mediaUnavailable, style: const TextStyle(color: Colors.white70)));
      }
      if (!video.value.isInitialized) return const Center(child: CircularProgressIndicator(strokeWidth: 2));
      return GestureDetector(
        onTap: () => video.value.isPlaying ? video.pause() : video.play(),
        child: Center(child: AspectRatio(aspectRatio: video.value.aspectRatio, child: VideoPlayer(video))),
      );
    }
    return FutureBuilder<Uint8List?>(
      future: _photo,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) return const Center(child: CircularProgressIndicator(strokeWidth: 2));
        final bytes = snapshot.data;
        if (bytes == null) return Center(child: Text(appStrings.photosPhotoUnavailable, style: const TextStyle(color: Colors.white70)));
        return InteractiveViewer(maxScale: 6, child: SizedBox.expand(child: Image.memory(bytes, fit: BoxFit.contain)));
      },
    );
  }

  Widget _backupBar() {
    final Widget child;
    switch (_backup) {
      case _LocalBackup.signedOut:
        child = Text(appStrings.photosLocalSignIn, textAlign: TextAlign.center, style: const TextStyle(color: Colors.white70));
      case _LocalBackup.checking:
        child = Text(appStrings.photosLocalChecking, style: const TextStyle(color: Colors.white70));
      case _LocalBackup.backingUp:
        child = Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const SizedBox(width: 14, height: 14, child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 10),
            Text(appStrings.photosLocalBackingUp, style: const TextStyle(color: Colors.white70)),
          ],
        );
      case _LocalBackup.backedUp:
        child = Row(
          key: const ValueKey<String>('local-backed-up'),
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(Icons.cloud_done_outlined, color: _success, size: 18),
            const SizedBox(width: 8),
            Text(appStrings.photosLocalBackedUp, style: const TextStyle(color: Colors.white)),
          ],
        );
      case _LocalBackup.notBackedUp:
      case _LocalBackup.failed:
        child = Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            if (_backup == _LocalBackup.failed)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Text(appStrings.photosLocalBackupFailed, style: const TextStyle(color: Colors.white70)),
              ),
            FilledButton.icon(
              key: const ValueKey<String>('local-back-up'),
              onPressed: () => unawaited(_backUp()),
              icon: const Icon(Icons.cloud_upload_outlined, size: 18),
              label: Text(appStrings.photosLocalBackUp),
            ),
          ],
        );
    }
    return Padding(padding: const EdgeInsets.fromLTRB(16, 8, 16, 12), child: Center(child: child));
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: !widget.media.cold,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) {
          widget.gallery.doneViewing();
        } else {
          _close();
        }
      },
      child: Scaffold(
        backgroundColor: Colors.black,
        body: Column(
          children: <Widget>[
            SafeArea(
              bottom: false,
              child: Row(
                children: <Widget>[
                  IconButton(tooltip: appStrings.close, onPressed: _close, icon: const Icon(Icons.arrow_back_rounded, color: Colors.white)),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      mainAxisSize: MainAxisSize.min,
                      children: <Widget>[
                        Text(widget.media.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(color: Colors.white, fontSize: 14, fontWeight: FontWeight.w600)),
                        Text(appStrings.photosLocalOnPhone, style: const TextStyle(color: Colors.white70, fontSize: 12)),
                      ],
                    ),
                  ),
                ],
              ),
            ),
            Expanded(child: _content()),
            SafeArea(top: false, child: _backupBar()),
          ],
        ),
      ),
    );
  }
}
