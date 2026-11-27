import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neolabs_flutter/neolabs_flutter.dart' show NeoUpdateSettings;
import 'package:shared_preferences/shared_preferences.dart';

import 'helpers/fake_http.dart';

Map<String, dynamic> _status({
  bool available = true,
  bool selfUpdate = true,
  String channel = 'beta',
}) => <String, dynamic>{
  'version': '1.0.0',
  'channel': channel,
  'channelLabel': channel,
  'selfUpdate': selfUpdate,
  'blockedReason': selfUpdate ? null : 'container',
  'checkedAt': '2026-01-01T10:00:00Z',
  'checkFailed': false,
  'available': available
      ? <String, dynamic>{
          'version': '1.1.0',
          'branch': 'beta',
          'changes': <String>['Faster uploads'],
        }
      : null,
  'job': <String, dynamic>{'state': 'idle'},
};

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<FakeHttp> _open(WidgetTester tester, FakeHandler handler) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final http = FakeHttp(handler);
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1', http: http),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
  )..isBooting = false;
  tester.view.physicalSize = const Size(1440, 3000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: ListenableBuilder(
          listenable: controller,
          builder: (context, _) => AdminPanel(controller: controller),
        ),
      ),
    ),
  );
  await _settle(tester);
  return http;
}

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await initializeDateFormatting();
  });

  testWidgets('a waiting version is offered and starts once confirmed', (
    tester,
  ) async {
    final http = await _open(tester, (call) {
      if (call.path == '/api/admin/update') {
        return FakeReply(call.method == 'POST' ? 202 : 200, _status());
      }
      return const FakeReply(404);
    });
    expect(find.text('Version 1.1.0 is ready'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, 'Update now'));
    await _settle(tester);
    expect(find.text('Update to 1.1.0?'), findsOneWidget);
    expect(http.where('POST', '/api/admin/update'), isEmpty);
    await tester.tap(find.widgetWithText(FilledButton, 'Update now').last);
    await _settle(tester);
    expect(http.where('POST', '/api/admin/update').length, 1);
  });

  testWidgets('the server tab changes the channel and shows refusals', (
    tester,
  ) async {
    final http = await _open(tester, (call) {
      if (call.path == '/api/admin/update') {
        return FakeReply(200, _status(available: false));
      }
      if (call.path == '/api/admin/update/channel') {
        return const FakeReply(409, <String, dynamic>{
          'error': 'An update is already running.',
          'code': 'UPDATE_RUNNING',
        });
      }
      if (call.path == '/api/admin/health') {
        return const FakeReply(200, <String, dynamic>{
          'version': '1.0.0',
          'runtime': 'Go',
          'database': 'ok',
        });
      }
      if (call.path == '/api/admin/logs') {
        return const FakeReply(200, <String, dynamic>{'logs': <Object>[]});
      }
      return const FakeReply(404);
    });
    await tester.tap(find.text('Server').first);
    await _settle(tester);
    expect(find.byType(NeoUpdateSettings), findsOneWidget);
    await tester.tap(find.text('Stable'));
    await _settle(tester);
    expect(
      http.where('PUT', '/api/admin/update/channel').single.body,
      <String, dynamic>{'channel': 'stable'},
    );
    expect(find.text('An update is already running.'), findsOneWidget);
  });

  testWidgets('a container install says how to update it', (tester) async {
    await _open(tester, (call) {
      if (call.path == '/api/admin/update') {
        return FakeReply(200, _status(selfUpdate: false));
      }
      return const FakeReply(404);
    });
    expect(find.widgetWithText(FilledButton, 'Update now'), findsNothing);
  });
}
