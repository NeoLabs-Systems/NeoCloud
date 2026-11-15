part of 'main.dart';

// Admin › Access › Email: NeoAgent's "Service email" card (sender, SMTP
// server, link lifetime, the TLS and notice switches, a write-only password)
// plus a test message that goes through the saved settings and shows the mail
// server's own answer. Saving changes what every account sees: with mail
// ready, sign-in offers password reset and sign-ups confirm their address.

typedef _EmailToggle = ({String key, String title, String subtitle});

List<_EmailToggle> _emailToggles() => <_EmailToggle>[
  (key: 'smtpSecure', title: appStrings.emailImplicitTls, subtitle: appStrings.emailImplicitTlsHelper),
  (key: 'smtpRequireTls', title: appStrings.emailRequireStarttls, subtitle: appStrings.emailRequireStarttlsHelper),
  (key: 'smtpRejectUnauthorized', title: appStrings.emailRejectInvalidTls, subtitle: appStrings.emailRejectInvalidTlsHelper),
  (key: 'requireSignupConfirmation', title: appStrings.emailConfirmSignups, subtitle: appStrings.emailConfirmSignupsHelper),
  (key: 'requireEmailChangeConfirmation', title: appStrings.emailConfirmChanges, subtitle: appStrings.emailConfirmChangesHelper),
  (key: 'notifyNewLogin', title: appStrings.emailNotifyLogin, subtitle: appStrings.emailNotifyLoginHelper),
  (key: 'notifyAccountChanges', title: appStrings.emailNotifyChanges, subtitle: appStrings.emailNotifyChangesHelper),
  (key: 'signingMails', title: appStrings.emailSigningMails, subtitle: appStrings.emailSigningMailsHelper),
];

/// What the server calls a missing setting, in the words of the form.
String _emailMissingLabel(String key) => switch (key) {
  'from' => appStrings.emailSenderAddress,
  'smtpHost' => appStrings.emailSmtpHost,
  _ => key,
};

/// A whole number within [min]..[max], else null.
int? _emailBoundedInt(String text, {required int min, required int max}) {
  final value = int.tryParse(text.trim());
  return value != null && value >= min && value <= max ? value : null;
}

/// Null for a blank or http(s) address with a host, else what is wrong.
String? _emailUrlProblem(String text, String label) {
  final value = text.trim();
  if (value.isEmpty) return null;
  final uri = Uri.tryParse(value);
  final isHttp = uri != null && (uri.scheme == 'http' || uri.scheme == 'https') && uri.host.isNotEmpty;
  return isHttp ? null : appStrings.emailUrlInvalid(label);
}

class _AdminEmailCard extends StatefulWidget {
  const _AdminEmailCard({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminEmailCard> createState() => _AdminEmailCardState();
}

class _AdminEmailCardState extends State<_AdminEmailCard> {
  static const List<String> _textKeys = <String>['from', 'smtpHost', 'smtpUser', 'replyTo', 'brandName', 'publicUrl', 'supportUrl'];

  final Map<String, TextEditingController> _text = <String, TextEditingController>{for (final key in _textKeys) key: TextEditingController()};
  final TextEditingController _port = TextEditingController();
  final TextEditingController _ttl = TextEditingController();
  final TextEditingController _password = TextEditingController();
  late final TextEditingController _testTo = TextEditingController(text: widget.controller.user?.email ?? '');
  final Map<String, bool> _flags = <String, bool>{};

  bool _loading = true;
  String? _loadError;
  bool _saving = false;
  String? _saveError;
  String? _saveNotice;
  bool _testing = false;
  String? _testError;
  String? _testNotice;

  bool _configured = false;
  List<String> _missing = const <String>[];
  bool _passwordStored = false;
  bool _obscured = true;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_fetch());
  }

  @override
  void dispose() {
    for (final input in _text.values) {
      input.dispose();
    }
    _port.dispose();
    _ttl.dispose();
    _password.dispose();
    _testTo.dispose();
    super.dispose();
  }

