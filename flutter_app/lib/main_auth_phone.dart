part of 'main.dart';

// The phone app's sign-in card, NeoAgent's AuthView as it renders on a
// phone (main_app_shell.dart): the logo set into the card's top edge on the
// dotted backdrop, a centered title, "Scan QR code" above the password form,
// the browser sign-in under it and the server with "Change server" below
// the card. It signs in through the session routes and swaps the session
// for a device token (src/device/password_login.dart).

class PhoneAuthView extends StatefulWidget {
  const PhoneAuthView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<PhoneAuthView> createState() => _PhoneAuthViewState();
}

class _PhoneAuthViewState extends State<PhoneAuthView> {
  final TextEditingController _usernameController = TextEditingController();
  final TextEditingController _emailController = TextEditingController();
  final TextEditingController _passwordController = TextEditingController();
  final TextEditingController _confirmPasswordController = TextEditingController();
  final TextEditingController _codeController = TextEditingController();
  final FocusNode _emailFocus = FocusNode();
  final FocusNode _passwordFocus = FocusNode();
  final FocusNode _confirmPasswordFocus = FocusNode();
  bool _registerMode = false;
  bool _obscurePassword = true;

  NeoCloudController get _controller => widget.controller;

  /// The first account of a new server, or a new one where sign-ups are open.
  bool get _register => _registerMode || !_controller.hasUser;

  @override
  void dispose() {
    _usernameController.dispose();
    _emailController.dispose();
    _passwordController.dispose();
    _confirmPasswordController.dispose();
    _codeController.dispose();
    _emailFocus.dispose();
    _passwordFocus.dispose();
    _confirmPasswordFocus.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (_controller.isAwaitingTwoFactor) {
      final code = _codeController.text.trim();
      if (code.isEmpty) return;
      await _controller.completeTwoFactorLogin(code: code);
      if (_controller.isAuthenticated) TextInput.finishAutofillContext();
      if (_controller.isAwaitingTwoFactor) _codeController.clear();
      return;
    }
    await _submitCredentials(
      _controller,
      register: _register,
      username: _usernameController.text,
      email: _emailController.text,
      password: _passwordController.text,
      confirmPassword: _confirmPasswordController.text,
    );
  }

  void _edited(String _) {
    _controller.clearInlineError();
    // The strength meter reads the username and email too.
    if (_register) setState(() {});
  }

  void _setRegisterMode(bool register) {
    _controller.clearInlineError();
    setState(() => _registerMode = register);
  }

  /// The theme's field, filled with the page color so it reads on the card.
  InputDecoration _inputDecoration({Widget? suffixIcon}) =>
      InputDecoration(filled: true, fillColor: _bgPrimary, suffixIcon: suffixIcon);

