part of 'main.dart';

// Settings › Devices: connected apps and their tokens, pairing a phone by
// QR code, and WebDAV app passwords (docs/clients.md §1, §5, §6).

/// Device tokens with this platform are WebDAV app passwords.
const String _webDavPlatform = 'webdav';

class _DevicesSettingsPage extends StatefulWidget {
  const _DevicesSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_DevicesSettingsPage> createState() => _DevicesSettingsPageState();
}

class _DevicesSettingsPageState extends State<_DevicesSettingsPage> with _PageLoad {
  List<DeviceInfo>? _devices;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final devices = await _controller.client.devices();
      if (mounted) setState(() => _devices = devices);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _pairPhone() async {
    await showPairPhoneDialog(context, _controller);
    await _load();
  }

  /// A token typed into a client by hand, for clients without a browser.
  Future<void> _connect() async {
    final name = await _promptText(context, title: appStrings.deviceConnect, label: appStrings.deviceName, confirmLabel: appStrings.create);
    if (name == null || !mounted) return;
    String? token;
    await runAction(_controller, () async {
      token = await _controller.client.createDevice(name);
    });
    if (token == null || !mounted) return;
    await _showSecretOnce(context, title: appStrings.deviceTokenTitle, message: appStrings.deviceTokenMessage, secret: token!);
    await _load();
  }

  Future<void> _createAppPassword() async {
    final name = await _promptText(
      context,
      title: appStrings.webDavCreatePassword,
      label: appStrings.webDavPasswordName,
      confirmLabel: appStrings.create,
      initialValue: appStrings.webDavPasswordDefaultName,
    );
    if (name == null || !mounted) return;
    String? password;
    await runAction(_controller, () async {
      password = await _controller.client.createAppPassword(name);
    });
    if (password == null || !mounted) return;
    await _showSecretOnce(context, title: appStrings.webDavPasswordTitle, message: appStrings.webDavPasswordMessage, secret: password!);
    await _load();
  }

  Future<void> _revoke(DeviceInfo device) => _confirmDelete(
    context,
    title: appStrings.deviceRevokeTitle(device.name),
    message: device.platform == _webDavPlatform ? appStrings.webDavRevokeMessage : appStrings.deviceRevokeMessage,
    confirmLabel: appStrings.deviceRevoke,
    onConfirm: () => runAction(_controller, () async {
      await _controller.client.revokeDevice(device.id);
      await _load();
    }),
  );

  /// The system a client reported, written as people know it.
  static String _platformName(String platform) {
    const names = <String, String>{'android': 'Android', 'ios': 'iOS', 'macos': 'macOS', 'windows': 'Windows', 'linux': 'Linux', 'web': 'Web'};
    return names[platform.toLowerCase()] ?? platform;
  }

  Widget _deviceRow(DeviceInfo device) {
    final webDav = device.platform == _webDavPlatform;
    final locale = currentAppLanguage.code;
    final String? tag;
    if (webDav) {
      tag = appStrings.webDavTag;
    } else {
      tag = device.platform.isEmpty ? null : _platformName(device.platform);
    }
    return _SettingsRow(
      label: device.name,
      tag: tag,
      description: device.lastSeenAt == null ? appStrings.deviceNeverSeen : appStrings.deviceLastSeen(formatDate(device.lastSeenAt, locale)),
      control: TextButton(onPressed: busy ? null : () => unawaited(_revoke(device)), child: Text(appStrings.deviceRevoke)),
    );
  }

  @override
  Widget build(BuildContext context) {
    final devices = _devices;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.pageDevices,
          trailing: Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              OutlinedButton.icon(
                onPressed: busy ? null : () => unawaited(_connect()),
                icon: const Icon(Icons.key_rounded, size: 18),
                label: Text(appStrings.deviceConnect),
              ),
              FilledButton.icon(
                onPressed: busy ? null : () => unawaited(_pairPhone()),
                icon: const Icon(Icons.qr_code_2_rounded, size: 18),
                label: Text(appStrings.pairPhone),
              ),
            ],
          ),
          children: <Widget>[
            if (devices == null) const _LoadingPlaceholder(),
            if (devices != null && devices.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.devicesEmpty)),
            for (final device in devices ?? const <DeviceInfo>[]) _deviceRow(device),
          ],
        ),
        _WebDavCard(controller: _controller, busy: busy, onCreatePassword: () => unawaited(_createAppPassword())),
      ],
    );
  }
}

/// A secret the server returns once: shown with Copy, gone after Close.
Future<void> _showSecretOnce(BuildContext context, {required String title, required String message, required String secret}) {
  return showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: _bgCard,
      title: Text(title),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(message, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: 14),
            SelectableText(secret, style: GoogleFonts.geistMono(fontSize: 13, color: _textPrimary)),
          ],
        ),
      ),
      actions: <Widget>[
        OutlinedButton.icon(
          onPressed: () => unawaited(Clipboard.setData(ClipboardData(text: secret))),
          icon: const Icon(Icons.copy_rounded, size: 16),
          label: Text(appStrings.copy),
        ),
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close)),
      ],
    ),
  );
}
