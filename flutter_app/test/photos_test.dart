import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:neocloud/main.dart';
import 'package:neocloud/src/api/backend_client.dart';
import 'package:neocloud/src/api/models.dart';
import 'package:neocloud/src/device/device_session_stub.dart';
import 'package:neocloud/src/mobile/camera_upload_stub.dart';
import 'package:neocloud/src/mobile/share_intake_stub.dart';
import 'package:neocloud/src/navigation/app_route.dart';
import 'package:neocloud/src/photos/photo_layout.dart';
import 'package:neocloud/src/photos/photo_models.dart';
import 'package:neocloud/src/photos/photo_thumbnails.dart';
import 'package:neocloud/src/photos/photo_timeline.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// A timeline of [days] days with [perDay] photos each (older days first
/// in the list's tail), served in pages like the server does.
class _FakeSource implements PhotoSource {
  _FakeSource(this.items);

  factory _FakeSource.generate({int days = 300, int perDay = 17, DateTime? newest}) {
    final start = newest ?? DateTime(2025, 6, 30);
    final items = <PhotoItem>[];
    for (var day = 0; day < days; day += 1) {
      final date = start.subtract(Duration(days: day * 2));
      final count = perDay + (day % 5);
      for (var n = 0; n < count; n += 1) {
        final taken = DateTime(date.year, date.month, date.day, 20).subtract(Duration(minutes: n));
        items.add(PhotoItem(
          id: 'p$day-$n',
          takenAt: taken.toIso8601String().substring(0, 19),
          kind: n % 7 == 3 ? PhotoKind.video : PhotoKind.photo,
          name: 'IMG_$day$n.jpg',
          etag: 'e$day-$n',
          durationSeconds: n % 7 == 3 ? 42 : null,
        ));
      }
    }
    return _FakeSource(items);
  }

  final List<PhotoItem> items;
  final List<String> requests = <String>[];

  @override
  Future<PhotoSummary> summary() async {
    final days = <PhotoDayCount>[];
    for (final item in items) {
      if (days.isNotEmpty && days.last.date == item.day) {
        days[days.length - 1] = PhotoDayCount(item.day, days.last.count + 1);
      } else {
        days.add(PhotoDayCount(item.day, 1));
      }
    }
    return PhotoSummary(days: days);
  }

  @override
  Future<PhotoPage> page({String? cursor, String? day, int limit = 200}) async {
    requests.add(cursor != null ? 'cursor:$cursor' : 'day:$day');
    var start = 0;
    if (cursor != null) {
      start = int.parse(cursor);
    } else if (day != null) {
      start = items.indexWhere((item) => item.day.compareTo(day) <= 0);
      if (start < 0) start = items.length;
    }
    final end = (start + limit).clamp(0, items.length);
    return PhotoPage(items: items.sublist(start, end), nextCursor: end < items.length ? '$end' : null);
  }
}

Future<PhotoTimeline> _loadedTimeline(_FakeSource source, {int pageSize = 200}) async {
  final timeline = PhotoTimeline(source, pageSize: pageSize)..attach('test');
  await timeline.refresh();
  return timeline;
}

/// The Photos grid on its own, with the page's selection and zoom.
class _GridHarness extends StatefulWidget {
  const _GridHarness({super.key, required this.timeline, required this.thumbnails});

  final PhotoTimeline timeline;
  final PhotoThumbnailCache thumbnails;

  @override
  State<_GridHarness> createState() => _GridHarnessState();
}

class _GridHarnessState extends State<_GridHarness> {
  final Set<String> selected = <String>{};
  final List<int> opened = <int>[];

  @override
  void initState() {
    super.initState();
    widget.timeline.addListener(_changed);
  }

  @override
  void dispose() {
    widget.timeline.removeListener(_changed);
    super.dispose();
  }

  void _changed() => setState(() {});

  @override
  Widget build(BuildContext context) {
    return PhotoGrid(
      timeline: widget.timeline,
      thumbnails: widget.thumbnails,
      selected: selected,
      onOpen: (index) {
        if (selected.isNotEmpty) {
          final id = widget.timeline.itemAt(index)!.id;
          setState(() => selected.contains(id) ? selected.remove(id) : selected.add(id));
        } else {
          opened.add(index);
        }
      },
      onToggle: (_) {},
      onSelect: (ids) => setState(() {
        selected
          ..clear()
          ..addAll(ids);
      }),
      onRefresh: () => widget.timeline.refresh(reloadItems: true),
      onZoom: (zoom) => setState(() => widget.timeline.zoom = zoom),
    );
  }
}

