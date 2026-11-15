part of 'main.dart';

// Settings › Cloud storage: the connections of this account, with a form to
// connect, change or disconnect one (main_cloud_storage_form.dart).

class _CloudStorageSettingsPage extends StatefulWidget {
  const _CloudStorageSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_CloudStorageSettingsPage> createState() => _CloudStorageSettingsPageState();
}

class _CloudStorageSettingsPageState extends State<_CloudStorageSettingsPage> with _PageLoad {
  NeoCloudController get _controller => widget.controller;
  CloudStorage get _storage => _controller.cloudStorage;

  @override
  void initState() {
    super.initState();
    _storage.addListener(_changed);
    unawaited(_storage.load());
  }

  @override
  void dispose() {
    _storage.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _edit([ExternalMount? mount]) async {
    final saved = await showCloudStorageForm(context, _controller, mount: mount);
    if (saved) await _storage.load();
  }

  Future<void> _disconnect(ExternalMount mount) => _confirmDelete(
    context,
    title: appStrings.cloudDisconnectTitle(mount.name),
    message: appStrings.cloudDisconnectMessage,
    confirmLabel: appStrings.cloudDisconnect,
    onConfirm: () => runAction(_controller, () async {
      await _controller.client.disconnectExternalMount(mount.id);
      await _storage.load();
    }),
  );

  Widget _row(ExternalMount mount) {
    return _SettingsRow(
      label: mount.name,
      description: '${mount.provider.label} · ${mount.summary}${mount.readOnly ? ' · ${appStrings.cloudReadOnly}' : ''}',
      control: Wrap(
        spacing: 4,
        children: <Widget>[
          IconButton(tooltip: appStrings.edit, onPressed: busy ? null : () => unawaited(_edit(mount)), icon: const Icon(Icons.edit_outlined, size: 18)),
          IconButton(tooltip: appStrings.cloudDisconnect, onPressed: busy ? null : () => unawaited(_disconnect(mount)), icon: const Icon(Icons.link_off_rounded, size: 18)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final mounts = _storage.mounts;
    final loadError = _storage.loadError;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.cloudConnectedTitle,
          description: appStrings.cloudConnectedDescription,
          trailing: FilledButton.icon(
            onPressed: busy || mounts == null ? null : () => unawaited(_edit()),
            icon: const Icon(Icons.add_rounded, size: 18),
            label: Text(appStrings.cloudConnect),
          ),
          children: <Widget>[
            if (mounts == null && loadError == null) const Padding(padding: EdgeInsets.all(24), child: Center(child: CircularProgressIndicator())),
            if (mounts == null && loadError != null) _LoadError(message: _cloudError(_controller, loadError), onRetry: () => unawaited(_storage.load())),
            if (mounts != null && mounts.isEmpty) _SettingsBlock(child: Text(appStrings.cloudNone, style: TextStyle(color: _textMuted))),
            for (final mount in mounts ?? const <ExternalMount>[]) _row(mount),
          ],
        ),
      ],
    );
  }
}
