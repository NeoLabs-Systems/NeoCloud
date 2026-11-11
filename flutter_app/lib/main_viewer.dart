part of 'main.dart';

/// Files up to this size are fetched whole for an in-app preview.
const int _previewLimitBytes = 100 * 1024 * 1024;

/// How the viewer shows a file. New kinds plug in at [_viewerKindOf] and
/// [_FileViewerState._previewBody].
enum _ViewerKind { image, text, pdf, office, video, audio, none }

_ViewerKind _viewerKindOf(FileNode node) {
  switch (fileKindOf(node)) {
    case FileKind.image:
      return _ViewerKind.image;
    case FileKind.text:
    case FileKind.code:
      return _ViewerKind.text;
    case FileKind.pdf:
      return _ViewerKind.pdf;
    case FileKind.document:
    case FileKind.spreadsheet:
    case FileKind.presentation:
      return _ViewerKind.office;
    case FileKind.video:
      return _ViewerKind.video;
    case FileKind.audio:
      return _ViewerKind.audio;
    case FileKind.folder:
    case FileKind.archive:
      return _ViewerKind.none;
    case FileKind.other:
      return isEditableText(node) ? _ViewerKind.text : _ViewerKind.none;
  }
}

/// The built-in viewer: images, text, PDF, Office (via the server's PDF
/// rendition), video and audio. Arrow keys and the side buttons step through
/// the other files of the folder. Text, Markdown and code files can be
/// edited in place. Vault files are decrypted in the app; their media can't
/// stream and the server makes no previews of them.
Future<void> showFileViewer(BuildContext context, NeoCloudController controller, List<FileNode> files, int index) {
  if (index < 0 || files.isEmpty) return Future<void>.value();
  return showDialog<void>(
    context: context,
    barrierColor: Colors.black.withValues(alpha: 0.72),
    builder: (_) => _FileViewer(controller: controller, files: files, initialIndex: index),
  );
}

class _FileViewer extends StatefulWidget {
  const _FileViewer({required this.controller, required this.files, required this.initialIndex});

  final NeoCloudController controller;
  final List<FileNode> files;
  final int initialIndex;

  @override
  State<_FileViewer> createState() => _FileViewerState();
}

class _FileViewerState extends State<_FileViewer> {
  late int _index = widget.initialIndex;
  late final List<FileNode> _files = List<FileNode>.of(widget.files);
  final Map<String, Future<Uint8List>> _cache = <String, Future<Uint8List>>{};
  final FocusNode _editorFocus = FocusNode(debugLabel: 'editor');
  TextEditingController? _editor;
  String _savedText = '';
  bool _saving = false;
  String? _editError;
  String? _editNotice;

  FileNode get _file => _files[_index];
  NeoCloudController get _controller => widget.controller;
  bool get _editing => _editor != null;
  bool get _dirty => _editing && _editor!.text != _savedText;

  @override
  void initState() {
    super.initState();
    _controller.markOpened(_file);
  }

  @override
  void dispose() {
    _editor?.dispose();
    _editorFocus.dispose();
    super.dispose();
  }

  Future<void> _step(int delta) async {
    final next = _index + delta;
    if (next < 0 || next >= _files.length) return;
    if (!await _leaveEditor()) return;
    setState(() => _index = next);
    _controller.markOpened(_file);
  }

  Future<Uint8List> _bytesOf(FileNode file, {bool rendition = false}) {
    return _cache.putIfAbsent(
      '${file.etag}-$rendition',
      () => rendition ? _controller.client.previewPdfBytes(file.id) : _controller.fileContent(file),
    );
  }

  // Editing

  bool get _canEdit => isEditableText(_file) && _file.trashedAt == null;

  Future<void> _startEditing() async {
    try {
      final text = utf8.decode(await _bytesOf(_file), allowMalformed: true);
      if (!mounted) return;
      setState(() {
        _savedText = text;
        _editor = TextEditingController(text: text)..addListener(_onEdited);
        _editError = null;
        _editNotice = null;
      });
    } catch (error) {
      if (mounted) setState(() => _editError = _controller.friendlyError(error));
    }
  }

  void _onEdited() => setState(() => _editNotice = null);

