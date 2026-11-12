import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Everything outside Admin is written for people who don't run the server:
// no admin paths, no "BYOK", no tokens. Rendered as a normal account.

class _FakeAi extends Fake implements AiApi {
  _FakeAi(this.currentStatus);

  final AiStatus currentStatus;

  @override
  Future<AiStatus> status() async => currentStatus;

  @override
  Future<List<ModelMeta>> models() async => const <ModelMeta>[];

  @override
  Future<List<Map<String, dynamic>>> byokProviders() async => <Map<String, dynamic>>[
    <String, dynamic>{'id': 'openai', 'label': 'OpenAI', 'supportsApiKey': true, 'configured': false},
    <String, dynamic>{'id': 'openai-compatible', 'label': 'Custom endpoint', 'supportsBaseUrl': true, 'isCustomEndpoint': true},
  ];

  @override
  Future<Map<String, dynamic>> neoRouter() async => <String, dynamic>{
    'roles': <String>['chat', 'embeddings'],
    'isAdmin': false,
    'server': <String, dynamic>{'connected': true},
    'user': <String, dynamic>{'connected': false},
  };
}

const AiStatus _notSetUp = AiStatus(available: false, enabled: true, optedIn: false, budgetUsed: 1200, budgetLimit: 10000);
const AiStatus _disabled = AiStatus(available: false, enabled: false, optedIn: false);

Future<NeoCloudController> _member(AiStatus status) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    ai: _FakeAi(status),
  );
  controller
    ..isBooting = false
    ..user = AccountUser(
      id: 7,
      username: 'ada',
      displayName: null,
      email: null,
      isAdmin: false,
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

/// Every piece of text on screen, Text and RichText alike.
List<String> _visibleTexts(WidgetTester tester) => <String>[
  for (final widget in tester.widgetList<RichText>(find.byType(RichText))) widget.text.toPlainText(),
  for (final widget in tester.widgetList<EditableText>(find.byType(EditableText))) widget.controller.text,
];

void _expectPlainWords(WidgetTester tester, String where) {
  final forbidden = RegExp(r'admin|byok|token', caseSensitive: false);
  final offenders = _visibleTexts(tester).where(forbidden.hasMatch).toList();
  expect(offenders, isEmpty, reason: '$where shows admin or technical words');
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  testWidgets('Files home speaks to a normal account', (tester) async {
    final controller = await _member(_notSetUp);
    await _pump(tester, FilesPanel(controller: controller));
    _expectPlainWords(tester, 'Files home');
  });

  testWidgets('every Settings page speaks to a normal account', (tester) async {
    final controller = await _member(_notSetUp);
    for (final page in SettingsPage.values.where((page) => page.visibleFor(controller))) {
      controller.setSettingsPage(page);
      await _pump(tester, SettingsPanel(controller: controller));
      _expectPlainWords(tester, 'Settings › ${page.name}');
    }
  });

  testWidgets('Settings › AI when AI is not set up points to the person who runs NeoCloud', (tester) async {
    final controller = await _member(_notSetUp);
    await _pump(tester, SingleChildScrollView(child: AiSettingsPage(controller: controller)));
    expect(find.text("AI isn't set up yet"), findsOneWidget);
    expect(find.textContaining('Ask the person who runs your NeoCloud'), findsOneWidget);
    expect(find.text('Open Admin'), findsNothing);
    // Advanced opens by itself here: the account's own keys are the way in.
    expect(find.text('Your own AI service'), findsOneWidget);
    expect(find.text('12% used · resets on the 1st'), findsOneWidget);
    _expectPlainWords(tester, 'Settings › AI (not set up)');
  });

  testWidgets('Settings › AI when AI is off says whom to ask', (tester) async {
    final controller = await _member(_disabled);
    await _pump(tester, SingleChildScrollView(child: AiSettingsPage(controller: controller)));
    expect(find.text('AI is turned off'), findsOneWidget);
    expect(find.textContaining('Ask the person who runs your NeoCloud'), findsOneWidget);
    expect(find.text('Open Admin'), findsNothing);
    _expectPlainWords(tester, 'Settings › AI (disabled)');
  });
}
