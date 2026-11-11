part of 'main.dart';

// The sign-in and registration screen, ported 1:1 from NeoRouter's
// main_auth.dart: one 420px card on the flat backdrop, the logo above a
// left-aligned title, fields with floating labels, one gold button and two
// quiet links. NeoCloud adds the sign-up key field and the 2FA step.

class AuthView extends StatefulWidget {
  const AuthView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AuthView> createState() => _AuthViewState();
}

class _AuthViewState extends State<AuthView> {
  final TextEditingController _usernameController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _confirmPasswordController = TextEditingController();
  final TextEditingController _signupKeyController = TextEditingController();
  final TextEditingController _codeController = TextEditingController();
  bool _registerMode = false;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _emailController.dispose();
    _confirmPasswordController.dispose();
    _signupKeyController.dispose();
    _codeController.dispose();
    super.dispose();
  }

  bool get _bootstrap => !widget.controller.hasUser;

  Future<void> _showForgotPasswordDialog() async {
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

  Future<void> _submit() async {
    final controller = widget.controller;
    if (controller.isAuthenticating) return;
    if (_registerMode || _bootstrap) {
      if (_passwordController.text != _confirmPasswordController.text) {
        controller.showInlineError(appStrings.passwordsDoNotMatch);
        return;
      }
      final strength = _passwordStrengthInfo(
        password: _passwordController.text,
        username: _usernameController.text,
        email: _emailController.text,
      );
      if (!strength.acceptable) {
        controller.showInlineError(strength.message);
        return;
      }
      await controller.register(
        username: _usernameController.text.trim(),
        email: _emailController.text.trim(),
        password: _passwordController.text,
        signupKey: _signupKeyController.text.trim().isEmpty ? null : _signupKeyController.text.trim(),
      );
    } else {
      if (_usernameController.text.trim().isEmpty || _passwordController.text.isEmpty) {
        controller.showInlineError(appStrings.enterYourUsernameOrEmail);
        return;
      }
      await controller.login(username: _usernameController.text.trim(), password: _passwordController.text);
    }
    if (controller.isAuthenticated) TextInput.finishAutofillContext();
  }

  Future<void> _submitCode() async {
    final code = _codeController.text.trim();
    if (code.isEmpty) return;
    await widget.controller.completeTwoFactorLogin(code: code);
    if (widget.controller.isAwaitingTwoFactor) _codeController.clear();
  }

  Widget _spinnerOr(String label) {
    return widget.controller.isAuthenticating
        ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
        : Text(label);
  }

  List<Widget> _twoFactorFields(NeoCloudController controller) {
    return <Widget>[
      TextField(
        controller: _codeController,
        autofocus: true,
        keyboardType: TextInputType.visiblePassword,
        autofillHints: const <String>[AutofillHints.oneTimeCode],
        onSubmitted: (_) => _submitCode(),
        decoration: InputDecoration(labelText: appStrings.twoFactorOrRecoveryCode, helperText: appStrings.openYourAuthenticator, helperMaxLines: 2),
      ),
      if (controller.errorMessage != null) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        _InlineError(message: controller.errorMessage!),
      ],
      const SizedBox(height: AppSpacing.lg),
      FilledButton(onPressed: controller.isAuthenticating ? null : _submitCode, child: _spinnerOr(appStrings.verify)),
      const SizedBox(height: AppSpacing.sm),
      TextButton(
        onPressed: controller.isAuthenticating
            ? null
            : () {
                _codeController.clear();
                controller.cancelTwoFactorLogin();
              },
        child: Text(appStrings.backToSignIn),
      ),
    ];
  }

  List<Widget> _credentialFields(NeoCloudController controller) {
    final register = _registerMode || _bootstrap;
    void edited(String _) => controller.clearInlineError();
    return <Widget>[
      if (register && controller.signupKeyRequired && !_bootstrap) ...<Widget>[
        TextField(
          controller: _signupKeyController,
          textCapitalization: TextCapitalization.characters,
          inputFormatters: <TextInputFormatter>[_SignupKeyFormatter()],
          onChanged: edited,
          decoration: InputDecoration(labelText: appStrings.signupKey, hintText: 'XXXX-XXXX-XXXX'),
        ),
        const SizedBox(height: AppSpacing.sm),
      ],
      TextField(
        controller: _usernameController,
        autofillHints: const <String>[AutofillHints.username],
        onChanged: edited,
        decoration: InputDecoration(labelText: register ? appStrings.username : appStrings.usernameOrEmail),
      ),
      if (register) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        TextField(
          controller: _emailController,
          keyboardType: TextInputType.emailAddress,
          autofillHints: const <String>[AutofillHints.email],
          onChanged: edited,
          decoration: InputDecoration(labelText: appStrings.email),
        ),
      ],
      const SizedBox(height: AppSpacing.sm),
      TextField(
        controller: _passwordController,
        obscureText: true,
        autofillHints: <String>[register ? AutofillHints.newPassword : AutofillHints.password],
        onChanged: edited,
        onSubmitted: (_) => register ? null : _submit(),
        decoration: InputDecoration(labelText: appStrings.password),
      ),
      if (register) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        TextField(
          controller: _confirmPasswordController,
          obscureText: true,
          onChanged: edited,
          onSubmitted: (_) => _submit(),
          decoration: InputDecoration(labelText: appStrings.confirmPassword),
        ),
      ],
      if (controller.errorMessage != null) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        _InlineError(message: controller.errorMessage!),
      ],
      const SizedBox(height: AppSpacing.lg),
      FilledButton(
        onPressed: controller.isAuthenticating ? null : _submit,
        child: _spinnerOr(register ? appStrings.authCreateAccount : appStrings.signIn),
      ),
      if (!_bootstrap) ...<Widget>[
        const SizedBox(height: AppSpacing.sm),
        Wrap(
          alignment: WrapAlignment.spaceBetween,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            TextButton(
              onPressed: controller.isAuthenticating
                  ? null
                  : () {
                      controller.clearInlineError();
                      setState(() => _registerMode = !_registerMode);
                    },
              child: Text(_registerMode ? appStrings.alreadyHaveAnAccount : appStrings.needANewAccount),
            ),
            if (!_registerMode)
              TextButton(
                onPressed: controller.isAuthenticating ? null : _showForgotPasswordDialog,
                child: Text(appStrings.forgotPassword),
              ),
          ],
        ),
      ],
    ];
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final twoFactor = controller.isAwaitingTwoFactor;
    final String title;
    final String subtitle;
    if (twoFactor) {
      title = appStrings.enter2faCode;
      subtitle = appStrings.openYourAuthenticator;
    } else if (_bootstrap) {
      title = appStrings.createTheFirstAccount;
      subtitle = appStrings.firstAccountSubtitle;
    } else if (_registerMode) {
      title = appStrings.authCreateAccount;
      subtitle = controller.signupKeyRequired ? appStrings.signupKeyStepSubtitle : appStrings.createAnotherAccount;
    } else {
      title = appStrings.authSignInTitle;
      subtitle = appStrings.authSignInSubtitle;
    }
    return _ControlSurfaceBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: LayoutBuilder(
            builder: (context, viewportConstraints) {
              return SingleChildScrollView(
                child: ConstrainedBox(
                  constraints: BoxConstraints(minHeight: viewportConstraints.maxHeight),
                  child: Padding(
                    padding: EdgeInsets.all(viewportConstraints.maxWidth < AppBreakpoints.mobile ? 14 : 24),
                    child: Center(
                      child: _EntranceMotion(
                        child: _GlassSurface(
                          width: 420,
                          padding: const EdgeInsets.all(AppSpacing.xl),
                          child: AutofillGroup(
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: <Widget>[
                                const Center(child: _BrandLockup(logoSize: 44, showName: false)),
                                const SizedBox(height: AppSpacing.md),
                                Text(title, style: _displayTitleStyle(24)),
                                const SizedBox(height: AppSpacing.sm),
                                Text(subtitle, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: _textSecondary)),
                                const SizedBox(height: AppSpacing.lg),
                                if (twoFactor) ..._twoFactorFields(controller) else ..._credentialFields(controller),
                              ],
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              );
            },
          ),
        ),
      ),
    );
  }
}

/// Sign-up keys as XXXX-XXXX-XXXX while they are typed or pasted.
class _SignupKeyFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(TextEditingValue oldValue, TextEditingValue newValue) {
    final raw = newValue.text.toUpperCase().replaceAll(RegExp(r'[^0-9A-Z]'), '');
    final clipped = raw.length > 12 ? raw.substring(0, 12) : raw;
    final buffer = StringBuffer();
    for (var i = 0; i < clipped.length; i++) {
      if (i > 0 && i % 4 == 0) buffer.write('-');
      buffer.write(clipped[i]);
    }
    final text = buffer.toString();
    return TextEditingValue(text: text, selection: TextSelection.collapsed(offset: text.length));
  }
}
