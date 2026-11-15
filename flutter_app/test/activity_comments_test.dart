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
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/fake_http.dart';

// The activity feed and the comments of one file, against a fake server.

final List<Map<String, dynamic>> _comments = <Map<String, dynamic>>[
  <String, dynamic>{'id': 2, 'author': 'Ben', 'mine': false, 'body': 'Looks <b>good</b>', 'createdAt': '2026-05-01T10:00:00Z', 'editedAt': null, 'canDelete': true},
  <String, dynamic>{'id': 1, 'author': 'ada', 'mine': true, 'body': 'First note', 'createdAt': '2026-05-01T09:00:00Z', 'editedAt': '2026-05-01T09:30:00Z', 'canDelete': true},
];

FakeReply _server(FakeCall call) {
  switch ('${call.method} ${call.path}') {
    case 'GET /api/files/f1/comments':
      return FakeReply(200, <String, dynamic>{'comments': _comments, 'nextCursor': null});
    case 'POST /api/files/f1/comments':
      return FakeReply(201, <String, dynamic>{'id': 3, 'author': 'ada', 'mine': true, 'body': call.body['body'], 'createdAt': '2026-05-01T11:00:00Z', 'editedAt': null, 'canDelete': true});
    case 'GET /api/activity':
      return const FakeReply(200, <String, dynamic>{
        'items': <Map<String, dynamic>>[
          <String, dynamic>{
            'id': 9, 'kind': 'upload', 'at': '2026-05-01T10:00:00Z', 'count': 3,
            'actor': <String, dynamic>{'name': 'Anna', 'self': false}, 'owner': <String, dynamic>{'name': 'ada', 'self': true},
            'node': <String, dynamic>{'id': 'n', 'name': 'c.txt', 'type': 'file'}, 'folder': 'Holiday', 'names': <String>['a.txt', 'b.txt', 'c.txt'],
          },
          <String, dynamic>{
            'id': 8, 'kind': 'rename', 'at': '2026-05-01T09:00:00Z', 'count': 1,
            'actor': <String, dynamic>{'name': 'ada', 'self': true}, 'owner': <String, dynamic>{'name': 'ada', 'self': true},
            'node': <String, dynamic>{'id': 'm', 'name': 'Contract final.pdf', 'type': 'file'}, 'from': 'Contract.pdf',
          },
        ],
        'nextCursor': null,
      });
    default:
      return const FakeReply(404, <String, dynamic>{'error': 'Not found'});
  }
}

Future<(NeoCloudController, FakeHttp)> _controller() async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final http = FakeHttp(_server);
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1', http: http),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
  );
  controller.isBooting = false;
  return (controller, http);
}

Future<void> _pump(WidgetTester tester, double width, Widget child) async {
  tester.view.physicalSize = Size(width, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: SingleChildScrollView(child: child))));
  for (var i = 0; i < 5; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await initializeDateFormatting('en');
  });

  test('feed items and comments parse the server shape', () {
    final item = ActivityItem.fromJson(<String, dynamic>{
      'id': 1, 'kind': 'share', 'at': '2026-05-01T10:00:00Z', 'count': 1,
      'actor': <String, dynamic>{'name': 'Anna', 'self': false}, 'owner': <String, dynamic>{'name': 'Anna', 'self': false},
      'node': <String, dynamic>{'id': 'n', 'name': 'Team', 'type': 'folder'}, 'member': <String, dynamic>{'name': 'ada', 'self': true},
    });
    expect(item.nodeIsFolder, isTrue);
    expect(item.member!.self, isTrue);
    expect(item.folder, isNull);
    expect(ActivityPage.fromJson(const <String, dynamic>{'items': <dynamic>[], 'nextCursor': 40}).nextCursor, 40);
    expect(CommentPage.fromJson(const <String, dynamic>{'comments': <dynamic>[]}).nextCursor, isNull);
  });

  for (final width in <double>[390, 1200]) {
    testWidgets('comments list, show text as written and post at $width px', (tester) async {
      final (controller, http) = await _controller();
      final node = FileNode.fromJson(const <String, dynamic>{'id': 'f1', 'type': 'file', 'name': 'plan.txt'});
      await _pump(tester, width, CommentsSection(controller: controller, node: node));
      expect(find.text('Looks <b>good</b>'), findsOneWidget);
      expect(find.text('First note'), findsOneWidget);
      expect(find.textContaining('edited'), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'A new thought');
      await tester.pump();
      await tester.tap(find.byIcon(Icons.send_rounded));
      for (var i = 0; i < 3; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(http.where('POST', '/api/files/f1/comments').single.body['body'], 'A new thought');
      expect(tester.takeException(), isNull);
    });

    testWidgets('the feed words each entry, for you and for others, at $width px', (tester) async {
      final (controller, _) = await _controller();
      await _pump(tester, width, ActivityFeed(controller: controller));
      expect(find.text('Anna uploaded 3 files'), findsOneWidget);
      expect(find.text('a.txt, b.txt, c.txt'), findsOneWidget);
      expect(find.text('You renamed Contract.pdf to Contract final.pdf'), findsOneWidget);
      expect(find.textContaining('in Holiday'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });
  }
}
