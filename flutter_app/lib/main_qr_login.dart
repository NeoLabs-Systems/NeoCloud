part of 'main.dart';

// QR sign-in, ported from NeoAgent (main_app_shell.dart's QR block and
// waiting card, main_account_settings.dart's approval). Two halves:
//
// - The web sign-in card shows a code (_QrLoginBlock); once a phone scans
//   it, the card waits for the decision (_QrLoginWaiting) and signs in.
// - The phone app scans any NeoCloud code (_scanQrCode): a browser's
//   sign-in code opens the approval sheet, a device-code link (the lite
//   desktop client's sign-in) opens the device approval page.

/// The QR code on its paper plate: dark modules on a light ground so phone
/// cameras read it in either theme.
class _AuthQrPlate extends StatelessWidget {
  const _AuthQrPlate({required this.size, required this.payload, required this.preparing});

  final double size;
  final String? payload;
  final bool preparing;

  @override
  Widget build(BuildContext context) {
    final data = payload;
    final ink = lightPalette.textPrimary;
    return Container(
      width: size,
      height: size,
      padding: EdgeInsets.all(size * 0.07),
      decoration: BoxDecoration(color: lightPalette.bgCard, borderRadius: BorderRadius.circular(size * 0.12)),
      child: data != null
          ? QrImageView(
              data: data,
              padding: EdgeInsets.zero,
              backgroundColor: lightPalette.bgCard,
              eyeStyle: QrEyeStyle(eyeShape: QrEyeShape.square, color: ink),
              dataModuleStyle: QrDataModuleStyle(dataModuleShape: QrDataModuleShape.square, color: ink),
              semanticsLabel: appStrings.qrLoginScanTitle,
            )
          : Center(
              child: preparing
                  ? SizedBox.square(dimension: 28, child: CircularProgressIndicator(strokeWidth: 2.5, color: ink))
                  : Icon(Icons.qr_code_2_rounded, size: size * 0.4, color: ink.withValues(alpha: 0.35)),
            ),
    );
  }
}

/// Phone sign-in on the web card: the code itself where the card has room,
/// else a row that opens it in a dialog. Counts down to the next code.
class _QrLoginBlock extends StatefulWidget {
  const _QrLoginBlock({required this.controller, required this.onDialogOpen});

  final NeoCloudController controller;

  /// Tells the card the code stays visible while the dialog is open.
  final ValueChanged<bool> onDialogOpen;

  @override
  State<_QrLoginBlock> createState() => _QrLoginBlockState();
}

class _QrLoginBlockState extends State<_QrLoginBlock> {
  QrLoginFlow get _flow => widget.controller.qrLogin;

  void _refresh() => unawaited(_flow.prepare(force: true));