  /// Ends editing, asking first when there are unsaved changes. False when
  /// the user chose to keep editing.
  Future<bool> _leaveEditor() async {
    if (_dirty && !await _confirmDiscardChanges(context)) return false;
    final editor = _editor;
    if (editor != null && mounted) {
      setState(() {
        _editor = null;
        _editError = null;
      });
      editor.dispose();
    }
    return true;
  }

  Future<void> _close() async {
    if (!await _leaveEditor() || !mounted) return;
    Navigator.of(context).pop();
  }

  /// Uploads the text as a new version, guarded by the etag the editor
  /// started from; a clash asks before overwriting.
  Future<void> _save({bool overwrite = false}) async {
    final editor = _editor;
    if (editor == null || _saving) return;
    final file = _file;
    final text = editor.text;
    final bytes = Uint8List.fromList(utf8.encode(text));
    setState(() {
      _saving = true;
      _editError = null;
    });
    try {
      final saved = await _controller.saveFileContent(file, bytes, baseEtag: overwrite ? null : file.etag);
      if (!mounted) return;
      setState(() {
        _files[_index] = saved;
        _cache['${saved.etag}-false'] = Future<Uint8List>.value(bytes);
        _savedText = text;
        _editNotice = appStrings.editorSaved;
      });
    } on ApiException catch (error) {
      if (!mounted) return;
      if (error.code == 'ETAG_MISMATCH') {
        setState(() => _saving = false);
        if (await _confirmOverwrite(context, file.name)) await _save(overwrite: true);
        return;
      }
      setState(() => _editError = _controller.friendlyError(error));
    } catch (error) {
      if (mounted) setState(() => _editError = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Widget _withBytes(Future<Uint8List> future, Widget Function(Uint8List data) builder) {
    return FutureBuilder<Uint8List>(
      future: future,
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          final error = snapshot.error;
          if (error is ApiException && error.code == 'PREVIEW_UNAVAILABLE') {
            return _NoPreview(file: _file, message: error.message, onDownload: _download);
          }
          return Center(child: Padding(padding: const EdgeInsets.all(24), child: _InlineError(message: _controller.friendlyError(error!))));
        }
        final data = snapshot.data;
        if (data == null) return const Center(child: CircularProgressIndicator(strokeWidth: 2));
        return builder(data);
      },
    );
  }

  Widget _pdf(Uint8List data, FileNode file) {
    return Container(
      color: _bgSecondary,
      child: PdfViewer.data(
        data,
        sourceName: '${file.id}-${file.etag}',
        params: PdfViewerParams(backgroundColor: _bgSecondary, margin: 12),
      ),
    );
  }

  Widget _previewBody() {
    final file = _file;
    final editor = _editor;
    if (editor != null) return _TextEditorPane(controller: editor, focusNode: _editorFocus, readOnly: _saving);
    final kind = _viewerKindOf(file);
    final tooLarge = file.size > _previewLimitBytes;
    switch (kind) {
      case _ViewerKind.video:
      case _ViewerKind.audio:
        // Vault content is ciphertext on the server, so it can't stream.
        if (file.inVault) return _NoPreview(file: file, message: appStrings.vaultNoMediaPreview, onDownload: _download);
        return _MediaPlayer(
          key: ValueKey<String>(file.id),
          uri: _controller.client.contentUri(file.id),
          headers: _controller.client.mediaHeaders,
          audioOnly: kind == _ViewerKind.audio,
          file: file,
          onDownload: _download,
        );
      case _ViewerKind.image:
        if (tooLarge) break;
        return _withBytes(
          _bytesOf(file),
          (data) => InteractiveViewer(
            maxScale: 8,
            child: Center(
              child: Image.memory(
                data,
                fit: BoxFit.contain,
                errorBuilder: (_, _, _) => _NoPreview(file: file, message: appStrings.noPreview, onDownload: _download),
              ),
            ),
          ),
        );
      case _ViewerKind.text:
        if (tooLarge) break;
        return _withBytes(
          _bytesOf(file),
          (data) => Container(
            color: _bgSecondary,
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(20),
              child: SelectableText(
                utf8.decode(data, allowMalformed: true),
                style: GoogleFonts.geistMono(fontSize: 13, height: 1.5, color: _textPrimary),
              ),
            ),
          ),
        );
      case _ViewerKind.pdf:
        if (tooLarge) break;
        return _withBytes(_bytesOf(file), (data) => _pdf(data, file));
      case _ViewerKind.office:
        if (!_controller.officePreview || file.inVault) {
          return _NoPreview(file: file, message: appStrings.officePreviewUnavailable, onDownload: _download);
        }
        return _withBytes(_bytesOf(file, rendition: true), (data) => _pdf(data, file));
      case _ViewerKind.none:
        return _NoPreview(file: file, message: appStrings.noPreview, onDownload: _download);
    }
    return _NoPreview(file: file, message: appStrings.previewTooLarge, onDownload: _download);
  }

