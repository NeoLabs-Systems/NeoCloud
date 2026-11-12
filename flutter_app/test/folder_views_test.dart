import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/files/folder_views.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:shared_preferences/shared_preferences.dart';

// Sort and view are remembered per folder and page, on the device and on
// the server (`folder_views`), and come back after a restart.

FileNode _node(String id, String name, {String? parentId = 'root-id', String type = 'file', int size = 1, DateTime? modified, DateTime? trashed}) => FileNode(
  id: id,
  parentId: parentId,
  type: type,
  name: name,
  size: size,
  mime: type == 'folder' ? null : 'text/plain',
  etag: id,
  version: 1,
  modifiedAt: modified ?? DateTime.utc(2026, 1, 1),
  trashedAt: trashed,
);

/// The server's side: listings record the sort they were asked for, and
/// `folder_views` is merged like the real store does.
class _FakeServer extends BackendClient {
  _FakeServer() : super(baseUrl: 'http://localhost:1');

  final List<(String, String, String)> listings = <(String, String, String)>[];
  final List<Map<String, Object?>> saved = <Map<String, Object?>>[];
  Map<String, Object?> folderViews = <String, Object?>{};
  List<FileNode> favoriteItems = const <FileNode>[];
  List<FileNode> trashItems = const <FileNode>[];

  @override
  Future<FolderListing> listFolder(String folderId, {String sort = 'name', String order = 'asc', int? limit, String? cursor}) async {
    listings.add((folderId, sort, order));
    final root = folderId == 'root' || folderId == 'root-id';
    final folder = _node(root ? 'root-id' : folderId, root ? '' : folderId, parentId: root ? null : 'root-id', type: 'folder');
    return FolderListing(
      folder: folder,
      path: <PathSegment>[if (!root) PathSegment(id: folderId, name: folderId)],
      items: <FileNode>[_node('$folderId-1', 'one.txt')],
    );
  }

  @override
  Future<List<FileNode>> recent({String? by}) async => const <FileNode>[];

  @override
  Future<List<FileNode>> favorites() async => favoriteItems;

  @override
  Future<List<FileNode>> trash() async => trashItems;

  @override
  Future<UserSettings> settings() async => UserSettings.fromJson(<String, dynamic>{'folder_views': folderViews});

  @override
  Future<UserSettings> updateSettings(Map<String, Object?> patch) async {
    saved.add(patch);
    final views = patch['folder_views'];
    if (views is Map) folderViews = <String, Object?>{...folderViews, ...views.cast<String, Object?>()};
    return UserSettings.fromJson(<String, dynamic>{'folder_views': folderViews});
  }
}

Future<NeoCloudController> _controller(_FakeServer server) async {
  final controller = NeoCloudController(
    client: server,
    preferences: await SharedPreferences.getInstance(),
    deviceSessions: createDeviceSessions(),
    cameraUploads: NoCameraUploads(),
    shareIntake: NoShareIntake(),
  );
  controller
    ..isBooting = false
    ..user = const AccountUser(
      id: 7,
      username: 'ada',
      displayName: null,
      email: null,
      isAdmin: false,
      storage: StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
    );
  controller.folderViews.open('7');
  return controller;
}

Future<void> _settle(WidgetTester tester) async {
  for (var i = 0; i < 5; i++) {
    await tester.pump(const Duration(milliseconds: 200));
  }
}

