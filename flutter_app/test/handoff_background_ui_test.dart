import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/handoff/background/handoff_background.dart';
import 'package:neocloud/src/mobile/background_reliability.dart';
import 'package:neocloud/src/mobile/camera_upload.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'handoff_background_test.dart' show FakePlatform;
import 'helpers/fake_http.dart';

/// A phone: Android with the camera library but no notification permission
/// and no battery exemption yet.
class _Phone extends NoCameraUploads {
  _Phone({this.notifications = false, this.battery = false});

  bool notifications;
  bool battery;

  @override
  bool get supported => true;

  @override
  Future<BackgroundReliability> backgroundReliability() async =>
      BackgroundReliability.evaluate(
        status: BackgroundPlatformStatus.fromMap(<String, Object?>{
          'platform': 'android',
          'manufacturer': 'Google',
          'brand': 'google',
          'ignoringBatteryOptimizations': battery,
          'notificationsEnabled': notifications,
        }),
        photos: CameraAccess.granted,
      );
}

Future<(NeoCloudController, FakePlatform)> _controller({
  _Phone? phone,
  Map<String, Object> values = const <String, Object>{},
  bool supported = true,
}) async {
  SharedPreferences.setMockInitialValues(values);
  final platform = FakePlatform(supported: supported);
  final client = BackendClient(
    baseUrl: 'https://cloud.example',
    http: FakeHttp(
      (call) => call.path == '/api/account/devices'
          ? const FakeReply(200, <String, dynamic>{'devices': <Object>[]})
          : const FakeReply(404),
    ),
  )..bearerToken = 'nc_token';
  final controller = NeoCloudController(
    client: client,
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: phone ?? _Phone(),
    shareIntake: NoShareIntake(),
    handoffBackgroundPlatform: platform,
  );
  controller
    ..isBooting = false
    ..user = AccountUser(
      id: 7,
      username: 'ada',
      displayName: null,
      email: null,
      isAdmin: false,
      storage: const StorageUsage(
        usedBytes: 0,
        quotaBytes: -1,
        trashBytes: 0,
        versionBytes: 0,
      ),
    );
  return (controller, platform);
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _devicesPage(
  WidgetTester tester,
  NeoCloudController controller,
) async {
  tester.view.physicalSize = const Size(900, 3000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  controller.setSettingsPage(SettingsPage.devices);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: SettingsPanel(controller: controller)),
    ),
  );
  await _settle(tester);
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  group('Settings › Devices', () {
    testWidgets(
      'offers "Receive files in the background" only where the phone can',
      (tester) async {
        final (controller, _) = await _controller(supported: false);
        await _devicesPage(tester, controller);
        expect(
          find.text('Let my other devices send files here'),
          findsOneWidget,
        );
        expect(find.text('Receive files in the background'), findsNothing);
      },
    );

    testWidgets(
      'turning it on configures the receiver and opens the permission sheet for what is missing',
      (tester) async {
        final (controller, platform) = await _controller();
        await _devicesPage(tester, controller);
        expect(find.text('Receive files in the background'), findsOneWidget);
        await tester.tap(find.text('Receive files in the background'));
        await _settle(tester);
        expect(controller.handoff.background.enabled, isTrue);
        expect(platform.calls, contains('configure'));
        // The sheet is about receiving only: no photo access, but notifications and battery.
        expect(find.text('Keep receiving files'), findsOneWidget);
        expect(find.text('Notifications'), findsOneWidget);
        expect(find.text('Unrestricted battery'), findsOneWidget);
        expect(find.text('Photos and videos'), findsNothing);
        expect(find.text('Allow all'), findsOneWidget);
        await tester.tap(find.text('Not now'));
        await _settle(tester);
        // Still on: the person may allow the rest later; the banner reminds them.
        expect(controller.handoff.background.enabled, isTrue);
        expect(
          find.text('Files may not arrive in the background'),
          findsOneWidget,
        );
      },
    );

    testWidgets('with everything granted turning it on shows no sheet', (
      tester,
    ) async {
      final (controller, platform) = await _controller(
        phone: _Phone(notifications: true, battery: true),
      );
      await _devicesPage(tester, controller);
      await tester.tap(find.text('Receive files in the background'));
      await _settle(tester);
      expect(platform.calls, contains('configure'));
      expect(find.text('Keep receiving files'), findsNothing);
      expect(find.text('Files may not arrive in the background'), findsNothing);
    });

    testWidgets('turning it off stops the receiver', (tester) async {
      final (controller, platform) = await _controller(
        phone: _Phone(notifications: true, battery: true),
        values: <String, Object>{HandoffBackground.enabledKey: true},
      );
      await _devicesPage(tester, controller);
      await tester.tap(find.text('Receive files in the background'));
      await _settle(tester);
      expect(controller.handoff.background.enabled, isFalse);
      expect(platform.calls.last, 'disable');
    });

    testWidgets('it needs "Let my other devices send files here" first', (
      tester,
    ) async {
      final (controller, _) = await _controller(
        values: <String, Object>{'handoff_receiving': false},
      );
      await _devicesPage(tester, controller);
      expect(
        find.text('Turn on “Let my other devices send files here” first.'),
        findsOneWidget,
      );
      final tile = tester.widget<SwitchListTile>(
        find.widgetWithText(SwitchListTile, 'Receive files in the background'),
      );
      expect(tile.onChanged, isNull);
    });
  });

  group('the permission sheet', () {
    Future<void> open(
      WidgetTester tester,
      NeoCloudController controller,
      BackgroundFeatures features,
    ) async {
      tester.view.physicalSize = const Size(900, 3000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      late BuildContext context;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Builder(
              builder: (inner) {
                context = inner;
                return const SizedBox();
              },
            ),
          ),
        ),
      );
      showBackgroundReliabilitySheet(
        context,
        controller.cameraUploads,
        features: features,
        handoff: controller.handoff.background,
      );
      await _settle(tester);
    }

    testWidgets('camera upload alone keeps its own title and items', (
      tester,
    ) async {
      final (controller, _) = await _controller();
      await open(tester, controller, const BackgroundFeatures());
      expect(find.text('Keep backup running'), findsOneWidget);
      expect(find.text('Photos and videos'), findsOneWidget);
      expect(find.text('Receive files in the background'), findsNothing);
    });

    testWidgets('camera upload and receiving together share one sheet', (
      tester,
    ) async {
      final (controller, _) = await _controller();
      await open(tester, controller, const BackgroundFeatures(receive: true));
      expect(find.text('Keep NeoCloud running'), findsOneWidget);
      expect(find.text('Photos and videos'), findsOneWidget);
      expect(find.text('Receive files in the background'), findsOneWidget);
      // One battery item for both, not one each.
      expect(find.text('Unrestricted battery'), findsOneWidget);
    });

    testWidgets('its own item turns receiving on', (tester) async {
      final (controller, platform) = await _controller();
      await open(
        tester,
        controller,
        const BackgroundFeatures(camera: false, receive: true),
      );
      expect(controller.handoff.background.enabled, isFalse);
      await tester.tap(find.text('Turn on'));
      await _settle(tester);
      expect(controller.handoff.background.enabled, isTrue);
      expect(platform.calls, contains('configure'));
    });
  });
}
