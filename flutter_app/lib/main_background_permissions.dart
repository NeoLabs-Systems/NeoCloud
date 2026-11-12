part of 'main.dart';

// What keeps camera upload running in the background, as NeoAgent's shared
// permission sheet (main_permission_sheet.dart): an icon, a title, a short
// explanation and how many are set, then one card per setting with a check
// or the button that fixes it, and "Allow all", which walks through the
// system prompts and screens in order. Each fix happens in a system prompt or
// the system settings; coming back re-reads them all. Also the banner on
// Settings › Camera upload. The checks are src/mobile/background_reliability.dart.

/// Shows the checklist. With [onlyIfOpen] (camera upload was just turned
/// on) it shows only when something isn't granted yet.
Future<void> showBackgroundReliabilitySheet(BuildContext context, CameraUploads uploads, {bool onlyIfOpen = false}) async {
  if (!uploads.supported) return;
  final initial = await uploads.backgroundReliability();
  if (onlyIfOpen && initial.openCount == 0) return;
  if (!context.mounted) return;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    showDragHandle: true,
    builder: (_) => _BackgroundReliabilitySheet(uploads: uploads, initial: initial),
  );
}

/// Whether [item] is done. The manufacturer's screens can't be read back,
/// so they never count; "Allow all" moves on once they were visited.
bool _backgroundItemDone(BackgroundItem item) => item.state == BackgroundCheckState.granted;

class _BackgroundReliabilitySheet extends StatefulWidget {
  const _BackgroundReliabilitySheet({required this.uploads, required this.initial});

  final CameraUploads uploads;
  final BackgroundReliability initial;

  @override
  State<_BackgroundReliabilitySheet> createState() => _BackgroundReliabilitySheetState();
}

class _BackgroundReliabilitySheetState extends State<_BackgroundReliabilitySheet> with WidgetsBindingObserver {
  late BackgroundReliability _reliability = widget.initial;

  /// "Allow all": what is left to ask for, the one asked now, and where it
  /// stands. A setting left without granting it ends the walk.
  final List<BackgroundCheck> _queue = <BackgroundCheck>[];
  BackgroundCheck? _asking;
  int _step = 0;
  int _steps = 0;

