part of 'main.dart';

/// Loads something for a settings page and keeps its error: the shared
/// shape of every page below.
mixin _PageLoad<T extends StatefulWidget> on State<T> {
  String? pageError;
  String? pageNotice;
  bool busy = false;

  Future<void> runAction(
    NeoCloudController controller,
    Future<void> Function() action, {
    String? notice,
  }) async {
    setState(() {
      busy = true;
      pageError = null;
      pageNotice = null;
    });
    try {
      await action();
      if (mounted) setState(() => pageNotice = notice);
    } catch (error) {
      if (mounted) setState(() => pageError = controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Widget feedback() {
    if (pageError != null) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 18),
        child: _InlineError(
          message: pageError!,
          onDismiss: () => setState(() => pageError = null),
        ),
      );
    }
    if (pageNotice != null) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 18),
        child: _InlineSuccess(message: pageNotice!),
      );
    }
    return const SizedBox.shrink();
  }
}

class _ProfileSettingsPage extends StatefulWidget {
  const _ProfileSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_ProfileSettingsPage> createState() => _ProfileSettingsPageState();
}

class _ProfileSettingsPageState extends State<_ProfileSettingsPage>
    with _PageLoad {
  late final TextEditingController _name = TextEditingController(
    text: widget.controller.user?.displayName ?? '',
  );

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final user = widget.controller.user;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.pageProfile,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.displayName,
              description: appStrings.displayNameHint,
              fillControl: true,
              control: Row(
                children: <Widget>[
                  Expanded(
                    child: TextField(
                      controller: _name,
                      decoration: const InputDecoration(isDense: true),
                    ),
                  ),
                  const SizedBox(width: 8),
                  IconButton(
                    tooltip: appStrings.save,
                    onPressed: busy
                        ? null
                        : () => unawaited(
                            runAction(widget.controller, () async {
                              await widget.controller.client.setDisplayName(
                                _name.text.trim(),
                              );
                              await widget.controller.refreshAccount();
                            }, notice: appStrings.saved),
                          ),
                    icon: const Icon(Icons.check_rounded),
                  ),
                ],
              ),
            ),
            _SettingsRow(
              label: appStrings.accountUsername,
              control: Text(
                user?.username ?? '',
                style: TextStyle(color: _textSecondary),
              ),
            ),
            _SettingsRow(
              label: appStrings.accountEmail,
              control: Text(
                user?.email ?? '—',
                style: TextStyle(color: _textSecondary),
              ),
            ),
            _SettingsRow(
              label: appStrings.accountRole,
              control: _StatusPill(
                label: (user?.isAdmin ?? false)
                    ? appStrings.roleAdmin
                    : appStrings.roleUser,
                color: (user?.isAdmin ?? false) ? _accent : _accentAlt,
              ),
            ),
            // NeoRouter's account page ends with Sign out, in the danger
            // tone; the phone disconnects instead, after asking.
            if (widget.controller.usesDeviceLogin)
              _SettingsRow(
                label: appStrings.disconnect,
                description: appStrings.disconnectDescription,
                control: OutlinedButton.icon(
                  onPressed: () => unawaited(_confirmDisconnect(context, widget.controller)),
                  style: OutlinedButton.styleFrom(foregroundColor: _danger),
                  icon: const Icon(Icons.logout_rounded, size: 16),
                  label: Text(appStrings.disconnect),
                ),
              )
            else
              _SettingsRow(
                label: appStrings.signOut,
                description: appStrings.signOutDescription,
                control: OutlinedButton.icon(
                  onPressed: () => unawaited(widget.controller.logout()),
                  style: OutlinedButton.styleFrom(foregroundColor: _danger),
                  icon: const Icon(Icons.logout_rounded, size: 16),
                  label: Text(appStrings.signOut),
                ),
              ),
          ],
        ),
        _AccountDataSection(controller: widget.controller),
      ],
    );
  }
}

class _SecuritySettingsPage extends StatefulWidget {
  const _SecuritySettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_SecuritySettingsPage> createState() => _SecuritySettingsPageState();
}

