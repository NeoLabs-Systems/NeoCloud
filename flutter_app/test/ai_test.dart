import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/ai/model_roles.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/navigation/app_route.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Answers the AI routes from memory; anything not set up throws a 404 like
/// a server without AI.
class _FakeAi extends Fake implements AiApi {
  _FakeAi({this.currentStatus, this.catalog = const <ModelMeta>[]});

  final AiStatus? currentStatus;
  final List<ModelMeta> catalog;

  static const ApiException _missing = ApiException(404, 'Not found', code: 'NOT_FOUND');

  @override
  Future<AiStatus> status() async => currentStatus ?? (throw _missing);

  @override
  Future<List<ModelMeta>> models() async => catalog;

  @override
  Future<List<Map<String, dynamic>>> byokProviders() async => <Map<String, dynamic>>[
    <String, dynamic>{'id': 'openai', 'label': 'OpenAI', 'supportsApiKey': true, 'configured': true},
    <String, dynamic>{'id': 'openai-compatible', 'label': 'Custom endpoint', 'supportsBaseUrl': true, 'isCustomEndpoint': true},
  ];

  @override
  Future<Map<String, dynamic>> neoRouter() async => <String, dynamic>{
    'roles': <String>['chat', 'embeddings', 'transcription', 'images'],
    'isAdmin': true,
    'server': <String, dynamic>{'connected': false},
    'user': <String, dynamic>{'connected': false},
  };

  @override
  Future<AdminAiConfig> adminConfig() async => const AdminAiConfig(enabled: true, monthlyTokens: 500000);

  @override
  Future<List<AdminAiProvider>> adminProviders() async => const <AdminAiProvider>[
    AdminAiProvider(key: 'NEOCLOUD_OPENAI_API_KEY', label: 'OpenAI', isUrl: false, configured: true, hint: 'sk-…a1b2'),
    AdminAiProvider(key: 'NEOCLOUD_ANTHROPIC_API_KEY', label: 'Anthropic', isUrl: false, configured: false, hint: ''),
    AdminAiProvider(key: 'NEOCLOUD_OLLAMA_URL', label: 'Ollama', isUrl: true, configured: false, hint: ''),
  ];

  @override
  Future<AdminModelCatalog> adminModels() async =>
      AdminModelCatalog(models: catalog, disabledModels: const <String>{'openai::gpt-4.1-mini'}, autoEnableNewModels: true);

  @override
  Future<AiUsageSummary> adminUsage() async => AiUsageSummary.fromJson(<String, dynamic>{
    'since': '2026-11-01 00:00:00',
    'users': <Map<String, dynamic>>[
      <String, dynamic>{'userId': 7, 'username': 'ada', 'tokens': 8000, 'calls': 10, 'errors': 1, 'limit': 500000},
    ],
    'features': <Map<String, dynamic>>[
      <String, dynamic>{'feature': 'rename', 'tokens': 8000, 'calls': 10},
    ],
    'models': <Map<String, dynamic>>[
      <String, dynamic>{'provider': 'openai', 'model': 'gpt-4o', 'tokens': 8000, 'calls': 10, 'avgLatencyMs': 900},
    ],
  });
}

const List<ModelMeta> _catalog = <ModelMeta>[
  ModelMeta(id: 'openai::gpt-4o', modelId: 'gpt-4o', label: 'GPT-4o', provider: 'openai', supportsVision: true, priceTier: 'medium'),
  ModelMeta(id: 'openai::gpt-4.1-mini', modelId: 'gpt-4.1-mini', label: 'GPT-4.1 mini', provider: 'openai', priceTier: 'cheap'),
  ModelMeta(id: 'anthropic::claude-haiku', modelId: 'claude-haiku', label: 'Claude Haiku', provider: 'anthropic'),
  ModelMeta(id: 'openai::text-embedding-3-small', modelId: 'text-embedding-3-small', label: 'Embedding 3 small', provider: 'openai', kind: 'embedding'),
];

Future<NeoCloudController> _controller(_FakeAi ai, {bool admin = false}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    ai: ai,
  );
  controller
    ..isBooting = false
    ..user = AccountUser(
      id: 7,
      username: 'ada',
      displayName: null,
      email: null,
      isAdmin: admin,
      storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
    );
  return controller;
}