  /// The app went to the background since the current prompt opened: the
  /// answer comes with the resume, not when the call returns.
  bool _leftApp = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    super.dispose();
  }

  /// Each setting is changed in a system prompt or screen; coming back
  /// re-reads them.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state != AppLifecycleState.resumed) {
      _leftApp = true;
      return;
    }
    if (_asking != null) {
      unawaited(_afterReturn());
    } else {
      unawaited(_refresh());
    }
  }

  Future<void> _refresh() async {
    final next = await widget.uploads.backgroundReliability();
    if (mounted) setState(() => _reliability = next);
  }

  /// Opens the prompt or setting for [item].
  Future<void> _fix(BackgroundItem item) async {
    final ios = !_reliability.status.android;
    switch (item.check) {
      case BackgroundCheck.photos:
        if (ios && item.state == BackgroundCheckState.recommended) {
          // iOS only offers "Full Access" in Settings once a selection was made.
          await widget.uploads.openSystemSettings();
        } else if (await widget.uploads.requestAccess() == CameraAccess.denied) {
          await widget.uploads.openSystemSettings();
        }
      case BackgroundCheck.notifications:
        if (ios && !_reliability.status.notificationsUndecided) {
          await NeoCloudBackground.openNotificationSettings();
        } else {
          await NeoCloudBackground.requestNotifications();
        }
      case BackgroundCheck.battery:
        await NeoCloudBackground.requestIgnoreBatteryOptimizations();
      case BackgroundCheck.manufacturer:
        await NeoCloudBackground.openManufacturerSettings();
      case BackgroundCheck.backgroundData:
        await NeoCloudBackground.openBackgroundDataSettings();
      case BackgroundCheck.backgroundRefresh:
        await NeoCloudBackground.openAppSettings();
      case BackgroundCheck.lowPowerMode:
        break;
    }
  }

  /// One setting from its own button.
  Future<void> _fixOne(BackgroundItem item) async {
    await _fix(item);
    // In-app prompts don't leave the app, so there is no resume to wait for.
    await _refresh();
  }

  void _allowAll() {
    _queue
      ..clear()
      ..addAll(<BackgroundCheck>[
        for (final item in _reliability.items)
          if (item.fixable && !_backgroundItemDone(item)) item.check,
      ]);
    _steps = _queue.length;
    _step = 0;
    unawaited(_askNext());
  }

  void _stopWalk() {
    _queue.clear();
    _asking = null;
  }

  Future<void> _askNext() async {
    if (!mounted) return;
    if (_queue.isEmpty) {
      setState(_stopWalk);
      if (_reliability.openCount == 0) Navigator.of(context).pop();
      return;
    }
    final check = _queue.removeAt(0);
    final item = _reliability.items.firstWhere((item) => item.check == check);
    setState(() {
      _asking = check;
      _step += 1;
    });
    _leftApp = false;
    try {
      await _fix(item);
    } catch (_) {
      if (mounted) setState(_stopWalk);
      return;
    }
    // A prompt answered without leaving the app (or nothing shown at all,
    // like a permission refused for good) has no resume to wait for.
    await Future<void>.delayed(const Duration(milliseconds: 800));
    if (!_leftApp && _asking == check) await _afterReturn();
  }

  /// Back from the setting [_asking] opened: on to the next one if it was
  /// granted, else the walk ends.
  Future<void> _afterReturn() async {
    final asked = _asking;
    if (asked == null) return;
    _asking = null;
    final next = await widget.uploads.backgroundReliability();
    if (!mounted) return;
    setState(() => _reliability = next);
    final granted = asked == BackgroundCheck.manufacturer || next.stateOf(asked) == BackgroundCheckState.granted;
    if (granted) {
      await _askNext();
    } else {
      setState(_stopWalk);
    }
  }

  Widget _header(bool android) {
    final items = _reliability.items;
    final granted = items.where(_backgroundItemDone).length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Center(
          child: Container(
            width: 64,
            height: 64,
            decoration: BoxDecoration(color: _accent.withValues(alpha: 0.12), shape: BoxShape.circle),
            child: Icon(Icons.cloud_upload_outlined, color: _accent, size: 30),
          ),
        ),
        const SizedBox(height: 16),
        Text(
          appStrings.backgroundTitle,
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 8),
        Text(
          android ? appStrings.backgroundIntroAndroid : appStrings.backgroundIntroIos,
          textAlign: TextAlign.center,
          style: TextStyle(color: _textSecondary, height: 1.45),
        ),
        const SizedBox(height: 18),
        Row(
          children: <Widget>[
            Expanded(
              child: ClipRRect(
                borderRadius: BorderRadius.circular(999),
                child: LinearProgressIndicator(
                  minHeight: 6,
                  value: items.isEmpty ? 0 : granted / items.length,
                  backgroundColor: _borderLight,
                  valueColor: AlwaysStoppedAnimation<Color>(_success),
                ),
              ),
            ),
            const SizedBox(width: 12),
            Text(appStrings.mobilePermissionsSet(granted, items.length), style: GoogleFonts.geistMono(fontSize: 12, color: _textMuted)),
          ],
        ),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final android = _reliability.status.android;
    final walking = _asking != null;
    final fixableOpen = _reliability.items.where((item) => item.fixable && !_backgroundItemDone(item)).length;
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(AppSpacing.lg, AppSpacing.xs, AppSpacing.lg, AppSpacing.md),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            _header(android),
            const SizedBox(height: AppSpacing.md),
            for (final item in _reliability.items)
              _BackgroundCheckRow(
                item: item,
                status: _reliability.status,
                asking: _asking == item.check,
                step: _step,
                steps: _steps,
                onFix: walking ? null : () => unawaited(_fixOne(item)),
              ),
            if (!android) ...<Widget>[
              const SizedBox(height: 4),
              _BackgroundNote(icon: Icons.swipe_up_outlined, text: appStrings.backgroundSwipeHint),
              _BackgroundNote(icon: Icons.info_outline_rounded, text: appStrings.cameraIosBackgroundTip),
            ],
            const SizedBox(height: AppSpacing.sm),
            if (fixableOpen > 1)
              FilledButton(
                onPressed: walking ? null : _allowAll,
                style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
                child: walking
                    ? const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2))
                    : Text(appStrings.backgroundAllowAll),
              ),
            TextButton(
              onPressed: () {
                _stopWalk();
                Navigator.of(context).pop();
              },
              style: TextButton.styleFrom(foregroundColor: _textSecondary, minimumSize: const Size.fromHeight(46)),
              child: Text(_reliability.openCount > 0 ? appStrings.backgroundNotNow : appStrings.done),
            ),
          ],
        ),
      ),
    );
  }
}

