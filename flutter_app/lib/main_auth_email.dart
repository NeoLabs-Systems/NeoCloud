part of 'main.dart';

// The sign-in card's mail pieces: the "Reset password" dialog behind
// "Forgot password?", the notice above the
// form once a link is on its way, and (NeoCloud's own) a way to send the
// sign-up confirmation again. None of it exists unless the server can send
// mail (NeoCloudController.emailConfigured).

/// "Forgot password?": the shared request dialog with the username field's
/// text filled in, or the plain "ask the person who runs your NeoCloud" dialog
/// where the server has no mail.
Future<void> _showForgotPasswordDialog(
  BuildContext context,
  NeoCloudController controller, {
  String initialAccount = '',
}) async {
  if (!controller.emailConfigured) return _showAskAdminDialog(context);
  final messenger = ScaffoldMessenger.of(context);
  await showDialog<void>(
    context: context,
    builder: (_) => NeoForgotPasswordDialog(
      initialAccount: initialAccount.trim(),
      title: appStrings.emailResetTitle,
      description: appStrings.emailResetBody,
      accountLabel: appStrings.usernameOrEmail,
      emptyError: appStrings.enterYourUsernameOrEmail,
      cancelLabel: appStrings.cancel,
      submitLabel: appStrings.emailSendLink,
      onSubmit: (account) async {
        final error = await controller.requestPasswordReset(account);
        if (error != null) {
          messenger.showSnackBar(SnackBar(content: Text(error)));
        }
        return error == null;
      },
      errorBuilder: (message) => _InlineError(message: message),
      backgroundColor: _bgCard,
      textColor: _textSecondary,
    ),
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

/// The sign-in card's mail notice: NeoAgent's green banner (reset
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
