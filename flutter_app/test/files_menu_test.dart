import 'package:flutter/gestures.dart';
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

FileNode _node(String id, String name, {required bool folder}) => FileNode(
  id: id,
  parentId: 'root',
  type: folder ? 'folder' : 'file',
  name: name,
  size: folder ? 0 : 120000,
  mime: folder ? null : 'application/pdf',
  etag: 'e-$id',
  version: 1,
  modifiedAt: DateTime(2026, 11, 10),
);

/// AI switched off, so the menus hold only what every account has.
class _NoAi extends Fake implements AiApi {
  @override
  Future<AiStatus> status() async => const AiStatus(available: false, enabled: false, optedIn: false);
}

class _FakeClient extends BackendClient {
  _FakeClient() : super(baseUrl: 'http://localhost:1');

  @override
  Future<FolderListing> listFolder(String folderId, {String sort = 'name', String order = 'asc', int? limit, String? cursor}) async => FolderListing(
    folder: FileNode(id: 'root', parentId: null, type: 'folder', name: 'Home', size: 0, mime: null, etag: 'e-root', version: 1, modifiedAt: DateTime(2026, 11, 10)),
    path: const <PathSegment>[],
    items: <FileNode>[_node('d1', 'Projects', folder: true), _node('f1', 'Contract.pdf', folder: false)],
  );

  @override
  Future<List<FileNode>> recent({String? by}) async => const <FileNode>[];
}

Future<void> _pumpFiles(WidgetTester tester, {Size size = const Size(1440, 900)}) async {
  SharedPreferences.setMockInitialValues(const <String, Object>{});
  final controller = NeoCloudController(
    client: _FakeClient(),
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
    ai: _NoAi(),
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
    );
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  controller.openRoute(const AppRoute('files'));
  await tester.pumpWidget(NeoCloudApp(controller: controller));
  await tester.pumpAndSettle();
}

/// Opens the ⋯ menu of the row showing [name].
Future<void> _openMenu(WidgetTester tester, String name) async {
  final rowTop = tester.getCenter(find.text(name)).dy;
  final menu = find.byTooltip('More actions').evaluate().firstWhere(
    (element) => ((element.renderObject! as RenderBox).localToGlobal(Offset.zero).dy - rowTop).abs() < 30,
  );
  await tester.tap(find.byWidget(menu.widget));
  await tester.pumpAndSettle();
}

Future<void> _closeMenu(WidgetTester tester) async {
  await tester.tapAt(const Offset(5, 5));
  await tester.pumpAndSettle();
}

void _expectTexts(List<String> labels, Matcher matcher) {
  for (final label in labels) {
    expect(find.text(label), matcher, reason: label);
  }
}

void main() {
  setUpAll(() => GoogleFonts.config.allowRuntimeFetching = false);

  testWidgets('a file’s menu: everyday actions on top, the rest in groups, trash last', (tester) async {
    await _pumpFiles(tester);
    await _openMenu(tester, 'Contract.pdf');
    _expectTexts(<String>['Open', 'Download', 'Rename', 'Move to trash'], findsOneWidget);
    for (final group in <String>['Share and send', 'Organize', 'Sign', 'Details']) {
      expect(find.widgetWithText(SubmenuButton, group), findsOneWidget, reason: group);
    }
    _expectTexts(<String>['Make a copy', 'Versions', 'Prepare for signing…', 'Send to device'], findsNothing);

    await tester.tap(find.widgetWithText(SubmenuButton, 'Organize'));
    await tester.pumpAndSettle();
    _expectTexts(<String>['Move to…', 'Cut', 'Make a copy', 'Add to favorites'], findsOneWidget);

    await tester.tap(find.widgetWithText(SubmenuButton, 'Sign'));
    await tester.pumpAndSettle();
    _expectTexts(<String>['Prepare for signing…', 'Sign it yourself…'], findsOneWidget);

    await tester.tap(find.widgetWithText(SubmenuButton, 'Share and send'));
    await tester.pumpAndSettle();
    _expectTexts(<String>['Share', 'Send to device'], findsOneWidget);
    await _closeMenu(tester);
  });

  testWidgets('a folder’s menu has no signing, and a group of one is shown as its action', (tester) async {
    await _pumpFiles(tester);
    await _openMenu(tester, 'Projects');
    _expectTexts(<String>['Download as ZIP', 'Comments and activity'], findsOneWidget);
    expect(find.widgetWithText(SubmenuButton, 'Sign'), findsNothing);
    expect(find.widgetWithText(SubmenuButton, 'Details'), findsNothing);

    await tester.tap(find.widgetWithText(SubmenuButton, 'Share and send'));
    await tester.pumpAndSettle();
    _expectTexts(<String>['Share', 'Request files'], findsOneWidget);

    await tester.tap(find.widgetWithText(SubmenuButton, 'Organize'));
    await tester.pumpAndSettle();
    expect(find.text('Convert to vault…'), findsOneWidget);
    await _closeMenu(tester);
  });

  testWidgets('right-click opens the same menu', (tester) async {
    await _pumpFiles(tester);
    await tester.tapAt(tester.getCenter(find.text('Contract.pdf')), buttons: kSecondaryButton);
    await tester.pumpAndSettle();
    expect(find.widgetWithText(SubmenuButton, 'Organize'), findsOneWidget);
    expect(find.text('Rename'), findsOneWidget);
    await _closeMenu(tester);
  });

  testWidgets('on a phone a group opens as the sheet’s second page, and Back returns', (tester) async {
    await _pumpFiles(tester, size: const Size(390, 844));
    await _openMenu(tester, 'Contract.pdf');
    expect(find.byType(BottomSheet), findsOneWidget);
    _expectTexts(<String>['Rename', 'Organize'], findsOneWidget);
    expect(find.text('Make a copy'), findsNothing);

    await tester.tap(find.text('Organize'));
    await tester.pumpAndSettle();
    _expectTexts(<String>['Move to…', 'Make a copy', 'Add to favorites'], findsOneWidget);
    expect(find.text('Rename'), findsNothing);

    await tester.tap(find.byTooltip('Back'));
    await tester.pumpAndSettle();
    expect(find.text('Rename'), findsOneWidget);
    expect(find.byType(BottomSheet), findsOneWidget);
  });
}