/// The maker as the phone names it: "samsung" → "Samsung", "OnePlus" stays.
String _backgroundMakerName(BackgroundPlatformStatus status) {
  final name = status.manufacturer.trim();
  if (name.isEmpty) return status.brand;
  final mixed = name != name.toLowerCase() && name != name.toUpperCase();
  return mixed ? name : '${name[0].toUpperCase()}${name.substring(1).toLowerCase()}';
}

String _backgroundStepText(BackgroundVendorStep step) => switch (step) {
  BackgroundVendorStep.samsungUnrestricted => appStrings.backgroundStepSamsungUnrestricted,
  BackgroundVendorStep.samsungNeverSleeping => appStrings.backgroundStepSamsungNeverSleeping,
  BackgroundVendorStep.xiaomiAutostart => appStrings.backgroundStepXiaomiAutostart,
  BackgroundVendorStep.xiaomiNoRestrictions => appStrings.backgroundStepXiaomiNoRestrictions,
  BackgroundVendorStep.huaweiAppLaunch => appStrings.backgroundStepHuaweiAppLaunch,
  BackgroundVendorStep.oppoBatteryUsage => appStrings.backgroundStepOppoBatteryUsage,
  BackgroundVendorStep.oppoDontOptimise => appStrings.backgroundStepOppoDontOptimise,
  BackgroundVendorStep.vivoBackgroundPower => appStrings.backgroundStepVivoBackgroundPower,
  BackgroundVendorStep.vivoAutostart => appStrings.backgroundStepVivoAutostart,
  BackgroundVendorStep.asusAutostart => appStrings.backgroundStepAsusAutostart,
  BackgroundVendorStep.lockInRecents => appStrings.backgroundStepLockInRecents,
};

class _BackgroundCheckRow extends StatelessWidget {
  const _BackgroundCheckRow({
    required this.item,
    required this.status,
    required this.asking,
    required this.step,
    required this.steps,
    required this.onFix,
  });

  final BackgroundItem item;
  final BackgroundPlatformStatus status;

  /// Whether "Allow all" has this one's prompt or screen open.
  final bool asking;
  final int step;
  final int steps;

  /// Null while "Allow all" walks through the list.
  final VoidCallback? onFix;

  (IconData, String, String) _describe() {
    final maker = _backgroundMakerName(status);
    switch (item.check) {
      case BackgroundCheck.photos:
        return (Icons.photo_library_outlined, appStrings.backgroundPhotosTitle, appStrings.backgroundPhotosDetail);
      case BackgroundCheck.notifications:
        return (
          Icons.notifications_none_rounded,
          appStrings.backgroundNotificationsTitle,
          status.android ? appStrings.backgroundNotificationsDetailAndroid : appStrings.backgroundNotificationsDetailIos,
        );
      case BackgroundCheck.battery:
        return (Icons.battery_charging_full_rounded, appStrings.backgroundBatteryTitle, appStrings.backgroundBatteryDetail);
      case BackgroundCheck.manufacturer:
        return (Icons.phone_android_rounded, appStrings.backgroundManufacturerTitle(maker), appStrings.backgroundManufacturerDetail(maker));
      case BackgroundCheck.backgroundData:
        return (Icons.data_saver_off_rounded, appStrings.backgroundDataTitle, appStrings.backgroundDataDetail);
      case BackgroundCheck.backgroundRefresh:
        return (
          Icons.sync_rounded,
          appStrings.backgroundRefreshTitle,
          item.fixable ? appStrings.backgroundRefreshDetail : appStrings.backgroundRefreshRestricted,
        );
      case BackgroundCheck.lowPowerMode:
        return (Icons.battery_saver_rounded, appStrings.backgroundLowPowerTitle, appStrings.backgroundLowPowerDetail);
    }
  }

  /// A setting the app can't change says where it stands as a dot and a word.
  (String, Color) _statusLabel() {
    switch (item.state) {
      case BackgroundCheckState.granted:
        // A mode that is off isn't a permission.
        final word = item.check == BackgroundCheck.lowPowerMode ? appStrings.backgroundStatusOff : appStrings.backgroundStatusGranted;
        return (word, _success);
      case BackgroundCheckState.needed:
        return (appStrings.backgroundStatusNeeded, _danger);
      case BackgroundCheckState.recommended:
        return (appStrings.backgroundStatusRecommended, _warning);
    }
  }

