part of 'main.dart';

// The Photos tab: every photo and video of the account on one timeline,
// newest first, laid out like a phone's gallery. The grid is in
// main_photos_grid.dart, the full-screen pager in main_photos_viewer.dart;
// the timeline and its thumbnails live in src/photos and outlast the tab.

/// "Today", "Yesterday", "Wed, 12 Nov" or "Wed, 12 Nov 2024".
String _photoDayLabel(String day, {DateTime? now}) {
  if (day == photoSortingKey) return appStrings.photosSortingSection;
  if (day == photoUnknownKey) return appStrings.photosUnknownDate;
  final date = DateTime.tryParse(day);
  if (date == null) return day;
  final today = now ?? DateTime.now();
  final todayDate = DateTime(today.year, today.month, today.day);
  final difference = todayDate.difference(DateTime(date.year, date.month, date.day)).inDays;
  if (difference == 0) return appStrings.photosToday;
  if (difference == 1) return appStrings.photosYesterday;
  final locale = currentAppLanguage.code;
  return date.year == today.year ? DateFormat.MMMEd(locale).format(date) : DateFormat.yMMMEd(locale).format(date);
}

/// The header of a section: a day, "November 2026" or "2026".
String _photoSectionLabel(PhotoZoom zoom, String key) {
  if (isPhotoSpecialKey(key)) return _photoDayLabel(key);
  switch (zoom) {
    case PhotoZoom.day:
      return _photoDayLabel(key);
    case PhotoZoom.month:
      final date = DateTime.tryParse('$key-01');
      return date == null ? key : DateFormat.yMMMM(currentAppLanguage.code).format(date);
    case PhotoZoom.year:
      return key;
  }
}

/// The scrubber's bubble: "Nov 2026".
String _photoMonthLabel(String key) {
  if (isPhotoSpecialKey(key)) return _photoDayLabel(key);
  final date = DateTime.tryParse('${key.length >= 7 ? key.substring(0, 7) : '$key-01'}-01');
  return date == null ? key : DateFormat.yMMM(currentAppLanguage.code).format(date);
}

/// "0:42" or "1:02:03".
String _photoDuration(double seconds) {
  final total = seconds.round();
  final hours = total ~/ 3600;
  final minutes = (total % 3600) ~/ 60;
  final rest = (total % 60).toString().padLeft(2, '0');
  return hours > 0 ? '$hours:${minutes.toString().padLeft(2, '0')}:$rest' : '$minutes:$rest';
}

