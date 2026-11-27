part of 'main.dart';

// The NeoLabs sign-in screen (neolabs_flutter) for the web app and the phone.
// The web adds the phone sign-in code and security keys. The phone signs in
// through the session routes and swaps the session for a device token
// (src/device/password_login.dart); it offers the pairing scanner, the browser
// sign-in and the way back to the server step.

class AuthView extends StatefulWidget {
  const AuthView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AuthView> createState() => _AuthViewState();
}

class _AuthViewState extends State<AuthView> {
  final TextEditingController _username = TextEditingController();
  final TextEditingController _email = TextEditingController();
  final TextEditingController _password = TextEditingController();
  final TextEditingController _confirmPassword = TextEditingController();
  final TextEditingController _code = TextEditingController();
  final FocusNode _usernameFocus = FocusNode();
  final FocusNode _emailFocus = FocusNode();
  final FocusNode _passwordFocus = FocusNode();
  final FocusNode _confirmPasswordFocus = FocusNode();
  bool _registerMode = false;
  bool _obscurePassword = true;

  // The QR code is watched only while someone can see it: the card shows
  // it (or its dialog is open) and the tab is in front.
  late final AppLifecycleListener _lifecycle;
  bool _appInFront = true;
  bool _qrInline = false;
  bool _qrDialogOpen = false;

  NeoCloudController get _controller => widget.controller;

  bool get _bootstrap => !_controller.hasUser;

  /// The first account of a new server, or a new one where sign-ups are open.
  bool get _register => _registerMode || _bootstrap;

  /// Phone sign-in is for the web card's own sign-in only: not registering,
  /// not the second factor.
  bool get _offersQrLogin =>
      !_controller.usesDeviceLogin &&
      !_controller.isAwaitingTwoFactor &&
      !_register;

  @override
  void initState() {
    super.initState();
    _lifecycle = AppLifecycleListener(
      onStateChange: (state) {
        _appInFront =
            state == AppLifecycleState.resumed ||
            state == AppLifecycleState.inactive;
        _applyQrVisibility();
      },
    );
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    _controller.qrLogin.pause();
    for (final controller in <TextEditingController>[
      _username,
      _email,
      _password,
      _confirmPassword,
      _code,
    ]) {
      controller.dispose();
    }
    for (final node in <FocusNode>[
      _usernameFocus,
      _emailFocus,
      _passwordFocus,
      _confirmPasswordFocus,
    ]) {
      node.dispose();
    }
    super.dispose();
  }

  /// Called during layout, so polling starts or pauses after the frame.
  void _setQrInline(bool inline) {
    if (_qrInline == inline) return;
    _qrInline = inline;
    WidgetsBinding.instance.addPostFrameCallback((_) => _applyQrVisibility());
  }

  void _setQrDialogOpen(bool open) {
    _qrDialogOpen = open;
    _applyQrVisibility();
  }

  void _applyQrVisibility() {
    if (!mounted) return;
    final flow = _controller.qrLogin;
    final onScreen =
        _qrDialogOpen ||
        (_offersQrLogin && (_qrInline || flow.scan != null));
    if (onScreen && _appInFront) {
      unawaited(flow.prepare());
    } else {
      flow.pause();
    }
  }

  Future<void> _submit() async {
    if (_controller.isAwaitingTwoFactor) {
      final code = _code.text.trim();
      if (code.isEmpty) return;
      await _controller.completeTwoFactorLogin(code: code);
      if (_controller.isAuthenticated) TextInput.finishAutofillContext();
      if (_controller.isAwaitingTwoFactor) _code.clear();
      return;
    }
    final register = _register;
    if (register) _controller.authInfoMessage = null;
    await _submitCredentials(
      _controller,
      register: register,
      username: _username.text,
      email: _email.text,
      password: _password.text,
      confirmPassword: _confirmPassword.text,
    );
    // Created, waiting for its confirmation link: back to sign-in.
    if (mounted && register && _registrationAwaitsEmail(_controller)) {
      _password.clear();
      _confirmPassword.clear();
      setState(() => _registerMode = false);
    }
  }

