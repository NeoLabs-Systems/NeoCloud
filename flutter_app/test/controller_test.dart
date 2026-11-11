import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:shared_preferences/shared_preferences.dart';

Future<NeoCloudController> _controller(Uri launchUri) async {
  SharedPreferences.setMockInitialValues(<String, Object>{});
  return NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    launchUri: launchUri,
  );
}

void main() {
  test('the device approval link is picked up from the launch URL', () async {
    final controller = await _controller(Uri.parse('https://cloud.example/app/device?code=abcd-efgh'));
    expect(controller.pendingDeviceCode, 'ABCD-EFGH');
    expect(controller.usesDeviceLogin, isFalse);
  });

  test('other URLs open the app normally', () async {
    final controller = await _controller(Uri.parse('https://cloud.example/app/'));
    expect(controller.pendingDeviceCode, isNull);
  });

  test('a device link without a code still opens the approval page', () async {
    final controller = await _controller(Uri.parse('https://cloud.example/app/device'));
    expect(controller.pendingDeviceCode, '');
  });

  test('the pairing link opened in a browser keeps its code for the landing page', () async {
    final controller = await _controller(Uri.parse('https://cloud.example/app/pair?code=abcDEF_123-xyz'));
    expect(controller.pendingPairCode, 'abcDEF_123-xyz');
    expect(controller.pendingDeviceCode, isNull);
  });
}