class PhotosPage extends StatefulWidget {
  const PhotosPage({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<PhotosPage> createState() => _PhotosPageState();
}

class _PhotosPageState extends State<PhotosPage> {
  NeoCloudController get controller => widget.controller;
  PhotoLibrary get library => controller.photoLibrary;
  PhotoTimeline get timeline => library.timeline;

  /// Selected photo ids; selecting while not empty.
  final Set<String> _selected = <String>{};

  @override
  void initState() {
    super.initState();
    library.attach('${controller.client.baseUrl}|${controller.user?.id}');
    timeline.addListener(_changed);
    controller.filesRevision.addListener(_filesChanged);
    unawaited(timeline.refresh());
  }

  @override
  void dispose() {
    _refreshSoon?.cancel();
    timeline.removeListener(_changed);
    controller.filesRevision.removeListener(_filesChanged);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  /// Files changed (an upload, a delete, camera backup sending a batch):
  /// one refresh once it settles, not one per file.
  Timer? _refreshSoon;

  void _filesChanged() {
    _refreshSoon?.cancel();
    _refreshSoon = Timer(const Duration(milliseconds: 800), () => unawaited(timeline.refresh()));
  }

  bool get _selecting => _selected.isNotEmpty;

  void _setSelection(Set<String> ids) => setState(() {
    _selected
      ..clear()
      ..addAll(ids);
  });

  void _toggle(int index) {
    final item = timeline.itemAt(index);
    if (item == null) return;
    setState(() {
      if (!_selected.remove(item.id)) _selected.add(item.id);
    });
  }

  void _open(int index) {
    if (_selecting) {
      _toggle(index);
      return;
    }
    if (timeline.itemAt(index) == null) return;
    unawaited(showPhotoViewer(context, controller, index));
  }

  List<FileNode> _selectedNodes() {
    final nodes = <FileNode>[];
    for (var index = 0; index < timeline.total && nodes.length < _selected.length; index += 1) {
      final item = timeline.itemAt(index);
      if (item != null && _selected.contains(item.id)) nodes.add(item.toFileNode());
    }
    return nodes;
  }

  Future<void> _act(Future<void> Function(List<FileNode> nodes) action) async {
    final nodes = _selectedNodes();
    if (nodes.isEmpty) return;
    await action(nodes);
  }

  @override
  Widget build(BuildContext context) {
    final phone = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    final Widget body;
    if (!timeline.loaded) {
      body = timeline.error != null
          ? _PhotosMessage(
              icon: Icons.cloud_off_outlined,
              title: appStrings.photosLoadFailed,
              action: TextButton(onPressed: () => unawaited(timeline.refresh()), child: Text(appStrings.retry)),
            )
          : const Center(child: CircularProgressIndicator(strokeWidth: 2));
    } else if (timeline.total == 0) {
      body = RefreshIndicator(
        onRefresh: () => timeline.refresh(reloadItems: true),
        child: LayoutBuilder(
          builder: (context, constraints) => SingleChildScrollView(
            physics: const AlwaysScrollableScrollPhysics(),
            child: SizedBox(height: constraints.maxHeight, child: _PhotosEmpty(controller: controller)),
          ),
        ),
      );
    } else {
      body = PhotoGrid(
        timeline: timeline,
        thumbnails: library.thumbnails,
        selected: _selected,
        onOpen: _open,
        onToggle: _toggle,
        onSelect: _setSelection,
        onRefresh: () => timeline.refresh(reloadItems: true),
        onZoom: (zoom) => setState(() => timeline.zoom = zoom),
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        AnimatedSwitcher(
          duration: const Duration(milliseconds: 160),
          child: _selecting
              ? _PhotoSelectionBar(
                  key: const ValueKey<String>('selection'),
                  count: _selected.length,
                  allFavorite: _selectedNodes().every((node) => node.favorite),
                  onClear: () => _setSelection(const <String>{}),
                  onShare: () => unawaited(_act((nodes) => _sharePhotos(context, controller, nodes))),
                  onDownload: () => unawaited(_act((nodes) => _downloadPhotos(controller, nodes))),
                  onFavorite: () => unawaited(_act((nodes) => _favoritePhotos(controller, nodes))),
                  onMove: () => unawaited(_act((nodes) async {
                    if (await _movePhotos(context, controller, nodes)) _setSelection(const <String>{});
                  })),
                  onDelete: () => unawaited(_act((nodes) async {
                    _setSelection(const <String>{});
                    await _trashPhotos(controller, nodes);
                  })),
                )
              : _PhotosHeader(key: const ValueKey<String>('header'), controller: controller, timeline: timeline, phone: phone),
        ),
        Expanded(child: body),
      ],
    );
  }
}

/// Above the grid: the title and camera backup's status on wider screens
/// (phones show the status in the top bar), and a note while new photos
/// are still being sorted by date.
class _PhotosHeader extends StatelessWidget {
  const _PhotosHeader({super.key, required this.controller, required this.timeline, required this.phone});

  final NeoCloudController controller;
  final PhotoTimeline timeline;
  final bool phone;

  @override
  Widget build(BuildContext context) {
    final pending = timeline.summary.pending;
    final note = pending > 0 ? Text(appStrings.photosSorting(pending), style: TextStyle(color: _textMuted, fontSize: 12.5)) : null;
    if (phone) {
      return note == null ? const SizedBox.shrink() : Padding(padding: const EdgeInsets.fromLTRB(16, 0, 16, 8), child: note);
    }
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 20, 24, 8),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Text(
                  appStrings.photosNav,
                  style: GoogleFonts.geist(fontSize: 22, fontWeight: FontWeight.w700, color: _textPrimary, letterSpacing: -0.5),
                ),
                if (note != null) Padding(padding: const EdgeInsets.only(top: 4), child: note),
              ],
            ),
          ),
          _PhotosBackupStatus(controller: controller),
        ],
      ),
    );
  }
}

/// Camera backup at a glance, where the Camera tab used to be: "Backup is
/// off · Turn on", or what it is doing. Opens Settings › Camera upload.
class _PhotosBackupStatus extends StatelessWidget {
  const _PhotosBackupStatus({required this.controller});

