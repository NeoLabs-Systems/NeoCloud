part of 'main.dart';

// Home shows the connected cloud storages as a row of folders under the
// recent files; each opens the browser (main_cloud_storage_browser.dart).
// Nothing shows while the account has none, so it stays out of the way.

class _CloudStorageStrip extends StatefulWidget {
  const _CloudStorageStrip({required this.controller});

  final NeoCloudController controller;

  @override
  State<_CloudStorageStrip> createState() => _CloudStorageStripState();
}

class _CloudStorageStripState extends State<_CloudStorageStrip> {
  CloudStorage get _storage => widget.controller.cloudStorage;

  @override
  void initState() {
    super.initState();
    _storage.addListener(_changed);
    _storage.ensureLoaded();
  }

  @override
  void dispose() {
    _storage.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final mounts = _storage.mounts;
    if (mounts == null || mounts.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(appStrings.cloudSectionTitle, style: TextStyle(color: _textSecondary, fontSize: 13, fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        Wrap(
          spacing: 10,
          runSpacing: 10,
          children: <Widget>[
            for (final mount in mounts)
              ActionChip(
                key: ValueKey<String>('cloud-${mount.id}'),
                avatar: Icon(mount.provider.icon, size: 18),
                label: Text(mount.name),
                onPressed: () => unawaited(openCloudStorage(context, widget.controller, mount)),
              ),
          ],
        ),
      ],
    );
  }
}
