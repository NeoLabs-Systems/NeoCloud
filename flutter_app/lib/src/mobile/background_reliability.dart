import 'camera_upload.dart';

// What decides whether camera upload keeps running in the background, as a
// checklist: each item is granted, needed or recommended, from what the
// platform reports (neocloud_background's `status`) and the photo access.
// Plain data so it can be tested; the screen is main_background_permissions.dart.

/// One thing on the checklist.
enum BackgroundCheck {
  /// Full access to photos and videos, not a selection.
  photos,

  /// The progress notification (Android) or backup notices (iOS).
  notifications,

  /// Android: exempt from battery optimisation.
  battery,

  /// Android: the manufacturer's own autostart or battery screen.
  manufacturer,

  /// Android: Data Saver doesn't block NeoCloud in the background.
  backgroundData,

  /// iOS: Background App Refresh is on.
  backgroundRefresh,

  /// iOS: Low Power Mode is off.
  lowPowerMode,
}

/// Shown as a dot plus a word.
enum BackgroundCheckState { granted, needed, recommended }

/// Manufacturers whose Android adds its own task killer on top of Doze;
/// see dontkillmyapp.com. [other] needs only Android's battery setting.
enum BackgroundVendor { samsung, xiaomi, huawei, oppo, vivo, asus, other }

/// The steps on the manufacturer's screens, in order.
enum BackgroundVendorStep {
  samsungUnrestricted,
  samsungNeverSleeping,
  xiaomiAutostart,
  xiaomiNoRestrictions,
  huaweiAppLaunch,
  oppoBatteryUsage,
  oppoDontOptimise,
  vivoBackgroundPower,
  vivoAutostart,
  asusAutostart,
  lockInRecents,
}

/// [manufacturer] and [brand] as Android reports them (Build.MANUFACTURER,
/// Build.BRAND). Redmi and POCO are Xiaomi; Honor is Huawei's software;
/// OnePlus and Realme run Oppo's.
BackgroundVendor backgroundVendorFor(String manufacturer, [String brand = '']) {
  final names = '${manufacturer.toLowerCase()} ${brand.toLowerCase()}';
  bool has(List<String> words) => words.any(names.contains);
  if (has(const <String>['samsung'])) return BackgroundVendor.samsung;
  if (has(const <String>['xiaomi', 'redmi', 'poco'])) return BackgroundVendor.xiaomi;
  if (has(const <String>['huawei', 'honor'])) return BackgroundVendor.huawei;
  if (has(const <String>['oneplus', 'oppo', 'realme'])) return BackgroundVendor.oppo;
  if (has(const <String>['vivo', 'iqoo'])) return BackgroundVendor.vivo;
  if (has(const <String>['asus'])) return BackgroundVendor.asus;
  return BackgroundVendor.other;
}

List<BackgroundVendorStep> backgroundVendorSteps(BackgroundVendor vendor) => switch (vendor) {
  BackgroundVendor.samsung => const <BackgroundVendorStep>[
    BackgroundVendorStep.samsungUnrestricted,
    BackgroundVendorStep.samsungNeverSleeping,
  ],
  BackgroundVendor.xiaomi => const <BackgroundVendorStep>[
    BackgroundVendorStep.xiaomiAutostart,
    BackgroundVendorStep.xiaomiNoRestrictions,
    BackgroundVendorStep.lockInRecents,
  ],
  BackgroundVendor.huawei => const <BackgroundVendorStep>[
    BackgroundVendorStep.huaweiAppLaunch,
    BackgroundVendorStep.lockInRecents,
  ],
  BackgroundVendor.oppo => const <BackgroundVendorStep>[
    BackgroundVendorStep.oppoBatteryUsage,
    BackgroundVendorStep.oppoDontOptimise,
    BackgroundVendorStep.lockInRecents,
  ],
  BackgroundVendor.vivo => const <BackgroundVendorStep>[
    BackgroundVendorStep.vivoBackgroundPower,
    BackgroundVendorStep.vivoAutostart,
  ],
  BackgroundVendor.asus => const <BackgroundVendorStep>[BackgroundVendorStep.asusAutostart],
  BackgroundVendor.other => const <BackgroundVendorStep>[],
};

/// The manufacturer's page on dontkillmyapp.com; the site's index for
/// [BackgroundVendor.other].
Uri backgroundVendorGuide(BackgroundVendor vendor, [String manufacturer = '']) {
  final String slug;
  switch (vendor) {
    case BackgroundVendor.samsung:
      slug = 'samsung';
    case BackgroundVendor.xiaomi:
      slug = 'xiaomi';
    case BackgroundVendor.huawei:
      slug = 'huawei';
    case BackgroundVendor.oppo:
      final name = manufacturer.toLowerCase();
      if (name.contains('oneplus')) {
        slug = 'oneplus';
      } else if (name.contains('realme')) {
        slug = 'realme';
      } else {
        slug = 'oppo';
      }
    case BackgroundVendor.vivo:
      slug = 'vivo';
    case BackgroundVendor.asus:
      slug = 'asus';
    case BackgroundVendor.other:
      slug = '';
  }
  return Uri.parse('https://dontkillmyapp.com/$slug');
}

enum BackgroundRefreshStatus { available, denied, restricted }

/// The platform's raw answer, parsed.
class BackgroundPlatformStatus {
  const BackgroundPlatformStatus({
    required this.android,
    this.manufacturer = '',
    this.brand = '',
    this.ignoringBatteryOptimizations = false,
    this.notificationsGranted = false,
    this.notificationsUndecided = false,
    this.backgroundDataRestricted = false,
    this.manufacturerSettingsAvailable = false,
    this.backgroundRefresh = BackgroundRefreshStatus.available,
    this.lowPowerMode = false,
  });

