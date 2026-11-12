part of 'main.dart';

/// Whether the account has 2FA on, so a confirmation asks for a code too.
Future<bool> _twoFactorEnabled(NeoCloudController controller) async =>
    ((await controller.client.account())['twoFactor'] as Map?)?['enabled'] == true;

/// Settings › Profile › Your data, ported from NeoRouter's
/// AccountDataSection: export the account's data, delete the account. Each
/// asks for the password again, and for a code when 2FA is on; deleting also
/// asks for the username.
class _AccountDataSection extends StatelessWidget {
  const _AccountDataSection({required this.controller});

  final NeoCloudController controller;

  String _fileName() {
    final date = DateTime.now().toIso8601String().substring(0, 10);
    return 'neocloud-account-${controller.user?.username ?? 'account'}-$date.json';
  }

  void _snack(BuildContext context, String message) =>
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(message)));

  Future<T?> _guard<T>(BuildContext context, Future<T> Function() action) async {
    try {
      return await action();
    } catch (error) {
      if (context.mounted) _snack(context, controller.friendlyError(error));
      return null;
    }
  }

  Future<void> _export(BuildContext context) async {
    final twoFactor = await _guard(context, () => _twoFactorEnabled(controller));
    if (twoFactor == null || !context.mounted) return;
    final data = await showAdaptiveSheet<Map<String, dynamic>>(
      context: context,
      builder: (_) => _ConfirmIdentityDialog<Map<String, dynamic>>(
        title: appStrings.accountExportTitle,
        confirmLabel: appStrings.accountExport,
        twoFactor: twoFactor,
        action: (password, code, _) => controller.client.exportAccountData(password, code),
      ),
    );
    if (data == null || !context.mounted) return;
    final bytes = Uint8List.fromList(utf8.encode(const JsonEncoder.withIndent('  ').convert(data)));
    final saved = await _guard(context, () async {
      await saveBytes(bytes, _fileName(), mime: 'application/json');
      return true;
    });
    if (saved == true && context.mounted) _snack(context, appStrings.accountExported);
  }

  Future<void> _delete(BuildContext context) async {
    final twoFactor = await _guard(context, () => _twoFactorEnabled(controller));
    if (twoFactor == null || !context.mounted) return;
    final username = controller.user?.username ?? '';
    final done = await showAdaptiveSheet<bool>(
      context: context,
      builder: (_) => _ConfirmIdentityDialog<bool>(
        title: appStrings.accountDeleteTitle,
        message: appStrings.accountDeleteConfirm,
        confirmLabel: appStrings.accountDelete,
        twoFactor: twoFactor,
        confirmUsername: username,
        destructive: true,
        action: (password, code, typed) async {
          await controller.client.deleteMyAccount(password, code, typed ?? '');
          return true;
        },
      ),
    );
    if (done != true || !context.mounted) return;
    _snack(context, appStrings.accountDeleted);
    await controller.logout();
  }

  @override
  Widget build(BuildContext context) {
    return _SettingsGroup(
      title: appStrings.accountDataTitle,
      children: <Widget>[
        _SettingsRow(
          label: appStrings.accountExportTitle,
          description: appStrings.accountExportHint,
          control: OutlinedButton(onPressed: () => unawaited(_export(context)), child: Text(appStrings.accountExport)),
        ),
        _SettingsRow(
          label: appStrings.accountDeleteTitle,
          description: appStrings.accountDeleteHint,
          control: OutlinedButton(
            onPressed: () => unawaited(_delete(context)),
            style: OutlinedButton.styleFrom(foregroundColor: _danger),
            child: Text(appStrings.accountDelete),
          ),
        ),
      ],
    );
  }
}

/// Password (and code, and for deleting the username) again, then runs the
/// action; wrong entries are shown in the dialog so they can be corrected.
/// Used by Your data, adding a security key and the master key export.
class _ConfirmIdentityDialog<T> extends StatefulWidget {
  const _ConfirmIdentityDialog({
    required this.title,
    required this.confirmLabel,
    required this.twoFactor,
    required this.action,
    this.message,
    this.confirmUsername,
    this.destructive = false,
  });

  final String title;
  final String? message;
  final String confirmLabel;
  final bool twoFactor;

