import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/l10n/app_language.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A 1x1 PNG, enough to be "a picture".
final Uint8List tinyPng = Uint8List.fromList(<int>[
  0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
  0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xff, 0xff, 0x3f,
  0x00, 0x05, 0xfe, 0x02, 0xfe, 0xa7, 0x35, 0x81, 0x84, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
]);

const String testToken = 'tok_test_1234567890';

SigningField field(
  String id,
  FieldType type, {
  String name = '',
  bool required = true,
  Map<String, Object?> prefs = const <String, Object?>{},
  double y = 0.1,
  double x = 0.1,
  int page = 0,
  String party = 'me',
  Object? defaultValue,
  bool readonly = false,
  SigningValue? value,
}) {
  final base = SigningField(
    id: id,
    partyId: party,
    type: type,
    name: name,
    required: required,
    readonly: readonly,
    defaultValue: defaultValue,
    prefs: FieldPrefs(prefs),
    areas: <FieldArea>[FieldArea(page: page, x: x, y: y, w: 0.3, h: 0.04)],
  );
  return value == null
      ? base
      : SigningField(id: id, partyId: party, type: type, name: name, required: required, prefs: FieldPrefs(prefs), areas: base.areas, value: value);
}

PublicSigningView viewOf({
  PublicSigningState state = PublicSigningState.ready,
  List<SigningField> fields = const <SigningField>[],
  List<SigningValue> myValues = const <SigningValue>[],
  DateTime? consentedAt,
  String signerName = '',
  String locale = 'en',
  int waitingCount = 0,
  PublicProgress progress = const PublicProgress(done: 0, total: 2),
  String title = 'Rental agreement',
}) {
  return PublicSigningView(
    state: state,
    document: PublicDocumentInfo(
      title: title,
      pageCount: 2,
      locale: locale,
      consentText: 'I agree to sign this document electronically.',
      consentVersion: '1',
    ),
    party: PublicParty(id: 'me', name: 'Tenant', color: '#2E86AB', status: PartyStatus.opened, consentedAt: consentedAt, signerName: signerName.isEmpty ? null : signerName),
    progress: progress,
    fields: fields,
    myValues: myValues,
    turn: PublicTurn(canSign: state == PublicSigningState.ready, waitingCount: waitingCount),
  );
}

/// Pages without PDFium: white sheets of the right shape.
Widget fakePages(BuildContext context, SigningPageController pages, PageOverlayBuilder overlay) {
  return SigningPageList(
    pageSizes: const <Size>[Size(595, 842), Size(595, 842)],
    controller: pages,
    overlayBuilder: overlay,
    pageContent: (context, index, size) => ColoredBox(color: Colors.white, child: Align(alignment: Alignment.topLeft, child: Text('Page ${index + 1}'))),
  );
}

Future<NeoCloudController> signingController(FakePublicSigningApi api, {Uri? launch, FakeSigningAccountApi? account, bool signedIn = false}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    publicSigningFactory: (token) => api,
    signingAccount: account,
    launchUri: launch ?? Uri.parse('https://cloud.example/app/sign/$testToken'),
  );
  if (signedIn) {
    controller.user = AccountUser(
      id: 7,
      username: 'ada',
      displayName: 'Ada Lovelace',
      email: 'ada@example.com',
      isAdmin: false,
      storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
    );
  } else {
    controller.signingSessionChecked = true;
  }
  return controller;
}

/// Pumps the whole app on a signing link and lets the first load finish.
Future<NeoCloudController> pumpSigning(
  WidgetTester tester,
  FakePublicSigningApi api, {
  Size size = const Size(1440, 900),
  double textScale = 1,
  Brightness brightness = Brightness.light,
  FakeSigningAccountApi? account,
  bool signedIn = false,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  tester.platformDispatcher.textScaleFactorTestValue = textScale;
  tester.platformDispatcher.platformBrightnessTestValue = brightness;
  addTearDown(() {
    tester.view.reset();
    tester.platformDispatcher.clearAllTestValues();
    debugPublicPagesBuilder = null;
    currentAppLanguage = AppLanguage.english;
  });
  debugPublicPagesBuilder = fakePages;
  currentAppLanguage = AppLanguage.english;
  final controller = await signingController(api, account: account, signedIn: signedIn);
  _active = controller;
  await tester.pumpWidget(NeoCloudApp(controller: controller));
  await settle(tester);
  return controller;
}

Future<void> settle(WidgetTester tester, {int frames = 8}) async {
  for (var i = 0; i < frames; i++) {
    await tester.pump(const Duration(milliseconds: 120));
  }
}

/// Taps a widget found by key and lets the screen settle.
Future<void> tapKey(WidgetTester tester, String key) async {
  final finder = find.byKey(ValueKey<String>(key));
  await tester.ensureVisible(finder);
  await tester.tap(finder);
  await settle(tester, frames: 4);
}

NeoCloudController? _active;

/// A widget test that always unmounts the page and stops the flow's timers
/// (polling, retries) at the end.
void testSigning(String name, Future<void> Function(WidgetTester tester) body) {
  testWidgets(name, (tester) async {
    try {
      await body(tester);
    } finally {
      await tester.pumpWidget(const SizedBox());
      _active?.signingFlow?.dispose();
      _active = null;
    }
  });
}

final DateTime consented = DateTime.utc(2026, 1, 1);
