part of 'main.dart';

// Quick sign: the owner signs a file of their own without parties or links.
// A draft is made from the file, the editor's own placement puts signature,
// initials, date and name boxes on the pages, and one request signs it
// (POST /documents/:id/quick-sign). The signed copy is sealed, audited and
// saved beside the original like any signed document.

/// Makes [node] a draft and opens quick sign on it.
Future<void> startQuickSign(BuildContext context, NeoCloudController controller, FileNode node) async {
  final navigator = Navigator.of(context);
  final detail = await _makeSigningDraft(context, controller, node);
  if (detail == null) return;
  await navigator.push(MaterialPageRoute<void>(fullscreenDialog: true, builder: (_) => QuickSignPage(controller: controller, detail: detail)));
}

/// Uploads a file from the device into [folderId] and quick-signs it.
Future<void> _uploadAndQuickSign(BuildContext context, NeoCloudController controller, String folderId) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  final picked = await FilePicker.platform.pickFiles(withData: true);
  final file = picked?.files.single;
  final bytes = file?.bytes;
  if (file == null || bytes == null || !context.mounted) return;
  if (!signableKindOf(file.name, null).isSignable) {
    messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingUnsupportedFile)));
    return;
  }
  try {
    messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingQuickUploading), duration: const Duration(seconds: 30)));
    final node = await controller.client.uploadSmall(folderId, file.name, bytes, conflict: 'rename');
    messenger?.hideCurrentSnackBar();
    if (context.mounted) await startQuickSign(context, controller, node);
  } catch (error) {
    messenger?.hideCurrentSnackBar();
    messenger?.showSnackBar(SnackBar(content: Text(controller.friendlyError(error))));
  }
}

class QuickSignPage extends StatefulWidget {
  const QuickSignPage({super.key, required this.controller, required this.detail, this.pagesBuilder = _pdfPagesBuilder});

  final NeoCloudController controller;

  /// A fresh draft of the file; it is deleted again when the page is left
  /// without signing.
  final SigningDetail detail;
  final SigningPagesBuilder pagesBuilder;

  @override
  State<QuickSignPage> createState() => _QuickSignPageState();
}

class _QuickSignPageState extends State<QuickSignPage> {
  late final SigningEditorState _state;
  final SigningPageController _pages = SigningPageController();
  final FocusNode _pagesFocus = FocusNode(debugLabel: 'quick-sign-pages');
  final TextEditingController _name = TextEditingController();
  final Map<SavedKind, QuickSignPicture> _pictures = <SavedKind, QuickSignPicture>{};
  final Map<String, Future<Uint8List>> _savedBytes = <String, Future<Uint8List>>{};
  final Set<SavedKind> _asked = <SavedKind>{};
  final DateTime _today = DateTime.now();
  List<ProfileImage> _saved = const <ProfileImage>[];
  Uint8List? _pdf;
  String? _pdfError;
  bool _busy = false;
  bool _signed = false;

  NeoCloudController get _controller => widget.controller;
  SigningDocument get _document => widget.detail.document;
  String get _isoToday => toSigningIsoDate(_today);
  String get _dateFormat => DateFormats.defaultFor(_document.locale);
  String get _signedName => '${_document.title} (${_document.locale == 'de' ? 'signiert' : 'signed'}).pdf';

  static SavedKind? _kindOf(FieldType type) => switch (type) {
    FieldType.signature => SavedKind.signature,
    FieldType.initials => SavedKind.initials,
    _ => null,
  };

  @override
  void initState() {
    super.initState();
    _state = SigningEditorState(api: _controller.signing, detail: widget.detail, partyName: (number) => appStrings.signingPartyDefaultName('$number'));
    _state.addListener(_fieldsChanged);
    final me = _controller.user;
    _name.text = me == null ? '' : ((me.displayName ?? '').trim().isEmpty ? me.username : me.displayName!.trim());
    unawaited(_loadPdf());
    unawaited(_loadProfile());
  }

