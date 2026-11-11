import 'package:flutter_test/flutter_test.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/navigation/app_route.dart';
import 'package:shared_preferences/shared_preferences.dart';

Future<(NeoCloudController, SharedPreferences)> _signedIn({bool admin = false, Map<String, Object> stored = const <String, Object>{}}) async {
  SharedPreferences.setMockInitialValues(stored);
  final preferences = await SharedPreferences.getInstance();
  final controller = NeoCloudController(
    client: BackendClient(baseUrl: 'http://localhost:1'),
    preferences: preferences,
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
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
  return (controller, preferences);
}

void main() {
  group('AppRoute.parse', () {
    test('reads sections, folder ids, settings pages and admin tabs', () {
      expect(AppRoute.parse('/files'), const AppRoute('files'));
      expect(AppRoute.parse('/files/abc-123'), const AppRoute('files', 'abc-123'));
      expect(AppRoute.parse('/settings/shared-links'), const AppRoute('settings', 'shared-links'));
      expect(AppRoute.parse('/admin/backups'), const AppRoute('admin', 'backups'));
      expect(AppRoute.parse('trash'), const AppRoute('trash'));
      expect(AppRoute.parse('/app/shared'), const AppRoute('shared'));
      expect(AppRoute.parse('/app/shared/abc-123'), const AppRoute('shared', 'abc-123'));
    });

    test('skips the /app prefix and ignores query and fragment', () {
      expect(AppRoute.parse('/app/files/xyz?x=1#top'), const AppRoute('files', 'xyz'));
      expect(AppRoute.parse('https://cloud.example/app/recent'), const AppRoute('recent'));
    });

    test('the root, unknown pages and the device and pairing links name no page', () {
      expect(AppRoute.parse('/').isNone, isTrue);
      expect(AppRoute.parse('').isNone, isTrue);
      expect(AppRoute.parse('/nope/1').isNone, isTrue);
      expect(AppRoute.parse('/device?code=ABCD').isNone, isTrue);
      expect(AppRoute.parse('/app/pair?code=x').isNone, isTrue);
    });

    test('location round-trips, ids are escaped', () {
      for (final route in <AppRoute>[
        const AppRoute('files'),
        const AppRoute('files', 'a b/c'),
        const AppRoute('settings', 'camera-upload'),
        const AppRoute('admin', 'server'),
      ]) {
        expect(AppRoute.parse(route.location), route);
      }
      expect(const AppRoute('files', 'a b').location, '/files/a%20b');
      expect(AppRoute.none.location, '/');
    });

    test('slug turns enum names into address words', () {
      expect(AppRoute.slug('sharedLinks'), 'shared-links');
      expect(AppRoute.slug('cameraUpload'), 'camera-upload');
      expect(AppRoute.slug('security'), 'security');
    });
  });

  group('controller routes', () {
    test('nothing is reported while signed out; a deep link waits', () async {
      final (controller, _) = await _signedIn();
      controller.user = null;
      expect(controller.currentRoute, isNull);
      controller.openRoute(const AppRoute('settings', 'security'), initial: true);
      expect(controller.selectedSection, AppSection.files);
    });

    test('opening a route sets the page and keeps it in the preferences', () async {
      final (controller, preferences) = await _signedIn();
      controller.openRoute(const AppRoute('settings', 'shared-links'), fromBrowser: true);
      expect(controller.selectedSection, AppSection.settings);
      expect(controller.settingsPage, SettingsPage.sharedLinks);
      expect(controller.currentRoute, const AppRoute('settings', 'shared-links'));
      expect(preferences.getString('app.section'), 'settings/shared-links');

      controller.openFolder('f1');
      expect(controller.currentRoute, const AppRoute('files', 'f1'));
      expect(preferences.getString('app.section'), 'files/f1');

      controller.setSelectedSection(AppSection.trash);
      expect(controller.currentRoute, const AppRoute('trash'));
      expect(preferences.getString('app.section'), 'trash');
    });

    test('admin routes fall back to files for other accounts', () async {
      final (controller, _) = await _signedIn();
      controller.openRoute(const AppRoute('admin', 'backups'), fromBrowser: true);
      expect(controller.selectedSection, AppSection.files);
      expect(controller.currentRoute, const AppRoute('files'));
    });

    test('admins get the tab from the address', () async {
      final (controller, preferences) = await _signedIn(admin: true);
      controller.openRoute(const AppRoute('admin', 'backups'), fromBrowser: true);
      expect(controller.selectedSection, AppSection.admin);
      expect(controller.currentRoute, const AppRoute('admin', 'backups'));
      expect(preferences.getString('app.section'), 'admin/backups');
      controller.openRoute(const AppRoute('admin', 'nonsense'), fromBrowser: true);
      expect(controller.currentRoute, const AppRoute('admin'));
    });

    test('the root from the browser opens files; an initial root keeps the page', () async {
      final (controller, _) = await _signedIn();
      controller.setSelectedSection(AppSection.recent);
      controller.openRoute(AppRoute.none, initial: true);
      expect(controller.selectedSection, AppSection.recent);
      controller.openRoute(AppRoute.none, fromBrowser: true);
      expect(controller.currentRoute, const AppRoute('files'));
    });

    test('shared with me has its list and its folders in the address', () async {
      final (controller, preferences) = await _signedIn();
      controller.openRoute(const AppRoute('shared', 'f9'), fromBrowser: true);
      expect(controller.selectedSection, AppSection.shared);
      expect(controller.openSharedFolderId, 'f9');
      expect(controller.currentRoute, const AppRoute('shared', 'f9'));
      expect(preferences.getString('app.section'), 'shared/f9');

      controller.openSharedFolder(null);
      expect(controller.currentRoute, const AppRoute('shared'));

      // The own files keep their folder apart from the shared one.
      controller.openFolder('mine');
      controller.openSharedFolder('theirs');
      controller.setSelectedSection(AppSection.files);
      expect(controller.currentRoute, const AppRoute('files', 'mine'));
    });

    test('a listing belongs to the share its first breadcrumb is', () async {
      final (controller, _) = await _signedIn();
      controller.sharedWithMe = <SharedWithMe>[
        SharedWithMe.fromJson(<String, dynamic>{
          'id': 3,
          'permission': 'write',
          'ownerName': 'Grace',
          'node': <String, dynamic>{'id': 'team', 'type': 'folder', 'name': 'Team', 'parentId': null},
        }),
      ];
      expect(controller.shareFor('team')?.permission, MemberPermission.write);
      expect(controller.shareFor('own-root'), isNull);
      expect(controller.shareFor(null), isNull);
    });

    test('unknown settings pages open the settings start', () async {
      final (controller, _) = await _signedIn();
      controller.openRoute(const AppRoute('settings', 'gone'), fromBrowser: true);
      expect(controller.selectedSection, AppSection.settings);
      expect(controller.settingsPage, isNull);
      expect(controller.currentRoute, const AppRoute('settings'));
    });
  });
}