  final NeoCloudController controller;

  void _open() {
    controller.setSettingsPage(SettingsPage.cameraUpload);
    controller.setSelectedSection(AppSection.settings);
  }

  @override
  Widget build(BuildContext context) {
    final camera = controller.cameraUploads;
    if (!camera.supported) return const SizedBox.shrink();
    return ListenableBuilder(
      listenable: camera,
      builder: (context, _) {
        if (!camera.settings.enabled) {
          return _BackupPill(dot: _textMuted, label: appStrings.photosBackupOff, action: appStrings.photosBackupTurnOn, onTap: _open);
        }
        final (label, color) = _cameraStatusLabel(camera.status);
        return _BackupPill(dot: color, label: label, onTap: _open);
      },
    );
  }
}

class _BackupPill extends StatelessWidget {
  const _BackupPill({required this.dot, required this.label, required this.onTap, this.action});

  final Color dot;
  final String label;
  final String? action;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(AppRadius.pill);
    return InkWell(
      key: const ValueKey<String>('photos-backup-status'),
      borderRadius: radius,
      onTap: onTap,
      child: Container(
        constraints: const BoxConstraints(maxWidth: 200),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(color: _bgTertiary, borderRadius: radius, border: Border.all(color: _border)),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Icon(Icons.cloud_upload_outlined, size: 15, color: _textMuted),
            const SizedBox(width: 6),
            Container(width: 6, height: 6, decoration: BoxDecoration(color: dot, shape: BoxShape.circle)),
            const SizedBox(width: 6),
            Flexible(child: Text(label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 12))),
            if (action != null) ...<Widget>[
              const SizedBox(width: 8),
              Text(action!, style: TextStyle(color: _accent, fontSize: 12, fontWeight: FontWeight.w600)),
            ],
          ],
        ),
      ),
    );
  }
}

/// Replaces the header while photos are selected.
class _PhotoSelectionBar extends StatelessWidget {
  const _PhotoSelectionBar({
    super.key,
    required this.count,
    required this.allFavorite,
    required this.onClear,
    required this.onShare,
    required this.onDownload,
    required this.onFavorite,
    required this.onMove,
    required this.onDelete,
  });

  final int count;
  final bool allFavorite;
  final VoidCallback onClear;
  final VoidCallback onShare;
  final VoidCallback onDownload;
  final VoidCallback onFavorite;
  final VoidCallback onMove;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(4, 2, 4, 2),
      decoration: BoxDecoration(color: _bgSecondary, border: Border(bottom: BorderSide(color: _border))),
      child: Row(
        children: <Widget>[
          IconButton(tooltip: appStrings.cancel, onPressed: onClear, icon: Icon(Icons.close_rounded, color: _textPrimary)),
          Expanded(
            child: Text(
              appStrings.selectedCount(count),
              key: const ValueKey<String>('photos-selected-count'),
              style: GoogleFonts.geist(fontSize: 15, fontWeight: FontWeight.w600, color: _textPrimary),
            ),
          ),
          IconButton(tooltip: appStrings.shareLink, onPressed: onShare, icon: Icon(Icons.link_rounded, color: _textSecondary)),
          IconButton(
            tooltip: allFavorite ? appStrings.filesUnstar : appStrings.filesStar,
            onPressed: onFavorite,
            icon: Icon(allFavorite ? Icons.star_rounded : Icons.star_outline_rounded, color: allFavorite ? _accent : _textSecondary),
          ),
          IconButton(tooltip: appStrings.download, onPressed: onDownload, icon: Icon(Icons.download_rounded, color: _textSecondary)),
          IconButton(tooltip: appStrings.moveTo, onPressed: onMove, icon: Icon(Icons.drive_file_move_outline, color: _textSecondary)),
          IconButton(tooltip: appStrings.moveToTrash, onPressed: onDelete, icon: Icon(Icons.delete_outline_rounded, color: _textSecondary)),
        ],
      ),
    );
  }
}