  /// A field with its label above it; the label is its accessible name.
  Widget _labeledField(String label, Widget field) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        ExcludeSemantics(
          child: Text(label, style: TextStyle(color: _textSecondary, fontSize: 13, fontWeight: FontWeight.w500)),
        ),
        const SizedBox(height: 6),
        Semantics(label: label, child: field),
      ],
    );
  }

  ButtonStyle get _inlineLinkStyle => TextButton.styleFrom(
    padding: const EdgeInsets.symmetric(horizontal: 4),
    minimumSize: const Size(0, 32),
    tapTargetSize: MaterialTapTargetSize.shrinkWrap,
    textStyle: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500),
  );

  Widget _passwordVisibilityToggle() {
    return IconButton(
      tooltip: _obscurePassword ? appStrings.authShowPassword : appStrings.authHidePassword,
      onPressed: () => setState(() => _obscurePassword = !_obscurePassword),
      icon: Icon(_obscurePassword ? Icons.visibility_outlined : Icons.visibility_off_outlined),
    );
  }

  List<Widget> _credentialFields() {
    final register = _register;
    final usernameField = TextField(
      controller: _usernameController,
      autofillHints: const <String>[AutofillHints.username],
      keyboardType: register ? TextInputType.text : TextInputType.emailAddress,
      autocorrect: false,
      enableSuggestions: false,
      textInputAction: TextInputAction.next,
      onChanged: _edited,
      onSubmitted: (_) => (register ? _emailFocus : _passwordFocus).requestFocus(),
      decoration: _inputDecoration(),
    );
    final passwordField = TextField(
      controller: _passwordController,
      focusNode: _passwordFocus,
      obscureText: _obscurePassword,
      autocorrect: false,
      enableSuggestions: false,
      autofillHints: <String>[register ? AutofillHints.newPassword : AutofillHints.password],
      textInputAction: register ? TextInputAction.next : TextInputAction.done,
      onChanged: _edited,
      onSubmitted: (_) => register ? _confirmPasswordFocus.requestFocus() : unawaited(_submit()),
      decoration: _inputDecoration(suffixIcon: _passwordVisibilityToggle()),
    );
    if (!register) {
      return <Widget>[
        _labeledField(appStrings.usernameOrEmail, usernameField),
        const SizedBox(height: 14),
        _labeledField(appStrings.password, passwordField),
      ];
    }
    return <Widget>[
      _labeledField(appStrings.username, usernameField),
      const SizedBox(height: 14),
      _labeledField(
        appStrings.email,
        TextField(
          controller: _emailController,
          focusNode: _emailFocus,
          keyboardType: TextInputType.emailAddress,
          autocorrect: false,
          enableSuggestions: false,
          autofillHints: const <String>[AutofillHints.email],
          textInputAction: TextInputAction.next,
          onChanged: _edited,
          onSubmitted: (_) => _passwordFocus.requestFocus(),
          decoration: _inputDecoration(),
        ),
      ),
      const SizedBox(height: 14),
      _labeledField(appStrings.password, passwordField),
      const SizedBox(height: 10),
      _PasswordStrengthIndicator(
        info: _passwordStrengthInfo(
          password: _passwordController.text,
          username: _usernameController.text,
          email: _emailController.text,
        ),
      ),
      const SizedBox(height: 14),
      _labeledField(
        appStrings.confirmPassword,
        TextField(
          controller: _confirmPasswordController,
          focusNode: _confirmPasswordFocus,
          obscureText: _obscurePassword,
          autocorrect: false,
          enableSuggestions: false,
          autofillHints: const <String>[AutofillHints.newPassword],
          textInputAction: TextInputAction.done,
          onChanged: _edited,
          onSubmitted: (_) => unawaited(_submit()),
          decoration: _inputDecoration(suffixIcon: _passwordVisibilityToggle()),
        ),
      ),
    ];
  }

  Widget _cardContent({required String title, required String subtitle}) {
    final controller = _controller;
    final awaitingTwoFactor = controller.isAwaitingTwoFactor;
    final register = _register;
    final showScanToPair = !awaitingTwoFactor && !register && qrScannerSupported;
    final submitBlocked =
        register &&
        !awaitingTwoFactor &&
        !_passwordStrengthInfo(
          password: _passwordController.text,
          username: _usernameController.text,
          email: _emailController.text,
        ).acceptable;
    final String submitLabel;
    if (awaitingTwoFactor) {
      submitLabel = appStrings.verify;
    } else if (register) {
      submitLabel = appStrings.authCreateAccount;
    } else {
      submitLabel = appStrings.signIn;
    }
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (awaitingTwoFactor) ...<Widget>[
          Text(appStrings.verification.toUpperCase(), textAlign: TextAlign.center, style: _entryEyebrowStyle()),
          const SizedBox(height: 8),
        ],
        Text(
          title,
          textAlign: TextAlign.center,
          style: TextStyle(color: _textPrimary, fontSize: 24, fontWeight: FontWeight.w700, letterSpacing: -0.5, height: 1.15),
        ),
        const SizedBox(height: 6),
        Text(subtitle, textAlign: TextAlign.center, style: TextStyle(color: _textSecondary, fontSize: 14, height: 1.45)),
        if (controller.errorMessage != null) ...<Widget>[
          const SizedBox(height: 20),
          _InlineError(message: controller.errorMessage!, onDismiss: controller.clearInlineError),
        ],
        if (showScanToPair) ...<Widget>[
          const SizedBox(height: 26),
          _ScanToPairButton(controller: controller),
          const SizedBox(height: 22),
          _orDivider(appStrings.authOrUsePassword),
          const SizedBox(height: 16),
        ] else
          const SizedBox(height: 24),
        if (awaitingTwoFactor)
          _labeledField(
            appStrings.twoFactorOrRecoveryCode,
            TextField(
              controller: _codeController,
              autofocus: true,
              // Recovery codes are alphanumeric (XXXXX-XXXXX).
              keyboardType: TextInputType.visiblePassword,
              autocorrect: false,
              enableSuggestions: false,
              autofillHints: const <String>[AutofillHints.oneTimeCode],
              textInputAction: TextInputAction.done,
              textAlign: TextAlign.center,
              style: GoogleFonts.geistMono(color: _textPrimary, fontSize: 20, fontWeight: FontWeight.w600, letterSpacing: 4),
              onSubmitted: (_) => unawaited(_submit()),
              decoration: _inputDecoration(),
            ),
          )
        else
          AutofillGroup(
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: _credentialFields()),
          ),
        const SizedBox(height: 20),
        FilledButton(
          onPressed: controller.isAuthenticating || submitBlocked ? null : () => unawaited(_submit()),
          style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(50)),
          child: controller.isAuthenticating
              ? const SizedBox.square(dimension: 20, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(submitLabel),
        ),
        if (awaitingTwoFactor) ...<Widget>[
          const SizedBox(height: 8),
          TextButton(
            onPressed: controller.isAuthenticating
                ? null
                : () {
                    _codeController.clear();
                    controller.cancelTwoFactorLogin();
                  },
            child: Text(appStrings.backToSignIn),
          ),
        ] else if (!register) ...<Widget>[
          if (showScanToPair)
            const SizedBox(height: 12)
          else ...<Widget>[
            const SizedBox(height: 20),
            _orDivider(appStrings.authOrContinueWith),
            const SizedBox(height: 14),
          ],
          OutlinedButton.icon(
            onPressed: controller.isAuthenticating || controller.isConnecting
                ? null
                : () => unawaited(controller.connect(controller.passwordLogin?.server ?? controller.connectedServer, openVerification: _openInBrowser)),
            style: _altSignInButtonStyle(),
            icon: const Icon(Icons.open_in_browser_rounded, size: 18),
            label: Text(appStrings.connectWithBrowser),
          ),
          // NeoCloud sends no email, so NeoAgent's line for that case.
          const SizedBox(height: 14),
          Text(
            appStrings.authForgotPasswordAskAdmin,
            textAlign: TextAlign.center,
            style: TextStyle(color: _textMuted, fontSize: 12, height: 1.45),
          ),
        ],
      ],
    );
  }

  /// The card, with the logo set into its top edge where NeoAgent's mascot
  /// sits.
  Widget _card({required Widget child, required bool compact}) {
    const logoSize = 76.0;
    const ring = 6.0;
    const overlap = logoSize / 2 + ring;
    final side = compact ? 20.0 : 32.0;
    return Stack(
      clipBehavior: Clip.none,
      alignment: Alignment.topCenter,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.only(top: overlap),
          child: DecoratedBox(
            decoration: BoxDecoration(
              color: _bgCard,
              borderRadius: BorderRadius.circular(28),
              border: Border.all(color: _border),
              boxShadow: _entryPanelShadow,
            ),
            child: Padding(padding: EdgeInsets.fromLTRB(side, overlap + 18, side, 28), child: child),
          ),
        ),
        Container(
          padding: const EdgeInsets.all(ring),
          decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(34)),
          child: const NeoCloudMark(size: logoSize),
        ),
      ],
    );
  }

  List<Widget> _footer() {
    final controller = _controller;
    if (controller.isAwaitingTwoFactor) return const <Widget>[];
    final server = controller.passwordLogin?.server ?? controller.connectedServer;
    final host = Uri.tryParse(server)?.authority ?? server;
    // With sign-up off only the way back from the form stays.
    final showRegisterToggle = controller.hasUser && (_registerMode || controller.registrationOpen);
    return <Widget>[
      if (showRegisterToggle) ...<Widget>[
        const SizedBox(height: 16),
        TextButton(
          onPressed: controller.isAuthenticating ? null : () => _setRegisterMode(!_registerMode),
          child: Text(_registerMode ? appStrings.alreadyHaveAnAccount : appStrings.needANewAccount),
        ),
      ],
      const SizedBox(height: 4),
      Wrap(
        alignment: WrapAlignment.center,
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 2,
        children: <Widget>[
          Text(appStrings.authSigningInTo(host), style: TextStyle(color: _textMuted, fontSize: 12)),
          TextButton(
            onPressed: controller.isAuthenticating ? null : controller.leavePasswordLogin,
            style: _inlineLinkStyle,
            child: Text(appStrings.connectChangeServer),
          ),
        ],
      ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final controller = _controller;
    final String title;
    final String subtitle;
    if (controller.isAwaitingTwoFactor) {
      title = appStrings.enter2faCode;
      subtitle = appStrings.openYourAuthenticator;
    } else if (!controller.hasUser) {
      title = appStrings.createTheFirstAccount;
      subtitle = appStrings.firstAccountSubtitle;
    } else if (_registerMode) {
      title = appStrings.authCreateAccount;
      subtitle = appStrings.createAnotherAccount;
    } else {
      title = appStrings.authPhoneSignInTitle;
      subtitle = appStrings.authPhoneSignInSubtitle;
    }
    return Theme(
      data: _phoneEntryTheme(Theme.of(context)),
      child: _AuthBackdrop(
        child: Scaffold(
          backgroundColor: Colors.transparent,
          body: SafeArea(
            child: LayoutBuilder(
              builder: (context, viewportConstraints) {
                final compact = viewportConstraints.maxWidth < AppBreakpoints.mobile;
                return SingleChildScrollView(
                  child: ConstrainedBox(
                    constraints: BoxConstraints(minHeight: viewportConstraints.maxHeight),
                    child: Padding(
                      padding: EdgeInsets.symmetric(horizontal: compact ? 14 : 24, vertical: compact ? 20 : 32),
                      child: Center(
                        child: ConstrainedBox(
                          constraints: const BoxConstraints(maxWidth: 440),
                          child: _EntranceMotion(
                            rise: true,
                            child: Column(
                              mainAxisSize: MainAxisSize.min,
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: <Widget>[
                                _card(compact: compact, child: _cardContent(title: title, subtitle: subtitle)),
                                ..._footer(),
                              ],
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
      ),
    );
  }
}

/// NeoAgent's raised entry panels: a deep drop shadow with a faint gold one.
List<BoxShadow> get _entryPanelShadow => <BoxShadow>[
  BoxShadow(color: Colors.black.withValues(alpha: 0.18), blurRadius: 40, offset: const Offset(0, 18)),
  BoxShadow(color: _accent.withValues(alpha: 0.07), blurRadius: 28, offset: const Offset(0, 8)),
];

/// NeoAgent's sign-in backdrop: a dot grid under a soft gold glow.
class _AuthBackdrop extends StatelessWidget {
  const _AuthBackdrop({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    return DecoratedBox(
      decoration: BoxDecoration(color: _bgPrimary),
      child: CustomPaint(
        painter: _AuthBackdropPainter(
          dot: _textPrimary.withValues(alpha: isLight ? 0.09 : 0.07),
          glow: _accent.withValues(alpha: isLight ? 0.08 : 0.10),
        ),
        child: child,
      ),
    );
  }
}

class _AuthBackdropPainter extends CustomPainter {
  const _AuthBackdropPainter({required this.dot, required this.glow});

  static const double _pitch = 22;
  static const double _glowRadius = 380;

  final Color dot;
  final Color glow;

  @override
  void paint(Canvas canvas, Size size) {
    final centre = Offset(size.width / 2, size.height * 0.3);
    canvas.drawCircle(
      centre,
      _glowRadius,
      Paint()
        ..shader = RadialGradient(
          colors: <Color>[glow, glow.withValues(alpha: 0)],
        ).createShader(Rect.fromCircle(center: centre, radius: _glowRadius)),
    );
    final points = <Offset>[
      for (var y = _pitch / 2; y < size.height; y += _pitch)
        for (var x = _pitch / 2; x < size.width; x += _pitch) Offset(x, y),
    ];
    canvas.drawPoints(
      PointMode.points,
      points,
      Paint()
        ..color = dot
        ..strokeWidth = 2
        ..strokeCap = StrokeCap.round,
    );
  }

  @override
  bool shouldRepaint(_AuthBackdropPainter oldDelegate) => oldDelegate.dot != dot || oldDelegate.glow != glow;
}