  /// When set, the username to type before the action is allowed.
  final String? confirmUsername;
  final bool destructive;
  final Future<T> Function(String password, String? code, String? typedUsername) action;

  @override
  State<_ConfirmIdentityDialog<T>> createState() => _ConfirmIdentityDialogState<T>();
}

class _ConfirmIdentityDialogState<T> extends State<_ConfirmIdentityDialog<T>> {
  final TextEditingController _password = TextEditingController();
  final TextEditingController _code = TextEditingController();
  final TextEditingController _username = TextEditingController();
  bool _working = false;
  String? _error;

  @override
  void dispose() {
    _password.dispose();
    _code.dispose();
    _username.dispose();
    super.dispose();
  }

  bool get _usernameMatches {
    final expected = widget.confirmUsername;
    return expected == null || _username.text.trim().toLowerCase() == expected.toLowerCase();
  }

  bool get _ready => _password.text.isNotEmpty && (!widget.twoFactor || _code.text.trim().length >= 6) && _usernameMatches;

  String _errorText(Object error) {
    if (error is ApiException) {
      switch (error.code) {
        case 'WRONG_PASSWORD':
          return appStrings.accountWrongPassword;
        case 'INVALID_CODE':
        case 'TWO_FACTOR_REQUIRED':
          return appStrings.accountWrongCode;
        case 'LAST_ADMIN':
          return appStrings.accountLastAdmin;
        case 'CONFIRMATION_MISMATCH':
          return appStrings.accountConfirmMismatch;
        case 'LOGIN_LOCKED':
          final wait = error.retryAfter;
          if (wait == null) return appStrings.signInRateLimited;
          return wait.inSeconds >= 90
              ? appStrings.confirmLockedMinutes((wait.inSeconds / 60).ceil())
              : appStrings.confirmLockedSeconds(math.max(1, wait.inSeconds));
        case 'RATE_LIMITED':
          return appStrings.signInRateLimited;
      }
    }
    return error is ApiException && error.message.isNotEmpty ? error.message : '$error';
  }

  Future<void> _submit() async {
    if (!_ready || _working) return;
    setState(() {
      _working = true;
      _error = null;
    });
    try {
      final result = await widget.action(
        _password.text,
        widget.twoFactor ? _code.text.trim() : null,
        widget.confirmUsername == null ? null : _username.text.trim(),
      );
      if (mounted) Navigator.of(context).pop(result);
    } catch (error) {
      if (mounted) {
        setState(() {
          _working = false;
          _error = _errorText(error);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final hint = TextStyle(color: _textSecondary, height: 1.45);
    final username = widget.confirmUsername;
    return _AdaptiveDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 400,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (widget.message != null) ...<Widget>[Text(widget.message!, style: hint), const SizedBox(height: 14)],
            Text(widget.twoFactor ? appStrings.accountConfirmPasswordCode : appStrings.accountConfirmPassword, style: hint),
            const SizedBox(height: 14),
            TextField(
              controller: _password,
              obscureText: true,
              autofocus: true,
              decoration: InputDecoration(labelText: appStrings.currentPassword),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => unawaited(_submit()),
            ),
            if (widget.twoFactor) ...<Widget>[
              const SizedBox(height: 10),
              TextField(
                controller: _code,
                autofillHints: const <String>[AutofillHints.oneTimeCode],
                decoration: InputDecoration(labelText: appStrings.twoFactorOrRecoveryCode),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => unawaited(_submit()),
              ),
            ],
            if (username != null) ...<Widget>[
              const SizedBox(height: 14),
              Text(appStrings.accountDeleteTypeUsername(username), style: hint),
              const SizedBox(height: 10),
              TextField(
                controller: _username,
                autocorrect: false,
                enableSuggestions: false,
                decoration: InputDecoration(labelText: appStrings.accountUsername),
                onChanged: (_) => setState(() {}),
                onSubmitted: (_) => unawaited(_submit()),
              ),
            ],
            if (_error != null) ...<Widget>[const SizedBox(height: 10), _InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _working ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _ready && !_working ? () => unawaited(_submit()) : null,
          style: widget.destructive ? FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white) : null,
          child: _working
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: Colors.white))
              : Text(widget.confirmLabel),
        ),
      ],
    );
  }
}