/// No photos yet: what shows up here, and the way to get some in (camera
/// backup on a phone, Files elsewhere).
class _PhotosEmpty extends StatelessWidget {
  const _PhotosEmpty({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final camera = controller.cameraUploads.supported;
    return _PhotosMessage(
      icon: Icons.photo_library_outlined,
      title: appStrings.photosEmptyTitle,
      body: appStrings.photosEmptyBody,
      action: camera
          ? FilledButton.icon(
              onPressed: () {
                controller.setSettingsPage(SettingsPage.cameraUpload);
                controller.setSelectedSection(AppSection.settings);
              },
              icon: const Icon(Icons.cloud_upload_outlined, size: 18),
              label: Text(appStrings.photosEmptyBackup),
            )
          : OutlinedButton(onPressed: () => controller.setSelectedSection(AppSection.files), child: Text(appStrings.photosEmptyOpenFiles)),
    );
  }
}

class _PhotosMessage extends StatelessWidget {
  const _PhotosMessage({required this.icon, required this.title, this.body, this.action});

  final IconData icon;
  final String title;
  final String? body;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 360),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(icon, size: 44, color: _textMuted),
              const SizedBox(height: 14),
              Text(title, textAlign: TextAlign.center, style: GoogleFonts.geist(fontSize: 17, fontWeight: FontWeight.w600, color: _textPrimary)),
              if (body != null) ...<Widget>[
                const SizedBox(height: 8),
                Text(body!, textAlign: TextAlign.center, style: TextStyle(color: _textSecondary, height: 1.45)),
              ],
              if (action != null) ...<Widget>[const SizedBox(height: 20), action!],
            ],
          ),
        ),
      ),
    );
  }
}

// --- Actions on photos (the same calls the file actions make) ---------------------

Future<void> _sharePhotos(BuildContext context, NeoCloudController controller, List<FileNode> nodes) async {
  if (nodes.length != 1) {
    ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.photosShareOne)));
    return;
  }
  await showShareLinkDialog(context, controller, nodes.single);
}

Future<void> _downloadPhotos(NeoCloudController controller, List<FileNode> nodes) async {
  if (nodes.length == 1) {
    await controller.downloadFile(nodes.single);
  } else {
    await controller.downloadZip(nodes);
  }
}

Future<void> _favoritePhotos(NeoCloudController controller, List<FileNode> nodes) async {
  final favorite = !nodes.every((node) => node.favorite);
  final timeline = controller.photoLibrary.timeline;
  timeline.setFavorite(nodes.map((node) => node.id).toSet(), favorite);
  final failures = await runBatch(nodes, (node) => controller.client.setFavorite(node.id, favorite));
  if (failures.isNotEmpty) timeline.setFavorite(failures.map((failure) => failure.item.id).toSet(), !favorite);
  final done = nodes.length - failures.length;
  if (done > 0) controller.undo.show(UndoEntry(message: favorite ? appStrings.filesStarred(done) : appStrings.filesUnstarred(done)));
}

/// Moves the photos to a folder; Undo moves them back. True when moved.
Future<bool> _movePhotos(BuildContext context, NeoCloudController controller, List<FileNode> nodes) async {
  final target = await showFolderPicker(context, controller, hideVaults: true);
  if (target == null) return false;
  final moving = nodes.where((node) => node.parentId != target.$1).toList();
  final failures = await runBatch(moving, (node) => controller.client.updateNode(node.id, parentId: target.$1));
  final done = moving.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
  if (done.isNotEmpty) {
    controller.undo.show(UndoEntry(
      message: appStrings.undoMoved(done.length, target.$2),
      onUndo: () async {
        await runBatch(done, (node) => controller.client.updateNode(node.id, parentId: node.parentId ?? 'root'));
        controller.notifyFilesChanged();
      },
    ));
    controller.notifyFilesChanged();
  }
  return true;
}

/// Moves the photos to the trash right away; Undo restores them.
Future<void> _trashPhotos(NeoCloudController controller, List<FileNode> nodes) async {
  final failures = await runBatch(nodes, (node) => controller.client.trashNode(node.id));
  final done = nodes.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
  if (done.isEmpty) return;
  controller.undo.show(UndoEntry(
    message: appStrings.undoTrashed(done.length, done.first.name),
    onUndo: () async {
      await runBatch(done, (node) => controller.client.restoreFromTrash(node.id));
      controller.notifyFilesChanged();
      await controller.photoLibrary.timeline.refresh();
    },
  ));
  controller.notifyFilesChanged();
  // Gone from the grid at once, not after the change settles.
  await controller.photoLibrary.timeline.refresh();
}
