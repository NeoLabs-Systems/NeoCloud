
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/signing/fake_signing_account_api.dart';
import 'package:neocloud/src/signing/fake_signing_api.dart';
import 'package:neocloud/src/signing/signing.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A signed-in controller whose signing calls go to [api].
Future<NeoCloudController> signingController(FakeSigningApi api, {FakeSigningAccountApi? account, bool emailConfigured = false}) async {
  SharedPreferences.setMockInitialValues(<String, Object>{});
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    signing: api,
    signingAccount: account,
  );
  controller.emailConfigured = emailConfigured;
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

/// Pages as plain white sheets: PDFium isn't available in tests.
Widget fakePages(BuildContext context, Uint8List pdf, SigningPageController controller, PageOverlayBuilder overlay) => SigningPageList(
  pageSizes: const <Size>[Size(595, 842), Size(595, 842)],
  controller: controller,
  overlayBuilder: overlay,
  pageContent: (context, index, size) => ColoredBox(color: Colors.white, child: Center(child: Text('Page ${index + 1}'))),
);

void useScreen(WidgetTester tester, Size size, {double ratio = 1}) {
  tester.view.physicalSize = size * ratio;
  tester.view.devicePixelRatio = ratio;
  addTearDown(tester.view.reset);
}

Widget app(Widget child) => MaterialApp(
  debugShowCheckedModeBanner: false,
  theme: neoCloudTheme(Brightness.light),
  darkTheme: neoCloudTheme(Brightness.dark),
  home: Scaffold(body: child),
);

/// Date formats need their locale data once per test file.
Future<void> initSigningTests() async {
  await initializeDateFormatting('en');
  await initializeDateFormatting('de');
}

/// A clipboard that remembers what was copied; read it from [clipboardText].
String? clipboardText;

void mockClipboard() {
  clipboardText = null;
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
    if (call.method == 'Clipboard.setData') clipboardText = (call.arguments as Map)['text'] as String?;
    if (call.method == 'Clipboard.getData') return <String, Object?>{'text': clipboardText};
    return null;
  });
}