  _PasswordStrengthInfo get _strength => _passwordStrengthInfo(
    password: _password.text,
    username: _username.text,
    email: _email.text,
  );

  void _edited(NeoCredentialField _) {
    _controller.clearInlineError();
    // The strength meter reads the username and email too.
    if (_register) setState(() {});
  }

  void _toggleRegisterMode() {
    _controller.clearInlineError();
    setState(() => _registerMode = !_registerMode);
  }

  Widget _labeled(String label, Widget field, [Widget? trailing]) =>
      NeoLabeledField(
        palette: _palette,
        label: label,
        field: field,
        trailing: trailing,
      );

  Widget _credentials() {
    final forgotPassword = _controller.hasUser && _controller.emailConfigured
        ? TextButton(
            onPressed: _controller.isAuthenticating
                ? null
                : () => unawaited(
                    _showForgotPasswordDialog(
                      context,
                      _controller,
                      initialAccount: _username.text,
                    ),
                  ),
            style: neoAuthLinkStyle(),
            child: Text(appStrings.forgotPassword),
          )
        : null;
    return NeoCredentialFields(
      register: _register,
      username: _username,
      email: _email,
      password: _password,
      confirmPassword: _confirmPassword,
      usernameFocus: _usernameFocus,
      emailFocus: _emailFocus,
      passwordFocus: _passwordFocus,
      confirmPasswordFocus: _confirmPasswordFocus,
      obscurePassword: _obscurePassword,
      onChanged: _edited,
      onSubmit: () => unawaited(_submit()),
      decoration: (_, suffix) =>
          neoAuthInputDecoration(_palette, suffixIcon: suffix),
      labeledField: _labeled,
      usernameLabel: appStrings.username,
      usernameOrEmailLabel: appStrings.usernameOrEmail,
      emailLabel: appStrings.email,
      passwordLabel: appStrings.password,
      confirmPasswordLabel: appStrings.confirmPassword,
      passwordToggle: IconButton(
        tooltip: _obscurePassword
            ? appStrings.authShowPassword
            : appStrings.authHidePassword,
        onPressed: () => setState(() => _obscurePassword = !_obscurePassword),
        icon: Icon(
          _obscurePassword
              ? Icons.visibility_outlined
              : Icons.visibility_off_outlined,
          size: 18,
        ),
      ),
      passwordStrength: _PasswordStrengthIndicator(info: _strength),
      forgotPassword: forgotPassword,
    );
  }

  Widget _twoFactorField() => _labeled(
    appStrings.twoFactorOrRecoveryCode,
    TextField(
      controller: _code,
      autofocus: true,
      // Recovery codes are alphanumeric (XXXXX-XXXXX).
      keyboardType: TextInputType.visiblePassword,
      autocorrect: false,
      enableSuggestions: false,
      autofillHints: const <String>[AutofillHints.oneTimeCode],
      textInputAction: TextInputAction.done,
      onSubmitted: (_) => unawaited(_submit()),
      decoration: neoAuthInputDecoration(_palette),
    ),
  );

  Widget _divider(String label) =>
      NeoOrDivider(palette: _palette, label: label);