  @override
  void dispose() {
    _state.removeListener(_fieldsChanged);
    // The draft was only made for this page: whatever is still being saved
    // goes out first, then an unsigned draft is deleted.
    final api = _controller.signing;
    final id = _document.id;
    final signed = _signed;
    unawaited(_state.flush().whenComplete(() async {
      _state.dispose();
      if (signed) return;
      try {
        await api.delete(id);
      } catch (_) {
        // It stays in the list as a draft.
      }
    }));
    _pages.dispose();
    _pagesFocus.dispose();
    _name.dispose();
    super.dispose();
  }

  Future<void> _loadPdf() async {
    try {
      final bytes = await _controller.signing.pdf(_document.id);
      if (mounted) setState(() => _pdf = bytes);
    } catch (error) {
      if (mounted) setState(() => _pdfError = signingErrorText(_controller, error, appStrings.signingPdfLoadFailed));
    }
  }

  /// The saved pictures, and the default ones ready to use.
  Future<void> _loadProfile() async {
    try {
      final profile = await _controller.signingAccount.profile();
      if (!mounted) return;
      if (profile.displayName.trim().isNotEmpty) _name.text = profile.displayName.trim();
      setState(() => _saved = profile.signatures);
      for (final kind in SavedKind.values) {
        final images = profile.imagesOf(kind);
        if (images.isEmpty || _pictures.containsKey(kind)) continue;
        final image = images.firstWhere((image) => image.isDefault, orElse: () => images.first);
        await _useSaved(image);
      }
    } catch (_) {
      // Without the profile the signer simply draws.
    }
  }

  Future<Uint8List> _bytesOf(String imageId) => _savedBytes.putIfAbsent(imageId, () => _controller.signingAccount.imageBytes(imageId));

  Future<void> _useSaved(ProfileImage image) async {
    final png = await _bytesOf(image.id);
    if (mounted) setState(() => _pictures[image.kind] = QuickSignPicture(png: png, method: image.method ?? SignatureMethod.drawn, imageId: image.id));
  }

