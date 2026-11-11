part of 'main.dart';

// What the mobile apps add around the app: files shared from other apps, and
// asking again about camera upload after a sign-out or a change of server
// or account. On the web this is a no-op.

class _NativeRuntime extends StatefulWidget {
  const _NativeRuntime({required this.controller, required this.child});

  final NeoCloudController controller;
  final Widget child;

  @override
  State<_NativeRuntime> createState() => _NativeRuntimeState();
}

class _NativeRuntimeState extends State<_NativeRuntime> {
  bool _pickingTarget = false;
  bool _askingCamera = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.shareIntake.pending.addListener(_sharedChanged);
    _controller.addListener(_sharedChanged);
    _controller.cameraUploads.addListener(_cameraChanged);
    _controller.addListener(_cameraChanged);
  }

  @override
  void dispose() {
    _controller.shareIntake.pending.removeListener(_sharedChanged);
    _controller.removeListener(_sharedChanged);
    _controller.cameraUploads.removeListener(_cameraChanged);
    _controller.removeListener(_cameraChanged);
    super.dispose();
  }

  void _cameraChanged() => unawaited(_askAboutCamera());

  /// Camera upload went off with a sign-out or a change of server or
  /// account: ask once whether to set it up for this account.
  Future<void> _askAboutCamera() async {
    final camera = _controller.cameraUploads;
    if (!camera.needsConfirmation || _askingCamera || !_controller.isAuthenticated) return;
    final navigatorContext = _navigatorKey.currentContext;
    if (navigatorContext == null) return;
    _askingCamera = true;
    try {
      final setUp = await showDialog<bool>(
        context: navigatorContext,
        builder: (dialogContext) => AlertDialog(
          title: Text(appStrings.cameraReconfirmTitle),
          content: SizedBox(
            width: 440,
            child: Text(
              appStrings.cameraReconfirmMessage(_controller.connectedServer),
              style: TextStyle(color: _textSecondary, height: 1.45),
            ),
          ),
          actions: <Widget>[
            TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cameraReconfirmLater)),
            FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.cameraReconfirmSetUp)),
          ],
        ),
      );
      await camera.dismissConfirmation();
      if (setUp == true && _controller.isAuthenticated) {
        _controller.setSettingsPage(SettingsPage.cameraUpload);
        _controller.setSelectedSection(AppSection.settings);
      }
    } finally {
      _askingCamera = false;
    }
  }

  void _sharedChanged() => unawaited(_onShared());

  /// One share at a time: pick a folder, then upload exactly the files that
  /// prompt was about. A share that arrives meanwhile gets its own prompt.
  Future<void> _onShared() async {
    final intake = _controller.shareIntake;
    if (intake.pending.value.isEmpty || _pickingTarget || !_controller.isAuthenticated) return;
    final navigatorContext = _navigatorKey.currentContext;
    if (navigatorContext == null) return;
    _pickingTarget = true;
    try {
      final batch = intake.pending.value.first;
      if (batch.tooLarge || batch.rejected > 0) {
        ScaffoldMessenger.maybeOf(navigatorContext)?.showSnackBar(
          SnackBar(content: Text(batch.tooLarge ? appStrings.shareTooLarge : appStrings.shareRejected(batch.rejected))),
        );
      }
      if (batch.files.isEmpty) {
        intake.remove(batch);
        return;
      }
      final target = await showFolderPicker(
        navigatorContext,
        _controller,
        title: appStrings.shareTargetTitle(batch.files.length),
        confirmLabel: appStrings.uploadHere,
        hideVaults: true,
      );
      intake.remove(batch);
      if (target == null || !_controller.isAuthenticated) {
        await intake.release(batch.files);
        return;
      }
      _controller.uploads.enqueue(target.$1, batch.files);
      releaseWhenUploaded(intake, _controller.uploads, batch);
    } finally {
      _pickingTarget = false;
      // The next share, if one came in while this prompt was open.
      _sharedChanged();
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}
