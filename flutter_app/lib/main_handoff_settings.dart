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

  HandoffController get _handoff => widget.controller.handoff;

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
      listenable: _handoff,
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