  void _download() {
    unawaited(_controller.downloadFile(_file).catchError((Object error) {
      if (mounted) setState(() => _editError = _controller.friendlyError(error));
    }));
  }

  List<Widget> _headerActions() {
    if (_editing) {
      return <Widget>[
        if (_dirty)
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: _StatusPill(label: appStrings.editorUnsaved, color: _warning),
          )
        else if (_editNotice != null)
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: _StatusPill(label: _editNotice!, color: _success),
          ),
        TextButton(onPressed: _saving ? null : () => unawaited(_leaveEditor()), child: Text(appStrings.editorDone)),
        const SizedBox(width: 6),
        FilledButton.icon(
          onPressed: _saving || !_dirty ? null : () => unawaited(_save()),
          icon: _saving
              ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.save_outlined, size: 16),
          label: Text(appStrings.save),
        ),
        const SizedBox(width: 4),
      ];
    }
    return <Widget>[
      if (_canEdit)
        OutlinedButton.icon(
          onPressed: () => unawaited(_startEditing()),
          icon: const Icon(Icons.edit_outlined, size: 16),
          label: Text(appStrings.edit),
        ),
      IconButton(tooltip: appStrings.download, onPressed: _download, icon: const Icon(Icons.download_rounded)),
    ];
  }

  Map<ShortcutActivator, VoidCallback> get _shortcuts {
    if (_editing) {
      return <ShortcutActivator, VoidCallback>{
        const SingleActivator(LogicalKeyboardKey.keyS, control: true): () => unawaited(_save()),
        const SingleActivator(LogicalKeyboardKey.keyS, meta: true): () => unawaited(_save()),
      };
    }
    return <ShortcutActivator, VoidCallback>{
      const SingleActivator(LogicalKeyboardKey.arrowLeft): () => unawaited(_step(-1)),
      const SingleActivator(LogicalKeyboardKey.arrowRight): () => unawaited(_step(1)),
    };
  }

  @override
  Widget build(BuildContext context) {
    final file = _file;
    final size = MediaQuery.sizeOf(context);
    return PopScope(
      canPop: !_dirty,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) unawaited(_close());
      },
      child: CallbackShortcuts(
        bindings: _shortcuts,
        child: Focus(
          autofocus: true,
          child: Dialog(
            insetPadding: EdgeInsets.all(size.width < 700 ? 8 : 32),
            backgroundColor: _bgCard,
            child: SizedBox(
              width: math.min(1200, size.width),
              height: size.height,
              child: Column(
                children: <Widget>[
                  Padding(
                    padding: const EdgeInsets.fromLTRB(18, 10, 8, 10),
                    child: Row(
                      children: <Widget>[
                        _FileIcon(node: file, size: 30),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Text(file.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w700, color: _textPrimary)),
                              Text(
                                '${formatBytes(file.size)} · ${formatDate(file.modifiedAt, currentAppLanguage.code)}',
                                style: TextStyle(color: _textMuted, fontSize: 12),
                              ),
                            ],
                          ),
                        ),
                        ..._headerActions(),
                        IconButton(tooltip: appStrings.close, onPressed: () => unawaited(_close()), icon: const Icon(Icons.close_rounded)),
                      ],
                    ),
                  ),
                  if (_editError != null)
                    Padding(
                      padding: const EdgeInsets.fromLTRB(18, 0, 18, 10),
                      child: _InlineError(message: _editError!, onDismiss: () => setState(() => _editError = null)),
                    ),
                  Divider(height: 1, color: _border),
                  Expanded(
                    child: Row(
                      children: <Widget>[
                        IconButton(
                          tooltip: '←',
                          onPressed: _index > 0 && !_editing ? () => unawaited(_step(-1)) : null,
                          icon: const Icon(Icons.chevron_left_rounded, size: 30),
                        ),
                        Expanded(
                          child: ClipRRect(
                            borderRadius: BorderRadius.circular(12),
                            child: KeyedSubtree(key: ValueKey<String>('${file.id}-$_editing'), child: _previewBody()),
                          ),
                        ),
                        IconButton(
                          tooltip: '→',
                          onPressed: _index < _files.length - 1 && !_editing ? () => unawaited(_step(1)) : null,
                          icon: const Icon(Icons.chevron_right_rounded, size: 30),
                        ),
                      ],
                    ),
                  ),
                  const SizedBox(height: 12),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Plays video or audio straight from the server; the player asks for byte
/// ranges, so seeking works without loading the whole file.
class _MediaPlayer extends StatefulWidget {
  const _MediaPlayer({
    super.key,
    required this.uri,
    required this.headers,
    required this.audioOnly,
    required this.file,
    required this.onDownload,
  });

  final VoidCallback onDownload;
  final Uri uri;
  final Map<String, String> headers;
  final bool audioOnly;
  final FileNode file;

  @override
  State<_MediaPlayer> createState() => _MediaPlayerState();
}

class _MediaPlayerState extends State<_MediaPlayer> {
  late final VideoPlayerController _player = VideoPlayerController.networkUrl(widget.uri, httpHeaders: widget.headers);
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _player.addListener(_onTick);
    _player.initialize().then((_) {
      if (mounted) unawaited(_player.play());
    }).catchError((Object _) {
      if (mounted) setState(() => _failed = true);
    });
  }

  void _onTick() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _player.removeListener(_onTick);
    unawaited(_player.dispose());
    super.dispose();
  }

  String _time(Duration value) {
    final minutes = value.inMinutes.remainder(60).toString().padLeft(2, '0');
    final seconds = value.inSeconds.remainder(60).toString().padLeft(2, '0');
    return value.inHours > 0 ? '${value.inHours}:$minutes:$seconds' : '$minutes:$seconds';
  }

  @override
  Widget build(BuildContext context) {
    if (_failed || _player.value.hasError) {
      return _NoPreview(file: widget.file, message: appStrings.mediaUnavailable, onDownload: widget.onDownload);
    }
    final value = _player.value;
    if (!value.isInitialized) return const Center(child: CircularProgressIndicator(strokeWidth: 2));
    final total = value.duration.inMilliseconds.toDouble();
    final position = value.position.inMilliseconds.toDouble().clamp(0, total > 0 ? total : 0).toDouble();
    return Container(
      color: Colors.black,
      child: Column(
        children: <Widget>[
          Expanded(
            child: Center(
              child: widget.audioOnly
                  ? _FileIcon(node: widget.file, size: 120)
                  : AspectRatio(aspectRatio: value.aspectRatio, child: VideoPlayer(_player)),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 4, 16, 8),
            child: Row(
              children: <Widget>[
                IconButton(
                  tooltip: value.isPlaying ? appStrings.pause : appStrings.play,
                  onPressed: () => value.isPlaying ? _player.pause() : _player.play(),
                  icon: Icon(value.isPlaying ? Icons.pause_rounded : Icons.play_arrow_rounded, color: Colors.white),
                ),
                Expanded(
                  child: Slider(
                    value: position,
                    max: total > 0 ? total : 1,
                    activeColor: _accent,
                    onChanged: total > 0 ? (ms) => _player.seekTo(Duration(milliseconds: ms.round())) : null,
                  ),
                ),
                Text('${_time(value.position)} / ${_time(value.duration)}', style: const TextStyle(color: Colors.white70, fontSize: 12)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _NoPreview extends StatelessWidget {
  const _NoPreview({required this.file, required this.message, required this.onDownload});

  final FileNode file;
  final String message;
  final VoidCallback onDownload;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            _FileIcon(node: file, size: 72),
            const SizedBox(height: 16),
            Text(message, textAlign: TextAlign.center, style: TextStyle(color: _textSecondary)),
            const SizedBox(height: 16),
            FilledButton.icon(onPressed: onDownload, icon: const Icon(Icons.download_rounded, size: 18), label: Text(appStrings.download)),
          ],
        ),
      ),
    );
  }
}
