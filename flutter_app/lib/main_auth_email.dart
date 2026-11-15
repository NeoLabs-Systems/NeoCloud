part of 'main.dart';

// The sign-in cards' mail pieces, copied from NeoAgent's login screen: the
// "Reset password" dialog behind "Forgot password?", the notice above the
// form once a link is on its way, and (NeoCloud's own) a way to send the
// sign-up confirmation again. None of it exists unless the server can send
// mail (NeoCloudController.emailConfigured).

/// "Forgot password?": NeoAgent's request dialog (a bottom sheet on phones)
/// with the username field's text filled in, or the plain "ask the person who
/// runs your NeoCloud" dialog where the server has no mail.
Future<void> _showForgotPasswordDialog(BuildContext context, NeoCloudController controller, {String initialAccount = ''}) async {
  if (!controller.emailConfigured) return _showAskAdminDialog(context);
  await showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _ForgotPasswordDialog(controller: controller, initialAccount: initialAccount.trim()),
  );
}

/// Without mail NeoCloud can't send a link; an administrator sets a new
/// password.
Future<void> _showAskAdminDialog(BuildContext context) async {
  await showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(appStrings.forgotPasswordTitle),
      content: SizedBox(
        width: 420,
        child: SelectableText(appStrings.forgotPasswordAskAdmin, style: TextStyle(color: _textSecondary, height: 1.45)),
      ),
      actions: <Widget>[
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close)),
      ],
    ),
  );
}

class _ForgotPasswordDialog extends StatefulWidget {
  const _ForgotPasswordDialog({required this.controller, required this.initialAccount});

  final NeoCloudController controller;
  final String initialAccount;

  @override
  State<_ForgotPasswordDialog> createState() => _ForgotPasswordDialogState();
}

class _ForgotPasswordDialogState extends State<_ForgotPasswordDialog> {
  late final TextEditingController _account = TextEditingController(text: widget.initialAccount);
  bool _sending = false;
  String? _error;

  @override
  void dispose() {
    _account.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    if (_sending) return;
    final account = _account.text.trim();
    if (account.isEmpty) {
      setState(() => _error = appStrings.enterYourUsernameOrEmail);
      return;
    }
    setState(() {
      _sending = true;
      _error = null;
    });
    final error = await widget.controller.requestPasswordReset(account);
    if (!mounted) return;
    if (error == null) {
      Navigator.of(context).pop();
      return;
    }
    setState(() {
      _sending = false;
      _error = error;
    });
  }

  @override
  Widget build(BuildContext context) {
    final error = _error;
    return _AdaptiveDialog(
      title: Text(appStrings.emailResetTitle),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(appStrings.emailResetBody, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: 16),
            TextField(
              controller: _account,
              autofocus: true,
              keyboardType: TextInputType.emailAddress,
              autocorrect: false,
              enableSuggestions: false,
              textInputAction: TextInputAction.done,
              onChanged: (_) {
                if (_error != null) setState(() => _error = null);
              },
              onSubmitted: (_) => unawaited(_send()),
              decoration: InputDecoration(labelText: appStrings.usernameOrEmail),
            ),
            if (error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: error)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _sending ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _sending ? null : () => unawaited(_send()),
          child: _sending ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.emailSendLink),
        ),
      ],
    );
  }
}

/// The mail notice of both sign-in cards: NeoAgent's green banner (reset
/// link sent, or "check your inbox") and, while an account waits for its
/// confirmation, the action that sends that link again.
class _AuthEmailNotices extends StatefulWidget {
  const _AuthEmailNotices({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AuthEmailNotices> createState() => _AuthEmailNoticesState();
}

class _AuthEmailNoticesState extends State<_AuthEmailNotices> {
  bool _sending = false;
  String? _error;

  Future<void> _resend() async {
    setState(() {
      _sending = true;
      _error = null;
    });
    final error = await widget.controller.resendConfirmation();
    if (mounted) {
      setState(() {
        _sending = false;
        _error = error;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final info = controller.authInfoMessage;
    final canResend = controller.emailConfigured && controller.confirmationAccount != null;
    final error = _error;
    if (info == null && !canResend && error == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (info != null) _InlineSuccess(message: info),
          if (error != null) ...<Widget>[const SizedBox(height: 8), _InlineError(message: error)],
          if (canResend)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: _sending || controller.isAuthenticating ? null : () => unawaited(_resend()),
                style: TextButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 4),
                  minimumSize: const Size(0, 32),
                  tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                ),
                icon: _sending
                    ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                    : const Icon(Icons.mark_email_unread_outlined, size: 16),
                label: Text(appStrings.emailResendConfirmation),
              ),
            ),
        ],
      ),
    );
  }
}

/// After registering with confirmation on, the card goes back to sign-in
/// (NeoAgent), with the passwords cleared.
bool _registrationAwaitsEmail(NeoCloudController controller) => !controller.isAuthenticated && controller.confirmationAccount != null && controller.authInfoMessage != null;
