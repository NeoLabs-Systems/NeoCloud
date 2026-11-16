import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/mobile/background_reliability.dart';
import 'package:neocloud/src/mobile/camera_upload.dart';

BackgroundPlatformStatus _android({
  bool battery = true,
  bool notifications = true,
  String manufacturer = 'Google',
}) => BackgroundPlatformStatus.fromMap(<String, Object?>{
  'platform': 'android',
  'manufacturer': manufacturer,
  'brand': manufacturer.toLowerCase(),
  'ignoringBatteryOptimizations': battery,
  'notificationsEnabled': notifications,
  'backgroundDataRestricted': false,
  'manufacturerSettingsAvailable': false,
});

BackgroundReliability _camera(BackgroundPlatformStatus status) =>
    BackgroundReliability.evaluate(
      status: status,
      photos: CameraAccess.granted,
    );

List<BackgroundCheck> _checks(BackgroundReliability reliability) =>
    <BackgroundCheck>[for (final item in reliability.items) item.check];

void main() {
  group('the checklist for the features that need it', () {
    test('camera upload alone is the checklist it always was', () {
      final base = _camera(_android());
      final same = base.forFeatures(const BackgroundFeatures());
      expect(_checks(same), _checks(base));
      expect(same.stateOf(BackgroundCheck.receiveFiles), isNull);
    });

    test('receiving alone has no photo access and starts with the switch', () {
      final reliability = _camera(
        _android(),
      ).forFeatures(const BackgroundFeatures(camera: false, receive: true));
      expect(_checks(reliability), <BackgroundCheck>[
        BackgroundCheck.receiveFiles,
        BackgroundCheck.battery,
        BackgroundCheck.notifications,
        BackgroundCheck.backgroundData,
      ]);
      expect(
        reliability.stateOf(BackgroundCheck.receiveFiles),
        BackgroundCheckState.needed,
      );
      expect(reliability.anyNeeded, isTrue);
    });

    test('the switch counts as granted once receiving is on', () {
      final reliability = _camera(_android()).forFeatures(
        const BackgroundFeatures(camera: false, receive: true, receiveOn: true),
      );
      expect(
        reliability.stateOf(BackgroundCheck.receiveFiles),
        BackgroundCheckState.granted,
      );
      expect(reliability.openCount, 0);
    });

    test(
      'both together share the battery and notification items instead of repeating them',
      () {
        final reliability = _camera(
          _android(),
        ).forFeatures(const BackgroundFeatures(receive: true, receiveOn: true));
        final checks = _checks(reliability);
        expect(checks.first, BackgroundCheck.receiveFiles);
        expect(
          checks.where((check) => check == BackgroundCheck.battery),
          hasLength(1),
        );
        expect(
          checks.where((check) => check == BackgroundCheck.notifications),
          hasLength(1),
        );
        expect(checks, contains(BackgroundCheck.photos));
      },
    );

    test(
      'without notifications there is no popup, so they are needed, not just recommended',
      () {
        final status = _android(notifications: false);
        expect(
          _camera(status).stateOf(BackgroundCheck.notifications),
          BackgroundCheckState.recommended,
        );
        final receiving = _camera(status).forFeatures(
          const BackgroundFeatures(
            camera: false,
            receive: true,
            receiveOn: true,
          ),
        );
        expect(
          receiving.stateOf(BackgroundCheck.notifications),
          BackgroundCheckState.needed,
        );
        expect(receiving.anyNeeded, isTrue);
      },
    );

    test(
      'the battery exemption is the one the camera checklist already asks for',
      () {
        final reliability = _camera(_android(battery: false)).forFeatures(
          const BackgroundFeatures(
            camera: false,
            receive: true,
            receiveOn: true,
          ),
        );
        expect(
          reliability.stateOf(BackgroundCheck.battery),
          BackgroundCheckState.needed,
        );
      },
    );

    test('the maker\'s screens stay for phones that need them', () {
      final reliability = _camera(
        _android(manufacturer: 'Xiaomi'),
      ).forFeatures(const BackgroundFeatures(camera: false, receive: true));
      expect(_checks(reliability), contains(BackgroundCheck.manufacturer));
    });

    test(
      'iOS never gets the receiving items: it cannot hold a connection for a closed app',
      () {
        final ios = BackgroundPlatformStatus.fromMap(<String, Object?>{
          'platform': 'ios',
          'notifications': 'granted',
          'backgroundRefresh': 'available',
          'lowPowerMode': false,
        });
        final reliability = _camera(
          ios,
        ).forFeatures(const BackgroundFeatures(receive: true));
        expect(_checks(reliability), _checks(_camera(ios)));
      },
    );
  });
}