Future<void> _pump(WidgetTester tester, Widget child) async {
  tester.view.physicalSize = const Size(1440, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(MaterialApp(home: Scaffold(body: child)));
  await _settle(tester);
}

Future<void> _chooseSort(WidgetTester tester, String label) async {
  await tester.tap(find.byTooltip('Sort').first);
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await _settle(tester);
}

const FolderView _sizeDesc = FolderView(sort: 'size', dir: 'desc', view: 'grid');
const FolderView _nameAsc = FolderView(sort: 'name', dir: 'asc', view: 'list');

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await initializeDateFormatting('en');
  });
  setUp(() => SharedPreferences.setMockInitialValues(const <String, Object>{}));

  testWidgets('a sort set in folder A stays with A; B opens with the defaults', (tester) async {
    final server = _FakeServer();
    final controller = await _controller(server);
    controller.openFolder('A');
    await _pump(tester, FilesPanel(controller: controller));
    expect(server.listings.last, ('A', 'name', 'asc'));

    await _chooseSort(tester, 'Size');
    expect(server.listings.last, ('A', 'size', 'asc'));

    // Grid for A only.
    await tester.tap(find.byTooltip('Grid'));
    await _settle(tester);
    expect(controller.folderViews['A'], const FolderView(sort: 'size', dir: 'asc', view: 'grid'));

    controller.openFolder('B');
    await _settle(tester);
    expect(server.listings.last, ('B', 'name', 'asc'));
    expect(controller.folderViews['B'], isNull);

    // A live refresh or an upload finishing reloads B as it was.
    controller.filesRevision.value += 1;
    await _settle(tester);
    expect(server.listings.last, ('B', 'name', 'asc'));

    controller.openFolder('A');
    await _settle(tester);
    expect(server.listings.last, ('A', 'size', 'asc'));
    controller.filesRevision.value += 1;
    await _settle(tester);
    expect(server.listings.last, ('A', 'size', 'asc'));

    // The account's defaults stay as they were.
    expect(server.saved.every((patch) => patch.keys.single == 'folder_views'), isTrue);
    await controller.folderViews.flush();
    expect(server.folderViews.keys, <String>['A']);
  });

  testWidgets('the root opened by its id shares the root entry', (tester) async {
    final server = _FakeServer();
    final controller = await _controller(server);
    controller.folderViews.set(FolderViewKeys.root, _sizeDesc);
    await _pump(tester, FilesPanel(controller: controller));
    expect(server.listings.last, ('root', 'size', 'desc'));
    controller.openFolder('A');
    await _settle(tester);
    controller.openFolder('root-id');
    await _settle(tester);
    expect(server.listings.last, ('root-id', 'size', 'desc'));
  });

  testWidgets('changes are sent once after a pause, only the entries that changed', (tester) async {
    final server = _FakeServer();
    final controller = await _controller(server);
    controller.folderViews
      ..set('A', _nameAsc)
      ..set('A', _sizeDesc)
      ..set(FolderViewKeys.recent, _nameAsc);
    await tester.pump(const Duration(milliseconds: 500));
    expect(server.saved, isEmpty);
    await tester.pump(const Duration(milliseconds: 400));
    expect(server.saved, <Map<String, Object?>>[
      <String, Object?>{'folder_views': <String, Object?>{'A': _sizeDesc.toJson(), 'recent': _nameAsc.toJson()}},
    ]);
    // Setting what is already there sends nothing.
    controller.folderViews.set('A', _sizeDesc);
    await tester.pump(const Duration(seconds: 1));
    expect(server.saved, hasLength(1));
  });

  group('restart', () {
    test('restores from the device at once, then takes the server’s entries', () async {
      final server = _FakeServer();
      final first = await _controller(server);
      first.folderViews
        ..set('A', _sizeDesc)
        ..set(FolderViewKeys.trash, _nameAsc);
      await first.folderViews.flush();
      expect(server.saved, hasLength(1));
      expect(server.saved.single['folder_views'], <String, Object?>{'A': _sizeDesc.toJson(), 'trash': _nameAsc.toJson()});

      // The app starts again on the same device: the cache answers before the server.
      server.folderViews = <String, Object?>{...server.folderViews, 'B': _nameAsc.toJson()};
      final second = await _controller(server);
      expect(second.folderViews['A'], _sizeDesc);
      expect(second.folderViews['B'], isNull);
      second.folderViews.reconcile((await server.settings()).folderViews);
      expect(second.folderViews['B'], _nameAsc);
      expect(second.folderViews['A'], _sizeDesc);
    });

    test('another device starts empty and gets everything from the server', () async {
      final server = _FakeServer()..folderViews = <String, Object?>{'A': _sizeDesc.toJson(), 'favorites': _nameAsc.toJson(), 'bad': <String, Object?>{'sort': 'owner'}};
      SharedPreferences.setMockInitialValues(const <String, Object>{});
      final controller = await _controller(server);
      expect(controller.folderViews['A'], isNull);
      controller.folderViews.reconcile((await server.settings()).folderViews);
      expect(controller.folderViews['A'], _sizeDesc);
      expect(controller.folderViews['favorites'], _nameAsc);
      expect(controller.folderViews['bad'], isNull);
    });

    test('a change not sent before the app closed is kept and sent on the next start', () async {
      final failing = _FailingServer();
      final first = await _controller(failing);
      first.folderViews.set('A', _sizeDesc);
      await first.folderViews.flush();
      expect(failing.attempts, 1);

      final server = _FakeServer()..folderViews = <String, Object?>{'A': _nameAsc.toJson()};
      final second = await _controller(server);
      // The server still has the old choice; the unsent one wins and goes out.
      second.folderViews.reconcile((await server.settings()).folderViews);
      expect(second.folderViews['A'], _sizeDesc);
      await second.folderViews.flush();
      expect(server.folderViews['A'], _sizeDesc.toJson());
      final stored = jsonDecode((await SharedPreferences.getInstance()).getString('files.folderViews.7')!) as Map;
      expect(stored['pending'], isEmpty);
    });

    test('accounts on one device keep their own entries', () async {
      final preferences = await SharedPreferences.getInstance();
      final views = FolderViews(preferences: preferences, save: (_) async {})..open('1');
      views.set('A', _sizeDesc);
      views.open('2');
      expect(views['A'], isNull);
      views.open('1');
      expect(views['A'], _sizeDesc);
    });
  });

  group('special pages', () {
    test('each page keeps its own entry, apart from folders and the defaults', () async {
      final controller = await _controller(_FakeServer());
      final views = controller.folderViews;
      views
        ..set(FolderViewKeys.recent, const FolderView(sort: 'name', dir: 'asc', view: 'list'))
        ..set(FolderViewKeys.favorites, const FolderView(sort: 'size', dir: 'desc', view: 'list'))
        ..set(FolderViewKeys.search, const FolderView(sort: 'relevance', dir: 'desc', view: 'grid'));
      expect(views[FolderViewKeys.recent]!.sort, 'name');
      expect(views[FolderViewKeys.favorites]!.sort, 'size');
      expect(views[FolderViewKeys.search]!.grid, isTrue);
      expect(views[FolderViewKeys.trash], isNull);
      expect(views[FolderViewKeys.shared], isNull);
      expect(views.viewFor(FolderViewKeys.trash, _nameAsc), _nameAsc);
    });

    testWidgets('Favorites and Trash sort on their own', (tester) async {
      final server = _FakeServer()
        ..favoriteItems = <FileNode>[_node('1', 'b.txt', size: 1), _node('2', 'a.txt', size: 9)]
        ..trashItems = <FileNode>[
          _node('3', 'old.txt', trashed: DateTime.utc(2026, 1, 1)),
          _node('4', 'new.txt', trashed: DateTime.utc(2026, 3, 1)),
        ];
      final controller = await _controller(server);

      await _pump(tester, FavoritesPanel(controller: controller));
      // The account's default: by name.
      expect(tester.getTopLeft(find.text('a.txt')).dy, lessThan(tester.getTopLeft(find.text('b.txt')).dy));
      await _chooseSort(tester, 'Size');
      await _chooseSort(tester, 'Descending');
      expect(controller.folderViews[FolderViewKeys.favorites], const FolderView(sort: 'size', dir: 'desc', view: 'list'));
      expect(tester.getTopLeft(find.text('a.txt')).dy, lessThan(tester.getTopLeft(find.text('b.txt')).dy));

      await _pump(tester, TrashPanel(controller: controller));
      // Newest deleted first until chosen otherwise; Favorites' choice does not apply.
      expect(tester.getTopLeft(find.text('new.txt')).dy, lessThan(tester.getTopLeft(find.text('old.txt')).dy));
      await _chooseSort(tester, 'Name');
      expect(controller.folderViews[FolderViewKeys.trash]!.sort, 'name');
      expect(controller.folderViews[FolderViewKeys.favorites]!.sort, 'size');
    });
  });

  test('sortedForView keeps folders first and relevance as it came', () {
    final items = <FileNode>[_node('1', 'b', size: 5), _node('2', 'z', type: 'folder'), _node('3', 'a', size: 7)];
    List<String> names(FolderView view) => sortedForView(items, view, (node) => node).map((node) => node.name).toList();
    expect(names(_nameAsc), <String>['z', 'a', 'b']);
    expect(names(_sizeDesc), <String>['z', 'a', 'b']);
    expect(names(const FolderView(sort: 'size', dir: 'asc', view: 'list')), <String>['z', 'b', 'a']);
    expect(names(const FolderView(sort: 'relevance', dir: 'desc', view: 'list')), <String>['b', 'z', 'a']);
  });
}

class _FailingServer extends _FakeServer {
  int attempts = 0;

  @override
  Future<UserSettings> updateSettings(Map<String, Object?> patch) async {
    attempts += 1;
    throw const ApiException(503, 'down');
  }
}
