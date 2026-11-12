import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/navigation/app_route.dart';
import 'package:shared_preferences/shared_preferences.dart';

FileNode _file(String id, String name, {String? vaultId, String parentId = 'root'}) => FileNode(
  id: id,
  parentId: parentId,
  type: 'file',
  name: name,
  size: 120000,
  mime: 'application/pdf',
  etag: 'e-$id',
  version: 1,
  modifiedAt: DateTime(2026, 11, 10),
  vaultId: vaultId,
);

FileNode _folder(String id, String name, {String? vaultId, bool excluded = false, String? parentId = 'root'}) => FileNode(
  id: id,
  parentId: parentId,
  type: 'folder',
  name: name,
  size: 0,
  mime: null,
  etag: 'e-$id',
  version: 1,
  modifiedAt: DateTime(2026, 11, 10),
  vaultId: vaultId,
  aiExcluded: excluded,
);

AiSuggestion _rename(String id, String nodeId, String from, String to, {String tier = 'medium'}) => AiSuggestion(
  id: id,
  nodeId: nodeId,
  kind: 'rename',
  from: from,
  to: to,
  reason: 'Invoice from Telekom dated 12 March 2026',
  confidence: tier == 'high' ? 0.92 : 0.7,
  tier: tier,
  model: 'openai::gpt-4o-mini',
  node: _file(nodeId, from),
);

/// The AI routes from memory, recording what was asked.
class _FakeAi extends Fake implements AiApi {
  _FakeAi({this.optedIn = true, List<AiSuggestion>? pending, this.proposals = const <AiRuleProposal>[], List<AiRule>? rules})
    : pending = pending ?? <AiSuggestion>[],
      ruleList = rules ?? <AiRule>[];

  final bool optedIn;
  final List<AiSuggestion> pending;
  List<AiRuleProposal> proposals;
  final List<AiRule> ruleList;
  final List<String> calls = <String>[];
  final Set<String> stale = <String>{};

  AiSuggestionCounts get _counts {
    int of(String kind) => pending.where((s) => s.kind == kind && s.tier != 'low').length;
    return AiSuggestionCounts(
      rename: of('rename'),
      move: of('move'),
      tags: of('tags'),
      total: pending.where((s) => s.tier != 'low').length,
      high: pending.where((s) => s.tier == 'high').length,
      hiddenLow: pending.where((s) => s.tier == 'low').length,
      proposals: proposals.length,
    );
  }

  @override
  Future<AiStatus> status() async => AiStatus(available: true, enabled: true, optedIn: optedIn);

  @override
  Future<AiSuggestionPage> suggestions({String? kind, String? tier, String? status, String? nodeId, int? limit, int? offset}) async {
    final items = pending.where((s) {
      if (kind != null && s.kind != kind) return false;
      if (tier == 'high') return s.tier == 'high';
      return tier == 'all' || s.tier != 'low';
    }).toList();
    return AiSuggestionPage(items: items, total: items.length, counts: _counts);
  }

  @override
  Future<AiSuggestionCounts> suggestionCounts() async => _counts;

  AiSuggestion _take(String id) {
    final suggestion = pending.firstWhere((s) => s.id == id);
    pending.remove(suggestion);
    return suggestion;
  }

  @override
  Future<AiSuggestion> acceptSuggestion(String id, {Object? value}) async {
    calls.add(value == null ? 'accept $id' : 'accept $id $value');
    if (stale.contains(id)) {
      pending.removeWhere((s) => s.id == id);
      throw const ApiException(409, 'Stale', code: 'AI_SUGGESTION_STALE');
    }
    return _take(id);
  }

  @override
  Future<AiSuggestion> rejectSuggestion(String id) async {
    calls.add('reject $id');
    return _take(id);
  }

  @override
  Future<AiSuggestion> undoSuggestion(String id) async {
    calls.add('undo $id');
    return AiSuggestion(id: id, nodeId: '', kind: 'rename', from: '', to: '', status: 'undone');
  }

