part of 'main.dart';

/// Settings › Devices: whether this device appears in other devices' "Send to
/// device" lists, and (web) browser notifications for incoming files.
class _HandoffSettingsCard extends StatefulWidget {
  const _HandoffSettingsCard({required this.controller});

  final NeoCloudController controller;

  @override
  State<_HandoffSettingsCard> createState() => _HandoffSettingsCardState();
}

class _HandoffSettingsCardState extends State<_HandoffSettingsCard> {
  final BrowserNotice _notice = createBrowserNotice();
  String? _message;
  late final AppLifecycleListener _lifecycle;

  /// What receiving in the background still lacks; re-read on resume, since
  /// it is changed in the system settings.
  BackgroundReliability? _reliability;

  HandoffController get _handoff => widget.controller.handoff;
  HandoffBackground get _background => _handoff.background;

  @override
  void initState() {
    super.initState();
    _lifecycle = AppLifecycleListener(onResume: () => unawaited(_loadReliability()));
    unawaited(_loadReliability());
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    super.dispose();
  }

  Future<void> _loadReliability() async {
    if (!_background.supported || !_background.enabled) {
      if (_reliability != null && mounted) setState(() => _reliability = null);
      return;
    }
    final next = await _loadBackgroundReliability(widget.controller.cameraUploads, _receiveFeatures(widget.controller), _background);
    if (mounted) setState(() => _reliability = next);
  }

  Future<void> _setBackground(bool on) async {
    if (on) {
      await turnOnBackgroundReceiving(context, widget.controller);
    } else {
      await _background.setEnabled(false);
    }
    await _loadReliability();
  }

  Future<void> _checkBackground() async {
    await showBackgroundReliabilitySheet(
      context,
      widget.controller.cameraUploads,
      features: _receiveFeatures(widget.controller),
      handoff: _background,
    );
    await _loadReliability();
  }

  /// Permission is asked here, from the tap, never on page load.
  Future<void> _setNotify(bool on) async {
    if (!on) {
      await _handoff.setNotifyInBrowser(false);
      return;
    }
    final granted = _notice.granted || await _notice.request();
    if (!mounted) return;
    setState(() => _message = granted ? null : appStrings.handoffNotifyBlocked);
    await _handoff.setNotifyInBrowser(granted);
  }

  @override
  Widget build(BuildContext context) {
    return ListenableBuilder(
      listenable: Listenable.merge(<Listenable>[_handoff, _background]),
      builder: (context, _) => _SettingsGroup(
        title: appStrings.handoffSectionTitle,
        children: <Widget>[
          if (!kIsWeb)
            SwitchListTile(
              value: _handoff.receiving,
              onChanged: _handoff.setReceiving,
              title: Text(appStrings.handoffReceiveSwitch),
              subtitle: Text(appStrings.handoffReceiveHint),
            ),
          if (_background.supported) ...<Widget>[
            SwitchListTile(
              value: _background.enabled,
              // Receiving at all has to be allowed first.
              onChanged: _handoff.receiving ? (on) => unawaited(_setBackground(on)) : null,
              title: Text(appStrings.handoffBackgroundSwitch),
              subtitle: Text(_handoff.receiving ? appStrings.handoffBackgroundHint : appStrings.handoffBackgroundNeedsReceive),
            ),
            if (_background.enabled && (_reliability?.anyNeeded ?? false))
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                child: _BackgroundReliabilityBanner(
                  missing: true,
                  since: null,
                  title: appStrings.backgroundBannerTitleReceive,
                  onFix: () => unawaited(_checkBackground()),
                ),
              ),
          ],
          if (_notice.supported)
            SwitchListTile(
              value: _handoff.notifyInBrowser && _notice.granted,
              onChanged: (on) => unawaited(_setNotify(on)),
              title: Text(appStrings.handoffNotifySwitch),
              subtitle: Text(_message ?? appStrings.handoffNotifyHint),
            ),
        ],
      ),
    );
  }
}