  Future<void> _fetch() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final data = await _controller.client.adminEmailConfig();
      if (!mounted) return;
      setState(() {
        _apply(data);
        _loading = false;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          _loadError = _controller.friendlyError(error);
        });
      }
    }
  }

  /// Takes the `{configured, missing, settings}` shape. The password never
  /// comes back: only whether one is stored.
  void _apply(Map<String, dynamic> data) {
    final settings = (data['settings'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    _text.forEach((key, input) => input.text = settings[key]?.toString() ?? '');
    _port.text = settings['smtpPort']?.toString() ?? '';
    _ttl.text = settings['tokenTtlHours']?.toString() ?? '';
    for (final toggle in _emailToggles()) {
      _flags[toggle.key] = settings[toggle.key] == true;
    }
    _passwordStored = settings['smtpPasswordConfigured'] == true;
    _configured = data['configured'] == true;
    _missing = ((data['missing'] as List?) ?? const <dynamic>[]).map((item) => '$item').toList();
    _password.clear();
    // The sign-in cards follow at once, not at the next page load.
    _controller.applyEmailConfigured(configured: _configured, confirmSignups: _flags['requireSignupConfirmation'] == true);
  }

  Future<void> _save({bool clearPassword = false}) async {
    final port = _emailBoundedInt(_port.text, min: 1, max: 65535);
    final ttl = _emailBoundedInt(_ttl.text, min: 1, max: 8760);
    final String? problem =
        (port == null ? appStrings.emailPortInvalid : null) ??
        (ttl == null ? appStrings.emailLifetimeInvalid : null) ??
        _emailUrlProblem(_text['publicUrl']!.text, appStrings.emailPublicUrl) ??
        _emailUrlProblem(_text['supportUrl']!.text, appStrings.emailSupportUrl);
    if (problem != null) {
      setState(() {
        _saveError = problem;
        _saveNotice = null;
      });
      return;
    }
    setState(() {
      _saving = true;
      _saveError = null;
      _saveNotice = null;
    });
    try {
      final answer = await _controller.client.adminSaveEmailConfig(<String, Object?>{
        for (final entry in _text.entries) entry.key: entry.value.text.trim(),
        ..._flags,
        'smtpPort': port,
        'tokenTtlHours': ttl,
        'smtpPassword': clearPassword ? '' : _password.text.trim(),
        'clearSmtpPassword': clearPassword,
      });
      // The answer has the GET shape; if it doesn't, ask again.
      final saved = answer['settings'] is Map ? answer : await _controller.client.adminEmailConfig();
      if (!mounted) return;
      setState(() {
        _apply(saved);
        _saveNotice = clearPassword ? appStrings.emailPasswordRemoved : appStrings.emailSettingsSaved;
      });
    } catch (error) {
      if (mounted) setState(() => _saveError = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _removePassword() => _confirmDelete(
    context,
    title: appStrings.emailRemovePasswordTitle,
    message: appStrings.emailRemovePasswordBody,
    confirmLabel: appStrings.emailRemove,
    onConfirm: () => _save(clearPassword: true),
  );

  Future<void> _sendTest() async {
    final to = _testTo.text.trim();
    if (to.isEmpty) {
      setState(() {
        _testError = appStrings.emailTestNeedsAddress;
        _testNotice = null;
      });
      return;
    }
    setState(() {
      _testing = true;
      _testError = null;
      _testNotice = null;
    });
    try {
      await _controller.client.adminSendTestEmail(to);
      if (mounted) setState(() => _testNotice = appStrings.emailTestSent(to));
    } catch (error) {
      // The mail server's own words, so a wrong host or login can be told apart.
      if (mounted) setState(() => _testError = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  void _toggle(String key, bool value) => setState(() {
    _flags[key] = value;
    _saveNotice = null;
  });

  Widget _field(String key, String label, {String? hint, String? helper, TextInputType? type}) => TextField(
    controller: _text[key],
    keyboardType: type,
    autocorrect: false,
    enableSuggestions: false,
    onChanged: (_) => setState(() => _saveNotice = null),
    decoration: InputDecoration(labelText: label, hintText: hint, helperText: helper, helperMaxLines: 3),
  );

  Widget _numberField(TextEditingController controller, String label, {String? hint, String? helper}) => TextField(
    controller: controller,
    keyboardType: TextInputType.number,
    inputFormatters: <TextInputFormatter>[FilteringTextInputFormatter.digitsOnly],
    onChanged: (_) => setState(() => _saveNotice = null),
    decoration: InputDecoration(labelText: label, hintText: hint, helperText: helper, helperMaxLines: 3),
  );

  /// The write-only SMTP password: leaving it blank keeps the stored one.
  Widget _passwordField() => TextField(
    controller: _password,
    obscureText: _obscured,
    autocorrect: false,
    enableSuggestions: false,
    onChanged: (_) => setState(() => _saveNotice = null),
    decoration: InputDecoration(
      labelText: appStrings.emailSmtpPassword,
      hintText: _passwordStored ? appStrings.emailLeaveBlank : null,
      helperText: _passwordStored ? appStrings.emailValueStored : appStrings.emailNothingStored,
      suffixIcon: IconButton(
        tooltip: _obscured ? appStrings.aiShow : appStrings.aiHide,
        onPressed: () => setState(() => _obscured = !_obscured),
        icon: Icon(_obscured ? Icons.visibility_outlined : Icons.visibility_off_outlined),
      ),
    ),
  );

  /// Two columns where there is room, one on a phone.
  Widget _grid(List<Widget> children, {Set<int> wide = const <int>{}}) {
    return LayoutBuilder(
      builder: (context, constraints) {
        const gap = 14.0;
        final full = constraints.maxWidth;
        final half = full >= 560 ? ((full - gap) / 2).floorToDouble() : full;
        return Wrap(
          spacing: gap,
          runSpacing: gap,
          children: <Widget>[
            for (final (index, child) in children.indexed) SizedBox(width: wide.contains(index) ? full : half, child: child),
          ],
        );
      },
    );
  }

  Widget _form() {
    final saveError = _saveError;
    final saveNotice = _saveNotice;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (!_configured && _missing.isNotEmpty) ...<Widget>[
          _InlineNote(
            icon: Icons.warning_amber_rounded,
            color: _warning,
            message: appStrings.emailMissingNote(_missing.map(_emailMissingLabel).join(', ')),
          ),
          const SizedBox(height: 16),
        ],
        _grid(<Widget>[
          _field('from', appStrings.emailSenderAddress, hint: appStrings.emailSenderHint, type: TextInputType.emailAddress),
          _field('smtpHost', appStrings.emailSmtpHost, hint: 'smtp.example.com', type: TextInputType.url),
          _numberField(_port, appStrings.emailSmtpPort, hint: '587'),
          _field('smtpUser', appStrings.emailSmtpUser),
          _passwordField(),
          _field('replyTo', appStrings.emailReplyTo, type: TextInputType.emailAddress),
          _field('brandName', appStrings.emailBrandName, hint: 'NeoCloud'),
          _field('publicUrl', appStrings.emailPublicUrl, helper: appStrings.emailPublicUrlHelper, type: TextInputType.url),
          _field('supportUrl', appStrings.emailSupportUrl, type: TextInputType.url),
          _numberField(_ttl, appStrings.emailLinkLifetime, helper: appStrings.emailLinkLifetimeHelper),
        ], wide: const <int>{0}),
        const SizedBox(height: 12),
        _grid(<Widget>[
          for (final toggle in _emailToggles())
            SwitchListTile(
              value: _flags[toggle.key] ?? false,
              contentPadding: EdgeInsets.zero,
              title: Text(toggle.title),
              subtitle: Text(toggle.subtitle),
              onChanged: (value) => _toggle(toggle.key, value),
            ),
        ]),
        const SizedBox(height: 16),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            FilledButton.icon(
              onPressed: _saving ? null : () => unawaited(_save()),
              icon: _saving
                  ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
                  : const Icon(Icons.check_rounded, size: 18),
              label: Text(_saving ? appStrings.saving : appStrings.emailSaveSettings),
            ),
            if (_passwordStored)
              TextButton.icon(
                onPressed: _saving ? null : () => unawaited(_removePassword()),
                style: TextButton.styleFrom(foregroundColor: _danger),
                icon: const Icon(Icons.delete_outline, size: 18),
                label: Text(appStrings.emailRemovePassword),
              ),
          ],
        ),
        if (saveError != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: saveError)],
        if (saveNotice != null) ...<Widget>[const SizedBox(height: 12), _InlineSuccess(message: saveNotice)],
        const SizedBox(height: 20),
        Divider(height: 1, color: _border),
        const SizedBox(height: 20),
        _testBlock(),
      ],
    );
  }

  /// The mail server's answer to a real message, shown where it was asked.
  Widget _testBlock() {
    final error = _testError;
    final notice = _testNotice;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.emailTestTitle, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: _textPrimary)),
        const SizedBox(height: 4),
        Text(appStrings.emailTestHelper, style: TextStyle(fontSize: 13, color: _textSecondary, height: 1.45)),
        const SizedBox(height: 12),
        _grid(<Widget>[
          TextField(
            controller: _testTo,
            keyboardType: TextInputType.emailAddress,
            autocorrect: false,
            enableSuggestions: false,
            onSubmitted: (_) {
              if (_configured && !_testing) unawaited(_sendTest());
            },
            decoration: InputDecoration(labelText: appStrings.emailTestTo),
          ),
          Align(
            alignment: Alignment.centerLeft,
            child: OutlinedButton.icon(
              onPressed: _testing || !_configured ? null : () => unawaited(_sendTest()),
              icon: _testing ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.send_outlined, size: 18),
              label: Text(appStrings.emailTestSend),
            ),
          ),
        ]),
        if (error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: error)],
        if (notice != null) ...<Widget>[const SizedBox(height: 12), _InlineSuccess(message: notice)],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final loaded = !_loading && _loadError == null;
    final Widget body;
    if (_loading) {
      body = const _LoadingPlaceholder();
    } else if (_loadError != null) {
      body = _LoadError(message: _loadError!, onRetry: () => unawaited(_fetch()));
    } else {
      body = _form();
    }
    return Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          _SettingsHeading(
            title: appStrings.emailAdminTitle,
            description: appStrings.emailAdminDescription,
            trailing: loaded ? _StatusPill(label: _configured ? appStrings.emailReady : appStrings.emailNotConfigured, color: _configured ? _success : _warning) : null,
          ),
          _AppCard(child: body),
        ],
      ),
    );
  }
}
