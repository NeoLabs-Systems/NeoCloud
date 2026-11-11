part of 'main.dart';

// First run of the Android and iOS apps: which server, then a device login
// approved in the browser (docs/clients.md §1). Same card as the
// sign-in screen.

const String _recentServersKey = 'connect.recentServers';

/// Servers this device connected to, newest first.
List<String> _recentServers(NeoCloudController controller) => controller._preferences.getStringList(_recentServersKey) ?? const <String>[];

Future<void> _rememberServer(NeoCloudController controller, String server) async {
  final next = <String>[server, ..._recentServers(controller).where((entry) => entry != server)].take(5).toList();
  await controller._preferences.setStringList(_recentServersKey, next);
}

Future<void> _forgetServer(NeoCloudController controller, String server) =>
    controller._preferences.setStringList(_recentServersKey, _recentServers(controller).where((entry) => entry != server).toList());

class ConnectView extends StatefulWidget {
  const ConnectView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<ConnectView> createState() => _ConnectViewState();
}

class _ConnectViewState extends State<ConnectView> {
  late final TextEditingController _server = TextEditingController(text: widget.controller.connectedServer);

  NeoCloudController get _controller => widget.controller;

  @override
  void dispose() {
    _server.dispose();
    super.dispose();
  }

  Future<void> _openInBrowser(Uri url) async {
    await launchUrl(url, mode: LaunchMode.externalApplication);
  }

  void _connect() {
    if (_controller.isConnecting) return;
    // A pasted pairing link ("Can't scan?") signs in without the browser.
    final link = parsePairingLink(_server.text);
    if (link != null) {
      _server.text = link.server.toString();
      unawaited(_controller.pair(link));
      return;
    }
    unawaited(_controller.connect(_server.text, openVerification: _openInBrowser));
  }

  List<Widget> _serverStep() {
    return <Widget>[
      TextField(
        controller: _server,
        autofocus: _server.text.isEmpty,
        keyboardType: TextInputType.url,
        autocorrect: false,
        onChanged: (_) => _controller.clearInlineError(),
        onSubmitted: (_) => _connect(),
        decoration: InputDecoration(labelText: appStrings.connectServerLabel, hintText: 'cloud.example.com'),
      ),
      if (_controller.errorMessage != null) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        _InlineError(message: _controller.errorMessage!),
      ],
      ..._recentServerChips(),
      const SizedBox(height: AppSpacing.lg),
      FilledButton(
        onPressed: _controller.isConnecting ? null : _connect,
        child: _controller.isConnecting
            ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
            : Text(appStrings.connectButton),
      ),
      if (qrScannerSupported) ...<Widget>[
        const SizedBox(height: AppSpacing.lg),
        _ScanToPairButton(controller: _controller),
      ],
    ];
  }

  List<Widget> _recentServerChips() {
    final servers = _recentServers(_controller);
    if (servers.isEmpty) return const <Widget>[];
    return <Widget>[
      const SizedBox(height: AppSpacing.md),
      Text(appStrings.mobileRecentServers.toUpperCase(), style: _sectionEyebrowStyle()),
      const SizedBox(height: 8),
      Wrap(
        spacing: 8,
        runSpacing: 8,
        children: <Widget>[
          for (final server in servers)
            InputChip(
              label: Text(Uri.tryParse(server)?.host.isNotEmpty == true ? Uri.parse(server).authority : server),
              selected: normalizeBackendUrl(_server.text) == server,
              showCheckmark: false,
              tooltip: server,
              onPressed: _controller.isConnecting
                  ? null
                  : () => setState(() {
                      _server.text = server;
                      _controller.clearInlineError();
                    }),
              deleteButtonTooltipMessage: appStrings.mobileForgetServer,
              onDeleted: () async {
                await _forgetServer(_controller, server);
                if (mounted) setState(() {});
              },
            ),
        ],
      ),
    ];
  }

  List<Widget> _approvalStep(PendingDeviceLogin login) {
    return <Widget>[
      _AppCard(
        padding: const EdgeInsets.symmetric(vertical: 18, horizontal: 16),
        child: Column(
          children: <Widget>[
            Text(appStrings.deviceCodeLabel.toUpperCase(), style: _sectionEyebrowStyle()),
            const SizedBox(height: 8),
            SelectableText(
              login.userCode,
              textAlign: TextAlign.center,
              style: GoogleFonts.geistMono(fontSize: 28, fontWeight: FontWeight.w600, letterSpacing: 4, color: _textPrimary),
            ),
          ],
        ),
      ),
      const SizedBox(height: AppSpacing.md),
      Row(
        children: <Widget>[
          SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 1.6, color: _textMuted)),
          const SizedBox(width: 10),
          Expanded(child: Text(appStrings.connectWaiting, style: TextStyle(color: _textSecondary, fontSize: 13.5))),
        ],
      ),
      const SizedBox(height: AppSpacing.lg),
      OutlinedButton.icon(
        onPressed: () => unawaited(_openInBrowser(login.verificationUrl)),
        icon: const Icon(Icons.open_in_new_rounded, size: 16),
        label: Text(appStrings.connectOpenBrowser),
      ),
      const SizedBox(height: AppSpacing.sm),
      SelectableText(
        login.verificationUrl.toString(),
        textAlign: TextAlign.center,
        style: TextStyle(color: _textMuted, fontSize: 12),
      ),
      const SizedBox(height: AppSpacing.sm),
      TextButton(onPressed: _controller.cancelConnect, child: Text(appStrings.cancel)),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final login = _controller.pendingLogin;
    return _ControlSurfaceBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: _EntranceMotion(
                child: _GlassSurface(
                  width: 420,
                  padding: const EdgeInsets.all(AppSpacing.xl),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      const Center(child: _BrandLockup(logoSize: 44, showName: false)),
                      const SizedBox(height: AppSpacing.md),
                      Text(login == null ? appStrings.connectTitle : appStrings.connectApproveTitle, style: _displayTitleStyle(24)),
                      const SizedBox(height: AppSpacing.sm),
                      Text(
                        login == null ? appStrings.connectSubtitle : appStrings.connectApproveSubtitle,
                        style: TextStyle(color: _textSecondary, height: 1.45),
                      ),
                      const SizedBox(height: AppSpacing.lg),
                      if (login == null) ..._serverStep() else ..._approvalStep(login),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