  /// A placed signature or initials box without a picture asks for one, once.
  void _fieldsChanged() {
    for (final field in _state.fields) {
      final kind = _kindOf(field.type);
      if (kind != null && !_pictures.containsKey(kind) && _asked.add(kind)) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (mounted) unawaited(_choosePicture(kind));
        });
      }
    }
  }

  /// A saved picture, or a new one drawn (and, if wanted, kept).
  Future<void> _choosePicture(SavedKind kind) async {
    final saved = _saved.where((image) => image.kind == kind).toList();
    if (saved.isNotEmpty) {
      final choice = await showAdaptiveSheet<String>(
        context: context,
        builder: (dialogContext) => _AdaptiveDialog(
          title: Text(kind == SavedKind.initials ? appStrings.signingQuickYourInitials : appStrings.signingQuickYourSignature),
          content: SizedBox(
            width: 420,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                for (final image in saved)
                  ListTile(
                    key: ValueKey<String>('quick-saved-${image.id}'),
                    contentPadding: EdgeInsets.zero,
                    leading: _PaperTile(height: 44, child: _SavedPicture(bytes: _bytesOf(image.id))),
                    title: Text(image.label ?? appStrings.signingProfileUnnamed),
                    subtitle: image.isDefault ? Text(appStrings.signingProfileDefault) : null,
                    onTap: () => Navigator.of(dialogContext).pop(image.id),
                  ),
                ListTile(
                  key: const ValueKey<String>('quick-draw-new'),
                  contentPadding: EdgeInsets.zero,
                  leading: const Icon(Icons.draw_outlined),
                  title: Text(appStrings.signingQuickDrawNew),
                  onTap: () => Navigator.of(dialogContext).pop(''),
                ),
              ],
            ),
          ),
          actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
        ),
      );
      if (choice == null || !mounted) return;
      if (choice.isNotEmpty) return _useSaved(saved.firstWhere((image) => image.id == choice));
    }
    await _drawPicture(kind);
  }

  Future<void> _drawPicture(SavedKind kind) async {
    final result = await showSignatureDialog(context, initials: kind == SavedKind.initials, useSavedStore: false, initialName: _name.text.trim());
    if (result == null || !mounted) return;
    var picture = QuickSignPicture(png: result.png, method: result.method, typedText: result.typedText);
    setState(() => _pictures[kind] = picture);
    if (_saved.where((image) => image.kind == kind).length >= _profileImageCap) return;
    final keep = await showAdaptiveSheet<bool>(
      context: context,
      builder: (dialogContext) => _AdaptiveDialog(
        title: Text(appStrings.signingQuickKeepTitle),
        content: Text(appStrings.signingQuickKeepBody, style: TextStyle(color: _textSecondary, height: 1.45)),
        actions: <Widget>[
          TextButton(key: const ValueKey<String>('quick-keep-no'), onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.signingQuickDontKeep)),
          FilledButton(key: const ValueKey<String>('quick-keep-yes'), onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.signingQuickKeep)),
        ],
      ),
    );
    if (keep != true || !mounted) return;
    try {
      final first = !_saved.any((image) => image.kind == kind);
      final image = await _controller.signingAccount.addImage(result.png, kind: kind, method: result.method, label: result.typedText, makeDefault: first);
      picture = QuickSignPicture(png: result.png, method: result.method, typedText: result.typedText, imageId: image.id);
      _savedBytes[image.id] = Future<Uint8List>.value(result.png);
      if (mounted) {
        setState(() {
          _saved = <ProfileImage>[..._saved, image];
          _pictures[kind] = picture;
        });
      }
    } catch (error) {
      _toast(signingErrorText(_controller, error, _controller.friendlyError(error)));
    }
  }

  void _toast(String message) => ScaffoldMessenger.maybeOf(context)
    ?..hideCurrentSnackBar()
    ..showSnackBar(SnackBar(content: Text(message)));

  /// What a box shows: the picture, the name or the date it will carry.
  Widget _boxContent(SigningField field) {
    final kind = _kindOf(field.type);
    if (kind != null) {
      final picture = _pictures[kind];
      if (picture != null) return Image.memory(picture.png, fit: BoxFit.contain, errorBuilder: (context, error, stack) => const SizedBox.shrink());
      return Center(child: Text(appStrings.signingQuickAdd, style: const TextStyle(color: Color(0x99000000), fontSize: 11)));
    }
    final text = field.type == FieldType.date ? formatSigningDate(_today, _dateFormat) : _name.text.trim();
    return Align(
      alignment: Alignment.centerLeft,
      child: FittedBox(fit: BoxFit.scaleDown, child: Text(text, maxLines: 1, style: const TextStyle(color: Colors.black, fontSize: 14))),
    );
  }

  QuickSignField _requestField(SigningField field) {
    final kind = _kindOf(field.type);
    return QuickSignField(
      type: field.type,
      areas: field.areas,
      picture: kind == null ? null : _pictures[kind],
      value: switch (field.type) {
        FieldType.date => _isoToday,
        FieldType.text => _name.text.trim(),
        _ => null,
      },
      prefs: field.type == FieldType.date ? <String, Object?>{PrefKeys.format: _dateFormat} : null,
    );
  }

  Future<void> _sign() async {
    final fields = _state.fields;
    final name = _name.text.trim();
    if (fields.isEmpty) return _toast(appStrings.signingQuickNothingPlaced);
    if (fields.length > quickSignMaxFields) return _toast(appStrings.signingQuickTooMany('$quickSignMaxFields'));
    if (name.isEmpty) return _toast(appStrings.signingQuickNeedName);
    for (final field in fields) {
      final kind = _kindOf(field.type);
      if (kind != null && !_pictures.containsKey(kind)) return _choosePicture(kind);
    }
    setState(() => _busy = true);
    try {
      await _state.flush();
      final now = DateTime.now();
      final zone = RegExp(r'^[A-Za-z0-9_+\-/]{1,64}$').hasMatch(now.timeZoneName) ? now.timeZoneName : null;
      await _controller.signing.quickSign(
        _document.id,
        QuickSignRequest(signerName: name, fields: <QuickSignField>[for (final field in fields) _requestField(field)], timezone: zone, tzOffsetMinutes: now.timeZoneOffset.inMinutes),
      );
      _signed = true;
      if (!mounted) return;
      final messenger = ScaffoldMessenger.maybeOf(context);
      Navigator.of(context).pop();
      _controller.openSigning(_document.id);
      messenger?.showSnackBar(SnackBar(content: Text(appStrings.signingQuickDone)));
    } catch (error) {
      if (!mounted) return;
      setState(() => _busy = false);
      _toast(signingErrorText(_controller, error, appStrings.signingQuickFailed));
      await _leaveIfGone();
    }
  }

  /// A failure after sending removes the document on the server; the page
  /// has nothing left to sign then.
  Future<void> _leaveIfGone() async {
    try {
      final detail = await _controller.signing.get(_document.id);
      if (detail.document.status == SigningStatus.draft) return;
    } catch (_) {
      // Gone.
    }
    _signed = true;
    if (mounted) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final wide = MediaQuery.sizeOf(context).width >= 900;
    // Delete and Backspace remove the selected box while the pages have the
    // focus; typing the name keeps them.
    final pages = Listener(
      onPointerDown: (_) => _pagesFocus.requestFocus(),
      child: CallbackShortcuts(
        bindings: <ShortcutActivator, VoidCallback>{
          const SingleActivator(LogicalKeyboardKey.delete): _state.deleteSelected,
          const SingleActivator(LogicalKeyboardKey.backspace): _state.deleteSelected,
        },
        child: Focus(
          focusNode: _pagesFocus,
          child: _PagesArea(state: _state, pages: _pages, pdf: _pdf, error: _pdfError, pagesBuilder: widget.pagesBuilder, boxContent: _boxContent, typeLabel: _QuickSignPanel.label),
        ),
      ),
    );
    final panel = _QuickSignPanel(
      state: _state,
      name: _name,
      pictures: _pictures,
      signedName: _signedName,
      busy: _busy,
      onPicture: (kind) => unawaited(_choosePicture(kind)),
      onNameChanged: () => setState(() {}),
      onSign: () => unawaited(_sign()),
    );
    return Scaffold(
      backgroundColor: _bgPrimary,
      appBar: AppBar(
        leading: IconButton(key: const ValueKey<String>('quick-close'), tooltip: appStrings.cancel, onPressed: () => Navigator.of(context).pop(), icon: const Icon(Icons.close_rounded)),
        title: Text('${appStrings.signingQuickTitle}: ${_document.title}', maxLines: 1, overflow: TextOverflow.ellipsis),
        actions: <Widget>[
          ListenableBuilder(
            listenable: _state,
            builder: (context, _) => Row(
              children: <Widget>[
                IconButton(tooltip: appStrings.signingUndo, onPressed: _state.canUndo ? _state.undo : null, icon: const Icon(Icons.undo_rounded)),
                IconButton(
                  key: const ValueKey<String>('quick-delete'),
                  tooltip: appStrings.signingDeleteField,
                  onPressed: _state.selected == null ? null : _state.deleteSelected,
                  icon: const Icon(Icons.delete_outline_rounded),
                ),
              ],
            ),
          ),
        ],
      ),
      body: wide
          ? Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                SizedBox(width: 320, child: SingleChildScrollView(padding: const EdgeInsets.all(16), child: panel)),
                VerticalDivider(width: 1, color: _border),
                Expanded(child: pages),
              ],
            )
          : Column(
              children: <Widget>[
                Expanded(child: pages),
                Divider(height: 1, color: _border),
                ConstrainedBox(
                  constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(context).height * 0.42),
                  child: SingleChildScrollView(padding: const EdgeInsets.all(12), child: SafeArea(top: false, child: panel)),
                ),
              ],
            ),
    );
  }
}