class _SecuritySettingsPageState extends State<_SecuritySettingsPage>
    with _PageLoad {
  final TextEditingController _current = TextEditingController();
  final TextEditingController _next = TextEditingController();
  Map<String, dynamic>? _twoFactor;
  List<SessionInfo>? _sessions;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _current.dispose();
    _next.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final account = await _controller.client.account();
      final sessions = await _controller.client.sessions();
      if (!mounted) return;
      setState(() {
        _twoFactor = (account['twoFactor'] as Map?)?.cast<String, dynamic>();
        _sessions = sessions;
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  _PasswordStrengthInfo get _strength => _passwordStrengthInfo(
    password: _next.text,
    username: _controller.user?.username ?? '',
    email: _controller.user?.email ?? '',
  );

  Future<void> _changePassword() => runAction(_controller, () async {
    await _controller.client.changePassword(_current.text, _next.text);
    _current.clear();
    _next.clear();
    if (mounted) setState(() {});
  }, notice: appStrings.passwordChanged);

  Future<void> _setUpTwoFactor() async {
    final password = await _promptText(
      context,
      title: appStrings.twoFactorTitle,
      label: appStrings.currentPassword,
      confirmLabel: appStrings.continueLabel,
      obscure: true,
    );
    if (password == null || !mounted) return;
    Map<String, dynamic> setup;
    try {
      setup = await _controller.client.beginTwoFactor(password);
    } catch (error) {
      setState(() => pageError = _controller.friendlyError(error));
      return;
    }
    if (!mounted) return;
    final codeController = TextEditingController();
    var verifying = false;
    String? verifyError;
    final recoveryCodes = await showDialog<List<String>>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => StatefulBuilder(
        builder: (dialogContext, setDialogState) {
          Future<void> verify() async {
            final code = codeController.text.trim();
            if (code.isEmpty || verifying) return;
            setDialogState(() { verifying = true; verifyError = null; });
            try {
              final codes = await _controller.client.enableTwoFactor(code);
              if (dialogContext.mounted) Navigator.of(dialogContext).pop(codes);
            } catch (error) {
              if (dialogContext.mounted) {
                setDialogState(() { verifying = false; verifyError = _controller.friendlyError(error); });
              }
            }
          }
          return AlertDialog(
          backgroundColor: _bgCard,
          title: Text(appStrings.twoFactorTitle),
          content: SizedBox(
            width: 420,
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Text(
                    appStrings.twoFactorSetupStep,
                    style: TextStyle(color: _textSecondary, height: 1.45),
                  ),
                  const SizedBox(height: 14),
                  if ((setup['otpauthUrl']?.toString() ?? '')
                      .isNotEmpty) ...<Widget>[
                    Center(
                      child: Container(
                        color: lightPalette.bgCard,
                        padding: const EdgeInsets.all(12),
                        child: QrImageView(
                          data: setup['otpauthUrl'].toString(),
                          version: QrVersions.auto,
                          size: 220,
                          backgroundColor: lightPalette.bgCard,
                          eyeStyle: QrEyeStyle(
                            eyeShape: QrEyeShape.square,
                            color: lightPalette.textPrimary,
                          ),
                          dataModuleStyle: QrDataModuleStyle(
                            dataModuleShape: QrDataModuleShape.square,
                            color: lightPalette.textPrimary,
                          ),
                        ),
                      ),
                    ),
                    const SizedBox(height: 14),
                  ],
                  SelectableText(
                    setup['manualKey']?.toString() ?? '',
                    textAlign: TextAlign.center,
                    style: GoogleFonts.geistMono(
                      fontSize: 16,
                      fontWeight: FontWeight.w600,
                      letterSpacing: 1.5,
                      color: _textPrimary,
                    ),
                  ),
                  const SizedBox(height: 14),
                  TextField(
                    controller: codeController,
                    keyboardType: TextInputType.number,
                    autofillHints: const <String>[AutofillHints.oneTimeCode],
                    textAlign: TextAlign.center,
                    decoration: InputDecoration(
                      labelText: appStrings.twoFactorCode,
                      errorText: verifyError,
                    ),
                    onSubmitted: (_) => unawaited(verify()),
                  ),
                ],
              ),
            ),
          ),
          actions: <Widget>[
            OutlinedButton(
              onPressed: verifying ? null : () => Navigator.of(dialogContext).pop(),
              child: Text(appStrings.cancel),
            ),
            FilledButton(
              onPressed: verifying ? null : () => unawaited(verify()),
              child: Text(appStrings.verify),
            ),
          ],
        );
        },
      ),
    );
    codeController.dispose();
    if (!mounted || recoveryCodes == null || recoveryCodes.isEmpty) return;
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        backgroundColor: _bgCard,
        title: Text(appStrings.recoveryCodesTitle),
        content: SizedBox(
          width: 420,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(
                appStrings.recoveryCodesMessage,
                style: TextStyle(color: _textSecondary, height: 1.45),
              ),
              const SizedBox(height: 14),
              SelectableText(
                recoveryCodes.join('\n'),
                style: GoogleFonts.geistMono(
                  fontSize: 14,
                  height: 1.7,
                  color: _textPrimary,
                ),
              ),
            ],
          ),
        ),
        actions: <Widget>[
          OutlinedButton.icon(
            onPressed: () => unawaited(_copyToClipboard(dialogContext, recoveryCodes.join('\n'))),
            icon: const Icon(Icons.copy_rounded, size: 16),
            label: Text(appStrings.copy),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: Text(appStrings.close),
          ),
        ],
      ),
    );
    await _load();
  }

  Future<void> _turnOffTwoFactor() async {
    final password = await _promptText(
      context,
      title: appStrings.twoFactorDisable,
      label: appStrings.currentPassword,
      confirmLabel: appStrings.continueLabel,
      obscure: true,
    );
    if (password == null || !mounted) return;
    final code = await _promptText(
      context,
      title: appStrings.twoFactorDisable,
      label: appStrings.twoFactorOrRecoveryCode,
      confirmLabel: appStrings.twoFactorDisable,
    );
    if (code == null) return;
    await runAction(
      _controller,
      () => _controller.client.disableTwoFactor(password, code),
    );
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final twoFactorOn = _twoFactor?['enabled'] == true;
    final strength = _strength;
    final locale = currentAppLanguage.code;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (_sessions != null || pageError == null) feedback(),
        _SettingsGroup(
          title: appStrings.qrPairing,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.pairPhone,
              description: appStrings.pairPhoneDescription,
              control: OutlinedButton.icon(
                onPressed: busy
                    ? null
                    : () =>
                          unawaited(showPairPhoneDialog(context, _controller)),
                icon: const Icon(Icons.qr_code_2_rounded, size: 18),
                label: Text(appStrings.showQrCode),
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.changePassword,
          children: <Widget>[
            _SettingsBlock(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  TextField(
                    controller: _current,
                    obscureText: true,
                    onChanged: (_) => setState(() {}),
                    decoration: InputDecoration(
                      labelText: appStrings.currentPassword,
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _next,
                    obscureText: true,
                    onChanged: (_) => setState(() {}),
                    decoration: InputDecoration(
                      labelText: appStrings.newPassword,
                    ),
                  ),
                  const SizedBox(height: 10),
                  _PasswordStrengthIndicator(info: strength),
                  const SizedBox(height: 14),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: FilledButton(
                      onPressed:
                          busy ||
                              !strength.acceptable ||
                              _current.text.isEmpty
                          ? null
                          : () => unawaited(_changePassword()),
                      child: Text(appStrings.changePassword),
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.twoFactorTitle,
          // NeoRouter's "2FA on" tag over the section.
          trailing: _twoFactor == null
              ? null
              : _StatusPill(
                  label: twoFactorOn ? appStrings.settingsTwoFactorOn : appStrings.settingsTwoFactorOff,
                  color: twoFactorOn ? _success : _textMuted,
                ),
          children: <Widget>[
            _SettingsRow(
              label: appStrings.twoFactorTitle,
              description: twoFactorOn
                  ? appStrings.twoFactorOn(
                      (_twoFactor?['recoveryCodesRemaining'] as num?)
                              ?.toInt() ??
                          0,
                    )
                  : appStrings.twoFactorOff,
              control: twoFactorOn
                  ? OutlinedButton(
                      onPressed: busy
                          ? null
                          : () => unawaited(_turnOffTwoFactor()),
                      style: OutlinedButton.styleFrom(foregroundColor: _danger),
                      child: Text(appStrings.twoFactorDisable),
                    )
                  : FilledButton(
                      onPressed: busy
                          ? null
                          : () => unawaited(_setUpTwoFactor()),
                      child: Text(appStrings.twoFactorEnable),
                    ),
            ),
          ],
        ),
        _SecurityKeysSection(controller: _controller),
        _SettingsGroup(
          title: appStrings.sessionsTitle,
          children: <Widget>[
            if (_sessions == null && pageError == null) const _LoadingPlaceholder(),
            if (_sessions == null && pageError != null)
              _SettingsBlock(
                child: _LoadError(
                  message: pageError!,
                  onRetry: () {
                    setState(() => pageError = null);
                    unawaited(_load());
                  },
                ),
              ),
            for (final session in _sessions ?? const <SessionInfo>[])
              _SettingsRow(
                label: session.userAgent.isEmpty
                    ? (session.ipAddress ?? '—')
                    : _shortAgent(session.userAgent),
                tag: session.current ? appStrings.sessionCurrent : null,
                description: appStrings.sessionLastSeen(
                  formatDate(session.lastSeenAt, locale),
                ),
                control: session.current
                    ? null
                    : TextButton(
                        onPressed: busy
                            ? null
                            : () => unawaited(
                                runAction(_controller, () async {
                                  await _controller.client.revokeSession(session.id);
                                  await _load();
                                }),
                              ),
                        child: Text(appStrings.sessionRevoke),
                      ),
              ),
          ],
        ),
      ],
    );
  }
}

/// "Chrome on macOS" from a user agent, close enough for a session list.
String _shortAgent(String agent) {
  final browser =
      <String, String>{
            'Edg/': 'Edge',
            'Firefox/': 'Firefox',
            'Chrome/': 'Chrome',
            'Safari/': 'Safari',
          }.entries
          .firstWhere(
            (entry) => agent.contains(entry.key),
            orElse: () => const MapEntry<String, String>('', 'Browser'),
          )
          .value;
  final os =
      <String, String>{
            'Mac OS X': 'macOS',
            'Windows': 'Windows',
            'Android': 'Android',
            'iPhone': 'iOS',
            'Linux': 'Linux',
          }.entries
          .firstWhere(
            (entry) => agent.contains(entry.key),
            orElse: () => const MapEntry<String, String>('', ''),
          )
          .value;
  return os.isEmpty ? browser : '$browser · $os';
}

class _GeneralSettingsPage extends StatelessWidget {
  const _GeneralSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final settings = controller.settings;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _SettingsGroup(
          title: appStrings.pageGeneral,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.theme,
              control: _SettingsChoice<ThemeMode>(
                value: controller.themeMode,
                options: <(ThemeMode, String)>[
                  (ThemeMode.system, appStrings.themeSystem),
                  (ThemeMode.light, appStrings.themeLight),
                  (ThemeMode.dark, appStrings.themeDark),
                ],
                onChanged: (mode) => unawaited(controller.setThemeMode(mode)),
              ),
            ),
            _SettingsRow(
              label: appStrings.language,
              control: _SettingsChoice<AppLanguage>(
                value: currentAppLanguage,
                options: <(AppLanguage, String)>[
                  for (final language in AppLanguage.values)
                    (language, language.label),
                ],
                onChanged: (language) =>
                    unawaited(controller.setLanguage(language)),
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.navFiles,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.fileView,
              control: _SettingsChoice<String>(
                value: settings.fileView,
                options: <(String, String)>[
                  ('list', appStrings.fileViewList),
                  ('grid', appStrings.fileViewGrid),
                ],
                onChanged: (view) => unawaited(
                  controller.updateSettings(<String, Object?>{
                    'fileView': view,
                  }),
                ),
              ),
            ),
            _SettingsRow(
              label: appStrings.uploadConflict,
              control: _SettingsChoice<String>(
                value: settings.uploadConflict,
                options: <(String, String)>[
                  ('ask', appStrings.uploadConflictAsk),
                  ('replace', appStrings.replace),
                  ('rename', appStrings.keepBoth),
                ],
                onChanged: (mode) => unawaited(
                  controller.updateSettings(<String, Object?>{
                    'uploadConflict': mode,
                  }),
                ),
              ),
            ),
          ],
        ),
        _SettingsGroup(
            title: appStrings.searchSettingsTitle,
            children: <Widget>[
              _SettingsRow(
                label: appStrings.searchSettingsContents,
                description: controller.fullTextSearch
                    ? appStrings.searchSettingsContentsHint
                    : appStrings.searchSettingsContentsOff,
                control: Switch(
                  value: settings.fullTextSearch,
                  onChanged: (value) => unawaited(
                    controller.updateSettings(<String, Object?>{
                      'fullTextSearch': value,
                    }),
                  ),
                ),
              ),
            ],
          ),
      ],
    );
  }
}
