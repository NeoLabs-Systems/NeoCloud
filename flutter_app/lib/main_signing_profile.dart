part of 'main.dart';

// Settings › Signing profile: the details and pictures NeoCloud fills in
// when this account signs a document, and where signed copies go. Pictures
// are kept straight away; everything else is saved with the one Save button.

const int _profileImageCap = 5;
const int _profilePresetCap = 30;

/// One editable detail row (label and value).
class _PresetEdit {
  _PresetEdit({this.id = '', String label = '', String value = ''})
    : label = TextEditingController(text: label),
      value = TextEditingController(text: value);

  final String id;
  final TextEditingController label;
  final TextEditingController value;

  void dispose() {
    label.dispose();
    value.dispose();
  }
}

class _SigningProfileSettingsPage extends StatefulWidget {
  const _SigningProfileSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_SigningProfileSettingsPage> createState() => _SigningProfileSettingsPageState();
}

class _SigningProfileSettingsPageState extends State<_SigningProfileSettingsPage> with _PageLoad {
  final TextEditingController _name = TextEditingController();
  final TextEditingController _email = TextEditingController();
  final TextEditingController _phone = TextEditingController();
  final List<_PresetEdit> _presets = <_PresetEdit>[];
  final Map<String, Future<Uint8List>> _pictures = <String, Future<Uint8List>>{};
  SigningProfile? _loaded;
  bool _autoFill = true;
  bool _autoSave = true;
  bool _notify = false;
  String? _folderId;
  String? _folderName;
  bool _loading = true;
  bool _tried = false;

