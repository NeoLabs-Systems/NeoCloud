part of 'main.dart';

// Browsing one cloud storage: a folder at a time with breadcrumbs, upload,
// new folder, rename, delete (at the provider), download and copying files
// to and from NeoCloud. A full page opened from Home (main_cloud_storage_strip.dart).

/// Files larger than this are not uploaded from the app yet: the bytes are held in memory.
const int _cloudUploadLimit = 256 * 1024 * 1024;

Future<void> openCloudStorage(BuildContext context, NeoCloudController controller, ExternalMount mount) {
  return Navigator.of(context).push<void>(MaterialPageRoute<void>(builder: (_) => CloudStorageBrowser(controller: controller, mount: mount)));
}

class CloudStorageBrowser extends StatefulWidget {
  const CloudStorageBrowser({super.key, required this.controller, required this.mount});

  final NeoCloudController controller;
  final ExternalMount mount;

  @override
  State<CloudStorageBrowser> createState() => _CloudStorageBrowserState();
}

class _CloudStorageBrowserState extends State<CloudStorageBrowser> {
  String _path = '';
  ExternalListing? _listing;
  String? _error;
  bool _busy = false;

  NeoCloudController get _controller => widget.controller;
  ExternalMount get _mount => widget.mount;
  BackendClient get _client => _controller.client;
  bool get _canWrite => !_mount.readOnly;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  String _child(String name) => _path.isEmpty ? name : '$_path/$name';

