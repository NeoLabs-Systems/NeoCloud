import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/security/password_strength.dart';
import 'package:neocloud/src/security/webauthn_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _FakeWebAuthnClient implements WebAuthnClient {
  _FakeWebAuthnClient({required this.isSupported});

  @override
  final bool isSupported;

  @override
  Future<Map<String, dynamic>> createCredential(Map<String, dynamic> options) async => const <String, dynamic>{};

  @override
  Future<Map<String, dynamic>> getAssertion(Map<String, dynamic> options) async => const <String, dynamic>{};
}

Future<NeoCloudController> _controller({bool securityKeys = true, bool registrationOpen = true}) async {
  SharedPreferences.setMockInitialValues(<String, Object>{});
  return NeoCloudController(
      client: BackendClient(baseUrl: 'http://localhost:1'),
      preferences: await SharedPreferences.getInstance(),
      deviceSessions: createDeviceSessions(),
      cameraUploads: NoCameraUploads(),
      shareIntake: NoShareIntake(),
    )
    ..webAuthn = _FakeWebAuthnClient(isSupported: securityKeys)
    ..registrationOpen = registrationOpen;
}

Future<void> _pumpAuth(WidgetTester tester, NeoCloudController controller) async {
  tester.view.physicalSize = const Size(1200, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: AuthView(controller: controller)));
  await tester.pump(const Duration(seconds: 1));
}

void main() {
  group('password strength (same rules as the server)', () {
    test('handles empty, weak, strong and personal passwords', () {
      expect(evaluatePasswordStrength(password: '').score, 0);
      expect(evaluatePasswordStrength(password: 'password123').score, lessThan(3));

      final strong = evaluatePasswordStrength(password: 'CorrectHorse9!Battery');
      expect(strong.score, 4);
      expect(strong.isAcceptable, isTrue);

      final personal = evaluatePasswordStrength(password: 'NeoUser2026!', username: 'neouser', email: 'neo@example.com');
      expect(personal.containsUserInfo, isTrue);
      expect(personal.isAcceptable, isFalse);
    });

    test('repetition reduces the score without rejecting an otherwise strong password', () {
      final varied = evaluatePasswordStrength(password: 'ORCHID7772018!!WIND');
      expect(varied.score, 3);
      expect(varied.isAcceptable, isTrue);
      expect(evaluatePasswordStrength(password: 'aaaAAAA1').isAcceptable, isFalse);
    });

    test('8 characters with a score of 3 is enough', () {
      expect(minPasswordLength, 8);
      expect(minPasswordScore, 3);
      final short = evaluatePasswordStrength(password: 'Plumtree9!');
      expect(short.score, 3);
      expect(short.isAcceptable, isTrue);
      expect(evaluatePasswordStrength(password: 'Tasdf!9Lmno').isAcceptable, isFalse);
      expect(evaluatePasswordStrength(password: 'mightyriver42').score, 3);
    });
  });

  group('sign-in view', () {
    setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

    testWidgets('sign-in fields advertise credentials to autofill services', (tester) async {
      final controller = await _controller();
      await _pumpAuth(tester, controller);
      final username = tester.widget<TextField>(find.widgetWithText(TextField, 'Username or email'));
      final password = tester.widget<TextField>(find.widgetWithText(TextField, 'Password'));
      expect(username.autofillHints, contains(AutofillHints.username));
      expect(password.autofillHints, contains(AutofillHints.password));
      expect(password.autocorrect, isFalse);
    });

    testWidgets('offers security key sign-in where the browser supports it', (tester) async {
      final controller = await _controller();
      await _pumpAuth(tester, controller);
      expect(find.text('Sign in with security key'), findsOneWidget);
      expect(find.text('or continue with'), findsOneWidget);
    });

    testWidgets('shows no security key sign-in where it is unsupported', (tester) async {
      final controller = await _controller(securityKeys: false);
      await _pumpAuth(tester, controller);
      expect(find.text('Sign in with security key'), findsNothing);
    });

    testWidgets('hides the register link while sign-up is off, keeps forgot password', (tester) async {
      final controller = await _controller(registrationOpen: false);
      await _pumpAuth(tester, controller);
      expect(find.text('Need a new account? Register'), findsNothing);
      expect(find.text('Forgot password?'), findsOneWidget);
    });

    testWidgets('the web sign-in card offers phone sign-in, registering does not', (tester) async {
      final controller = await _controller();
      await _pumpAuth(tester, controller);
      expect(find.text('Scan with NeoCloud on your phone'), findsOneWidget);
      expect(find.text('or use your password'), findsOneWidget);

      await tester.tap(find.text('Need a new account? Register'));
      await tester.pump();
      expect(find.text('Scan with NeoCloud on your phone'), findsNothing);
      controller.qrLogin.pause();
    });

    testWidgets('registration shows the strength meter and blocks weak passwords', (tester) async {
      final controller = await _controller();
      await _pumpAuth(tester, controller);
      await tester.tap(find.text('Need a new account? Register'));
      await tester.pump();

      expect(find.text('Sign in with security key'), findsNothing);
      expect(find.textContaining('Sign-up key'), findsNothing);
      expect(find.text('Use 8+ characters. Longer passphrases work well.'), findsOneWidget);

      final password = find.widgetWithText(TextField, 'Password');
      await tester.enterText(password, 'short');
      await tester.pump();
      expect(find.text('Password strength: too weak, will be rejected'), findsOneWidget);
      expect(find.text('Use at least 8 characters.'), findsOneWidget);
      FilledButton submit() => tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Create account'));
      expect(submit().onPressed, isNull);

      await tester.enterText(password, 'CorrectHorse9!Battery');
      await tester.pump();
      expect(find.text('Password strength: strong'), findsOneWidget);
      expect(submit().onPressed, isNotNull);
    });
  });
}