/// The side panel (bottom on phones): what to place, the pictures and name
/// it uses, where the result goes, and the one button.
class _QuickSignPanel extends StatelessWidget {
  const _QuickSignPanel({
    required this.state,
    required this.name,
    required this.pictures,
    required this.signedName,
    required this.busy,
    required this.onPicture,
    required this.onNameChanged,
    required this.onSign,
  });

  final SigningEditorState state;
  final TextEditingController name;
  final Map<SavedKind, QuickSignPicture> pictures;
  final String signedName;
  final bool busy;
  final ValueChanged<SavedKind> onPicture;
  final VoidCallback onNameChanged;
  final VoidCallback onSign;

  static String label(FieldType type) => type == FieldType.text ? appStrings.signingQuickName : FieldTypes.of(type).label(appStrings);

  @override
  Widget build(BuildContext context) {
    final touch = MediaQuery.sizeOf(context).width < 700;
    final hint = TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.signingQuickHint, style: hint),
        const SizedBox(height: 10),
        ListenableBuilder(
          listenable: state,
          builder: (context, _) => GridView.count(
            crossAxisCount: 2,
            shrinkWrap: true,
            physics: const NeverScrollableScrollPhysics(),
            mainAxisSpacing: 8,
            crossAxisSpacing: 8,
            childAspectRatio: 2,
            children: <Widget>[
              for (final type in quickSignTypes)
                _PaletteTile(
                  key: ValueKey<String>('palette-${type.name}'),
                  spec: FieldTypes.of(type),
                  label: label(type),
                  armed: state.armedType == type,
                  enabled: state.canAddField,
                  longPressToDrag: touch,
                  color: state.colorOf(state.activePartyId),
                  onTap: () => state.arm(type),
                ),
            ],
          ),
        ),
        const SizedBox(height: 16),
        TextField(
          key: const ValueKey<String>('quick-name'),
          controller: name,
          onChanged: (_) => onNameChanged(),
          decoration: InputDecoration(labelText: appStrings.signingQuickYourName, helperText: appStrings.signingQuickNameHint, helperMaxLines: 2, isDense: true),
        ),
        const SizedBox(height: 12),
        for (final kind in SavedKind.values) _QuickPictureRow(kind: kind, picture: pictures[kind], onTap: busy ? null : () => onPicture(kind)),
        const SizedBox(height: 4),
        Text(appStrings.signingQuickSavedTo(signedName), style: hint),
        const SizedBox(height: 6),
        Text(appStrings.signingQuickConsent, style: hint),
        const SizedBox(height: 12),
        FilledButton.icon(
          key: const ValueKey<String>('quick-submit'),
          onPressed: busy ? null : onSign,
          icon: busy ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.draw_rounded, size: 18),
          label: Text(appStrings.signingQuickSubmit),
        ),
      ],
    );
  }
}