  Future<void> _showDialog() async {
    widget.onDialogOpen(true);
    try {
      var closing = false;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => ListenableBuilder(
          listenable: widget.controller,
          builder: (context, _) {
            // A phone scanned the code: the card shows the waiting state.
            if (_flow.scan != null && !closing) {
              closing = true;
              WidgetsBinding.instance.addPostFrameCallback((_) => Navigator.of(dialogContext).pop());
            }
            return AlertDialog(
            title: Text(appStrings.qrLoginShowCode),
            content: SizedBox(
              width: 340,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Text(appStrings.qrLoginScanBody, style: TextStyle(color: _textSecondary, height: 1.45)),
                  const SizedBox(height: AppSpacing.md),
                  Center(
                    child: _AuthQrPlate(
                      size: 240,
                      payload: _flow.canShowCode ? _flow.challenge!.qrPayload : null,
                      preparing: _flow.isPreparing,
                    ),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  Center(child: _QrCountdown(flow: _flow)),
                  if (_flow.errorMessage != null) ...<Widget>[
                    const SizedBox(height: AppSpacing.sm),
                    _InlineError(message: _flow.errorMessage!),
                  ],
                ],
              ),
            ),
            actions: <Widget>[
              TextButton(onPressed: _flow.isPreparing ? null : _refresh, child: Text(appStrings.qrLoginNewCode)),
              FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close)),
            ],
          );
          },
        ),
      );
    } finally {
      widget.onDialogOpen(false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final decoration = BoxDecoration(
      color: _bgPrimary,
      borderRadius: BorderRadius.circular(AppRadius.panel),
      border: Border.all(color: _accent.withValues(alpha: 0.38)),
    );
    return LayoutBuilder(
      builder: (context, constraints) {
        final Widget block;
        if (constraints.maxWidth >= 340) {
          block = Container(
            padding: const EdgeInsets.all(AppSpacing.md),
            decoration: decoration,
            child: Row(
              children: <Widget>[
                _AuthQrPlate(size: 116, payload: _flow.canShowCode ? _flow.challenge!.qrPayload : null, preparing: _flow.isPreparing),
                const SizedBox(width: AppSpacing.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(
                        appStrings.qrLoginScanTitle,
                        style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600, height: 1.25),
                      ),
                      const SizedBox(height: 6),
                      Text(appStrings.qrLoginScanBody, style: TextStyle(color: _textSecondary, fontSize: 12.5, height: 1.45)),
                      Row(
                        children: <Widget>[
                          Expanded(child: _QrCountdown(flow: _flow)),
                          IconButton(
                            tooltip: appStrings.qrLoginNewCode,
                            onPressed: _flow.isPreparing ? null : _refresh,
                            color: _textSecondary,
                            icon: const Icon(Icons.refresh_rounded, size: 18),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
          );
        } else {
          block = Material(
            color: Colors.transparent,
            child: Ink(
              decoration: decoration,
              child: InkWell(
                borderRadius: BorderRadius.circular(AppRadius.panel),
                onTap: () => unawaited(_showDialog()),
                child: Padding(
                  padding: const EdgeInsets.all(AppSpacing.sm),
                  child: Row(
                    children: <Widget>[
                      Container(
                        width: 44,
                        height: 44,
                        decoration: BoxDecoration(color: lightPalette.bgCard, borderRadius: BorderRadius.circular(AppRadius.control)),
                        child: Icon(Icons.qr_code_2_rounded, color: lightPalette.textPrimary),
                      ),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: <Widget>[
                            Text(appStrings.qrLoginShowCode, style: TextStyle(color: _textPrimary, fontSize: 14.5, fontWeight: FontWeight.w600)),
                            const SizedBox(height: 2),
                            Text(appStrings.qrLoginScanTitle, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
                          ],
                        ),
                      ),
                      Icon(Icons.chevron_right_rounded, color: _textMuted),
                    ],
                  ),
                ),
              ),
            ),
          );
        }
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            block,
            if (_flow.errorMessage != null) ...<Widget>[
              const SizedBox(height: 10),
              _InlineError(message: _flow.errorMessage!),
            ],
          ],
        );
      },
    );
  }
}

/// "New code in 87s", ticking each second.
class _QrCountdown extends StatefulWidget {
  const _QrCountdown({required this.flow});

  final QrLoginFlow flow;

  @override
  State<_QrCountdown> createState() => _QrCountdownState();
}

class _QrCountdownState extends State<_QrCountdown> {
  Timer? _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(const Duration(seconds: 1), (_) => setState(() {}));
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final flow = widget.flow;
    return Text(
      flow.canShowCode ? appStrings.qrLoginRefreshesIn(flow.challenge!.secondsRemaining) : appStrings.qrLoginPreparing,
      style: GoogleFonts.geistMono(color: _textMuted, fontSize: 11, letterSpacing: 0.4),
    );
  }
}

/// A phone scanned the code: the card waits for its decision.
class _QrLoginWaiting extends StatelessWidget {
  const _QrLoginWaiting({required this.controller, required this.scan});

  final NeoCloudController controller;
  final QrLoginScan scan;

  Widget _detail(String label, String value) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(label, style: TextStyle(color: _textMuted, fontSize: 12.5)),
        const SizedBox(height: 3),
        Text(value, style: TextStyle(color: _textPrimary, fontSize: 12.5)),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final flow = controller.qrLogin;
    final location = scan.requestLocationLabel.trim();
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Container(
          padding: const EdgeInsets.all(AppSpacing.md),
          decoration: BoxDecoration(color: _bgPrimary, borderRadius: BorderRadius.circular(AppRadius.panel), border: Border.all(color: _border)),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Row(
                children: <Widget>[
                  Container(
                    width: 44,
                    height: 44,
                    decoration: BoxDecoration(color: _accentMuted, borderRadius: BorderRadius.circular(AppRadius.control)),
                    child: Icon(scan.scannerDeviceClass == 'tablet' ? Icons.tablet_mac_rounded : Icons.smartphone_rounded, color: _accent, size: 20),
                  ),
                  const SizedBox(width: 14),
                  Expanded(
                    child: Text(
                      scan.scannerLabel,
                      style: TextStyle(color: _textPrimary, fontSize: 14.5, fontWeight: FontWeight.w600),
                    ),
                  ),
                ],
              ),
              if (scan.requestedDeviceLabel.isNotEmpty || location.isNotEmpty) ...<Widget>[
                const SizedBox(height: 14),
                Divider(height: 1, color: _border),
                const SizedBox(height: 14),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    if (scan.requestedDeviceLabel.isNotEmpty) Expanded(child: _detail(appStrings.qrLoginSigningInOn, scan.requestedDeviceLabel)),
                    if (location.isNotEmpty) Expanded(child: _detail(appStrings.qrLoginFrom, location)),
                  ],
                ),
              ],
            ],
          ),
        ),
        const SizedBox(height: 20),
        Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: <Widget>[
            _WaitingDots(color: _accent),
            const SizedBox(width: 10),
            Flexible(
              child: Text(
                appStrings.qrLoginWaitingForApproval.toUpperCase(),
                style: GoogleFonts.geistMono(color: _accentHover, fontSize: 11.5, fontWeight: FontWeight.w600, letterSpacing: 0.8),
              ),
            ),
          ],
        ),
        if (flow.errorMessage != null) ...<Widget>[
          const SizedBox(height: 14),
          _InlineError(message: flow.errorMessage!),
        ],
        const SizedBox(height: 22),
        OutlinedButton(
          onPressed: flow.isPreparing ? null : () => unawaited(flow.cancelScan()),
          child: Text(appStrings.qrLoginCancelAndUsePassword),
        ),
      ],
    );
  }
}