  NeoCloudController get _controller => widget.controller;
  SigningAccountApi get _api => _controller.signingAccount;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    for (final item in <TextEditingController>[_name, _email, _phone]) {
      item.dispose();
    }
    for (final preset in _presets) {
      preset.dispose();
    }
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final profile = await _api.profile();
      if (!mounted) return;
      setState(() {
        _fill(profile);
        _loading = false;
      });
      unawaited(_loadFolderName());
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          pageError = _controller.friendlyError(error);
        });
      }
    }
  }

  void _fill(SigningProfile profile) {
    _loaded = profile;
    _name.text = profile.displayName;
    _email.text = profile.email;
    _phone.text = profile.phone;
    for (final preset in _presets) {
      preset.dispose();
    }
    _presets
      ..clear()
      ..addAll(profile.presets.map((preset) => _PresetEdit(id: preset.id, label: preset.label, value: preset.value)));
    _autoFill = profile.autoFill;
    _autoSave = profile.autoSave;
    _notify = profile.notifyOnEachSignature;
    _folderId = profile.saveFolderId;
  }

  Future<void> _loadFolderName() async {
    final id = _folderId;
    if (id == null) {
      if (mounted) setState(() => _folderName = null);
      return;
    }
    try {
      final node = await _controller.client.getNode(id);
      if (mounted) setState(() => _folderName = node.name);
    } catch (_) {
      if (mounted) setState(() => _folderName = null);
    }
  }

  /// The profile as the form shows it now (pictures stay as they are).
  SigningProfile _draft() => (_loaded ?? const SigningProfile()).copyWith(
    displayName: _name.text.trim(),
    email: _email.text.trim(),
    phone: _phone.text.trim(),
    presets: <ProfilePreset>[
      for (final preset in _presets)
        if (preset.label.text.trim().isNotEmpty && preset.value.text.trim().isNotEmpty) ProfilePreset(id: preset.id, label: preset.label.text.trim(), value: preset.value.text.trim()),
    ],
    autoFill: _autoFill,
    autoSave: _autoSave,
    saveFolderId: _folderId,
    notifyOnEachSignature: _notify,
  );

  bool get _dirty => _loaded != null && jsonEncode(_draft().toJson()) != jsonEncode(_loaded!.toJson());

  bool get _emailOk => _email.text.trim().isEmpty || _emailPattern.hasMatch(_email.text.trim());

  Future<void> _save() async {
    setState(() => _tried = true);
    if (!_emailOk) return;
    await _run(() async {
      final saved = await _api.saveProfile(_draft());
      if (mounted) setState(() => _fill(saved));
      unawaited(_loadFolderName());
    }, notice: appStrings.saved);
  }

  /// Like the settings pages' runAction, but says what the signing server said
  /// about a refused entry (a repeated label, a full picture shelf).
  Future<void> _run(Future<void> Function() action, {String? notice}) async {
    setState(() {
      busy = true;
      pageError = null;
      pageNotice = null;
    });
    try {
      await action();
      if (mounted) setState(() => pageNotice = notice);
    } catch (error) {
      if (mounted) setState(() => pageError = signingErrorText(_controller, error, _controller.friendlyError(error)));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  // Pictures

  Future<Uint8List> _picture(String id) => _pictures.putIfAbsent(id, () => _api.imageBytes(id));

  Future<void> _add(SavedKind kind) async {
    final result = await showSignatureDialog(context, initials: kind == SavedKind.initials, useSavedStore: false, initialName: _name.text.trim());
    if (result == null || !mounted) return;
    await _run(() async {
      final hasAny = (_loaded?.imagesOf(kind) ?? const <ProfileImage>[]).isNotEmpty;
      await _api.addImage(result.png, kind: kind, method: result.method, label: result.typedText, makeDefault: !hasAny);
      await _reloadPictures();
    });
  }

  /// Takes the pictures from the server but leaves the unsaved form alone.
  Future<void> _reloadPictures() async {
    final fresh = await _api.profile();
    if (mounted) setState(() => _loaded = _loaded?.copyWith(signatures: fresh.signatures) ?? fresh);
  }

  Future<void> _makeDefault(ProfileImage image) => _run(() async {
    await _api.updateImage(image.id, isDefault: true);
    await _reloadPictures();
  });

  Future<void> _rename(ProfileImage image) async {
    final label = await showAdaptiveSheet<String>(context: context, builder: (dialogContext) => _RenameDialog(initial: image.label ?? ''));
    if (label == null || !mounted) return;
    await _run(() async {
      await _api.updateImage(image.id, label: label);
      await _reloadPictures();
    });
  }

  Future<void> _remove(ProfileImage image) => _confirmDelete(
    context,
    title: appStrings.signingProfileDeleteTitle,
    message: appStrings.signingProfileDeleteBody,
    confirmLabel: appStrings.signingProfileDelete,
    onConfirm: () => _run(() async {
      await _api.deleteImage(image.id);
      _pictures.remove(image.id);
      await _reloadPictures();
    }),
  );

  // Folder

  Future<void> _chooseFolder() async {
    final target = await showFolderPicker(context, _controller, title: appStrings.signingProfileFolderPick, confirmLabel: appStrings.signingProfileFolderHere, hideVaults: true);
    if (target == null || !mounted) return;
    setState(() {
      _folderId = target.$1;
      _folderName = target.$2;
    });
  }

  void _addPreset([String label = '']) {
    if (_presets.length >= _profilePresetCap) return;
    setState(() => _presets.add(_PresetEdit(label: label)));
  }

  // Layout

  Widget _imageGroup(SavedKind kind) {
    final images = _loaded?.imagesOf(kind) ?? const <ProfileImage>[];
    final signature = kind == SavedKind.signature;
    final full = images.length >= _profileImageCap;
    return _SettingsGroup(
      title: signature ? appStrings.signingProfileSignatures : appStrings.signingProfileInitials,
      description: signature ? appStrings.signingProfileSignaturesHint : appStrings.signingProfileInitialsHint,
      children: <Widget>[
        for (final image in images)
          _SettingsBlock(
            key: ValueKey<String>('profile-image-${image.id}'),
            child: _ProfileImageRow(
              image: image,
              picture: _picture(image.id),
              busy: busy,
              onDefault: () => unawaited(_makeDefault(image)),
              onRename: () => unawaited(_rename(image)),
              onDelete: () => unawaited(_remove(image)),
            ),
          ),
        _SettingsBlock(
          child: Align(
            alignment: Alignment.centerLeft,
            child: Wrap(
              spacing: 12,
              runSpacing: 6,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: <Widget>[
                OutlinedButton.icon(
                  key: ValueKey<String>('profile-add-${kind.name}'),
                  onPressed: busy || full ? null : () => unawaited(_add(kind)),
                  icon: const Icon(Icons.add_rounded, size: 18),
                  label: Text(signature ? appStrings.signingProfileAddSignature : appStrings.signingProfileAddInitials),
                ),
                if (full) Text(appStrings.signingProfileLimit(_profileImageCap), style: TextStyle(color: _textMuted, fontSize: 12.5)),
              ],
            ),
          ),
        ),
      ],
    );
  }

  Widget _presetRow(_PresetEdit preset, int index) {
    final label = TextField(key: ValueKey<String>('preset-label-$index'), controller: preset.label, onChanged: (_) => setState(() {}), maxLength: 40, decoration: InputDecoration(labelText: appStrings.signingProfileLabel, isDense: true, counterText: ''));
    final value = TextField(key: ValueKey<String>('preset-value-$index'), controller: preset.value, onChanged: (_) => setState(() {}), maxLength: 500, decoration: InputDecoration(labelText: appStrings.signingProfileValue, isDense: true, counterText: ''));
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: LayoutBuilder(
        builder: (context, constraints) => Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Expanded(
              child: constraints.maxWidth >= 480
                  ? Row(children: <Widget>[Expanded(flex: 2, child: label), const SizedBox(width: 10), Expanded(flex: 3, child: value)])
                  : Column(children: <Widget>[label, const SizedBox(height: 8), value]),
            ),
            IconButton(
              key: ValueKey<String>('preset-remove-$index'),
              tooltip: appStrings.signingProfileDelete,
              onPressed: () => setState(() => _presets.removeAt(index).dispose()),
              icon: const Icon(Icons.close_rounded, size: 18),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Padding(padding: EdgeInsets.only(top: 24), child: Center(child: CircularProgressIndicator()));
    if (_loaded == null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[feedback(), Align(alignment: Alignment.centerLeft, child: OutlinedButton(onPressed: () => unawaited(_load()), child: Text(appStrings.retry)))],
      );
    }
    final dirty = _dirty;
    final suggestions = <String>[appStrings.signingProfileAddress, appStrings.signingProfileCompany, appStrings.signingProfileTitle, appStrings.signingProfilePhone]
        .where((label) => !_presets.any((preset) => preset.label.text.trim().toLowerCase() == label.toLowerCase()))
        .toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.signingProfileDetails,
          description: appStrings.signingProfileDetailsHint,
          children: <Widget>[
            _SettingsRow(label: appStrings.displayName, fillControl: true, control: TextField(key: const ValueKey<String>('profile-name'), controller: _name, onChanged: (_) => setState(() {}), decoration: const InputDecoration(isDense: true))),
            _SettingsRow(
              label: appStrings.accountEmail,
              fillControl: true,
              control: TextField(
                key: const ValueKey<String>('profile-email'),
                controller: _email,
                keyboardType: TextInputType.emailAddress,
                onChanged: (_) => setState(() {}),
                decoration: InputDecoration(isDense: true, errorText: _tried && !_emailOk ? appStrings.signingEmailInvalid : null),
              ),
            ),
            _SettingsRow(label: appStrings.signingProfilePhone, fillControl: true, control: TextField(key: const ValueKey<String>('profile-phone'), controller: _phone, keyboardType: TextInputType.phone, onChanged: (_) => setState(() {}), decoration: const InputDecoration(isDense: true))),
          ],
        ),
        _imageGroup(SavedKind.signature),
        _imageGroup(SavedKind.initials),
        _SettingsGroup(
          title: appStrings.signingProfilePresets,
          description: appStrings.signingProfilePresetsHint,
          children: <Widget>[
            _SettingsBlock(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  for (final (index, preset) in _presets.indexed) _presetRow(preset, index),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: <Widget>[
                      OutlinedButton.icon(key: const ValueKey<String>('preset-add'), onPressed: _presets.length >= _profilePresetCap ? null : _addPreset, icon: const Icon(Icons.add_rounded, size: 18), label: Text(appStrings.signingProfileAddDetail)),
                      for (final label in suggestions) ActionChip(key: ValueKey<String>('preset-suggest-$label'), label: Text(label), onPressed: () => _addPreset(label)),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.signingProfileBehavior,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.signingProfileAutoFill,
              description: appStrings.signingProfileAutoFillHint,
              control: Switch(key: const ValueKey<String>('profile-autofill'), value: _autoFill, onChanged: (value) => setState(() => _autoFill = value)),
            ),
            _SettingsRow(
              label: appStrings.signingProfileAutoSave,
              description: appStrings.signingProfileAutoSaveHint,
              control: Switch(key: const ValueKey<String>('profile-autosave'), value: _autoSave, onChanged: (value) => setState(() => _autoSave = value)),
            ),
            if (_autoSave)
              _SettingsRow(
                label: appStrings.signingProfileFolder,
                description: _folderName ?? appStrings.signingProfileFolderDefault,
                control: OutlinedButton.icon(key: const ValueKey<String>('profile-folder'), onPressed: () => unawaited(_chooseFolder()), icon: const Icon(Icons.folder_outlined, size: 18), label: Text(appStrings.signingProfileFolderChange)),
              ),
            if (_controller.emailConfigured)
              _SettingsRow(
                label: appStrings.signingProfileNotify,
                description: appStrings.signingProfileNotifyHint,
                control: Switch(key: const ValueKey<String>('profile-notify'), value: _notify, onChanged: (value) => setState(() => _notify = value)),
              ),
          ],
        ),
        Row(
          children: <Widget>[
            FilledButton(key: const ValueKey<String>('profile-save'), onPressed: busy || !dirty ? null : () => unawaited(_save()), child: busy ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.save)),
            const SizedBox(width: 14),
            Expanded(child: Text(dirty ? appStrings.signingProfileUnsaved : appStrings.signingProfileSaved, key: const ValueKey<String>('profile-state'), style: TextStyle(color: _textMuted, fontSize: 13))),
          ],
        ),
      ],
    );
  }
}

