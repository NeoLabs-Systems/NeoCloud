part of 'main.dart';

// How camera backup tells what it is doing: one specific status (dot +
// word), numbers that add up and open the files behind them, the
// limited-access and "delayed by the system" banners, and the small chip in
// the phone's app bar.

(String, Color) _cameraStatusLabel(CameraStatus status) {
  switch (status.kind) {
    case CameraStatusKind.off:
      return (appStrings.cameraStatusOff, _textMuted);
    case CameraStatusKind.upToDate:
      return (appStrings.cameraStatusUpToDate, _success);
    case CameraStatusKind.uploading:
      return (appStrings.cameraStatusUploading(status.current, status.total), _info);
    case CameraStatusKind.pending:
      return (appStrings.cameraStatusPending(status.count), _info);
    case CameraStatusKind.waitingWifi:
      return (appStrings.cameraStatusWaitingWifi, _warning);
    case CameraStatusKind.waitingCharger:
      return (appStrings.cameraStatusWaitingCharger, _warning);
    case CameraStatusKind.limitedAccess:
      return (appStrings.cameraStatusLimited, _warning);
    case CameraStatusKind.noAccess:
      return (appStrings.cameraStatusNoAccess, _danger);
    case CameraStatusKind.noFolder:
      return (appStrings.cameraStatusNoFolder, _warning);
    case CameraStatusKind.serverUnreachable:
      return (appStrings.cameraStatusUnreachable, _warning);
    case CameraStatusKind.signedOut:
      return (appStrings.cameraStatusSignedOut, _danger);
    case CameraStatusKind.serverFull:
      return (appStrings.cameraStatusServerFull, _danger);
    case CameraStatusKind.failed:
      return (appStrings.cameraStatusFailed(status.count), _danger);
  }
}

String _cameraReasonText(String? reason) {
  switch (reason) {
    case null:
      return '';
    case CameraReason.wifi:
      return appStrings.cameraReasonWifi;
    case CameraReason.icloud:
      return appStrings.cameraReasonIcloud;
    case CameraReason.notAvailable:
      return appStrings.cameraReasonNotAvailable;
    default:
      return reason;
  }
}

