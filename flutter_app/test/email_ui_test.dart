import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session.dart' hide createDeviceSessions;
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/device/password_login.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/fake_http.dart';

// Everything mail related: it exists only when the server can send mail
// (email.configured), and it speaks the contract in docs/api.md.

const String _strongPassword = 'CorrectHorse9!Battery';
const String _uniformReset = 'If that account has a confirmed email, we will send a link.';

class _PhoneSessions implements DeviceSessions {
  @override
  bool get supported => true;
  @override
  String get deviceName => 'Pixel';
  @override
  String get platformName => 'android';
  @override
  Future<DeviceConnection?> read() async => null;
  @override
  Future<void> write(DeviceConnection connection) async {}
  @override
  Future<void> clear() async {}
  @override
  Future<String?> lastServer() async => null;
  @override
  Future<PendingDeviceLogin> start(Uri server) => throw UnimplementedError();
  @override
  Future<DeviceConnection> pair(Uri server, String code) => throw UnimplementedError();
}

AccountUser _user({String? email = 'ada@example.com', bool verified = true, String? pending, bool isAdmin = false}) => AccountUser(
  id: 7,
  username: 'ada',
  displayName: null,
  email: email,
  isAdmin: isAdmin,
  storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
  emailVerifiedAt: verified ? DateTime.utc(2026, 1, 1) : null,
  pendingEmail: pending,
);

Future<NeoCloudController> _controller(FakeHttp http, {bool mail = true, bool phone = false, AccountUser? user}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  const server = 'https://cloud.example.com';
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1', http: http),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: phone ? _PhoneSessions() : createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
  );
  controller
    ..isBooting = false
    ..hasUser = true
    ..emailConfigured = mail
    ..signupConfirmationRequired = mail;
  if (phone) controller.passwordLogin = PasswordDeviceLogin(server: server, client: BackendClient(baseUrl: server, http: http), deviceName: 'Pixel', platform: 'android');
  if (user != null) controller.user = user;
  return controller;
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

/// [builder] runs again whenever [controller] changes, as the app's root does.
Future<void> _pump(WidgetTester tester, NeoCloudController controller, Widget Function() builder, {Size size = const Size(1440, 2400)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: AnimatedBuilder(animation: controller, builder: (context, _) => builder()))));
  await _settle(tester);
}

Finder _field(String label) => find.widgetWithText(TextField, label);

/// OutlinedButton.icon builds a private subclass, so match by subtype.
Finder _sendTestButton() => find.ancestor(of: find.text('Send test email'), matching: find.bySubtype<OutlinedButton>());

Finder _forgotLink() => find.text('Forgot password?');