/// Three dots stepping while the card waits; still with reduced motion.
class _WaitingDots extends StatefulWidget {
  const _WaitingDots({required this.color});

  final Color color;

  @override
  State<_WaitingDots> createState() => _WaitingDotsState();
}

class _WaitingDotsState extends State<_WaitingDots> {
  Timer? _timer;
  int _step = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (MediaQuery.disableAnimationsOf(context)) {
      _timer?.cancel();
      _timer = null;
    } else {
      _timer ??= Timer.periodic(const Duration(milliseconds: 400), (_) => setState(() => _step = (_step + 1) % 3));
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final still = _timer == null;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        for (var i = 0; i < 3; i++) ...<Widget>[
          if (i > 0) const SizedBox(width: 4),
          Container(
            width: 5,
            height: 5,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: still || i == _step ? widget.color : widget.color.withValues(alpha: 0.25),
            ),
          ),
        ],
      ],
    );
  }
}

// The phone's side.

/// How this phone names itself to the browser it signs in.
Map<String, Object?> _phoneQrMetadata(NeoCloudController controller) {
  final platformLabel = defaultTargetPlatform == TargetPlatform.iOS ? 'iPhone' : 'Android';
  // Android often reports its host name as "localhost".
  final name = controller.deviceSessions.deviceName;
  return <String, Object?>{
    'deviceLabel': name.isEmpty || name == 'localhost' ? platformLabel : name,
    'platformLabel': platformLabel,
    'browserLabel': 'NeoCloud',
    'deviceClass': 'mobile',
    'platform': controller.deviceSessions.platformName,
    'appMode': 'app',
  };
}

