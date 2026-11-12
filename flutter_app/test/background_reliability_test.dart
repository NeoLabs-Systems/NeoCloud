import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/src/mobile/background_reliability.dart';
import 'package:neocloud/src/mobile/camera_upload.dart';

Map<String, Object?> _android({
  String manufacturer = 'Google',
  String brand = 'google',
  bool battery = true,
  bool notifications = true,
  bool dataRestricted = false,
}) => <String, Object?>{
  'platform': 'android',
  'sdkInt': 35,
  'manufacturer': manufacturer,
  'brand': brand,
  'ignoringBatteryOptimizations': battery,
  'notificationsEnabled': notifications,
  'backgroundDataRestricted': dataRestricted,
  'manufacturerSettingsAvailable': false,
};

Map<String, Object?> _ios({String notifications = 'granted', String refresh = 'available', bool lowPower = false}) =>
    <String, Object?>{'platform': 'ios', 'notifications': notifications, 'backgroundRefresh': refresh, 'lowPowerMode': lowPower};

BackgroundReliability _evaluate(Map<String, Object?> raw, {CameraAccess photos = CameraAccess.granted}) =>
    BackgroundReliability.evaluate(status: BackgroundPlatformStatus.fromMap(raw), photos: photos);

void main() {
  group('manufacturer', () {
    test('maps Android makers and their sub-brands to one set of instructions', () {
      expect(backgroundVendorFor('samsung', 'samsung'), BackgroundVendor.samsung);
      expect(backgroundVendorFor('Xiaomi', 'Redmi'), BackgroundVendor.xiaomi);
      expect(backgroundVendorFor('Xiaomi', 'POCO'), BackgroundVendor.xiaomi);
      expect(backgroundVendorFor('HUAWEI', 'HUAWEI'), BackgroundVendor.huawei);
      expect(backgroundVendorFor('HONOR', 'HONOR'), BackgroundVendor.huawei);
      expect(backgroundVendorFor('OnePlus', 'OnePlus'), BackgroundVendor.oppo);
      expect(backgroundVendorFor('OPPO', 'OPPO'), BackgroundVendor.oppo);
      expect(backgroundVendorFor('realme', 'realme'), BackgroundVendor.oppo);
      expect(backgroundVendorFor('vivo', 'iQOO'), BackgroundVendor.vivo);
      expect(backgroundVendorFor('asus', 'asus'), BackgroundVendor.asus);
      expect(backgroundVendorFor('Google', 'google'), BackgroundVendor.other);
      expect(backgroundVendorFor('motorola', 'motorola'), BackgroundVendor.other);
      expect(backgroundVendorFor(''), BackgroundVendor.other);
    });

    test('every maker with its own task killer has steps; stock Android has none', () {
      for (final vendor in BackgroundVendor.values) {
        final steps = backgroundVendorSteps(vendor);
        if (vendor == BackgroundVendor.other) {
          expect(steps, isEmpty);
        } else {
          expect(steps, isNotEmpty, reason: vendor.name);
        }
      }
      expect(backgroundVendorSteps(BackgroundVendor.xiaomi), <BackgroundVendorStep>[
        BackgroundVendorStep.xiaomiAutostart,
        BackgroundVendorStep.xiaomiNoRestrictions,
        BackgroundVendorStep.lockInRecents,
      ]);
      expect(backgroundVendorSteps(BackgroundVendor.samsung), contains(BackgroundVendorStep.samsungNeverSleeping));
      expect(backgroundVendorSteps(BackgroundVendor.huawei), contains(BackgroundVendorStep.huaweiAppLaunch));
    });

    test('links to the maker’s page on dontkillmyapp.com', () {
      expect(backgroundVendorGuide(BackgroundVendor.samsung).toString(), 'https://dontkillmyapp.com/samsung');
      expect(backgroundVendorGuide(BackgroundVendor.oppo, 'OnePlus').toString(), 'https://dontkillmyapp.com/oneplus');
      expect(backgroundVendorGuide(BackgroundVendor.oppo, 'realme').toString(), 'https://dontkillmyapp.com/realme');
      expect(backgroundVendorGuide(BackgroundVendor.oppo, 'OPPO').toString(), 'https://dontkillmyapp.com/oppo');
      expect(backgroundVendorGuide(BackgroundVendor.other).toString(), 'https://dontkillmyapp.com/');
    });
  });

  group('Android checklist', () {
    test('everything granted on stock Android: no manufacturer row, nothing open', () {
      final reliability = _evaluate(_android());
      expect(reliability.items.map((item) => item.check), <BackgroundCheck>[
        BackgroundCheck.photos,
        BackgroundCheck.battery,
        BackgroundCheck.notifications,
        BackgroundCheck.backgroundData,
      ]);
      expect(reliability.openCount, 0);
      expect(reliability.anyNeeded, isFalse);
    });

    test('battery optimisation is needed; notifications and Data Saver are recommended', () {
      final reliability = _evaluate(_android(battery: false, notifications: false, dataRestricted: true));
      expect(reliability.stateOf(BackgroundCheck.battery), BackgroundCheckState.needed);
      expect(reliability.stateOf(BackgroundCheck.notifications), BackgroundCheckState.recommended);
      expect(reliability.stateOf(BackgroundCheck.backgroundData), BackgroundCheckState.recommended);
      expect(reliability.anyNeeded, isTrue);
      expect(reliability.openCount, 3);
    });

    test('a maker with its own task killer adds a row that stays recommended', () {
      final reliability = _evaluate(_android(manufacturer: 'Xiaomi', brand: 'Redmi'));
      expect(reliability.status.vendor, BackgroundVendor.xiaomi);
      expect(reliability.stateOf(BackgroundCheck.manufacturer), BackgroundCheckState.recommended);
      expect(reliability.anyNeeded, isFalse);
      expect(reliability.openCount, 1);
    });

    test('photos: denied is needed, a selection is recommended, full access is granted', () {
      expect(_evaluate(_android(), photos: CameraAccess.denied).stateOf(BackgroundCheck.photos), BackgroundCheckState.needed);
      expect(_evaluate(_android(), photos: CameraAccess.limited).stateOf(BackgroundCheck.photos), BackgroundCheckState.recommended);
      expect(_evaluate(_android()).stateOf(BackgroundCheck.photos), BackgroundCheckState.granted);
    });
  });

  group('iOS checklist', () {
    test('lists photos, Background App Refresh, Low Power Mode and notifications', () {
      final reliability = _evaluate(_ios());
      expect(reliability.items.map((item) => item.check), <BackgroundCheck>[
        BackgroundCheck.photos,
        BackgroundCheck.backgroundRefresh,
        BackgroundCheck.lowPowerMode,
        BackgroundCheck.notifications,
      ]);
      expect(reliability.openCount, 0);
      expect(reliability.status.vendor, BackgroundVendor.other);
    });

    test('Background App Refresh off is needed; restricted by Screen Time can’t be fixed from the app', () {
      final denied = _evaluate(_ios(refresh: 'denied'));
      expect(denied.stateOf(BackgroundCheck.backgroundRefresh), BackgroundCheckState.needed);
      expect(denied.items.firstWhere((item) => item.check == BackgroundCheck.backgroundRefresh).fixable, isTrue);
      final restricted = _evaluate(_ios(refresh: 'restricted'));
      expect(restricted.stateOf(BackgroundCheck.backgroundRefresh), BackgroundCheckState.needed);
      expect(restricted.items.firstWhere((item) => item.check == BackgroundCheck.backgroundRefresh).fixable, isFalse);
    });

    test('Low Power Mode is a recommendation without a button; undecided notifications can still be asked', () {
      final reliability = _evaluate(_ios(lowPower: true, notifications: 'notDetermined'));
      final lowPower = reliability.items.firstWhere((item) => item.check == BackgroundCheck.lowPowerMode);
      expect(lowPower.state, BackgroundCheckState.recommended);
      expect(lowPower.fixable, isFalse);
      expect(reliability.stateOf(BackgroundCheck.notifications), BackgroundCheckState.recommended);
      expect(reliability.status.notificationsUndecided, isTrue);
      expect(reliability.anyNeeded, isFalse);
    });

    test('an empty status (web, or the plugin missing) reads as iOS with nothing granted beyond refresh', () {
      final status = BackgroundPlatformStatus.fromMap(const <String, Object?>{});
      expect(status.android, isFalse);
      expect(status.notificationsGranted, isFalse);
      expect(status.backgroundRefresh, BackgroundRefreshStatus.available);
    });
  });

  group('banner', () {
    final now = DateTime(2026, 11, 11, 12);
    final fine = _evaluate(_android());
    final missing = _evaluate(_android(battery: false));

    test('only while camera upload is on', () {
      expect(backgroundBannerDue(enabled: false, reliability: missing, lastSuccess: null, enabledAt: null, now: now), isFalse);
    });

    test('when something needed is missing', () {
      expect(backgroundBannerDue(enabled: true, reliability: missing, lastSuccess: now, enabledAt: now, now: now), isTrue);
    });

    test('when the last successful pass is more than a day old', () {
      final recent = now.subtract(const Duration(hours: 23));
      final stale = now.subtract(const Duration(hours: 25));
      expect(backgroundBannerDue(enabled: true, reliability: fine, lastSuccess: recent, enabledAt: null, now: now), isFalse);
      expect(backgroundBannerDue(enabled: true, reliability: fine, lastSuccess: stale, enabledAt: null, now: now), isTrue);
    });

    test('before the first success, counts from turning it on', () {
      final stale = now.subtract(const Duration(days: 2));
      expect(backgroundBannerDue(enabled: true, reliability: fine, lastSuccess: null, enabledAt: now, now: now), isFalse);
      expect(backgroundBannerDue(enabled: true, reliability: fine, lastSuccess: null, enabledAt: stale, now: now), isTrue);
      expect(backgroundBannerDue(enabled: true, reliability: null, lastSuccess: null, enabledAt: null, now: now), isFalse);
    });
  });
}