FakeReply _auth(FakeCall call) {
  switch ('${call.method} ${call.path}') {
    case 'POST /api/auth/password/forgot':
      if (call.body['account'] == 'limited') return const FakeReply(429, <String, dynamic>{'error': 'Too many requests', 'code': 'RATE_LIMITED'});
      return const FakeReply(200, <String, dynamic>{'success': true, 'message': _uniformReset});
    case 'POST /api/auth/email/resend':
      return const FakeReply(200, <String, dynamic>{'success': true, 'message': 'If that account is waiting, a new link is on its way.'});
    case 'POST /api/auth/register':
      return const FakeReply(202, <String, dynamic>{'success': false, 'requiresEmailConfirmation': true, 'email': 'ada@example.com', 'message': 'Check your email.'});
    case 'POST /api/auth/login':
      return const FakeReply(403, <String, dynamic>{'error': 'Email not confirmed', 'code': 'EMAIL_NOT_CONFIRMED', 'requiresEmailConfirmation': true});
    default:
      return const FakeReply(404, <String, dynamic>{'error': 'Not found'});
  }
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('web sign-in card', () {
    testWidgets('without mail the link keeps asking the person who runs NeoCloud', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http, mail: false);
      await _pump(tester, controller, () => AuthView(controller: controller));
      expect(_forgotLink(), findsOneWidget);
      await tester.tap(_forgotLink());
      await _settle(tester);
      expect(find.textContaining('Ask the person who runs your NeoCloud'), findsOneWidget);
      expect(find.text('Send link'), findsNothing);
      expect(find.text('Resend confirmation email'), findsNothing);
    });

    testWidgets('with mail the link opens the request dialog, filled from the username', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http);
      await _pump(tester, controller, () => AuthView(controller: controller));
      expect(_forgotLink(), findsOneWidget);
      await tester.enterText(_field('Username or email').first, 'ada');
      await tester.tap(_forgotLink());
      await _settle(tester);
      expect(find.text('Reset password'), findsOneWidget);
      expect(find.textContaining('NeoCloud will send a reset link'), findsOneWidget);
      final dialogField = tester.widget<TextField>(_field('Username or email').last);
      expect(dialogField.controller!.text, 'ada');
      expect(find.text('Send link'), findsOneWidget);
    });

    testWidgets('sending asks the server once and shows its uniform answer', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http);
      await _pump(tester, controller, () => AuthView(controller: controller));
      await tester.enterText(_field('Username or email').first, 'ada');
      await tester.tap(_forgotLink());
      await _settle(tester);
      await tester.tap(find.text('Send link'));
      await _settle(tester);
      expect(http.where('POST', '/api/auth/password/forgot').single.body, <String, dynamic>{'account': 'ada'});
      expect(find.text('Send link'), findsNothing, reason: 'the dialog closes');
      expect(find.text(_uniformReset), findsOneWidget);
    });

    testWidgets('an empty account and a refused request are shown in the dialog', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http);
      await _pump(tester, controller, () => AuthView(controller: controller));
      await tester.tap(_forgotLink());
      await _settle(tester);
      await tester.tap(find.text('Send link'));
      await _settle(tester);
      expect(find.text('Enter your username or email.'), findsOneWidget);
      expect(http.where('POST', '/api/auth/password/forgot'), isEmpty);

      await tester.enterText(_field('Username or email').last, 'limited');
      await tester.tap(find.text('Send link'));
      await _settle(tester);
      expect(find.text('Send link'), findsOneWidget, reason: 'the dialog stays open');
      expect(find.textContaining('Too many'), findsWidgets);
      expect(find.text(_uniformReset), findsNothing);
    });

    testWidgets('registering with confirmation shows the check-your-inbox state and can resend', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http)..registrationOpen = true;
      await _pump(tester, controller, () => AuthView(controller: controller));
      await tester.tap(find.text('Need a new account? Register'));
      await _settle(tester);
      await tester.enterText(_field('Username').first, 'ada');
      await tester.enterText(_field('Email').first, 'ada@example.com');
      await tester.enterText(_field('Password').first, _strongPassword);
      await tester.enterText(_field('Confirm password').first, _strongPassword);
      await tester.pump();
      await tester.tap(find.widgetWithText(FilledButton, 'Create account'));
      await _settle(tester);

      expect(http.where('POST', '/api/auth/register').length, 1);
      expect(find.widgetWithText(FilledButton, 'Sign in'), findsOneWidget, reason: 'back on the sign-in form');
      expect(find.textContaining('ada@example.com'), findsOneWidget);
      expect(find.textContaining('confirmation link'), findsOneWidget);
      expect(find.text('Resend confirmation email'), findsOneWidget);
      expect(controller.isAuthenticated, isFalse);

      await tester.tap(find.text('Resend confirmation email'));
      await _settle(tester);
      expect(http.where('POST', '/api/auth/email/resend').single.body, <String, dynamic>{'account': 'ada'});
      expect(find.textContaining('a new link is on its way'), findsOneWidget);
    });

    testWidgets('an unconfirmed account is told so after the right password, with the resend action', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http);
      await _pump(tester, controller, () => AuthView(controller: controller));
      await tester.enterText(_field('Username or email').first, 'ada');
      await tester.enterText(_field('Password').first, _strongPassword);
      await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
      await _settle(tester);
      expect(find.textContaining('Confirm your email address before signing in'), findsOneWidget);
      expect(find.text('Resend confirmation email'), findsOneWidget);
      await tester.tap(find.text('Resend confirmation email'));
      await _settle(tester);
      expect(http.where('POST', '/api/auth/email/resend').single.body['account'], 'ada');
      expect(find.textContaining('Confirm your email address before signing in'), findsNothing);
    });

    testWidgets('without mail a refused sign-in has no resend action', (tester) async {
      final http = FakeHttp((call) => const FakeReply(401, <String, dynamic>{'error': 'Invalid', 'code': 'INVALID_CREDENTIALS'}));
      final controller = await _controller(http, mail: false);
      await _pump(tester, controller, () => AuthView(controller: controller));
      await tester.enterText(_field('Username or email').first, 'ada');
      await tester.enterText(_field('Password').first, 'nope-nope-nope');
      await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
      await _settle(tester);
      expect(find.text('Resend confirmation email'), findsNothing);
    });
  });

  group('phone sign-in card', () {
    const phoneSize = Size(412, 915);

    testWidgets('without mail the plain line stays and there is no link', (tester) async {
      final controller = await _controller(FakeHttp(_auth), mail: false, phone: true);
      await _pump(tester, controller, () => PhoneAuthView(controller: controller), size: phoneSize);
      expect(_forgotLink(), findsNothing);
      expect(find.textContaining('Ask the person who runs your NeoCloud'), findsOneWidget);
    });

    testWidgets('with mail the link sits on the password row and opens a sheet', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http, phone: true);
      await _pump(tester, controller, () => PhoneAuthView(controller: controller), size: phoneSize);
      expect(find.textContaining('Ask the person who runs your NeoCloud'), findsNothing);
      expect(_forgotLink(), findsOneWidget);
      await tester.enterText(_field('').first, 'ada');
      await tester.tap(_forgotLink());
      await _settle(tester);
      expect(find.byType(BottomSheet), findsOneWidget);
      await tester.tap(find.text('Send link'));
      await _settle(tester);
      expect(http.where('POST', '/api/auth/password/forgot').single.body['account'], 'ada');
      expect(find.text(_uniformReset), findsOneWidget);
    });

    testWidgets('a 403 from the sign-in shows the message and resends through the phone client', (tester) async {
      final http = FakeHttp(_auth);
      final controller = await _controller(http, phone: true);
      await _pump(tester, controller, () => PhoneAuthView(controller: controller), size: phoneSize);
      await tester.enterText(find.byType(TextField).at(0), 'ada');
      await tester.enterText(find.byType(TextField).at(1), _strongPassword);
      await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
      await _settle(tester);
      expect(find.textContaining('Confirm your email address before signing in'), findsOneWidget);
      expect(controller.authErrorCode, 'EMAIL_NOT_CONFIRMED');
      await tester.tap(find.text('Resend confirmation email'));
      await _settle(tester);
      expect(http.where('POST', '/api/auth/email/resend').single.body['account'], 'ada');
    });

    test('the phone sign-in reads the mail status and survives a 202', () async {
      final http = FakeHttp((call) {
        if (call.path == '/api/auth/status') {
          return const FakeReply(200, <String, dynamic>{
            'hasUser': true,
            'registrationOpen': true,
            'email': <String, dynamic>{'enabled': true, 'configured': true, 'signupConfirmationRequired': true},
          });
        }
        return _auth(call);
      });
      final login = PasswordDeviceLogin(server: 'https://x', client: BackendClient(baseUrl: 'https://x', http: http), deviceName: 'Pixel', platform: 'android');
      final status = await login.status();
      expect(status.emailConfigured, isTrue);
      expect(status.signupConfirmationRequired, isTrue);
      final result = await login.register(username: 'ada', email: 'ada@example.com', password: _strongPassword);
      expect(result.requiresEmailConfirmation, isTrue);
      expect(result.confirmationEmail, 'ada@example.com');
      expect(result.connection, isNull);
      await expectLater(
        login.signIn('ada', _strongPassword),
        throwsA(isA<ApiException>().having((error) => error.code, 'code', 'EMAIL_NOT_CONFIRMED')),
      );
    });

    test('an older server without a mail status means no mail', () async {
      final http = FakeHttp((call) => const FakeReply(200, <String, dynamic>{'hasUser': true}));
      final login = PasswordDeviceLogin(server: 'https://x', client: BackendClient(baseUrl: 'https://x', http: http), deviceName: 'Pixel', platform: 'android');
      expect((await login.status()).emailConfigured, isFalse);
    });
  });

  group('Settings › Profile › email', () {
    Future<(FakeHttp, NeoCloudController)> open(WidgetTester tester, {AccountUser? user, bool mail = true, FakeHandler? handler}) async {
      final http = FakeHttp(handler ?? (call) => const FakeReply(404));
      final controller = await _controller(http, mail: mail, user: user ?? _user());
      controller.setSettingsPage(SettingsPage.profile);
      await _pump(tester, controller, () => SettingsPanel(controller: controller));
      return (http, controller);
    }

    testWidgets('without mail the address stays a read-only row', (tester) async {
      await open(tester, mail: false);
      expect(find.text('ada@example.com'), findsOneWidget);
      expect(find.text('Save email'), findsNothing);
      expect(find.text('Verified'), findsNothing);
    });

    testWidgets('a confirmed address shows the Verified pill and no resend action', (tester) async {
      await open(tester);
      expect(find.text('Current email: ada@example.com'), findsOneWidget);
      expect(find.text('Verified'), findsOneWidget);
      expect(find.text('Save email'), findsOneWidget);
      expect(find.text('Resend confirmation email'), findsNothing);
    });

    testWidgets('an unconfirmed address says so and can resend', (tester) async {
      final (http, _) = await open(tester, user: _user(verified: false), handler: (call) => const FakeReply(200, <String, dynamic>{'success': true}));
      expect(find.text('Not confirmed'), findsOneWidget);
      await tester.tap(find.text('Resend confirmation email'));
      await _settle(tester);
      expect(http.where('POST', '/api/account/email/resend'), hasLength(1));
      expect(find.text('Confirmation email sent.'), findsOneWidget);
    });

    testWidgets('saving needs the password, then shows the pending address', (tester) async {
      final (http, _) = await open(
        tester,
        handler: (call) {
          if (call.method == 'PUT' && call.path == '/api/account/email') {
            return const FakeReply(200, <String, dynamic>{'emailChangePending': true, 'pendingEmail': 'new@example.com', 'user': <String, dynamic>{}});
          }
          return const FakeReply(404);
        },
      );
      await tester.enterText(_field('Email').last, 'new@example.com');
      await tester.ensureVisible(find.text('Save email'));
      await tester.tap(find.text('Save email'));
      await _settle(tester);
      expect(find.text('Enter your current password to save email changes.'), findsOneWidget);
      expect(http.where('PUT', '/api/account/email'), isEmpty);

      await tester.enterText(_field('Current password').last, 'hunter2-long');
      await tester.tap(find.text('Save email'));
      await _settle(tester);
      expect(http.where('PUT', '/api/account/email').single.body, <String, dynamic>{'email': 'new@example.com', 'currentPassword': 'hunter2-long'});
      expect(find.textContaining('confirmation link to new@example.com'), findsOneWidget);
      expect(find.text('Resend confirmation email'), findsOneWidget);
      expect(tester.widget<TextField>(_field('Current password').last).controller!.text, isEmpty, reason: 'the password is cleared');
    });

    testWidgets('a pending address from the account is shown with its resend action', (tester) async {
      await open(tester, user: _user(pending: 'later@example.com'));
      expect(find.textContaining('confirmation link to later@example.com'), findsOneWidget);
      expect(find.text('Resend confirmation email'), findsOneWidget);
    });
  });

  group('Admin › Access › email', () {
    Map<String, dynamic> settings({bool stored = false, String host = '', String from = ''}) => <String, dynamic>{
      'from': from,
      'smtpHost': host,
      'smtpPort': 587,
      'smtpUser': '',
      'replyTo': '',
      'brandName': 'NeoCloud',
      'publicUrl': '',
      'supportUrl': '',
      'tokenTtlHours': 24,
      'smtpSecure': false,
      'smtpRequireTls': true,
      'smtpRejectUnauthorized': true,
      'requireSignupConfirmation': true,
      'requireEmailChangeConfirmation': true,
      'notifyNewLogin': true,
      'notifyAccountChanges': true,
      'smtpPasswordConfigured': stored,
    };

    Future<(FakeHttp, NeoCloudController)> open(WidgetTester tester, FakeHandler handler, {bool mail = false}) async {
      final http = FakeHttp((call) {
        if (call.method == 'GET' && call.path == '/api/admin/access') return const FakeReply(200, <String, dynamic>{'allowSignup': true});
        return handler(call);
      });
      final controller = await _controller(http, mail: mail, user: _user(isAdmin: true));
      await _pump(tester, controller, () => AdminPanel(controller: controller));
      await tester.tap(find.text('Access'));
      await _settle(tester);
      return (http, controller);
    }

    FakeReply notConfigured(FakeCall call) {
      if (call.path == '/api/admin/config/email' && call.method == 'GET') {
        return FakeReply(200, <String, dynamic>{'configured': false, 'missing': <String>['from', 'smtpHost'], 'settings': settings()});
      }
      return const FakeReply(404);
    }

    testWidgets('shows the card below the signup switch, not configured, with what is missing', (tester) async {
      await open(tester, notConfigured);
      expect(find.text('Email'), findsWidgets);
      expect(find.text('Not configured'), findsOneWidget);
      expect(find.textContaining('Sender address, SMTP host'), findsOneWidget);
      for (final label in <String>['Sender address', 'SMTP host', 'SMTP port', 'SMTP username', 'SMTP password', 'Reply-To address', 'Brand name', 'Public URL override', 'Support URL', 'Link lifetime (hours)']) {
        expect(_field(label), findsOneWidget, reason: label);
      }
      for (final toggle in <String>['Implicit TLS', 'Require STARTTLS', 'Reject invalid TLS certificates', 'Confirm new sign-ups', 'Confirm email changes', 'New sign-in alerts', 'Account change alerts']) {
        expect(find.text(toggle), findsOneWidget, reason: toggle);
      }
      expect(find.text('Nothing stored yet.'), findsOneWidget);
      expect(find.text('Remove SMTP password'), findsNothing);
      final send = tester.widget<OutlinedButton>(_sendTestButton());
      expect(send.onPressed, isNull, reason: 'nothing to test until it is configured');
    });

    testWidgets('the stored password is never shown, only that one exists', (tester) async {
      await open(tester, (call) {
        if (call.path == '/api/admin/config/email') {
          return FakeReply(200, <String, dynamic>{'configured': true, 'missing': <String>[], 'settings': settings(stored: true, host: 'smtp.example.com', from: 'NeoCloud <no-reply@example.com>')});
        }
        return const FakeReply(404);
      }, mail: true);
      expect(find.text('Ready'), findsOneWidget);
      expect(find.text('A value is stored.'), findsOneWidget);
      expect(tester.widget<TextField>(_field('SMTP password')).controller!.text, isEmpty);
      expect(tester.widget<TextField>(_field('SMTP password')).obscureText, isTrue);
      await tester.tap(find.byTooltip('Show'));
      await tester.pump();
      expect(tester.widget<TextField>(_field('SMTP password')).obscureText, isFalse);
      expect(find.text('Remove SMTP password'), findsOneWidget);
    });

    testWidgets('validates before sending, then saves and turns the app on', (tester) async {
      var saved = false;
      final (http, controller) = await open(tester, (call) {
        if (call.path == '/api/admin/config/email' && call.method == 'GET') return notConfigured(call);
        if (call.path == '/api/admin/config/email' && call.method == 'PUT') {
          saved = true;
          return FakeReply(200, <String, dynamic>{'configured': true, 'missing': <String>[], 'settings': settings(stored: true, host: 'smtp.example.com', from: 'no-reply@example.com')});
        }
        return const FakeReply(404);
      });
      expect(controller.emailConfigured, isFalse);

      await tester.enterText(_field('SMTP port'), '70000');
      await tester.ensureVisible(find.text('Save email settings'));
      await tester.tap(find.text('Save email settings'));
      await _settle(tester);
      expect(find.text('SMTP port must be a whole number from 1 to 65535.'), findsOneWidget);
      expect(saved, isFalse);

      await tester.enterText(_field('SMTP port'), '587');
      await tester.enterText(_field('Support URL'), 'ftp://help');
      await tester.tap(find.text('Save email settings'));
      await _settle(tester);
      expect(find.text('Support URL must be an http or https address.'), findsOneWidget);
      expect(saved, isFalse);

      await tester.enterText(_field('Support URL'), '');
      await tester.enterText(_field('Link lifetime (hours)'), '0');
      await tester.tap(find.text('Save email settings'));
      await _settle(tester);
      expect(find.text('Link lifetime must be from 1 to 8760 hours.'), findsOneWidget);

      await tester.enterText(_field('Link lifetime (hours)'), '48');
      await tester.enterText(_field('Sender address'), 'no-reply@example.com');
      await tester.enterText(_field('SMTP host'), 'smtp.example.com');
      await tester.enterText(_field('SMTP password'), 's3cret');
      await tester.tap(find.text('Save email settings'));
      await _settle(tester);

      final body = http.where('PUT', '/api/admin/config/email').single.body;
      expect(body['from'], 'no-reply@example.com');
      expect(body['smtpHost'], 'smtp.example.com');
      expect(body['smtpPort'], 587);
      expect(body['tokenTtlHours'], 48);
      expect(body['smtpPassword'], 's3cret');
      expect(body['clearSmtpPassword'], isFalse);
      expect(body['requireSignupConfirmation'], isTrue);
      expect(find.text('Email settings saved.'), findsOneWidget);
      expect(find.text('Ready'), findsOneWidget);
      expect(find.text('A value is stored.'), findsOneWidget);
      expect(tester.widget<TextField>(_field('SMTP password')).controller!.text, isEmpty, reason: 'the typed password is not kept');
      expect(controller.emailConfigured, isTrue);
      expect(controller.signupConfirmationRequired, isTrue);
    });

    testWidgets('removing the password asks first, then clears it', (tester) async {
      final (http, _) = await open(tester, (call) {
        if (call.path == '/api/admin/config/email') {
          final method = call.method;
          return FakeReply(200, <String, dynamic>{
            'configured': method == 'GET',
            'missing': method == 'GET' ? <String>[] : <String>['smtpPassword'],
            'settings': settings(stored: method == 'GET', host: 'smtp.example.com', from: 'a@example.com'),
          });
        }
        return const FakeReply(404);
      }, mail: true);
      await tester.ensureVisible(find.text('Remove SMTP password'));
      await tester.tap(find.text('Remove SMTP password'));
      await _settle(tester);
      expect(find.text('Remove the SMTP password?'), findsOneWidget);
      expect(http.where('PUT', '/api/admin/config/email'), isEmpty);
      await tester.tap(find.text('Cancel'));
      await _settle(tester);
      expect(http.where('PUT', '/api/admin/config/email'), isEmpty);

      await tester.tap(find.text('Remove SMTP password'));
      await _settle(tester);
      await tester.tap(find.widgetWithText(FilledButton, 'Remove'));
      await _settle(tester);
      final body = http.where('PUT', '/api/admin/config/email').single.body;
      expect(body['clearSmtpPassword'], isTrue);
      expect(body['smtpPassword'], '');
      expect(find.text('SMTP password removed.'), findsOneWidget);
      expect(find.text('Nothing stored yet.'), findsOneWidget);
    });

    testWidgets('the test email goes to the admin by default and shows success or the mail server error', (tester) async {
      var fail = false;
      final (http, _) = await open(tester, (call) {
        if (call.path == '/api/admin/config/email' && call.method == 'GET') {
          return FakeReply(200, <String, dynamic>{'configured': true, 'missing': <String>[], 'settings': settings(stored: true, host: 'smtp.example.com', from: 'a@example.com')});
        }
        if (call.path == '/api/admin/config/email/test') {
          return fail
              ? const FakeReply(502, <String, dynamic>{'error': 'SMTP 535 Authentication failed', 'code': 'EMAIL_TEST_FAILED'})
              : const FakeReply(200, <String, dynamic>{'ok': true, 'message': 'Sent'});
        }
        return const FakeReply(404);
      }, mail: true);
      expect(tester.widget<TextField>(_field('Send to')).controller!.text, 'ada@example.com');

      await tester.ensureVisible(_sendTestButton());
      await tester.tap(_sendTestButton());
      await _settle(tester);
      expect(http.where('POST', '/api/admin/config/email/test').single.body, <String, dynamic>{'to': 'ada@example.com'});
      expect(find.text('Test email sent to ada@example.com.'), findsOneWidget);

      fail = true;
      await tester.tap(_sendTestButton());
      await _settle(tester);
      expect(find.text('SMTP 535 Authentication failed'), findsOneWidget);
      expect(find.text('Test email sent to ada@example.com.'), findsNothing);
    });

    testWidgets('the admin search finds the email card', (tester) async {
      final http = FakeHttp((call) => const FakeReply(404));
      final controller = await _controller(http, user: _user(isAdmin: true));
      await _pump(tester, controller, () => AdminPanel(controller: controller));
      await tester.enterText(find.byType(TextField).first, 'smtp');
      await _settle(tester);
      expect(find.text('Email'), findsOneWidget);
      expect(find.text('Access'), findsWidgets);
    });
  });

  group('Admin › Users', () {
    Map<String, dynamic> row(int id, String name, {bool verified = true, String? email}) => <String, dynamic>{
      'id': id,
      'username': name,
      'email': email ?? '$name@example.com',
      'isAdmin': id == 7,
      'disabled': false,
      'usedBytes': 0,
      'quotaBytes': -1,
      'quotaMode': 'default',
      'fileCount': 0,
      'lastLogin': null,
      'emailVerified': verified,
    };

    FakeReply users(FakeCall call) {
      if (call.method == 'GET' && call.path == '/api/admin/users') {
        return FakeReply(200, <String, dynamic>{
          'users': <Map<String, dynamic>>[row(7, 'ada'), row(8, 'bob', verified: false)],
        });
      }
      if (call.method == 'GET' && call.path == '/api/admin/config/storage') return const FakeReply(200, <String, dynamic>{'defaultQuotaBytes': -1});
      if (call.method == 'POST' && call.path == '/api/admin/users/8/email/verify') return const FakeReply(200, <String, dynamic>{'success': true});
      return const FakeReply(404);
    }

    testWidgets('an unverified address gets a chip and a "Mark email as verified" action', (tester) async {
      final http = FakeHttp(users);
      final controller = await _controller(http, user: _user(isAdmin: true));
      await _pump(tester, controller, () => AdminPanel(controller: controller));
      expect(find.text('Email unverified'), findsOneWidget);
      await tester.tap(find.byTooltip('Details').last);
      await _settle(tester);
      await tester.tap(find.text('Mark email as verified'));
      await _settle(tester);
      expect(http.where('POST', '/api/admin/users/8/email/verify'), hasLength(1));
      expect(find.textContaining("bob's email is marked as verified"), findsOneWidget);
    });

    testWidgets('without mail the users list is as before', (tester) async {
      final controller = await _controller(FakeHttp(users), mail: false, user: _user(isAdmin: true));
      await _pump(tester, controller, () => AdminPanel(controller: controller));
      expect(find.text('Email unverified'), findsNothing);
    });
  });
}