/// Scans a browser's sign-in code or a device-code link and acts on it.
Future<void> _scanQrCode(BuildContext context, NeoCloudController controller) async {
  final code = await scanQrCode(
    context,
    accept: (code) => code is QrLoginScanPayload || code is DeviceCodeScan,
    hint: appStrings.scanAnyHint,
  );
  if (code == null || !context.mounted) return;
  final messenger = ScaffoldMessenger.of(context);
  final server = controller.connectedServer.isNotEmpty ? controller.connectedServer : controller.client.uri('/').origin;
  if (!sameServer(code.server, server)) {
    messenger.showSnackBar(SnackBar(content: Text(appStrings.scanOtherServer(code.server.toString()))));
    return;
  }
  switch (code) {
    case QrLoginScanPayload():
      await _approveQrLogin(context, controller, code);
    case DeviceCodeScan():
      await Navigator.of(context).push<void>(
        MaterialPageRoute<void>(
          builder: (routeContext) => DeviceApprovalView(
            controller: controller,
            userCode: code.userCode,
            onDone: () => Navigator.of(routeContext).pop(),
          ),
        ),
      );
    case PairingScan():
      break;
  }
}

Future<void> _approveQrLogin(BuildContext context, NeoCloudController controller, QrLoginScanPayload code) async {
  final messenger = ScaffoldMessenger.of(context);
  final QrLoginPreview preview;
  try {
    preview = await controller.client.resolveQrLogin(code.challengeId, code.secret, _phoneQrMetadata(controller));
  } catch (error) {
    messenger.showSnackBar(SnackBar(content: Text(_qrLoginError(controller, error))));
    return;
  }
  if (!context.mounted) return;
  final approved = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    showDragHandle: true,
    backgroundColor: _bgCard,
    builder: (_) => _QrLoginApprovalSheet(controller: controller, code: code, preview: preview),
  );
  if (approved == true) {
    messenger.showSnackBar(SnackBar(content: Text(appStrings.qrLoginApproved(preview.deviceLabel))));
    return;
  }
  // Dismissed or declined: tell the browser, so it stops waiting. The code
  // expires on its own if this fails.
  unawaited(controller.client.declineQrLogin(code.challengeId, code.secret).catchError((Object _) {}));
  if (approved == false) messenger.showSnackBar(SnackBar(content: Text(appStrings.qrLoginDeclined)));
}

String _qrLoginError(NeoCloudController controller, Object error) {
  if (error is ApiException && error.statusCode == 404) return appStrings.qrLoginPreviewExpired;
  if (error is ApiException && error.code == 'QR_LOGIN_USED') return appStrings.qrLoginPreviewUsed;
  return controller.friendlyError(error);
}

/// Which browser asks, from where, and Approve / Decline. Pops true once
/// approved, false when declined.
class _QrLoginApprovalSheet extends StatefulWidget {
  const _QrLoginApprovalSheet({required this.controller, required this.code, required this.preview});

  final NeoCloudController controller;
  final QrLoginScanPayload code;
  final QrLoginPreview preview;

  @override
  State<_QrLoginApprovalSheet> createState() => _QrLoginApprovalSheetState();
}

class _QrLoginApprovalSheetState extends State<_QrLoginApprovalSheet> {
  bool _busy = false;
  String? _error;

  QrLoginPreview get _preview => widget.preview;

  IconData get _deviceIcon => switch (_preview.deviceClass) {
    'mobile' => Icons.smartphone_rounded,
    'tablet' => Icons.tablet_mac_rounded,
    'desktop' => Icons.laptop_mac_rounded,
    'server' => Icons.dns_outlined,
    _ => Icons.devices_other_outlined,
  };