/// Small status in the phone's app bar; opens Settings › Camera upload.
class _CameraStatusChip extends StatelessWidget {
  const _CameraStatusChip({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final uploads = controller.cameraUploads;
    return ListenableBuilder(
      listenable: uploads,
      builder: (context, _) {
        if (!uploads.supported || !uploads.settings.enabled) return const SizedBox.shrink();
        final status = uploads.status;
        final (label, color) = _cameraStatusLabel(status);
        return Padding(
          padding: const EdgeInsets.only(right: 8),
          child: InkWell(
            borderRadius: BorderRadius.circular(AppRadius.pill),
            onTap: () {
              controller.setSettingsPage(SettingsPage.cameraUpload);
              controller.setSelectedSection(AppSection.settings);
            },
            child: Container(
              constraints: const BoxConstraints(maxWidth: 170),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
              decoration: BoxDecoration(
                color: _bgTertiary,
                borderRadius: BorderRadius.circular(AppRadius.pill),
                border: Border.all(color: _border),
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Icon(Icons.photo_camera_outlined, size: 14, color: _textMuted),
                  const SizedBox(width: 6),
                  Container(width: 6, height: 6, decoration: BoxDecoration(color: color, shape: BoxShape.circle)),
                  const SizedBox(width: 6),
                  Flexible(
                    child: Text(label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 12)),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }
}

/// On phone / backed up / pending / failed, each opening its files.
class _CameraCountsRow extends StatelessWidget {
  const _CameraCountsRow({required this.uploads});

  final CameraUploads uploads;

  @override
  Widget build(BuildContext context) {
    final counts = uploads.counts;
    Widget cell(String label, int value, CameraRecordStatus? filter) {
      return Expanded(
        child: InkWell(
          borderRadius: BorderRadius.circular(AppRadius.control),
          onTap: () => unawaited(_showCameraRecords(context, uploads, filter, label)),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 6),
            child: Column(
              children: <Widget>[
                Text('$value', style: GoogleFonts.geist(fontSize: 20, fontWeight: FontWeight.w600, color: _textPrimary)),
                const SizedBox(height: 2),
                Text(label, textAlign: TextAlign.center, style: TextStyle(color: _textMuted, fontSize: 12)),
              ],
            ),
          ),
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            cell(appStrings.cameraCountOnPhone, counts.onPhone, null),
            cell(appStrings.cameraCountBackedUp, counts.backedUp, CameraRecordStatus.done),
            cell(appStrings.cameraCountPending, counts.pending, CameraRecordStatus.pending),
            cell(appStrings.cameraCountFailed, counts.failed, CameraRecordStatus.failed),
          ],
        ),
        if (counts.waitingOnCloud > 0)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(appStrings.cameraWaitingCloud(counts.waitingOnCloud), textAlign: TextAlign.center, style: TextStyle(color: _textMuted, fontSize: 12.5)),
          ),
      ],
    );
  }
}

Future<void> _showCameraRecords(BuildContext context, CameraUploads uploads, CameraRecordStatus? filter, String title) {
  final locale = currentAppLanguage.code;
  final records = uploads.records(filter);
  return showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    backgroundColor: _bgCard,
    builder: (sheetContext) => SafeArea(
      child: SizedBox(
        height: MediaQuery.sizeOf(sheetContext).height * 0.7,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 20, 20, 8),
              child: Text('$title · ${records.length}', style: _sectionTitleStyle()),
            ),
            Expanded(
              child: records.isEmpty
                  ? Padding(padding: const EdgeInsets.symmetric(horizontal: 20), child: _EmptyText(appStrings.cameraListEmpty))
                  : ListView.separated(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
                      itemCount: records.length,
                      separatorBuilder: (_, _) => Divider(height: 1, color: _border),
                      itemBuilder: (context, index) {
                        final record = records[index];
                        final details = <String>[
                          if (record.created != null) formatDate(record.created, locale),
                          if (record.size > 0) formatBytes(record.size),
                          if (record.status == CameraRecordStatus.failed) appStrings.cameraAttempts(record.attempts),
                          if (record.reason != null) _cameraReasonText(record.reason),
                        ];
                        return Padding(
                          padding: const EdgeInsets.symmetric(vertical: 10),
                          child: Row(
                            children: <Widget>[
                              Icon(record.isVideo ? Icons.videocam_outlined : Icons.photo_outlined, size: 18, color: _textMuted),
                              const SizedBox(width: 12),
                              Expanded(
                                child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: <Widget>[
                                    Text(
                                      record.name.isEmpty ? appStrings.cameraUnnamed : record.name,
                                      maxLines: 1,
                                      overflow: TextOverflow.ellipsis,
                                      style: TextStyle(color: _textPrimary, fontSize: 14),
                                    ),
                                    Text(details.join(' · '), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                                  ],
                                ),
                              ),
                            ],
                          ),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    ),
  );
}

/// Shown when the last pass is more than a day old: the OS is probably
/// keeping NeoCloud from running in the background.
class _CameraDelayedBanner extends StatelessWidget {
  const _CameraDelayedBanner({required this.lastRun});

  final DateTime? lastRun;

  static bool due(CameraUploads uploads) {
    final last = uploads.lastRun;
    return uploads.settings.enabled && last != null && DateTime.now().difference(last) > const Duration(hours: 24);
  }

  @override
  Widget build(BuildContext context) {
    final android = defaultTargetPlatform == TargetPlatform.android;
    final tips = android
        ? <String>[
            appStrings.cameraBatteryTipGeneric,
            appStrings.cameraBatteryTipSamsung,
            appStrings.cameraBatteryTipXiaomi,
            appStrings.cameraBatteryTipHuawei,
            appStrings.cameraBatteryTipOnePlus,
          ]
        : <String>[appStrings.cameraIosBackgroundTip];
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: _warning.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(Icons.battery_alert_outlined, size: 18, color: _warning),
              const SizedBox(width: 8),
              Expanded(child: Text(appStrings.cameraDelayedTitle, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 13.5))),
            ],
          ),
          const SizedBox(height: 6),
          Text(appStrings.cameraDelayedMessage(formatDate(lastRun, currentAppLanguage.code)), style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4)),
          const SizedBox(height: 8),
          for (final tip in tips)
            Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Text('• $tip', style: TextStyle(color: _textSecondary, fontSize: 12.5, height: 1.4)),
            ),
          if (android)
            TextButton.icon(
              onPressed: () => unawaited(launchUrl(Uri.parse('https://dontkillmyapp.com/'), mode: LaunchMode.externalApplication)),
              icon: const Icon(Icons.open_in_new_rounded, size: 15),
              label: Text(appStrings.cameraBatteryMore),
            ),
        ],
      ),
    );
  }
}

/// Asked when backup is turned on: new photos only, or the whole library.
Future<CameraScope?> _askCameraScope(BuildContext context, CameraUploads uploads, {required bool includeVideos}) {
  final size = uploads.librarySize(includeVideos: includeVideos);
  return showModalBottomSheet<CameraScope>(
    context: context,
    backgroundColor: _bgCard,
    builder: (sheetContext) => SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 24, 20, 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(appStrings.cameraScopeTitle, style: _displayTitleStyle(20)),
            const SizedBox(height: 14),
            FutureBuilder<CameraLibrarySize>(
              future: size,
              builder: (context, snapshot) {
                final value = snapshot.data;
                final String label;
                if (value == null) {
                  label = appStrings.cameraScopeCounting;
                } else if (value.bytes == null) {
                  label = appStrings.cameraScopeEverythingCount(value.count);
                } else {
                  label = appStrings.cameraScopeEverything(value.count, formatBytes(value.bytes!));
                }
                return _AppCard(
                  onTap: () => Navigator.of(sheetContext).pop(CameraScope.everything),
                  padding: const EdgeInsets.all(16),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(label, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 14.5)),
                      const SizedBox(height: 4),
                      Text(appStrings.cameraScopeEverythingHint, style: TextStyle(color: _textSecondary, fontSize: 13)),
                    ],
                  ),
                );
              },
            ),
            const SizedBox(height: 10),
            _AppCard(
              onTap: () => Navigator.of(sheetContext).pop(CameraScope.newOnly),
              padding: const EdgeInsets.all(16),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(appStrings.cameraScopeNew, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 14.5)),
                  const SizedBox(height: 4),
                  Text(appStrings.cameraScopeNewHint, style: TextStyle(color: _textSecondary, fontSize: 13)),
                ],
              ),
            ),
            const SizedBox(height: 8),
            TextButton(onPressed: () => Navigator.of(sheetContext).pop(), child: Text(appStrings.cancel)),
          ],
        ),
      ),
    ),
  );
}