Future<GlobalKey<_GridHarnessState>> _pumpGrid(WidgetTester tester, PhotoTimeline timeline, {PhotoThumbnailCache? thumbnails}) async {
  tester.view.physicalSize = const Size(412, 915);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
  final key = GlobalKey<_GridHarnessState>();
  await tester.pumpWidget(MaterialApp(
    home: Scaffold(
      body: _GridHarness(
        key: key,
        timeline: timeline,
        thumbnails: thumbnails ?? PhotoThumbnailCache(fetch: (_, _) async => null),
      ),
    ),
  ));
  await tester.pumpAndSettle();
  return key;
}

void main() {
  setUpAll(() async {
    GoogleFonts.config.allowRuntimeFetching = false;
    await initializeDateFormatting('en');
  });

  group('navigation', () {
    test('/app/photos opens Photos and the address follows it', () async {
      SharedPreferences.setMockInitialValues(const <String, Object>{});
      final controller = NeoCloudController(
        client: BackendClient(baseUrl: 'http://localhost:1'),
        preferences: await SharedPreferences.getInstance(),
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
          isAdmin: false,
          storage: const StorageUsage(usedBytes: 0, quotaBytes: -1, trashBytes: 0, versionBytes: 0),
        );
      expect(AppRoute.parse('/app/photos'), const AppRoute('photos'));
      controller.openRoute(AppRoute.parse('/app/photos'));
      expect(controller.selectedSection, AppSection.photos);
      expect(controller.currentRoute?.location, '/photos');
      expect(AppSection.photos.group, SidebarGroup.photos);
    });
  });

  group('grid layout', () {
    const days = <PhotoDayCount>[
      PhotoDayCount('2026-11-12', 5),
      PhotoDayCount('2026-11-10', 3),
      PhotoDayCount('2026-10-01', 9),
      PhotoDayCount('2025-01-01', 2),
    ];

    test('groups by day, month and year with phone-sized columns', () {
      final day = PhotoGridLayout.build(days, zoom: PhotoZoom.day, width: 412);
      expect(day.columns, 4);
      expect(day.sections.map((section) => section.key), <String>['2026-11-12', '2026-11-10', '2026-10-01', '2025-01-01']);
      // Header + rows of 4: 5 → 2 rows, 3 → 1, 9 → 3, 2 → 1.
      expect(day.rows.length, 4 + 2 + 1 + 3 + 1);
      expect(day.totalItems, 19);

      final month = PhotoGridLayout.build(days, zoom: PhotoZoom.month, width: 412);
      expect(month.columns, 5);
      expect(month.sections.map((section) => (section.key, section.count)), <(String, int)>[('2026-11', 8), ('2026-10', 9), ('2025-01', 2)]);

      final year = PhotoGridLayout.build(days, zoom: PhotoZoom.year, width: 412);
      expect(year.columns, greaterThanOrEqualTo(8));
      expect(year.sections.map((section) => (section.key, section.count)), <(String, int)>[('2026', 17), ('2025', 2)]);
      expect(year.yearMarks().map((mark) => mark.$1), <String>['2026', '2025']);

      expect(photoColumns(PhotoZoom.day, 360), 3);
      expect(photoColumns(PhotoZoom.day, 1280), greaterThan(10));
    });

    test('knows every photo’s place before any is loaded', () {
      final layout = PhotoGridLayout.build(days, zoom: PhotoZoom.day, width: 412, spacing: 2);
      final tile = layout.tileExtent;
      expect(tile, closeTo((412 - 6) / 4, 0.001));
      // Day 1: header (row 0), photos 0–3 (row 1), photo 4 (row 2).
      expect(layout.rowOfItem(4), 2);
      expect(layout.rows[2].firstItem, 4);
      // Photo 5 opens day 2: its header is row 3.
      expect(layout.rowOfItem(5), 4);
      // Day 2 starts after header 48 + two rows.
      final secondHeader = layout.rows[layout.sections[1].headerRow];
      expect(secondHeader.offset, closeTo(48 + 2 * (tile + 2), 0.001));
      expect(layout.itemAt(10, 5), isNull, reason: 'a header');
      expect(layout.itemAt(10, 50), 0);
      expect(layout.itemAt(tile + 4, 50), 1);
      expect(layout.itemAt(412 - 1, 48 + tile + 4), isNull, reason: 'the second row of day 1 has one photo');
      expect(layout.itemAtOffset(secondHeader.offset), 5);
      expect(layout.sectionOfItem(8), 2);
    });

    test('pinching steps between the zoom levels', () {
      expect(PhotoZoom.day.denser, PhotoZoom.month);
      expect(PhotoZoom.month.denser, PhotoZoom.year);
      expect(PhotoZoom.year.denser, isNull);
      expect(PhotoZoom.year.bigger, PhotoZoom.month);
      expect(PhotoZoom.day.bigger, isNull);
    });
  });

  group('timeline', () {
    test('loads the page around a far photo without loading what is above it', () async {
      final source = _FakeSource.generate();
      final timeline = await _loadedTimeline(source);
      expect(timeline.total, source.items.length);
      expect(timeline.itemAt(4000), isNull);
      timeline.ensureLoaded(4000);
      timeline.ensureLoaded(4001);
      await pumpEventQueue();
      expect(timeline.itemAt(4000)?.id, source.items[4000].id);
      expect(timeline.itemAt(0), isNull);
      expect(source.requests, hasLength(1));
      expect(source.requests.single, startsWith('day:'));
    });

    test('a long day continues from where the last page stopped', () async {
      final source = _FakeSource.generate(days: 2, perDay: 450);
      final timeline = await _loadedTimeline(source, pageSize: 200);
      timeline.ensureLoaded(0);
      await pumpEventQueue();
      expect(timeline.itemAt(199), isNotNull);
      expect(timeline.itemAt(200), isNull);
      timeline.ensureLoaded(250);
      await pumpEventQueue();
      expect(timeline.itemAt(250)?.id, source.items[250].id);
      expect(source.requests.last, 'cursor:200');
    });

    test('counts that moved since the summary reload it', () async {
      final source = _FakeSource.generate(days: 3, perDay: 4);
      final timeline = await _loadedTimeline(source);
      source.items.insert(0, PhotoItem(id: 'new', takenAt: '2025-06-30T23:00:00', kind: PhotoKind.photo, name: 'new.jpg', etag: 'n'));
      source.items.insert(0, PhotoItem(id: 'newer', takenAt: '2025-07-01T09:00:00', kind: PhotoKind.photo, name: 'newer.jpg', etag: 'n'));
      timeline.ensureLoaded(0);
      await pumpEventQueue();
      expect(timeline.total, source.items.length);
      timeline.ensureLoaded(0);
      await pumpEventQueue();
      expect(timeline.itemAt(0)?.id, 'newer');
    });

    test('another account starts over; the same keeps the scroll place', () async {
      final timeline = await _loadedTimeline(_FakeSource.generate(days: 3));
      timeline.savedOffset = 500;
      timeline.attach('test');
      expect(timeline.savedOffset, 500);
      timeline.attach('someone-else');
      expect(timeline.savedOffset, 0);
      expect(timeline.loaded, isFalse);
    });
  });

  group('thumbnails', () {
    test('on-screen first, newest request first; dropped when scrolled away', () async {
      final started = <String>[];
      final gates = <String, Completer<Uint8List?>>{};
      final cache = PhotoThumbnailCache(
        maxParallel: 1,
        fetch: (id, _) {
          started.add(id);
          return (gates[id] = Completer<Uint8List?>()).future;
        },
      );
      unawaited(cache.load('a', '1'));
      cache.prefetch('ahead', '1');
      unawaited(cache.load('b', '1'));
      unawaited(cache.load('c', '1'));
      cache.release('b', '1');
      await pumpEventQueue();
      expect(started, <String>['a']);
      gates['a']!.complete(Uint8List(4));
      await pumpEventQueue();
      expect(started, <String>['a', 'c']);
      gates['c']!.complete(Uint8List(4));
      await pumpEventQueue();
      expect(started, <String>['a', 'c', 'ahead']);
      expect(cache.peek('a', '1'), isNotNull);
    });
  });

  group('grid', () {
    testWidgets('day headers, sticky header and video length', (tester) async {
      final timeline = await _loadedTimeline(_FakeSource.generate(days: 40));
      await _pumpGrid(tester, timeline);
      expect(find.text('Mon, Jun 30, 2025'), findsOneWidget);
      expect(find.text('0:42'), findsWidgets);
      expect(find.byKey(const ValueKey<String>('photos-sticky-header')), findsNothing);
      await tester.drag(find.byKey(const ValueKey<String>('photos-grid')), const Offset(0, -300));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey<String>('photos-sticky-header')), findsOneWidget);
    });

    testWidgets('the scrubber jumps through the timeline and names the month', (tester) async {
      final timeline = await _loadedTimeline(_FakeSource.generate());
      await _pumpGrid(tester, timeline);
      await tester.drag(find.byKey(const ValueKey<String>('photos-grid')), const Offset(0, -200));
      await tester.pump();
      final scrubber = find.byKey(const ValueKey<String>('photos-scrubber'));
      expect(scrubber, findsOneWidget);
      final gesture = await tester.startGesture(tester.getCenter(scrubber));
      await gesture.moveBy(const Offset(0, 400));
      await tester.pump();
      expect(find.byKey(const ValueKey<String>('photos-scrubber-label')), findsOneWidget);
      final scrollable = tester.state<ScrollableState>(find.byType(Scrollable).first);
      final position = scrollable.position;
      expect(position.pixels / position.maxScrollExtent, closeTo(0.5, 0.1));
      await gesture.up();
      await tester.pumpAndSettle();
      // The photos where the scrubber stopped load at once.
      final first = PhotoGridLayout.build(timeline.summary.days, zoom: PhotoZoom.day, width: 412).itemAtOffset(position.pixels);
      expect(timeline.itemAt(first), isNotNull);
    });

    testWidgets('long press and drag selects a range; taps then toggle', (tester) async {
      final timeline = await _loadedTimeline(_FakeSource.generate(days: 10));
      final key = await _pumpGrid(tester, timeline);
      final layout = PhotoGridLayout.build(timeline.summary.days, zoom: PhotoZoom.day, width: 412);
      Offset centerOf(int item) {
        final row = layout.rows[layout.rowOfItem(item)];
        final column = item - row.firstItem;
        final origin = tester.getTopLeft(find.byKey(const ValueKey<String>('photos-grid')));
        return origin + Offset(column * (layout.tileExtent + 2) + layout.tileExtent / 2, row.offset + layout.tileExtent / 2);
      }

      final gesture = await tester.startGesture(centerOf(1));
      await tester.pump(const Duration(milliseconds: 600));
      await gesture.moveTo(centerOf(6));
      await tester.pump();
      await gesture.up();
      await tester.pumpAndSettle();
      expect(key.currentState!.selected, hasLength(6));
      expect(find.byTooltip('Select all here'), findsWidgets);

      await tester.tapAt(centerOf(0));
      await tester.pump();
      expect(key.currentState!.selected, hasLength(7));
      await tester.tapAt(centerOf(1));
      await tester.pump();
      expect(key.currentState!.selected, hasLength(6));
      expect(key.currentState!.opened, isEmpty);
    });

    testWidgets('pinching in shows months, then years', (tester) async {
      final timeline = await _loadedTimeline(_FakeSource.generate(days: 40));
      await _pumpGrid(tester, timeline);
      final center = tester.getCenter(find.byKey(const ValueKey<String>('photos-grid')));
      Future<void> pinch(double from, double to) async {
        final a = await tester.startGesture(center - Offset(from, 0), pointer: 7);
        final b = await tester.startGesture(center + Offset(from, 0), pointer: 8);
        await a.moveTo(center - Offset(to, 0));
        await b.moveTo(center + Offset(to, 0));
        await a.up();
        await b.up();
        await tester.pumpAndSettle();
      }

      await pinch(150, 60);
      expect(timeline.zoom, PhotoZoom.month);
      expect(find.text('June 2025'), findsOneWidget);
      await pinch(150, 60);
      expect(timeline.zoom, PhotoZoom.year);
      await pinch(60, 150);
      expect(timeline.zoom, PhotoZoom.month);
    });

    testWidgets('scrolls 5,000 photos building only what is on screen', (tester) async {
      final source = _FakeSource.generate(days: 270);
      expect(source.items.length, greaterThan(5000));
      final timeline = await _loadedTimeline(source);
      var thumbnailsAsked = 0;
      await _pumpGrid(tester, timeline, thumbnails: PhotoThumbnailCache(fetch: (_, _) async {
        thumbnailsAsked += 1;
        return null;
      }));
      final grid = find.byKey(const ValueKey<String>('photos-grid'));
      final frames = <int>[];
      for (var fling = 0; fling < 30; fling += 1) {
        await tester.fling(grid, const Offset(0, -900), 6000);
        for (var frame = 0; frame < 20; frame += 1) {
          final watch = Stopwatch()..start();
          await tester.pump(const Duration(milliseconds: 16));
          frames.add(watch.elapsedMicroseconds);
        }
      }
      frames.sort();
      final median = frames[frames.length ~/ 2];
      final p95 = frames[(frames.length * 0.95).floor()];
      // Only the rows on screen and just beyond are built.
      expect(find.byType(Image).evaluate().length + find.byIcon(Icons.movie_outlined).evaluate().length, lessThan(200));
      final scrollable = tester.state<ScrollableState>(find.byType(Scrollable).first);
      expect(scrollable.position.pixels, greaterThan(20000));
      // Pages are loaded around where the flings went, not all of them.
      expect(source.requests.length, lessThan(source.items.length ~/ 200));
      // ignore: avoid_print
      print('photos grid: ${frames.length} frames over ${source.items.length} photos, '
          'median ${median / 1000} ms, p95 ${p95 / 1000} ms per frame (test binding, no raster), '
          '${source.requests.length} page loads, $thumbnailsAsked thumbnails asked for');
    });
  });
}