  Future<void> _approve() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.controller.client.approveQrLogin(widget.code.challengeId, widget.code.secret, _phoneQrMetadata(widget.controller));
      if (mounted) Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        _error = _qrLoginError(widget.controller, error);
      });
    }
  }

  Widget _pill(IconData icon, String label) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.pill), border: Border.all(color: _border)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Icon(icon, size: 14, color: _textMuted),
          const SizedBox(width: 6),
          Text(label, style: TextStyle(color: _textSecondary, fontSize: 12)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final locale = currentAppLanguage.code;
    final canApprove = _preview.canApprove && !_preview.isExpired && !_preview.isClaimed;
    final String note;
    if (_preview.isClaimed) {
      note = appStrings.qrLoginPreviewUsed;
    } else if (_preview.isExpired) {
      note = appStrings.qrLoginPreviewExpired;
    } else {
      note = appStrings.qrLoginApproveHint(widget.controller.accountLabel);
    }
    return SingleChildScrollView(
      padding: EdgeInsets.fromLTRB(AppSpacing.lg, 0, AppSpacing.lg, AppSpacing.lg + MediaQuery.viewPaddingOf(context).bottom),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(appStrings.qrLoginApproveTitle, style: _displayTitleStyle(22)),
          const SizedBox(height: AppSpacing.md),
          _AppCard(
            padding: const EdgeInsets.all(AppSpacing.md),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(color: _accentMuted, borderRadius: BorderRadius.circular(AppRadius.control)),
                  child: Icon(_deviceIcon, color: _accent, size: 20),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(_preview.deviceLabel, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 15)),
                      const SizedBox(height: 6),
                      Row(
                        children: <Widget>[
                          Icon(Icons.place_outlined, size: 15, color: _textMuted),
                          const SizedBox(width: 6),
                          Expanded(child: Text(_preview.locationLabel, style: TextStyle(color: _textSecondary, fontSize: 13))),
                        ],
                      ),
                      if (_preview.requestedAt != null) ...<Widget>[
                        const SizedBox(height: 4),
                        Text(
                          appStrings.qrLoginRequestedAt(formatDate(_preview.requestedAt, locale)),
                          style: TextStyle(color: _textMuted, fontSize: 12.5),
                        ),
                      ],
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              if (_preview.platformLabel.isNotEmpty) _pill(Icons.devices_outlined, _preview.platformLabel),
              if (_preview.browserLabel.isNotEmpty) _pill(Icons.language_outlined, _preview.browserLabel),
            ],
          ),
          const SizedBox(height: AppSpacing.md),
          Text(note, style: TextStyle(color: _textSecondary, height: 1.45, fontSize: 13.5)),
          if (_error != null) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            _InlineError(message: _error!),
          ],
          const SizedBox(height: AppSpacing.lg),
          FilledButton.icon(
            onPressed: !canApprove || _busy ? null : () => unawaited(_approve()),
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            icon: _busy
                ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.verified_user_outlined, size: 18),
            label: Text(appStrings.qrLoginApprove),
          ),
          const SizedBox(height: AppSpacing.xs),
          OutlinedButton(
            onPressed: _busy ? null : () => Navigator.of(context).pop(false),
            style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(48)),
            child: Text(appStrings.qrLoginDecline),
          ),
        ],
      ),
    );
  }
}

/// "Scan QR code" as a card row: the phone settings list leads with it.
class _ScanQrTile extends StatelessWidget {
  const _ScanQrTile({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    return _AppCard(
      padding: EdgeInsets.zero,
      child: InkWell(
        borderRadius: BorderRadius.circular(AppRadius.card),
        onTap: () => unawaited(_scanQrCode(context, controller)),
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.md),
          child: Row(
            children: <Widget>[
              Container(
                width: 44,
                height: 44,
                decoration: BoxDecoration(color: _accentMuted, borderRadius: BorderRadius.circular(AppRadius.control)),
                child: Icon(Icons.qr_code_scanner_rounded, color: _accent, size: 22),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(appStrings.scanTitle, style: TextStyle(color: _textPrimary, fontSize: 15, fontWeight: FontWeight.w600)),
                    const SizedBox(height: 2),
                    Text(appStrings.scanQrSubtitle, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
                  ],
                ),
              ),
              Icon(Icons.chevron_right_rounded, color: _textMuted),
            ],
          ),
        ),
      ),
    );
  }
}