/// "Your signature" / "Your initials": the picture in use and where it is
/// kept, with Add or Change.
class _QuickPictureRow extends StatelessWidget {
  const _QuickPictureRow({required this.kind, required this.picture, required this.onTap});

  final SavedKind kind;
  final QuickSignPicture? picture;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final picture = this.picture;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        children: <Widget>[
          _PaperTile(height: 44, child: picture == null ? const SizedBox(width: 72) : _pictureImage(picture.png)),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(kind == SavedKind.initials ? appStrings.signingQuickYourInitials : appStrings.signingQuickYourSignature, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 13)),
                if (picture != null)
                  Text(picture.isSaved ? appStrings.signingQuickFromProfile : appStrings.signingQuickOnlyNow, style: TextStyle(color: _textMuted, fontSize: 12)),
              ],
            ),
          ),
          TextButton(key: ValueKey<String>('quick-picture-${kind.name}'), onPressed: onTap, child: Text(picture == null ? appStrings.signingQuickAdd : appStrings.signingQuickChange)),
        ],
      ),
    );
  }
}

class _SavedPicture extends StatelessWidget {
  const _SavedPicture({required this.bytes});

  final Future<Uint8List> bytes;

  @override
  Widget build(BuildContext context) => FutureBuilder<Uint8List>(
    future: bytes,
    builder: (context, snapshot) => snapshot.hasData ? _pictureImage(snapshot.data!) : const SizedBox(width: 72),
  );
}

/// A small signature picture; one that cannot be decoded leaves a blank.
Widget _pictureImage(Uint8List png) => Image.memory(png, width: 72, fit: BoxFit.contain, errorBuilder: (context, error, stack) => const SizedBox(width: 72));