  @override
  Future<AiAcceptHighResult> acceptHigh({String? kind}) async {
    final high = pending.where((s) => s.tier == 'high').toList();
    calls.add('accept-high ${high.length}');
    pending.removeWhere((s) => s.tier == 'high');
    return AiAcceptHighResult(accepted: high.length, failed: const <String, String>{});
  }

  @override
  Future<List<AiRuleProposal>> ruleProposals() async => proposals;

  @override
  Future<AiRule> acceptProposal(int id, {String? name}) async {
    calls.add('rule-from-proposal $id');
    proposals = proposals.where((p) => p.id != id).toList();
    return AiRule(id: 9, name: 'Telekom invoice', targetFolderId: 'f-tel');
  }

  @override
  Future<void> dismissProposal(int id) async {
    calls.add('dismiss-proposal $id');
    proposals = proposals.where((p) => p.id != id).toList();
  }

  @override
  Future<List<AiRule>> rules() async => List<AiRule>.of(ruleList);

  @override
  Future<void> deleteRule(int id) async {
    calls.add('delete-rule $id');
    ruleList.removeWhere((rule) => rule.id == id);
  }

  @override
  Future<AiRule> updateRule(int id, Map<String, Object?> patch) async {
    calls.add('update-rule $id $patch');
    return ruleList.firstWhere((rule) => rule.id == id);
  }

  @override
  Future<AiSummary> summarize(String fileId, {bool refresh = false}) async {
    calls.add(refresh ? 'summarize $fileId refresh' : 'summarize $fileId');
    return AiSummary(
      summary: refresh ? 'A fresher summary.' : 'Telekom bills 39.95 EUR for March.',
      model: 'openai::gpt-4o',
      createdAt: DateTime(2026, 11, 12),
      cached: !refresh,
    );
  }

  @override
  Future<String> requestSuggestions({List<String>? nodeIds, String? folderId, List<String>? kinds}) async {
    calls.add('jobs ${folderId ?? nodeIds} $kinds');
    return 'batch-1';
  }

  @override
  Future<AiJobStatus> jobStatus({String? batchId}) async =>
      const AiJobStatus(done: 12, batches: <AiJobBatch>[AiJobBatch(id: 'batch-1', total: 12, done: 10, skipped: 2)]);
}

/// A server that answers one folder listing.
class _FakeClient extends BackendClient {
  _FakeClient(this.listing) : super(baseUrl: 'http://localhost:1');

  final FolderListing listing;

  @override
  Future<FolderListing> listFolder(String folderId, {String sort = 'name', String order = 'asc', int? limit, String? cursor}) async => listing;

  @override
  Future<List<FileNode>> recent({String? by}) async => const <FileNode>[];

  @override
  Future<Uint8List?> thumbnailBytes(String fileId, {int size = 256}) async => null;
}

Future<NeoCloudController> _controller(_FakeAi ai, {FolderListing? listing}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: listing == null ? BackendClient(baseUrl: 'http://localhost:1') : _FakeClient(listing),
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
      isAdmin: false,
      storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
    )
    ..settings = const UserSettings(aiOptIn: true);
  await controller.aiReview.refresh();
  return controller;
}

Future<void> _pump(WidgetTester tester, Widget child, {Size size = const Size(1440, 1400)}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: child)));
  await tester.pumpAndSettle();
}

