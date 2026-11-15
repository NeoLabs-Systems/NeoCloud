part of 'main.dart';

// Settings › Profile › Email, ported from NeoAgent's account page: the
// current address with its confirmation status, then the address and
// current-password fields with "Save email". A changed address stays pending
// until its link is opened. All of it exists only when the server has mail;
// without mail the row stays the read-only address it always was.

/// The account's email rows for the profile group.
List<Widget> _emailSettingsRows(NeoCloudController controller) {
  final user = controller.user;
  if (!controller.emailConfigured || user == null) {
    return <Widget>[
      _SettingsRow(
        label: appStrings.accountEmail,
        control: Text(user?.email ?? '—', style: TextStyle(color: _textSecondary)),
      ),
    ];
  }
  final verified = user.email != null && user.emailVerifiedAt != null;
  return <Widget>[
    _SettingsRow(
      label: appStrings.accountEmail,
      description: user.email == null ? appStrings.emailNone : appStrings.emailCurrent(user.email!),
      control: user.email == null
          ? null
          : _StatusPill(label: verified ? appStrings.emailVerifiedLabel : appStrings.emailUnverifiedLabel, color: verified ? _success : _warning),
    ),
    _SettingsBlock(child: _EmailEditBlock(controller: controller)),
  ];
}

class _EmailEditBlock extends StatefulWidget {
  const _EmailEditBlock({required this.controller});

  final NeoCloudController controller;

  @override
  State<_EmailEditBlock> createState() => _EmailEditBlockState();
}

class _EmailEditBlockState extends State<_EmailEditBlock> {
  late final TextEditingController _email = TextEditingController(text: widget.controller.user?.email ?? '');
  final TextEditingController _password = TextEditingController();
  bool _saving = false;
  bool _resending = false;
  String? _error;
  String? _notice;

  /// What the last save answered, until the account reloads with it.
  String? _pendingFromSave;

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  String? get _pending => widget.controller.user?.pendingEmail ?? _pendingFromSave;

  Future<void> _save() async {
    if (_saving) return;
    if (_password.text.trim().isEmpty) {
      setState(() {
        _error = appStrings.emailEnterPassword;
        _notice = null;
      });
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
      _notice = null;
    });
    try {
      final answer = await widget.controller.updateAccountEmail(email: _email.text.trim(), currentPassword: _password.text);
      if (!mounted) return;
      final pending = answer['emailChangePending'] == true ? answer['pendingEmail'] as String? : null;
      setState(() {
        _pendingFromSave = pending;
        _password.clear();
        _notice = pending == null ? appStrings.emailSaved : appStrings.emailPending(pending);
      });
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _resend() async {
    setState(() {
      _resending = true;
      _error = null;
      _notice = null;
    });
    try {
      await widget.controller.resendAccountEmailConfirmation();
      if (mounted) setState(() => _notice = appStrings.emailConfirmationSent);
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _resending = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = widget.controller.user;
    final pending = _pending;
    final waiting = pending != null || (user?.email != null && user?.emailVerifiedAt == null);
    final error = _error;
    final notice = _notice;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (pending != null && notice == null) ...<Widget>[_InlineNote(message: appStrings.emailPending(pending), icon: Icons.mark_email_unread_outlined), const SizedBox(height: 14)],
        TextField(
          controller: _email,
          keyboardType: TextInputType.emailAddress,
          autocorrect: false,
          enableSuggestions: false,
          autofillHints: const <String>[AutofillHints.email],
          decoration: InputDecoration(labelText: appStrings.email),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _password,
          obscureText: true,
          autocorrect: false,
          enableSuggestions: false,
          onSubmitted: (_) => unawaited(_save()),
          decoration: InputDecoration(labelText: appStrings.currentPassword, helperText: appStrings.emailPasswordHelper),
        ),
        if (error != null) ...<Widget>[const SizedBox(height: 10), _InlineError(message: error)],
        if (notice != null) ...<Widget>[const SizedBox(height: 10), _InlineSuccess(message: notice)],
        const SizedBox(height: 14),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            FilledButton(
              onPressed: _saving ? null : () => unawaited(_save()),
              child: _saving ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.emailSave),
            ),
            if (waiting)
              TextButton(onPressed: _resending || _saving ? null : () => unawaited(_resend()), child: Text(appStrings.emailResendConfirmation)),
          ],
        ),
      ],
    );
  }
}