  /// From neocloud_background's `status`. Android's map has `platform:
  /// android`; anything else reads as iOS.
  factory BackgroundPlatformStatus.fromMap(Map<String, Object?> raw) {
    if (raw['platform'] == 'android') {
      return BackgroundPlatformStatus(
        android: true,
        manufacturer: raw['manufacturer'] as String? ?? '',
        brand: raw['brand'] as String? ?? '',
        ignoringBatteryOptimizations: raw['ignoringBatteryOptimizations'] == true,
        notificationsGranted: raw['notificationsEnabled'] == true,
        backgroundDataRestricted: raw['backgroundDataRestricted'] == true,
        manufacturerSettingsAvailable: raw['manufacturerSettingsAvailable'] == true,
      );
    }
    return BackgroundPlatformStatus(
      android: false,
      notificationsGranted: raw['notifications'] == 'granted',
      notificationsUndecided: raw['notifications'] == 'notDetermined',
      backgroundRefresh: switch (raw['backgroundRefresh']) {
        'denied' => BackgroundRefreshStatus.denied,
        'restricted' => BackgroundRefreshStatus.restricted,
        _ => BackgroundRefreshStatus.available,
      },
      lowPowerMode: raw['lowPowerMode'] == true,
    );
  }

  final bool android;
  final String manufacturer;
  final String brand;
  final bool ignoringBatteryOptimizations;
  final bool notificationsGranted;

  /// iOS: never asked, so the system prompt can still show.
  final bool notificationsUndecided;
  final bool backgroundDataRestricted;
  final bool manufacturerSettingsAvailable;
  final BackgroundRefreshStatus backgroundRefresh;
  final bool lowPowerMode;

  BackgroundVendor get vendor => android ? backgroundVendorFor(manufacturer, brand) : BackgroundVendor.other;
}

class BackgroundItem {
  const BackgroundItem(this.check, this.state, {this.fixable = true});

  final BackgroundCheck check;
  final BackgroundCheckState state;

  /// Has a button that opens the prompt or setting to change it. iOS's
  /// Low Power Mode and a Background App Refresh restricted by Screen Time
  /// or a profile can't be reached from an app.
  final bool fixable;
}

/// The checklist, in the order the screen shows it.
class BackgroundReliability {
  const BackgroundReliability({required this.status, required this.items});

  factory BackgroundReliability.evaluate({required BackgroundPlatformStatus status, required CameraAccess photos}) {
    final photoState = switch (photos) {
      CameraAccess.granted => BackgroundCheckState.granted,
      CameraAccess.limited => BackgroundCheckState.recommended,
      CameraAccess.denied => BackgroundCheckState.needed,
    };
    BackgroundCheckState recommendedUnless(bool ok) => ok ? BackgroundCheckState.granted : BackgroundCheckState.recommended;
    final items = <BackgroundItem>[BackgroundItem(BackgroundCheck.photos, photoState)];
    if (status.android) {
      items.addAll(<BackgroundItem>[
        BackgroundItem(
          BackgroundCheck.battery,
          status.ignoringBatteryOptimizations ? BackgroundCheckState.granted : BackgroundCheckState.needed,
        ),
        if (status.vendor != BackgroundVendor.other)
          // Android can't tell whether these are set; they stay a recommendation.
          const BackgroundItem(BackgroundCheck.manufacturer, BackgroundCheckState.recommended),
        BackgroundItem(BackgroundCheck.notifications, recommendedUnless(status.notificationsGranted)),
        BackgroundItem(BackgroundCheck.backgroundData, recommendedUnless(!status.backgroundDataRestricted)),
      ]);
    } else {
      items.addAll(<BackgroundItem>[
        BackgroundItem(
          BackgroundCheck.backgroundRefresh,
          status.backgroundRefresh == BackgroundRefreshStatus.available ? BackgroundCheckState.granted : BackgroundCheckState.needed,
          fixable: status.backgroundRefresh != BackgroundRefreshStatus.restricted,
        ),
        BackgroundItem(BackgroundCheck.lowPowerMode, recommendedUnless(!status.lowPowerMode), fixable: false),
        BackgroundItem(BackgroundCheck.notifications, recommendedUnless(status.notificationsGranted)),
      ]);
    }
    return BackgroundReliability(status: status, items: items);
  }

  final BackgroundPlatformStatus status;
  final List<BackgroundItem> items;

  bool get anyNeeded => items.any((item) => item.state == BackgroundCheckState.needed);

  /// Items that are not granted, recommended ones included.
  int get openCount => items.where((item) => item.state != BackgroundCheckState.granted).length;

  BackgroundCheckState? stateOf(BackgroundCheck check) {
    for (final item in items) {
      if (item.check == check) return item.state;
    }
    return null;
  }
}

/// How long camera upload may go without a successful pass before the app
/// says the system is holding it back.
const Duration backgroundStaleAfter = Duration(hours: 24);

/// The banner on Settings › Camera upload: something needed is missing, or
/// the last successful pass (or, before the first, turning it on) is more
/// than [backgroundStaleAfter] ago.
bool backgroundBannerDue({
  required bool enabled,
  required BackgroundReliability? reliability,
  required DateTime? lastSuccess,
  required DateTime? enabledAt,
  required DateTime now,
}) {
  if (!enabled) return false;
  if (reliability?.anyNeeded ?? false) return true;
  final since = lastSuccess ?? enabledAt;
  return since != null && now.difference(since) > backgroundStaleAfter;
}