Future<void> _pumpApp(WidgetTester tester, NeoCloudController controller) async {
  tester.view.physicalSize = const Size(1440, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  controller.openRoute(const AppRoute('files'));
  await tester.pumpWidget(NeoCloudApp(controller: controller));
  await tester.pumpAndSettle();
}

/// Leaves no undo or reload timer behind.
Future<void> _finish(WidgetTester tester, NeoCloudController controller) async {
  controller.undo.dismiss();
  await tester.pump(const Duration(seconds: 1));
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  test('suggestions, counts, rules and jobs parse from the server\'s shapes', () {
    final move = AiSuggestion.fromJson(<String, dynamic>{
      'id': 's1',
      'nodeId': 'f1',
      'kind': 'move',
      'from': <String, dynamic>{'id': 'root', 'path': '/'},
      'to': <String, dynamic>{'id': null, 'parentId': 'p', 'name': 'Telekom', 'path': '/Finance/Telekom'},
      'confidence': 0.91,
      'tier': 'high',
      'source': 'rule',
      'ruleId': 3,
      'node': <String, dynamic>{'id': 'f1', 'type': 'file', 'name': 'scan.pdf', 'aiTags': <String>['invoice'], 'aiMeta': <String, dynamic>{'issuer': 'Telekom'}},
    });
    expect(move.toFolder!.isNew, isTrue);
    expect(move.toFolder!.path, '/Finance/Telekom');
    expect(move.fromRule, isTrue);
    expect(move.node!.aiTags, <String>['invoice']);
    expect(move.node!.aiMeta!['issuer'], 'Telekom');
    final tags = AiSuggestion.fromJson(<String, dynamic>{'id': 's2', 'kind': 'tags', 'from': <String>[], 'to': <String>['tax', 'invoice']});
    expect(tags.toTags, <String>['tax', 'invoice']);
    final counts = AiSuggestionCounts.fromJson(<String, dynamic>{
      'pending': <String, dynamic>{'rename': 2, 'move': 1, 'tags': 0, 'total': 3},
      'high': 1,
      'hiddenLow': 4,
      'proposals': 1,
    });
    expect((counts.total, counts.ofKind('move'), counts.hiddenLow), (3, 1, 4));
    final rule = AiRule.fromJson(<String, dynamic>{
      'id': 4,
      'name': 'Telekom',
      'match': <String, dynamic>{'type': 'invoice', 'issuer': 'Telekom', 'extension': null, 'namePattern': null},
      'targetFolderId': 'f-tel',
      'targetPath': '/Finance/Telekom',
      'enabled': false,
    });
    expect((rule.type, rule.extension, rule.enabled), ('invoice', null, false));
    expect((rule.toJson()['match']! as Map)['issuer'], 'Telekom');
    final jobs = AiJobStatus.fromJson(<String, dynamic>{
      'pending': 0,
      'paused': null,
      'batches': <Map<String, dynamic>>[<String, dynamic>{'id': 'b', 'total': 3, 'pending': 0, 'done': 3}],
    });
    expect(jobs.batches.single.finished, isTrue);
    expect(AdminUserAi.fromJson(<String, dynamic>{'modelIds': <String>['a::b'], 'monthlyTokens': 0}).monthlyTokens, 0);
    expect(AdminUser.fromJson(<String, dynamic>{'id': 1, 'aiMonthlyTokens': null}).aiMonthlyTokens, isNull);
    // Unset switches follow the server's defaults.
    const settings = UserSettings(aiFeatures: <String, bool>{'names': false});
    expect((settings.aiFeatureOn('names'), settings.aiFeatureOn('folders'), settings.aiFeatureOn('newFolders')), (false, true, false));
  });

  testWidgets('Review: accept offers Undo, Undo and Reject reach the server', (tester) async {
    final ai = _FakeAi(pending: <AiSuggestion>[
      _rename('s1', 'f1', 'IMG_2041.pdf', '2026-03-12 Telekom – Invoice.pdf'),
      _rename('s2', 'f2', 'scan0001.pdf', '2026-02-01 Allianz – Policy.pdf'),
    ]);
    final controller = await _controller(ai);
    await _pump(tester, AiReviewPage(controller: controller));
    expect(find.text('Names'), findsOneWidget);
    expect(find.text('2026-03-12 Telekom – Invoice.pdf'), findsOneWidget);
    expect(find.text('Medium'), findsNWidgets(2));
    expect(find.text('AI'), findsNWidgets(2));

    await tester.tap(find.widgetWithText(OutlinedButton, 'Accept').first);
    await tester.pumpAndSettle();
    expect(ai.calls, contains('accept s1'));
    expect(find.text('2026-03-12 Telekom – Invoice.pdf'), findsNothing);
    expect(controller.undo.current!.message, 'Renamed to “2026-03-12 Telekom – Invoice.pdf”');
    await controller.undo.undo();
    expect(ai.calls, contains('undo s1'));

    await tester.tap(find.widgetWithText(TextButton, 'Reject').first);
    await tester.pumpAndSettle();
    expect(ai.calls, contains('reject s2'));
    expect(find.text('Nothing to review'), findsOneWidget);
    await _finish(tester, controller);
  });

  testWidgets('Review: a stale suggestion leaves with a short note', (tester) async {
    final ai = _FakeAi(pending: <AiSuggestion>[_rename('s1', 'f1', 'IMG_2041.pdf', 'Telekom.pdf')])..stale.add('s1');
    final controller = await _controller(ai);
    await _pump(tester, AiReviewPage(controller: controller));
    await tester.tap(find.widgetWithText(OutlinedButton, 'Accept'));
    await tester.pumpAndSettle();
    expect(find.text('Telekom.pdf'), findsNothing);
    expect(find.text('The file changed since this was suggested, so the suggestion was removed.'), findsOneWidget);
    expect(controller.undo.current, isNull);
    await _finish(tester, controller);
  });

  testWidgets('Review: accept all high-confidence after confirming, with Undo for them all', (tester) async {
    final ai = _FakeAi(pending: <AiSuggestion>[
      _rename('s1', 'f1', 'IMG_1.pdf', 'One.pdf', tier: 'high'),
      _rename('s2', 'f2', 'IMG_2.pdf', 'Two.pdf', tier: 'high'),
      _rename('s3', 'f3', 'IMG_3.pdf', 'Three.pdf'),
      _rename('s4', 'f4', 'IMG_4.pdf', 'Four.pdf', tier: 'low'),
    ]);
    final controller = await _controller(ai);
    await _pump(tester, AiReviewPage(controller: controller));
    expect(find.text('Four.pdf'), findsNothing);
    expect(find.text('Show low-confidence (1)'), findsOneWidget);

    await tester.tap(find.text('Accept all high-confidence (2)'));
    await tester.pumpAndSettle();
    await tester.tap(find.descendant(of: find.byType(AlertDialog), matching: find.widgetWithText(FilledButton, 'Accept')));
    await tester.pumpAndSettle();
    expect(ai.calls, contains('accept-high 2'));
    expect(find.text('One.pdf'), findsNothing);
    expect(find.text('Three.pdf'), findsOneWidget);
    expect(controller.undo.current!.message, '2 suggestions accepted');
    await controller.undo.undo();
    expect(ai.calls, containsAll(<String>['undo s1', 'undo s2']));

    await tester.tap(find.text('Show low-confidence (1)'));
    await tester.pumpAndSettle();
    expect(find.text('Four.pdf'), findsOneWidget);
    expect(find.text('Low'), findsOneWidget);
    await _finish(tester, controller);
  });

  testWidgets('Review: a rule proposal leads the page and becomes a rule', (tester) async {
    final ai = _FakeAi(
      pending: <AiSuggestion>[_rename('s1', 'f1', 'IMG_1.pdf', 'One.pdf')],
      proposals: const <AiRuleProposal>[
        AiRuleProposal(id: 5, type: 'invoice', issuer: 'Telekom', targetFolderId: 'f-tel', targetPath: '/Finance/Telekom', accepts: 3),
      ],
    );
    final controller = await _controller(ai);
    await _pump(tester, AiReviewPage(controller: controller));
    expect(find.text('Always move Invoice from Telekom to Home / Finance / Telekom?'), findsOneWidget);
    await tester.tap(find.text('Create rule'));
    await tester.pumpAndSettle();
    expect(ai.calls, contains('rule-from-proposal 5'));
    expect(find.text('Create rule'), findsNothing);
    await _finish(tester, controller);
  });

  group('the file list', () {
    final listing = FolderListing(
      folder: _folder('root', 'Home', parentId: null),
      path: const <PathSegment>[],
      items: <FileNode>[
        _folder('d1', 'Inbox'),
        _folder('v1', 'Vault', vaultId: 'vault-1'),
        _file('f1', 'IMG_2041.pdf'),
      ],
    );

    /// Opens the ⋯ menu of the row showing [name].
    Future<void> openMenu(WidgetTester tester, String name) async {
      final rowTop = tester.getCenter(find.text(name)).dy;
      final menus = find.byIcon(Icons.more_horiz_rounded).evaluate().toList();
      final menu = menus.firstWhere((element) => ((element.renderObject! as RenderBox).localToGlobal(Offset.zero).dy - rowTop).abs() < 30);
      await tester.tap(find.byWidget(menu.widget));
      await tester.pumpAndSettle();
    }

    testWidgets('marks a file with a pending suggestion and decides it inline', (tester) async {
      final ai = _FakeAi(pending: <AiSuggestion>[_rename('s1', 'f1', 'IMG_2041.pdf', '2026-03-12 Telekom – Invoice.pdf')]);
      final controller = await _controller(ai, listing: listing);
      await _pumpApp(tester, controller);
      expect(find.text('Review'), findsOneWidget);
      expect(find.text('AI suggestion'), findsOneWidget);
      await tester.tap(find.text('AI suggestion'));
      await tester.pumpAndSettle();
      expect(find.text('2026-03-12 Telekom – Invoice.pdf'), findsOneWidget);
      await tester.tap(find.widgetWithText(FilledButton, 'Accept'));
      await tester.pumpAndSettle();
      expect(ai.calls, contains('accept s1'));
      expect(find.text('AI suggestion'), findsNothing);
      await _finish(tester, controller);
    });

    testWidgets('menus offer AI on files and folders, never on a vault', (tester) async {
      final controller = await _controller(_FakeAi(), listing: listing);
      await _pumpApp(tester, controller);
      await openMenu(tester, 'IMG_2041.pdf');
      expect(find.text('Suggest a name'), findsOneWidget);
      expect(find.text('Suggest a folder'), findsOneWidget);
      expect(find.text('Summarize'), findsOneWidget);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();

      await openMenu(tester, 'Inbox');
      expect(find.text('Organize this folder…'), findsOneWidget);
      await tester.tapAt(const Offset(5, 5));
      await tester.pumpAndSettle();

      await openMenu(tester, 'Vault');
      expect(find.text('Organize this folder…'), findsNothing);
      expect(find.text('Exclude from AI'), findsNothing);
      await _finish(tester, controller);
    });

    testWidgets('an excluded folder offers no AI for its files', (tester) async {
      final excluded = FolderListing(
        folder: _folder('d1', 'Private', excluded: true),
        path: const <PathSegment>[PathSegment(id: 'd1', name: 'Private')],
        items: <FileNode>[_file('f1', 'IMG_2041.pdf', parentId: 'd1')],
      );
      final controller = await _controller(_FakeAi(), listing: excluded);
      await _pumpApp(tester, controller);
      await openMenu(tester, 'IMG_2041.pdf');
      expect(find.text('Rename'), findsOneWidget);
      expect(find.text('Suggest a name'), findsNothing);
      await _finish(tester, controller);
    });

    testWidgets('with AI off nothing of it shows', (tester) async {
      final ai = _FakeAi(optedIn: false, pending: <AiSuggestion>[_rename('s1', 'f1', 'IMG_2041.pdf', 'Telekom.pdf')]);
      final controller = await _controller(ai, listing: listing);
      expect(controller.aiReview.enabled, isFalse);
      await _pumpApp(tester, controller);
      // Review stays: its Duplicates tab works without AI.
      expect(find.text('AI suggestion'), findsNothing);
      await openMenu(tester, 'IMG_2041.pdf');
      expect(find.text('Rename'), findsOneWidget);
      expect(find.text('Suggest a name'), findsNothing);
      expect(find.text('Summarize'), findsNothing);
      await _finish(tester, controller);
    });
  });

  testWidgets('Organize this folder starts a backfill and follows it to Review', (tester) async {
    final ai = _FakeAi();
    final controller = await _controller(ai);
    await _pump(tester, AiOrganizeSheet(controller: controller, folder: _folder('d1', 'Downloads')));
    expect(find.text('Organize “Downloads”'), findsOneWidget);
    await tester.tap(find.text('Tags'));
    await tester.pump();
    await tester.tap(find.text('Start'));
    await tester.pumpAndSettle();
    expect(ai.calls, contains('jobs d1 [names, folders]'));
    expect(find.text('Done'), findsOneWidget);
    expect(find.text('12 of 12 files'), findsOneWidget);
    expect(find.text('Open Review'), findsOneWidget);
    await _finish(tester, controller);
  });

  testWidgets('the summary section summarizes, names the model and refreshes', (tester) async {
    final ai = _FakeAi();
    final controller = await _controller(ai);
    await _pump(tester, SizedBox(width: 360, child: AiSummarySection(controller: controller, node: _file('f1', 'Invoice.pdf'))));
    expect(find.text('Summarize'), findsOneWidget);
    await tester.tap(find.text('Summarize'));
    await tester.pumpAndSettle();
    expect(find.text('Telekom bills 39.95 EUR for March.'), findsOneWidget);
    expect(find.text('Written by gpt-4o'), findsOneWidget);
    await tester.tap(find.byTooltip('Summarize again'));
    await tester.pumpAndSettle();
    expect(ai.calls, <String>['summarize f1', 'summarize f1 refresh']);
    expect(find.text('A fresher summary.'), findsOneWidget);
    // Kept for the file's content: opening it again asks nothing.
    await _pump(tester, AiSummarySection(controller: controller, node: _file('f1', 'Invoice.pdf')));
    expect(find.text('A fresher summary.'), findsOneWidget);
    expect(ai.calls.length, 2);
  });

  testWidgets('the rules card lists, switches and deletes rules', (tester) async {
    final ai = _FakeAi(rules: <AiRule>[
      const AiRule(id: 4, name: 'Telekom bills', type: 'invoice', issuer: 'Telekom', extension: 'pdf', targetFolderId: 'f-tel', targetPath: '/Finance/Telekom'),
    ]);
    final controller = await _controller(ai);
    await _pump(tester, SingleChildScrollView(child: AiRulesCard(controller: controller)));
    expect(find.text('Sorting rules'), findsOneWidget);
    expect(find.text('Telekom bills'), findsOneWidget);
    expect(find.text('Invoice · Telekom · .pdf → Home / Finance / Telekom'), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(ai.calls.single, startsWith('update-rule 4 {enabled: false}'));
    await tester.tap(find.byTooltip('Delete'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Delete'));
    await tester.pumpAndSettle();
    expect(ai.calls, contains('delete-rule 4'));
    expect(find.text('Telekom bills'), findsNothing);
  });

  testWidgets('Settings › AI has the new switches, Review and the rules', (tester) async {
    final controller = await _controller(_FakeAi());
    await _pump(tester, SingleChildScrollView(child: AiSettingsPage(controller: controller)), size: const Size(1440, 3200));
    expect(find.text('Suggest new tags'), findsOneWidget);
    expect(find.text('Suggest new folders'), findsOneWidget);
    expect(find.text('Open Review'), findsOneWidget);
    expect(find.text('Sorting rules'), findsOneWidget);
  });
}
