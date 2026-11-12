part of 'main.dart';

// First run of the Android and iOS apps: which server, ported from
// NeoAgent's BackendSetupView (main_setup.dart). The servers this phone
// used stand where NeoAgent lists the ones it found nearby. The server then
// opens the sign-in card (main_auth_phone.dart); its browser sign-in waits
// for approval here (docs/clients.md §1).

Future<void> _openInBrowser(Uri url) async {
  await launchUrl(url, mode: LaunchMode.externalApplication);
}

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

  /// The address field; open from the start while there is nothing to pick.
  late bool _showAdvanced = _recentServers(widget.controller).isEmpty;

  NeoCloudController get _controller => widget.controller;

  @override
  void dispose() {
    _server.dispose();
    super.dispose();
  }

  void _connect() {
    if (_controller.isConnecting) return;
    // A pasted pairing link ("Can't scan?") signs in without a password.
    final link = parsePairingLink(_server.text);
    if (link != null) {
      _server.text = link.server.toString();
      unawaited(_controller.pair(link));
      return;
    }
    unawaited(_controller.chooseServer(_server.text));
  }

  void _connectRecent(String server) {
    _server.text = server;
    _connect();
  }

  Future<void> _confirmForget(String server) async {
    final forget = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(_serverLabel(server)),
        content: Text(appStrings.connectForgetServerBody, style: TextStyle(color: _textSecondary, height: 1.45)),
        actions: <Widget>[
          TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.mobileForgetServer)),
        ],
      ),
    );
    if (forget != true) return;
    await _forgetServer(_controller, server);
    if (mounted) setState(() {});
  }

  static String _serverLabel(String server) {
    final uri = Uri.tryParse(server);
    return uri != null && uri.host.isNotEmpty ? uri.authority : server;
  }

  /// One server this phone used, as NeoAgent shows a server it found.
  Widget _recentServerTile(String server) {
    final radius = BorderRadius.circular(18);
    return Material(
      color: _bgSecondary.withValues(alpha: 0.72),
      borderRadius: radius,
      child: InkWell(
        borderRadius: radius,
        onTap: _controller.isConnecting ? null : () => _connectRecent(server),
        onLongPress: _controller.isConnecting ? null : () => unawaited(_confirmForget(server)),
        child: Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(borderRadius: radius, border: Border.all(color: _borderLight)),
          child: Row(
            children: <Widget>[
              Container(
                width: 42,
                height: 42,
                decoration: BoxDecoration(color: _accent.withValues(alpha: 0.1), borderRadius: BorderRadius.circular(14)),
                child: Icon(Icons.dns_outlined, color: _accent),
              ),
              const SizedBox(width: 13),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(_serverLabel(server), style: const TextStyle(fontWeight: FontWeight.w700)),
                    const SizedBox(height: 3),
                    Text(appStrings.connectReadyToSignIn, style: TextStyle(color: _textSecondary, fontSize: 12)),
                  ],
                ),
              ),
              const Icon(Icons.arrow_forward_rounded),
            ],
          ),
        ),
      ),
    );
  }

  List<Widget> _serverStep(BuildContext context) {
    final servers = _recentServers(_controller);
    final connecting = _controller.isConnecting;
    return <Widget>[
      if (servers.isNotEmpty) ...<Widget>[
        Text(
          appStrings.mobileRecentServers,
          style: Theme.of(context).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
        ),
        const SizedBox(height: 10),
        for (final server in servers) ...<Widget>[
          _recentServerTile(server),
          const SizedBox(height: 10),
        ],
      ],
      if (_controller.errorMessage case final message?) ...<Widget>[
        const SizedBox(height: 16),
        _InlineError(message: message),
      ],
      if (qrScannerSupported) ...<Widget>[
        const SizedBox(height: 16),
        _ScanToPairButton(controller: _controller),
      ],
      const SizedBox(height: 10),
      // ListTile needs its own Material inside the card's colored box.
      Material(
        type: MaterialType.transparency,
        child: ExpansionTile(
          initiallyExpanded: _showAdvanced,
          onExpansionChanged: (value) => setState(() => _showAdvanced = value),
          tilePadding: EdgeInsets.zero,
          childrenPadding: EdgeInsets.zero,
          title: Text(appStrings.connectEnterAddress),
          subtitle: Text(appStrings.connectEnterAddressHint),
          children: <Widget>[
            const SizedBox(height: 8),
            TextField(
              controller: _server,
              keyboardType: TextInputType.url,
              autocorrect: false,
              textInputAction: TextInputAction.done,
              onChanged: (_) => _controller.clearInlineError(),
              onSubmitted: (_) => _connect(),
              decoration: InputDecoration(
                labelText: appStrings.connectServerAddress,
                prefixIcon: const Icon(Icons.dns_outlined),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: OutlinedButton.icon(
                onPressed: connecting ? null : _connect,
                icon: connecting
                    ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                    : const Icon(Icons.arrow_forward_rounded),
                label: Text(connecting ? appStrings.connectConnecting : appStrings.connectToThisServer),
              ),
            ),
          ],
        ),
      ),
    ];
  }

  /// The browser sign-in: the code to compare while the browser approves.
  List<Widget> _approvalStep(PendingDeviceLogin login) {
    return <Widget>[
      Container(
        padding: const EdgeInsets.symmetric(vertical: 18, horizontal: 16),
        decoration: BoxDecoration(
          color: _bgSecondary.withValues(alpha: 0.72),
          borderRadius: BorderRadius.circular(18),
          border: Border.all(color: _borderLight),
        ),
        child: Column(
          children: <Widget>[
            Text(appStrings.deviceCodeLabel.toUpperCase(), style: _entryEyebrowStyle()),
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
      SizedBox(
        width: double.infinity,
        child: OutlinedButton.icon(
          onPressed: () => unawaited(_openInBrowser(login.verificationUrl)),
          icon: const Icon(Icons.open_in_new_rounded, size: 18),
          label: Text(appStrings.connectOpenBrowser),
        ),
      ),
      const SizedBox(height: AppSpacing.sm),
      Center(
        child: SelectableText(
          login.verificationUrl.toString(),
          textAlign: TextAlign.center,
          style: TextStyle(color: _textMuted, fontSize: 12),
        ),
      ),
      const SizedBox(height: AppSpacing.sm),
      Center(child: TextButton(onPressed: _controller.cancelConnect, child: Text(appStrings.cancel))),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final login = _controller.pendingLogin;
    return Theme(
      data: _phoneEntryTheme(Theme.of(context)),
      child: Builder(
        builder: (context) => _ControlSurfaceBackdrop(
          child: Scaffold(
            backgroundColor: Colors.transparent,
            body: SafeArea(
              child: Center(
                child: SingleChildScrollView(
                  padding: const EdgeInsets.all(24),
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 680),
                    child: _EntranceMotion(
                      rise: true,
                      child: Container(
                        decoration: BoxDecoration(
                          color: _bgCard,
                          borderRadius: BorderRadius.circular(34),
                          border: Border.all(color: _border),
                          boxShadow: _entryPanelShadow,
                        ),
                        clipBehavior: Clip.antiAlias,
                        child: Padding(
                          padding: const EdgeInsets.fromLTRB(34, 32, 34, 30),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Column(
                                mainAxisSize: MainAxisSize.min,
                                children: <Widget>[
                                  const NeoCloudMark(size: 60),
                                  const SizedBox(height: 18),
                                  Text(
                                    appStrings.appName,
                                    style: GoogleFonts.geist(
                                      fontSize: 28,
                                      fontWeight: FontWeight.w700,
                                      color: _textPrimary,
                                      letterSpacing: -0.4,
                                    ),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 22),
                              Text(appStrings.connectWelcome, style: _entryEyebrowStyle()),
                              const SizedBox(height: 10),
                              Text(
                                login == null ? appStrings.connectTitle : appStrings.connectApproveTitle,
                                style: TextStyle(
                                  fontSize: 34,
                                  fontWeight: FontWeight.w700,
                                  height: 1.08,
                                  letterSpacing: -0.8,
                                  color: _textPrimary,
                                ),
                              ),
                              const SizedBox(height: 12),
                              Text(
                                login == null ? appStrings.connectSubtitle : appStrings.connectApproveSubtitle,
                                style: TextStyle(color: _textSecondary, height: 1.55),
                              ),
                              const SizedBox(height: 24),
                              if (login == null) ..._serverStep(context) else ..._approvalStep(login),
                            ],
                          ),
                        ),
                      ),
                    ),
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