  /// What follows the submit button: the other ways in.
  List<Widget> _otherWays() {
    final controller = _controller;
    if (controller.isAwaitingTwoFactor) {
      return <Widget>[
        const SizedBox(height: 8),
        TextButton(
          onPressed: controller.isAuthenticating
              ? null
              : () {
                  _code.clear();
                  controller.cancelTwoFactorLogin();
                },
          child: Text(appStrings.backToSignIn),
        ),
      ];
    }
    if (_register) return const <Widget>[];
    final buttonStyle = OutlinedButton.styleFrom(
      minimumSize: const Size.fromHeight(46),
    );
    if (!controller.usesDeviceLogin) {
      if (!controller.supportsSecurityKeys) return const <Widget>[];
      return <Widget>[
        const SizedBox(height: 20),
        _divider(appStrings.authOrContinueWith),
        const SizedBox(height: 14),
        OutlinedButton.icon(
          onPressed: controller.isAuthenticating
              ? null
              : () => unawaited(
                  controller.signInWithSecurityKey(username: _username.text),
                ),
          style: buttonStyle,
          icon: const Icon(Icons.key_rounded, size: 18),
          label: Text(appStrings.authSignInWithSecurityKey),
        ),
      ];
    }
    return <Widget>[
      if (qrScannerSupported)
        const SizedBox(height: 12)
      else ...<Widget>[
        const SizedBox(height: 20),
        _divider(appStrings.authOrContinueWith),
        const SizedBox(height: 14),
      ],
      OutlinedButton.icon(
        onPressed: controller.isAuthenticating || controller.isConnecting
            ? null
            : () => unawaited(
                controller.connect(
                  controller.passwordLogin?.server ??
                      controller.connectedServer,
                  openVerification: _openInBrowser,
                ),
              ),
        style: buttonStyle,
        icon: const Icon(Icons.open_in_browser_rounded, size: 18),
        label: Text(appStrings.connectWithBrowser),
      ),
      // Without mail there is no reset link to offer.
      if (!controller.emailConfigured) ...<Widget>[
        const SizedBox(height: 14),
        Text(
          appStrings.authForgotPasswordAskAdmin,
          textAlign: TextAlign.center,
          style: TextStyle(color: _textMuted, fontSize: 12, height: 1.45),
        ),
      ],
    ];
  }

  (String, String) _heading() {
    if (_controller.isAwaitingTwoFactor) {
      return (appStrings.enter2faCode, appStrings.openYourAuthenticator);
    }
    if (_bootstrap) {
      return (
        appStrings.createTheFirstAccount,
        appStrings.firstAccountSubtitle,
      );
    }
    if (_registerMode) {
      return (appStrings.authCreateAccount, appStrings.createAnotherAccount);
    }
    return _controller.usesDeviceLogin
        ? (appStrings.authPhoneSignInTitle, appStrings.authPhoneSignInSubtitle)
        : (appStrings.authSignInTitle, appStrings.authSignInSubtitle);
  }

