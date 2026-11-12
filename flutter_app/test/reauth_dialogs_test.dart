import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/security/webauthn_client.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Adding a security key and exporting the master key ask for the password
// again, and for a code when 2FA is on, in the same dialog as Your data.

class _Call {
  const _Call(this.password, this.code);

  final String password;
  final String? code;
}

/// Answers the calls these screens make; everything else fails like an
/// unreachable server, which the screens show as an error and move on.
class _FakeClient extends BackendClient {
  _FakeClient({required this.twoFactor}) : super(baseUrl: 'http://localhost:1');

  final bool twoFactor;
  final List<_Call> registrationStarts = <_Call>[];
  final List<_Call> masterKeyExports = <_Call>[];
  bool rejectPassword = false;

  ApiException get _wrongPassword => const ApiException(401, 'Current password is incorrect', code: 'WRONG_PASSWORD');

  @override
  Future<Map<String, dynamic>> account() async => <String, dynamic>{
    'twoFactor': <String, dynamic>{'enabled': twoFactor},
    'securityKeys': const <Object>[],
  };

  @override
  Future<List<SecurityKeyItem>> securityKeys() async => const <SecurityKeyItem>[];

  @override
  Future<Map<String, dynamic>> beginSecurityKeyRegistration(String password, String? code) async {
    registrationStarts.add(_Call(password, code));
    if (rejectPassword) throw _wrongPassword;
    return <String, dynamic>{
      'options': <String, dynamic>{'challenge': 'abc'},
    };
  }

  @override
  Future<List<SecurityKeyItem>> completeSecurityKeyRegistration(Map<String, dynamic> response, String label) async =>
      <SecurityKeyItem>[
        SecurityKeyItem.fromJson(<String, dynamic>{
          'id': 1,
          'label': label,
          'createdAt': '2026-11-12T10:00:00Z',
          'lastUsedAt': null,
          'transports': const <String>[],
          'backedUp': false,
          'rpId': 'localhost',
        }),
      ];

  @override
  Future<Map<String, dynamic>> adminBackups() async => <String, dynamic>{'targets': const <Object>[]};

  @override
  Future<String> adminMasterKey(String password, String? code) async {
    masterKeyExports.add(_Call(password, code));
    if (rejectPassword) throw _wrongPassword;
    return 'a' * 64;
  }
}

class _FakeWebAuthn implements WebAuthnClient {
  final List<Map<String, dynamic>> created = <Map<String, dynamic>>[];

  @override
  bool get isSupported => true;

  @override
  Future<Map<String, dynamic>> createCredential(Map<String, dynamic> options) async {
    created.add(options);
    return <String, dynamic>{'id': 'cred'};
  }

  @override
  Future<Map<String, dynamic>> getAssertion(Map<String, dynamic> options) async => const <String, dynamic>{};
}

Future<NeoCloudController> _controller(_FakeClient client, _FakeWebAuthn webAuthn, {bool isAdmin = false}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: client,
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
  );
  controller
    ..webAuthn = webAuthn
    ..isBooting = false
    ..user = AccountUser(
      id: 7,
      username: 'ada',
      displayName: null,
      email: null,
      isAdmin: isAdmin,
      storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
    );
  return controller;
}

Future<void> _pump(WidgetTester tester, Widget child) async {
  tester.view.physicalSize = const Size(1440, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: child)));
  for (var i = 0; i < 5; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 5; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

/// The dialog sits on top, so its fields come last (Settings › Security has
/// its own current-password field for changing the password).
Finder _dialogField(String label) => find.widgetWithText(TextField, label).last;

int _fieldCount(String label) => find.widgetWithText(TextField, label).evaluate().length;

Future<void> _openSecurityKeyDialog(WidgetTester tester) async {
  final add = find.text('Add security key');
  expect(add, findsOneWidget);
  await tester.ensureVisible(add);
  await tester.tap(add);
  await _settle(tester);
}

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await initializeDateFormatting('en');
  });

  testWidgets('adding a security key asks for the password before the key prompt', (tester) async {
    final client = _FakeClient(twoFactor: false);
    final webAuthn = _FakeWebAuthn();
    final controller = await _controller(client, webAuthn);
    controller.setSettingsPage(SettingsPage.security);
    await _pump(tester, SettingsPanel(controller: controller));

    final pageFields = _fieldCount('Current password');
    await _openSecurityKeyDialog(tester);
    expect(_fieldCount('Current password'), pageFields + 1);
    expect(_fieldCount('2FA or recovery code'), 0);
    expect(client.registrationStarts, isEmpty);
    expect(webAuthn.created, isEmpty, reason: 'no key prompt before the password');

    await tester.enterText(_dialogField('Current password'), 'hunter2-long');
    await tester.pump();
    await tester.tap(find.text('Add security key').last);
    await _settle(tester);

    expect(client.registrationStarts.single.password, 'hunter2-long');
    expect(client.registrationStarts.single.code, isNull);
    expect(webAuthn.created.single['challenge'], 'abc');
    expect(_fieldCount('Current password'), pageFields, reason: 'the dialog closes');
  });

  testWidgets('with 2FA on the security key dialog sends the code, and shows a wrong password in place', (tester) async {
    final client = _FakeClient(twoFactor: true)..rejectPassword = true;
    final webAuthn = _FakeWebAuthn();
    final controller = await _controller(client, webAuthn);
    controller.setSettingsPage(SettingsPage.security);
    await _pump(tester, SettingsPanel(controller: controller));

    await _openSecurityKeyDialog(tester);
    await tester.enterText(_dialogField('Current password'), 'wrong-password');
    await tester.enterText(_dialogField('2FA or recovery code'), '123456');
    await tester.pump();
    await tester.tap(find.text('Add security key').last);
    await _settle(tester);

    expect(client.registrationStarts.single.code, '123456');
    expect(find.text('The password is not correct.'), findsOneWidget);
    expect(webAuthn.created, isEmpty);
  });

  testWidgets('the master key export sends the password and the 2FA code', (tester) async {
    final client = _FakeClient(twoFactor: true);
    final controller = await _controller(client, _FakeWebAuthn(), isAdmin: true);
    await _pump(tester, AdminPanel(controller: controller));

    await tester.tap(find.widgetWithText(FilledButton, 'Export master key'));
    await _settle(tester);
    await tester.enterText(_dialogField('Current password'), 'admin-password');
    await tester.enterText(_dialogField('2FA or recovery code'), '654321');
    await tester.pump();
    await tester.tap(find.widgetWithText(FilledButton, 'Show master key'));
    await _settle(tester);

    expect(client.masterKeyExports.single.password, 'admin-password');
    expect(client.masterKeyExports.single.code, '654321');
    expect(find.textContaining('a' * 64), findsWidgets, reason: 'the key is shown');
  });
}