/// One kept picture on white, its name and its actions.
class _ProfileImageRow extends StatelessWidget {
  const _ProfileImageRow({required this.image, required this.picture, required this.busy, required this.onDefault, required this.onRename, required this.onDelete});

  final ProfileImage image;
  final Future<Uint8List> picture;
  final bool busy;
  final VoidCallback onDefault;
  final VoidCallback onRename;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final preview = _PaperTile(
      height: 64,
      child: FutureBuilder<Uint8List>(
        future: picture,
        builder: (context, snapshot) => snapshot.hasData ? Image.memory(snapshot.data!, width: 140, fit: BoxFit.contain) : const SizedBox(width: 140),
      ),
    );
    final info = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Wrap(
          spacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Text(image.label ?? appStrings.signingProfileUnnamed, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
            if (image.isDefault) _SettingsTag(label: appStrings.signingProfileDefault),
          ],
        ),
      ],
    );
    final actions = Wrap(
      spacing: 4,
      children: <Widget>[
        if (!image.isDefault) TextButton(key: ValueKey<String>('image-default-${image.id}'), onPressed: busy ? null : onDefault, child: Text(appStrings.signingProfileMakeDefault)),
        TextButton(key: ValueKey<String>('image-rename-${image.id}'), onPressed: busy ? null : onRename, child: Text(appStrings.signingProfileRename)),
        TextButton(key: ValueKey<String>('image-delete-${image.id}'), onPressed: busy ? null : onDelete, style: TextButton.styleFrom(foregroundColor: _danger), child: Text(appStrings.signingProfileDelete)),
      ],
    );
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 520) {
          return Column(crossAxisAlignment: CrossAxisAlignment.start, children: <Widget>[preview, const SizedBox(height: 8), info, const SizedBox(height: 4), actions]);
        }
        return Row(children: <Widget>[preview, const SizedBox(width: 16), Expanded(child: info), actions]);
      },
    );
  }
}

class _RenameDialog extends StatefulWidget {
  const _RenameDialog({required this.initial});

  final String initial;

  @override
  State<_RenameDialog> createState() => _RenameDialogState();
}

class _RenameDialogState extends State<_RenameDialog> {
  late final TextEditingController _text = TextEditingController(text: widget.initial);

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(appStrings.signingProfileRename),
      content: SizedBox(
        width: 400,
        child: TextField(key: const ValueKey<String>('profile-rename-field'), controller: _text, autofocus: true, maxLength: 60, decoration: InputDecoration(labelText: appStrings.signingProfileLabel)),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(key: const ValueKey<String>('profile-rename-save'), onPressed: () => Navigator.of(context).pop(_text.text.trim()), child: Text(appStrings.save)),
      ],
    );
  }
}