  String _buttonLabel() {
    switch (item.check) {
      case BackgroundCheck.photos:
        return item.state == BackgroundCheckState.recommended ? appStrings.backgroundAllowAll : appStrings.backgroundAllow;
      case BackgroundCheck.notifications:
      case BackgroundCheck.battery:
        return appStrings.backgroundAllow;
      case BackgroundCheck.manufacturer:
      case BackgroundCheck.backgroundData:
      case BackgroundCheck.backgroundRefresh:
      case BackgroundCheck.lowPowerMode:
        return appStrings.backgroundOpenSettings;
    }
  }

  Widget _trailing() {
    final (word, color) = _statusLabel();
    if (asking) {
      return SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2, color: _accent));
    }
    // The manufacturer's screens can't be read back, so their button stays.
    if (item.fixable && (item.state != BackgroundCheckState.granted || item.check == BackgroundCheck.manufacturer)) {
      return OutlinedButton(
        onPressed: onFix,
        style: OutlinedButton.styleFrom(minimumSize: const Size(72, 40), shape: const StadiumBorder()),
        child: Text(_buttonLabel(), textAlign: TextAlign.center),
      );
    }
    if (item.state == BackgroundCheckState.granted) {
      return Icon(Icons.check_circle_rounded, color: _success, semanticLabel: word);
    }
    return _StatusPill(label: word, color: color);
  }

  @override
  Widget build(BuildContext context) {
    final (icon, title, detail) = _describe();
    final steps = item.check == BackgroundCheck.manufacturer ? backgroundVendorSteps(status.vendor) : const <BackgroundVendorStep>[];
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Container(
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: _bgSecondary,
          borderRadius: BorderRadius.circular(16),
          border: Border.all(color: asking ? _accent.withValues(alpha: 0.6) : _border),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(color: _accent.withValues(alpha: 0.12), borderRadius: BorderRadius.circular(12)),
                  child: Icon(icon, size: 20, color: _accent),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      if (asking) ...<Widget>[
                        Text(appStrings.mobilePermissionStep(step, this.steps), style: _sectionEyebrowStyle()),
                        const SizedBox(height: 2),
                      ],
                      Text(title, style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600)),
                      const SizedBox(height: 2),
                      Text(detail, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.35)),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                // At a large font scale "Open settings" wraps instead of
                // squeezing the description out.
                ConstrainedBox(
                  constraints: BoxConstraints(maxWidth: MediaQuery.sizeOf(context).width * 0.3),
                  child: _trailing(),
                ),
              ],
            ),
            // The maker's steps take the card's full width under the row.
            if (steps.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(left: 58),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    for (var index = 0; index < steps.length; index++)
                      Padding(
                        padding: const EdgeInsets.only(top: 6),
                        child: Text(
                          '${index + 1}. ${_backgroundStepText(steps[index])}',
                          style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.35),
                        ),
                      ),
                    TextButton.icon(
                      onPressed: () => unawaited(
                        launchUrl(backgroundVendorGuide(status.vendor, status.manufacturer), mode: LaunchMode.externalApplication),
                      ),
                      style: TextButton.styleFrom(padding: EdgeInsets.zero, foregroundColor: _textSecondary),
                      icon: const Icon(Icons.open_in_new_rounded, size: 15),
                      label: Text(appStrings.backgroundManufacturerGuide),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _BackgroundNote extends StatelessWidget {
  const _BackgroundNote({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(icon, size: 18, color: _textMuted),
          const SizedBox(width: 10),
          Expanded(child: Text(text, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4))),
        ],
      ),
    );
  }
}

/// On Settings › Camera upload when something needed is missing or no
/// pass has succeeded for a day.
class _BackgroundReliabilityBanner extends StatelessWidget {
  const _BackgroundReliabilityBanner({required this.missing, required this.since, required this.onFix});

  /// Something needed is off; otherwise the backup is just stale.
  final bool missing;
  final DateTime? since;
  final VoidCallback onFix;

  @override
  Widget build(BuildContext context) {
    final message = missing ? appStrings.backgroundBannerMissing : appStrings.backgroundBannerStale(formatDate(since, currentAppLanguage.code));
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 14, 14, 6),
      decoration: BoxDecoration(color: _warning.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(Icons.battery_alert_outlined, size: 18, color: _warning),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  appStrings.backgroundBannerTitle,
                  style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 13.5),
                ),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(message, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4)),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(onPressed: onFix, child: Text(appStrings.backgroundBannerFix)),
          ),
        ],
      ),
    );
  }
}
