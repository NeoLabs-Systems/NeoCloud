part of 'main.dart';

// What keeps camera upload running in the background, as NeoAgent's
// permission sheet (main_call_permissions.dart): a title, a short
// explanation, then one row per setting with its status (dot + word) and a
// button that fixes it. Each fix happens in a system prompt or the system
// settings; coming back re-reads them all. Also the banner on Settings ›
// Camera upload. The checks are src/mobile/background_reliability.dart.

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
    backgroundColor: Colors.transparent,
    builder: (_) => _BackgroundReliabilitySheet(uploads: uploads, initial: initial),
  );
}

class _BackgroundReliabilitySheet extends StatefulWidget {
  const _BackgroundReliabilitySheet({required this.uploads, required this.initial});

  final CameraUploads uploads;
  final BackgroundReliability initial;

  @override
  State<_BackgroundReliabilitySheet> createState() => _BackgroundReliabilitySheetState();
}

class _BackgroundReliabilitySheetState extends State<_BackgroundReliabilitySheet> with WidgetsBindingObserver {
  late BackgroundReliability _reliability = widget.initial;

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

  /// Each setting is changed in system settings; coming back re-reads them.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(_refresh());
  }

  Future<void> _refresh() async {
    final next = await widget.uploads.backgroundReliability();
    if (mounted) setState(() => _reliability = next);
  }

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
    // In-app prompts don't leave the app, so there is no resume to wait for.
    await _refresh();
  }

  @override
  Widget build(BuildContext context) {
    final android = _reliability.status.android;
    return Container(
      constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(context).height * 0.92),
      decoration: BoxDecoration(
        color: _bgCard,
        borderRadius: const BorderRadius.vertical(top: Radius.circular(28)),
      ),
      padding: const EdgeInsets.fromLTRB(24, 12, 24, 24),
      child: SafeArea(
        top: false,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Center(
                child: Container(
                  width: 38,
                  height: 4,
                  decoration: BoxDecoration(color: _border, borderRadius: BorderRadius.circular(2)),
                ),
              ),
              const SizedBox(height: 24),
              Text(
                appStrings.backgroundTitle,
                style: GoogleFonts.geist(fontSize: 24, fontWeight: FontWeight.w600, letterSpacing: -0.4, color: _textPrimary),
              ),
              const SizedBox(height: 8),
              Text(
                android ? appStrings.backgroundIntroAndroid : appStrings.backgroundIntroIos,
                style: GoogleFonts.geist(fontSize: 15, height: 1.45, color: _textSecondary),
              ),
              const SizedBox(height: 20),
              for (final item in _reliability.items)
                _BackgroundCheckRow(item: item, status: _reliability.status, onFix: () => _fix(item)),
              if (!android) ...<Widget>[
                const SizedBox(height: 8),
                _BackgroundNote(icon: Icons.swipe_up_outlined, text: appStrings.backgroundSwipeHint),
                _BackgroundNote(icon: Icons.info_outline_rounded, text: appStrings.cameraIosBackgroundTip),
              ],
              const SizedBox(height: 12),
              TextButton(
                onPressed: () => Navigator.of(context).pop(),
                style: TextButton.styleFrom(foregroundColor: _textSecondary, minimumSize: const Size.fromHeight(48)),
                child: Text(_reliability.anyNeeded ? appStrings.backgroundNotNow : appStrings.done),
              ),
            ],
          ),
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
  const _BackgroundCheckRow({required this.item, required this.status, required this.onFix});

  final BackgroundItem item;
  final BackgroundPlatformStatus status;
  final Future<void> Function() onFix;

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

  @override
  Widget build(BuildContext context) {
    final (icon, title, detail) = _describe();
    final (word, color) = _statusLabel();
    // The manufacturer's screens can't be read back, so their button stays.
    final showButton = item.fixable && (item.state != BackgroundCheckState.granted || item.check == BackgroundCheck.manufacturer);
    final steps = item.check == BackgroundCheck.manufacturer ? backgroundVendorSteps(status.vendor) : const <BackgroundVendorStep>[];
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Container(
            width: 44,
            height: 44,
            decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(12)),
            child: Icon(icon, size: 20, color: _textSecondary),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(detail, style: TextStyle(color: _textMuted, fontSize: 13, height: 1.35)),
                for (var index = 0; index < steps.length; index++)
                  Padding(
                    padding: const EdgeInsets.only(top: 6),
                    child: Text(
                      '${index + 1}. ${_backgroundStepText(steps[index])}',
                      style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.35),
                    ),
                  ),
                if (steps.isNotEmpty)
                  TextButton.icon(
                    onPressed: () => unawaited(
                      launchUrl(backgroundVendorGuide(status.vendor, status.manufacturer), mode: LaunchMode.externalApplication),
                    ),
                    style: TextButton.styleFrom(padding: EdgeInsets.zero, foregroundColor: _textSecondary),
                    icon: const Icon(Icons.open_in_new_rounded, size: 15),
                    label: Text(appStrings.backgroundManufacturerGuide),
                  ),
                const SizedBox(height: 6),
                _StatusPill(label: word, color: color),
              ],
            ),
          ),
          if (showButton) ...<Widget>[
            const SizedBox(width: 12),
            FilledButton(
              onPressed: () => unawaited(onFix()),
              style: FilledButton.styleFrom(minimumSize: const Size(72, 40), shape: const StadiumBorder()),
              child: Text(_buttonLabel()),
            ),
          ],
        ],
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