  Future<void> _load([String? path]) async {
    final target = path ?? _path;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final listing = await _client.listExternal(_mount.id, target);
      if (mounted) {
        setState(() {
          _path = target;
          _listing = listing;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = _cloudError(_controller, error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Runs a change at the provider, then shows the folder again.
  Future<void> _change(Future<void> Function() action, {String? done}) async {
    setState(() => _busy = true);
    try {
      await action();
      if (done != null && mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(done)));
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = _cloudError(_controller, error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _newFolder() async {
    final name = await _promptText(context, title: appStrings.newFolder, label: appStrings.folderName, confirmLabel: appStrings.create);
    if (name != null) await _change(() => _client.createExternalFolder(_mount.id, _child(name)));
  }

  Future<void> _upload() async {
    final result = await FilePicker.platform.pickFiles(allowMultiple: true, withData: true);
    if (result == null) return;
    await _change(() async {
      for (final file in result.files) {
        final bytes = file.bytes;
        if (bytes == null) continue;
        if (bytes.length > _cloudUploadLimit) throw ApiException(413, appStrings.cloudUploadTooLarge(formatBytes(_cloudUploadLimit)));
        await _client.uploadExternal(_mount.id, _child(file.name), bytes);
      }
    }, done: appStrings.cloudUploaded);
  }

  Future<void> _rename(ExternalItem item) async {
    final name = await _promptText(context, title: appStrings.rename, label: appStrings.folderName, confirmLabel: appStrings.rename, initialValue: item.name, selectStem: !item.isFolder);
    if (name == null || name == item.name) return;
    final folder = item.path.contains('/') ? item.path.substring(0, item.path.lastIndexOf('/')) : '';
    await _change(() => _client.moveExternal(_mount.id, item.path, folder.isEmpty ? name : '$folder/$name'));
  }

  Future<void> _delete(ExternalItem item) => _confirmDelete(
    context,
    title: appStrings.cloudDeleteTitle(item.name),
    message: appStrings.cloudDeleteMessage,
    onConfirm: () => _change(() => _client.deleteExternal(_mount.id, item.path)),
  );

  Future<void> _download(ExternalItem item) =>
      startDownload(_client.externalFileUri(_mount.id, item.path, download: true), fileName: item.name, headers: _client.mediaHeaders);

  Future<void> _copyToNeoCloud(ExternalItem item) async {
    final target = await showFolderPicker(context, _controller, title: appStrings.cloudCopyToNeoCloud, confirmLabel: appStrings.cloudCopyHere, hideVaults: true);
    if (target == null) return;
    await _change(() async {
      await _client.copyFromExternal(_mount.id, path: item.path, parentId: target.$1);
      _controller.notifyFilesChanged();
    }, done: appStrings.cloudCopied);
  }

  Future<void> _copyFromNeoCloud() async {
    final node = await _pickNeoCloudFile(context, _controller);
    if (node == null) return;
    await _change(() => _client.copyToExternal(_mount.id, nodeId: node.id, folder: _path), done: appStrings.cloudCopied);
  }

  List<PopupMenuEntry<VoidCallback>> _menu(ExternalItem item) => <PopupMenuEntry<VoidCallback>>[
    if (!item.isFolder) PopupMenuItem<VoidCallback>(value: () => unawaited(_download(item)), child: Text(appStrings.download)),
    if (!item.isFolder) PopupMenuItem<VoidCallback>(value: () => unawaited(_copyToNeoCloud(item)), child: Text(appStrings.cloudCopyToNeoCloud)),
    if (_canWrite) PopupMenuItem<VoidCallback>(value: () => unawaited(_rename(item)), child: Text(appStrings.rename)),
    if (_canWrite) PopupMenuItem<VoidCallback>(value: () => unawaited(_delete(item)), child: Text(appStrings.delete)),
  ];

  Widget _crumbs() {
    final parts = _path.isEmpty ? const <String>[] : _path.split('/');
    return Wrap(
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        TextButton(onPressed: () => unawaited(_load('')), child: Text(_mount.name)),
        for (var index = 0; index < parts.length; index++) ...<Widget>[
          Icon(Icons.chevron_right_rounded, size: 18, color: _textMuted),
          TextButton(onPressed: () => unawaited(_load(parts.take(index + 1).join('/'))), child: Text(parts[index])),
        ],
      ],
    );
  }

  Widget _tile(ExternalItem item) {
    final locale = currentAppLanguage.code;
    final subtitle = item.isFolder ? formatDate(item.modified, locale) : '${formatBytes(item.size)} · ${formatDate(item.modified, locale)}';
    final kind = item.isFolder ? FileKind.folder : fileKindOf(FileNode(id: item.path, parentId: null, type: 'file', name: item.name, size: item.size, mime: item.mime, etag: '', version: 1, modifiedAt: item.modified));
    return ListTile(
      key: ValueKey<String>('cloud-${item.path}'),
      leading: Icon(fileKindIcon(kind), color: item.isFolder ? _accent : _textSecondary),
      title: Text(item.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: Text(subtitle, style: TextStyle(color: _textMuted, fontSize: 12.5)),
      onTap: () => item.isFolder ? unawaited(_load(item.path)) : unawaited(_download(item)),
      trailing: PopupMenuButton<VoidCallback>(onSelected: (action) => action(), itemBuilder: (_) => _menu(item)),
    );
  }

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    return Scaffold(
      backgroundColor: _bgPrimary,
      appBar: AppBar(
        title: Row(children: <Widget>[Flexible(child: Text(_mount.name, overflow: TextOverflow.ellipsis)), if (_mount.readOnly) ...<Widget>[const SizedBox(width: 8), _SettingsTag(label: appStrings.cloudReadOnly)]]),
        actions: <Widget>[
          IconButton(tooltip: appStrings.refresh, onPressed: _busy ? null : () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded)),
          if (_canWrite) IconButton(tooltip: appStrings.newFolder, onPressed: _busy ? null : () => unawaited(_newFolder()), icon: const Icon(Icons.create_new_folder_outlined)),
          if (_canWrite) IconButton(tooltip: appStrings.cloudCopyFromNeoCloud, onPressed: _busy ? null : () => unawaited(_copyFromNeoCloud()), icon: const Icon(Icons.drive_file_move_outline)),
          if (_canWrite) IconButton(tooltip: appStrings.uploadFiles, onPressed: _busy ? null : () => unawaited(_upload()), icon: const Icon(Icons.upload_rounded)),
        ],
      ),
      body: Align(
        alignment: Alignment.topCenter,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: kContentMaxWidth),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Padding(padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8), child: _crumbs()),
              if (_busy) const LinearProgressIndicator(minHeight: 2),
              if (_error != null) Padding(padding: const EdgeInsets.all(12), child: _InlineError(message: _error!, onDismiss: () => setState(() => _error = null))),
              Expanded(
                child: listing == null
                    ? (_busy || _error != null ? const SizedBox.shrink() : const Center(child: CircularProgressIndicator()))
                    : listing.items.isEmpty
                        ? Center(child: Text(appStrings.cloudFolderEmpty, style: TextStyle(color: _textMuted)))
                        : ListView(
                            children: <Widget>[
                              for (final item in listing.items) _tile(item),
                              if (listing.truncated) Padding(padding: const EdgeInsets.all(16), child: Text(appStrings.cloudTruncated(listing.items.length), style: TextStyle(color: _textMuted))),
                            ],
                          ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// A small browser over NeoCloud's own files to choose one to copy; vault files are left out.
Future<FileNode?> _pickNeoCloudFile(BuildContext context, NeoCloudController controller) {
  return showAdaptiveSheet<FileNode>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(appStrings.cloudPickFileTitle),
      content: SizedBox(width: 460, height: 380, child: _NeoCloudFilePicker(controller: controller, onPick: (node) => Navigator.of(dialogContext).pop(node))),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
    ),
  );
}

class _NeoCloudFilePicker extends StatefulWidget {
  const _NeoCloudFilePicker({required this.controller, required this.onPick});

  final NeoCloudController controller;
  final ValueChanged<FileNode> onPick;

  @override
  State<_NeoCloudFilePicker> createState() => _NeoCloudFilePickerState();
}

class _NeoCloudFilePickerState extends State<_NeoCloudFilePicker> {
  String _folderId = 'root';
  FolderListing? _listing;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final listing = await widget.controller.client.listFolder(_folderId);
      if (mounted) setState(() => _listing = listing);
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  @override
  Widget build(BuildContext context) {
    final listing = _listing;
    if (_error != null) return _LoadError(message: _error!, onRetry: () => unawaited(_load()));
    if (listing == null) return const Center(child: CircularProgressIndicator());
    final parent = listing.folder.parentId;
    return ListView(
      children: <Widget>[
        if (parent != null && _folderId != 'root')
          ListTile(
            leading: const Icon(Icons.arrow_upward_rounded),
            title: Text(listing.path.length > 1 ? listing.path[listing.path.length - 2].name : '/'),
            onTap: () {
              _folderId = parent;
              unawaited(_load());
            },
          ),
        for (final node in listing.items.where((node) => !node.inVault))
          ListTile(
            leading: Icon(node.isFolder ? Icons.folder_outlined : Icons.insert_drive_file_outlined),
            title: Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis),
            onTap: () {
              if (node.isFolder) {
                _folderId = node.id;
                unawaited(_load());
              } else {
                widget.onPick(node);
              }
            },
          ),
      ],
    );
  }
}