  Widget _cardContent() {
    final controller = _controller;
    final qrScan = _offersQrLogin ? controller.qrLogin.scan : null;
    final twoFactor = controller.isAwaitingTwoFactor;
    final register = _register;
    final scanToPair =
        controller.usesDeviceLogin &&
        !twoFactor &&
        !register &&
        qrScannerSupported;
    final (title, subtitle) = qrScan != null
        ? (appStrings.qrLoginWaitingTitle, appStrings.qrLoginWaitingBody)
        : _heading();
    final String submitLabel;
    if (twoFactor) {
      submitLabel = appStrings.verify;
    } else if (register) {
      submitLabel = appStrings.authCreateAccount;
    } else {
      submitLabel = appStrings.signIn;
    }
    final blocked = register && !twoFactor && !_strength.acceptable;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        NeoAuthHeading(palette: _palette, title: title, subtitle: subtitle),
        if (qrScan != null) ...<Widget>[
          const SizedBox(height: 24),
          _QrLoginWaiting(controller: controller, scan: qrScan),
        ] else ...<Widget>[
          if (controller.errorMessage != null) ...<Widget>[
            const SizedBox(height: 20),
            _InlineError(
              message: controller.errorMessage!,
              onDismiss: controller.clearInlineError,
            ),
          ],
          if (controller.emailConfigured && !twoFactor)
            _AuthEmailNotices(controller: controller),
          if (_offersQrLogin || scanToPair) ...<Widget>[
            const SizedBox(height: 26),
            if (scanToPair)
              _ScanToPairButton(controller: controller)
            else
              _QrLoginBlock(
                controller: controller,
                onLayout: _setQrInline,
                onDialogOpen: _setQrDialogOpen,
              ),
            const SizedBox(height: 22),
            _divider(appStrings.authOrUsePassword),
            const SizedBox(height: 16),
          ] else
            const SizedBox(height: 24),
          AutofillGroup(child: twoFactor ? _twoFactorField() : _credentials()),
          const SizedBox(height: 20),
          FilledButton(
            onPressed: controller.isAuthenticating || blocked
                ? null
                : () => unawaited(_submit()),
            style: FilledButton.styleFrom(
              minimumSize: const Size.fromHeight(50),
            ),
            child: controller.isAuthenticating
                ? const SizedBox.square(
                    dimension: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(submitLabel),
          ),
          ..._otherWays(),
        ],
      ],
    );
  }

  List<Widget> _footer() {
    final controller = _controller;
    if (controller.isAwaitingTwoFactor ||
        (_offersQrLogin && controller.qrLogin.scan != null)) {
      return const <Widget>[];
    }
    // With sign-up off only the way back from the form stays.
    final showRegisterToggle =
        !_bootstrap && (_registerMode || controller.registrationOpen);
    // With mail the link sits on the password row instead.
    final showAskAdmin =
        !controller.usesDeviceLogin && !_register && !controller.emailConfigured;
    final server =
        controller.passwordLogin?.server ?? controller.connectedServer;
    return <Widget>[
      if (showRegisterToggle || showAskAdmin) ...<Widget>[
        const SizedBox(height: 16),
        Wrap(
          alignment: WrapAlignment.center,
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: 12,
          children: <Widget>[
            if (showRegisterToggle)
              TextButton(
                onPressed: controller.isAuthenticating
                    ? null
                    : _toggleRegisterMode,
                child: Text(
                  _registerMode
                      ? appStrings.alreadyHaveAnAccount
                      : appStrings.needANewAccount,
                ),
              ),
            if (showAskAdmin)
              TextButton(
                onPressed: controller.isAuthenticating
                    ? null
                    : () => unawaited(_showAskAdminDialog(context)),
                child: Text(appStrings.forgotPassword),
              ),
          ],
        ),
      ],
      if (controller.usesDeviceLogin) ...<Widget>[
        const SizedBox(height: 4),
        Wrap(
          alignment: WrapAlignment.center,
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: 2,
          children: <Widget>[
            Text(
              appStrings.authSigningInTo(
                Uri.tryParse(server)?.authority ?? server,
              ),
              style: TextStyle(color: _textMuted, fontSize: 12),
            ),
            TextButton(
              onPressed: controller.isAuthenticating
                  ? null
                  : controller.leavePasswordLogin,
              style: neoAuthLinkStyle(),
              child: Text(appStrings.connectChangeServer),
            ),
          ],
        ),
      ],
    ];
  }

  @override
  Widget build(BuildContext context) {
    if (!_offersQrLogin) _setQrInline(false);
    final compact = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    return NeoAuthPage(
      palette: _palette,
      compact: compact,
      children: <Widget>[
        NeoAuthCard(
          palette: _palette,
          compact: compact,
          mark: const NeoCloudMark(size: 76),
          child: _cardContent(),
        ),
        ..._footer(),
      ],
    );
  }
}

/// The sign-in card's submit: registration (passwords match and are strong
/// enough) or sign-in.
Future<void> _submitCredentials(
  NeoCloudController controller, {
  required bool register,
  required String username,
  required String email,
  required String password,
  required String confirmPassword,
}) async {
  if (controller.isAuthenticating) return;
  if (register) {
    if (password != confirmPassword) {
      controller.showInlineError(appStrings.passwordsDoNotMatch);
      return;
    }
    final strength = _passwordStrengthInfo(
      password: password,
      username: username,
      email: email,
    );
    if (!strength.acceptable) {
      controller.showInlineError(strength.message);
      return;
    }
    await controller.register(
      username: username.trim(),
      email: email.trim(),
      password: password,
    );
  } else {
    if (username.trim().isEmpty || password.isEmpty) {
      controller.showInlineError(appStrings.enterYourUsernameOrEmail);
      return;
    }
    await controller.login(username: username.trim(), password: password);
  }
  if (controller.isAuthenticated) TextInput.finishAutofillContext();
}
