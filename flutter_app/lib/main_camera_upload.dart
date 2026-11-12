part of 'main.dart';

// Settings › Camera upload (Android and iOS). Every photo not yet backed up
// goes to the chosen folder — on enable the user picks "new only" or
// "everything" — while the app is open, on every resume and from a periodic
// background task the OS schedules. Status pieces: main_camera_status.dart;
// the background reliability checklist: main_background_permissions.dart.

class _CameraUploadSettingsPage extends StatefulWidget {
  const _CameraUploadSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_CameraUploadSettingsPage> createState() => _CameraUploadSettingsPageState();
}

class _CameraUploadSettingsPageState extends State<_CameraUploadSettingsPage> with _PageLoad {
  NeoCloudController get _controller => widget.controller;
  CameraUploads get _uploads => _controller.cameraUploads;

  /// What lets camera upload run in the background; re-read on resume,
  /// since it is changed in the system settings.
  BackgroundReliability? _reliability;
  late final AppLifecycleListener _lifecycle;

  @override
  void initState() {
    super.initState();
    _uploads.addListener(_changed);
    _lifecycle = AppLifecycleListener(onResume: () => unawaited(_loadReliability()));
    unawaited(_uploads.load());
    unawaited(_loadReliability());
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _uploads.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _loadReliability() async {
    final next = await _uploads.backgroundReliability();
    if (mounted) setState(() => _reliability = next);
  }

  Future<void> _showReliability({bool onlyIfOpen = false}) async {
    await showBackgroundReliabilitySheet(context, _uploads, onlyIfOpen: onlyIfOpen);
    await _loadReliability();
  }

  /// Explains why the library is needed before the system asks.
  Future<bool> _askForAccess() async {
    final proceed = await showModalBottomSheet<bool>(
      context: context,
      backgroundColor: _bgCard,
      builder: (sheetContext) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(24, 24, 24, 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Icon(Icons.photo_library_outlined, size: 32, color: _accent),
              const SizedBox(height: 14),
              Text(appStrings.cameraAccessTitle, style: _displayTitleStyle(20)),
              const SizedBox(height: 8),
              Text(appStrings.cameraAccessMessage, style: TextStyle(color: _textSecondary, height: 1.45)),
              const SizedBox(height: 20),
              FilledButton(onPressed: () => Navigator.of(sheetContext).pop(true), child: Text(appStrings.continueLabel)),
              TextButton(onPressed: () => Navigator.of(sheetContext).pop(false), child: Text(appStrings.cancel)),
            ],
          ),
        ),
      ),
    );
    if (proceed != true) return false;
    final access = await _uploads.requestAccess();
    if (access != CameraAccess.denied) return true;
    if (mounted) setState(() => pageError = appStrings.cameraAccessDenied);
    return false;
  }

  Future<void> _setEnabled(bool enabled) async {
    final settings = _uploads.settings;
    if (!enabled) {
      await runAction(_controller, () => _uploads.update(settings.copyWith(enabled: false)));
      return;
    }
    if (!await _askForAccess() || !mounted) return;
    final scope = await _askCameraScope(context, _uploads, includeVideos: settings.includeVideos);
    if (scope == null || !mounted) return;
    var next = settings.copyWith(scope: scope);
    if (next.folderId == null) {
      final target = await _pickFolder();
      if (target == null) return;
      next = next.copyWith(folderId: target.$1, folderName: target.$2);
    }
    await runAction(_controller, () async {
      await _uploads.update(next.copyWith(enabled: true));
      unawaited(_uploads.runNow(_controller.client).catchError((Object _) {}));
    }, notice: appStrings.cameraUploadOn);
    // What else it takes to keep running in the background.
    if (mounted && _uploads.settings.enabled) await _showReliability(onlyIfOpen: true);
  }

  Future<(String, String)?> _pickFolder() => showFolderPicker(
    context,
    _controller,
    title: appStrings.cameraUploadFolder,
    confirmLabel: appStrings.syncChooseThisFolder,
    hideVaults: true,
  );

  Future<void> _changeFolder() async {
    final target = await _pickFolder();
    if (target == null) return;
    await runAction(_controller, () => _uploads.update(_uploads.settings.copyWith(folderId: target.$1, folderName: target.$2)));
  }

  String? _reliabilitySummary() {
    final reliability = _reliability;
    if (reliability == null) return null;
    final open = reliability.openCount;
    return open == 0 ? appStrings.backgroundReliabilityAllSet : appStrings.backgroundReliabilityOpen(open);
  }

  Future<void> _set(CameraUploadSettings next) => runAction(_controller, () => _uploads.update(next));

  Future<void> _exportLog() async {
    final lines = _uploads.log;
    final text = lines.isEmpty ? appStrings.cameraLogEmpty : lines.join('\n');
    await FilePicker.platform.saveFile(fileName: 'neocloud-camera-upload.log', bytes: Uint8List.fromList(utf8.encode('$text\n')));
  }

  Widget _statusCard() {
    final settings = _uploads.settings;
    final (label, color) = _cameraStatusLabel(_uploads.status);
    final locale = currentAppLanguage.code;
    final progress = _uploads.progress;
    final last = _uploads.lastRun;
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    _StatusPill(label: label, color: color),
                    const SizedBox(height: 4),
                    Text(
                      last == null ? appStrings.cameraNeverRun : appStrings.cameraUploadLastRun(formatDate(last, locale)),
                      style: TextStyle(color: _textMuted, fontSize: 12.5),
                    ),
                  ],
                ),
              ),
              FilledButton(
                onPressed: !settings.enabled || _uploads.running
                    ? null
                    : () => unawaited(runAction(_controller, () => _uploads.runNow(_controller.client, manual: true))),
                child: Text(appStrings.cameraBackUpNow),
              ),
            ],
          ),
          if (progress != null) ...<Widget>[
            const SizedBox(height: 10),
            ClipRRect(
              borderRadius: BorderRadius.circular(AppRadius.pill),
              child: LinearProgressIndicator(
                minHeight: 4,
                value: progress.total == 0 ? null : progress.current / progress.total,
                backgroundColor: _bgTertiary,
                valueColor: AlwaysStoppedAnimation<Color>(_textSecondary),
              ),
            ),
            if (progress.cloud != null)
              Padding(
                padding: const EdgeInsets.only(top: 6),
                child: Text('${appStrings.cameraReasonIcloud} · ${(progress.cloud! * 100).round()} %', style: TextStyle(color: _textMuted, fontSize: 12)),
              ),
          ],
          const SizedBox(height: 10),
          Divider(height: 1, color: _border),
          const SizedBox(height: 4),
          _CameraCountsRow(uploads: _uploads),
          Wrap(
            spacing: 8,
            children: <Widget>[
              if (_uploads.counts.failed > 0)
                TextButton.icon(
                  onPressed: busy
                      ? null
                      : () => unawaited(runAction(_controller, () async {
                          await _uploads.retryFailed();
                          await _uploads.runNow(_controller.client, manual: true);
                        })),
                  icon: const Icon(Icons.refresh_rounded, size: 16),
                  label: Text(appStrings.cameraRetryFailed),
                ),
              TextButton.icon(
                onPressed: () => unawaited(_exportLog().catchError((Object _) {})),
                icon: const Icon(Icons.receipt_long_outlined, size: 16),
                label: Text(appStrings.cameraExportLog),
              ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final settings = _uploads.settings;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        if (settings.enabled) ...<Widget>[
          if (_uploads.limitedAccess) ...<Widget>[
            _Notice(message: appStrings.cameraLimitedMessage, color: _warning, icon: Icons.photo_library_outlined),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton(onPressed: () => unawaited(_uploads.openSystemSettings()), child: Text(appStrings.cameraAllowFullAccess)),
            ),
            const SizedBox(height: AppSpacing.sm),
          ],
          if (backgroundBannerDue(
            enabled: settings.enabled,
            reliability: _reliability,
            lastSuccess: _uploads.lastSuccess,
            enabledAt: settings.enabledAt,
            now: DateTime.now(),
          )) ...<Widget>[
            _BackgroundReliabilityBanner(
              missing: _reliability?.anyNeeded ?? false,
              since: _uploads.lastSuccess ?? settings.enabledAt,
              onFix: () => unawaited(_showReliability()),
            ),
            const SizedBox(height: AppSpacing.md),
          ],
          _statusCard(),
          const SizedBox(height: AppSpacing.lg),
        ],
        _SettingsGroup(
          title: appStrings.cameraBackupTitle,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.cameraUploadEnable,
              description: appStrings.cameraBackupEnableDescription,
              control: Switch(value: settings.enabled, onChanged: busy ? null : (value) => unawaited(_setEnabled(value))),
            ),
            _SettingsRow(
              label: appStrings.cameraUploadFolder,
              description: settings.folderId == null ? appStrings.syncNothingChosen : settings.folderName,
              control: OutlinedButton(onPressed: busy ? null : () => unawaited(_changeFolder()), child: Text(appStrings.syncChoose)),
            ),
            _SettingsRow(
              label: appStrings.backgroundReliabilityTitle,
              description: _reliabilitySummary(),
              control: OutlinedButton(onPressed: () => unawaited(_showReliability()), child: Text(appStrings.backgroundReliabilityCheck)),
            ),
            if (settings.enabled)
              _SettingsRow(
                label: appStrings.cameraScopeSetting,
                control: _SettingsChoice<CameraScope>(
                  value: settings.scope,
                  options: <(CameraScope, String)>[
                    (CameraScope.everything, appStrings.cameraScopeEverythingShort),
                    (CameraScope.newOnly, appStrings.cameraScopeNewShort),
                  ],
                  onChanged: busy ? null : (scope) => unawaited(_set(settings.copyWith(scope: scope))),
                ),
              ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.cameraOptions,
          description: appStrings.cameraUploadBackgroundHint,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.cameraUploadWifiOnly,
              control: Switch(value: settings.wifiOnly, onChanged: busy ? null : (value) => unawaited(_set(settings.copyWith(wifiOnly: value)))),
            ),
            _SettingsRow(
              label: appStrings.cameraChargingOnly,
              control: Switch(value: settings.chargingOnly, onChanged: busy ? null : (value) => unawaited(_set(settings.copyWith(chargingOnly: value)))),
            ),
            _SettingsRow(
              label: appStrings.cameraUploadVideos,
              control: Switch(value: settings.includeVideos, onChanged: busy ? null : (value) => unawaited(_set(settings.copyWith(includeVideos: value)))),
            ),
            if (settings.includeVideos && !settings.wifiOnly)
              _SettingsRow(
                label: appStrings.cameraVideosWifiOnly,
                description: appStrings.cameraVideosWifiOnlyHint,
                control: Switch(
                  value: settings.videosWifiOnly,
                  onChanged: busy ? null : (value) => unawaited(_set(settings.copyWith(videosWifiOnly: value))),
                ),
              ),
            _SettingsRow(
              label: appStrings.cameraSkipSimilar,
              control: Switch(value: settings.skipSimilar, onChanged: busy ? null : (value) => unawaited(_set(settings.copyWith(skipSimilar: value)))),
            ),
          ],
        ),
      ],
    );
  }
}