Future<void> _pump(WidgetTester tester, Widget child, {Size size = const Size(1440, 1800)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: SingleChildScrollView(child: child))));
  await tester.pumpAndSettle();
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('model roles', () {
    test('each role offers only the models that can do its work', () {
      List<String> ids(AiRole role) => modelsForRole(role, _catalog).map((model) => model.id).toList();
      expect(ids(AiRole.text), <String>['openai::gpt-4o', 'openai::gpt-4.1-mini', 'anthropic::claude-haiku']);
      expect(ids(AiRole.vision), <String>['openai::gpt-4o']);
      expect(ids(AiRole.embedding), <String>['openai::text-embedding-3-small']);
    });

    test('an unavailable model is never offered, a missing choice is auto', () {
      const off = ModelMeta(id: 'openai::gpt-x', modelId: 'gpt-x', label: 'X', provider: 'openai', supportsVision: true, available: false);
      expect(modelsForRole(AiRole.vision, <ModelMeta>[off]), isEmpty);
      expect(selectedModel(AiRole.smart, const <String, String>{}), 'auto');
      expect(selectedModel(AiRole.smart, const <String, String>{'ai_model_smart': 'openai::gpt-4o'}), 'openai::gpt-4o');
    });

    test('settings read the AI keys and models read vision and kind', () {
      final settings = UserSettings.fromJson(<String, dynamic>{
        'ai_opt_in': true,
        'ai_model_text': 'auto',
        'ai_model_vision': 'openai::gpt-4o',
        'ai_features': <String, dynamic>{'names': true, 'folders': false, 'junk': 'x'},
        'ai_naming_template': '{date} {subject}',
      });
      expect(settings.aiOptIn, isTrue);
      expect(settings.aiModels, <String, String>{'ai_model_text': 'auto', 'ai_model_vision': 'openai::gpt-4o'});
      expect(settings.aiFeatures, <String, bool>{'names': true, 'folders': false});
      final model = ModelMeta.fromJson(<String, dynamic>{'id': 'neorouter::auto', 'supportsVision': true, 'kind': 'chat', 'extra': 1});
      expect(model.supportsVision, isTrue);
      expect(model.modelId, 'neorouter::auto');
    });

    test('the server\'s status, usage, config and folder shapes parse', () {
      final status = AiStatus.fromJson(<String, dynamic>{
        'available': true,
        'enabled': true,
        'optedIn': true,
        'roles': <String, dynamic>{
          'text': <String, dynamic>{'ready': true, 'model': 'openai::gpt-4o', 'source': 'auto', 'requestedUnavailable': true},
          'transcription': <String, dynamic>{'ready': false, 'model': null, 'source': null, 'requestedUnavailable': false},
        },
        'budget': <String, dynamic>{'used': 10, 'limit': null},
      });
      expect(status.roles['text']!.requestedUnavailable, isTrue);
      expect(status.roles['text']!.source, 'auto');
      expect(status.budgetLimit, isNull);
      final usage = AiUsageSummary.fromJson(<String, dynamic>{
        'since': '2026-11-01 00:00:00',
        'users': <Map<String, dynamic>>[
          <String, dynamic>{'userId': 7, 'username': 'ada', 'tokens': 300, 'calls': 3, 'errors': 1, 'limit': 1000},
          <String, dynamic>{'userId': 8, 'username': 'grace', 'tokens': 200, 'calls': 2, 'errors': 0, 'limit': null},
        ],
        'features': <Map<String, dynamic>>[],
        'models': <Map<String, dynamic>>[<String, dynamic>{'provider': 'openai', 'model': 'gpt-4o', 'tokens': 500, 'calls': 5}],
      });
      expect((usage.calls, usage.tokens, usage.errors), (5, 500, 1));
      expect(usage.users.first.limit, 1000);
      expect(usage.models.single.detail, 'openai');
      expect(AdminAiConfig.fromJson(<String, dynamic>{'enabled': true, 'monthlyTokens': 5000}).monthlyTokens, 5000);
      expect(FileNode.fromJson(<String, dynamic>{'id': 'f', 'type': 'folder', 'name': 'Tax', 'aiExcluded': true}).aiExcluded, isTrue);
    });
  });

  test('Settings › AI and Admin › AI have their own addresses', () async {
    final controller = await _controller(_FakeAi(), admin: true);
    controller.openRoute(const AppRoute('settings', 'ai'));
    expect(controller.settingsPage, SettingsPage.ai);
    expect(SettingsPage.ai.visibleFor(controller), isTrue);
    for (final tab in <String>['providers', 'models']) {
      controller.openRoute(AppRoute('admin', tab));
      expect(controller.currentRoute, AppRoute('admin', tab));
    }
  });

  testWidgets('the vision picker lists only models that can see images', (tester) async {
    await _pump(
      tester,
      SizedBox(
        height: 600,
        child: ModelPickerPanel(models: _catalog, role: AiRole.vision, value: 'auto', onChanged: (_) {}),
      ),
    );
    expect(find.text('Auto'), findsOneWidget);
    expect(find.text('GPT-4o'), findsOneWidget);
    expect(find.text('GPT-4.1 mini'), findsNothing);
    expect(find.text('Claude Haiku'), findsNothing);
    expect(find.text('Embedding 3 small'), findsNothing);
  });

  testWidgets('Settings › AI without a model leads with the setup card and still offers keys', (tester) async {
    final ai = _FakeAi(currentStatus: const AiStatus(available: false, enabled: true, optedIn: false));
    await _pump(tester, AiSettingsPage(controller: await _controller(ai, admin: true)));
    expect(find.text("AI isn't set up yet"), findsOneWidget);
    expect(find.text('Open Admin'), findsOneWidget);
    // Advanced opens by itself: the account's own keys are a way in.
    expect(find.text('Your own AI service'), findsOneWidget);
    expect(find.text('NeoRouter'), findsOneWidget);
    expect(find.text('Using your key'), findsOneWidget);
  });

  testWidgets('Settings › AI when ready shows a model per role and the usage', (tester) async {
    final ai = _FakeAi(
      currentStatus: const AiStatus(
        available: true,
        enabled: true,
        optedIn: true,
        roles: <String, AiRoleStatus>{'text': AiRoleStatus(ready: true, model: 'neorouter::auto:fast')},
        budgetUsed: 1200,
        budgetLimit: 10000,
      ),
      catalog: _catalog,
    );
    await _pump(tester, AiSettingsPage(controller: await _controller(ai)));
    expect(find.text("AI isn't set up yet"), findsNothing);
    expect(find.text('Use AI features'), findsOneWidget);
    expect(find.text('Suggest names'), findsOneWidget);
    expect(find.text('Suggest tags'), findsOneWidget);
    expect(find.text('Apply high-confidence suggestions automatically'), findsOneWidget);
    // Usage is a plain share; the token counts stay in the tooltip.
    expect(find.text('12% used · resets on the 1st'), findsOneWidget);
    expect(find.byTooltip('1,200 of 10,000 tokens'), findsOneWidget);
    expect(find.text('Open Admin'), findsNothing);
    // The models and own keys wait under Advanced.
    expect(find.text('Everyday tasks'), findsNothing);
    expect(find.text('Your own AI service'), findsNothing);
    await tester.tap(find.text('Advanced'));
    await tester.pumpAndSettle();
    for (final label in <String>['Everyday tasks', 'Images', 'Similar files']) {
      expect(find.text(label), findsOneWidget);
    }
    // "Summaries" is both a suggestion and a kind of work.
    expect(find.text('Summaries'), findsNWidgets(2));
    expect(find.text('Auto'), findsNWidgets(4));
    expect(find.text('Your own AI service'), findsOneWidget);
  });

  testWidgets('Settings › AI on a server without AI says so', (tester) async {
    await _pump(tester, AiSettingsPage(controller: await _controller(_FakeAi())));
    expect(find.text("AI isn't available yet"), findsOneWidget);
    expect(find.text("This feature isn't available yet."), findsOneWidget);
  });

  testWidgets('Admin › Providers shows NeoRouter first, then the provider rows', (tester) async {
    final ai = _FakeAi(catalog: _catalog);
    await _pump(tester, AdminAiProvidersTab(controller: await _controller(ai, admin: true)), size: const Size(1440, 2000));
    // NeoAgent's eyebrow title over the card, and the row's label.
    expect(find.text('NEOROUTER'), findsOneWidget);
    expect(find.text('NeoRouter'), findsOneWidget);
    expect(find.text('Connect'), findsOneWidget);
    expect(find.text('SERVER PROVIDER CREDENTIALS'), findsOneWidget);
    for (final label in <String>['OpenAI', 'Anthropic', 'Ollama']) {
      expect(find.text(label), findsOneWidget);
    }
    expect(find.text('sk-…a1b2'), findsOneWidget);
    expect(find.text('1 of 3 set'), findsOneWidget);
    expect(find.text('Replace'), findsOneWidget);
    expect(find.text('Set URL'), findsOneWidget);
  });

  testWidgets('Admin › Models shows the switch, the budget, the models and the usage', (tester) async {
    final ai = _FakeAi(catalog: _catalog);
    await _pump(tester, AdminAiModelsTab(controller: await _controller(ai, admin: true)), size: const Size(1440, 3000));
    expect(find.text('Enable AI features'), findsOneWidget);
    expect(find.text('Monthly token budget'), findsOneWidget);
    expect(find.text('Model availability'), findsOneWidget);
    expect(find.text('Busiest accounts'), findsOneWidget);
    expect(find.text('By feature'), findsOneWidget);
    expect(find.text('Requests'), findsOneWidget);
  });
}
