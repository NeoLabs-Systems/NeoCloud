part of 'main.dart';

// Review's banner for empty folders AI left behind: "N empty folders
// created by AI — Remove", and "Remove empty folders" for other empty
// top-level folders of the last week (ones AI made before NeoCloud kept
// track look like the person's own). Removing lists the folders by name to
// confirm first and moves them to the trash.

class AiCleanupBanner extends StatefulWidget {
  const AiCleanupBanner({super.key, required this.controller, this.refreshToken = 0});

  final NeoCloudController controller;

  /// Read the lists again whenever this changes (Review reloaded).
  final int refreshToken;

  @override
  State<AiCleanupBanner> createState() => _AiCleanupBannerState();
}

class _AiCleanupBannerState extends State<AiCleanupBanner> {
  AiEmptyFolders _folders = const AiEmptyFolders();
  bool _busy = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void didUpdateWidget(AiCleanupBanner oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.refreshToken != widget.refreshToken) unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final folders = await _controller.ai.emptyFolders();
      if (mounted) setState(() => _folders = folders);
    } catch (_) {
      // An older server, or offline: no banner.
    }
  }

  Future<void> _remove(List<AiEmptyFolder> folders, String body) async {
    if (_busy || folders.isEmpty) return;
    final chosen = await showAdaptiveSheet<List<String>>(
      context: context,
      builder: (_) => _AiCleanupDialog(folders: folders, body: body),
    );
    if (chosen == null || chosen.isEmpty || !mounted) return;
    final messenger = ScaffoldMessenger.maybeOf(context);
    setState(() => _busy = true);
    try {
      final result = await _controller.ai.removeEmptyFolders(chosen);
      _controller.notifyFilesChanged();
      messenger?.showSnackBar(SnackBar(content: Text(appStrings.aiCleanupDone(result.removed))));
    } catch (error) {
      messenger?.showSnackBar(SnackBar(content: Text(_aiFriendlyError(_controller, error))));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await _load();
  }

  Widget _row({required String text, required String action, required VoidCallback onPressed}) {
    return _AppCard(
      padding: const EdgeInsets.fromLTRB(18, 12, 12, 12),
      child: Row(
        children: <Widget>[
          Icon(Icons.folder_off_outlined, size: 18, color: _textSecondary),
          const SizedBox(width: 12),
          Expanded(child: Text(text, style: TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w500))),
          const SizedBox(width: 12),
          OutlinedButton(onPressed: _busy ? null : onPressed, child: Text(action)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final aiCreated = _folders.aiCreated;
    final recent = _folders.recent;
    if (aiCreated.isEmpty && recent.isEmpty) return const SizedBox.shrink();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (aiCreated.isNotEmpty)
          _row(
            text: appStrings.aiCleanupBanner(aiCreated.length),
            action: appStrings.aiCleanupRemove,
            onPressed: () => unawaited(_remove(aiCreated, appStrings.aiCleanupBodyAi)),
          ),
        if (aiCreated.isNotEmpty && recent.isNotEmpty) const SizedBox(height: 8),
        if (recent.isNotEmpty)
          _row(
            text: appStrings.aiCleanupRecentBanner(recent.length),
            action: appStrings.aiCleanupRecentAction,
            onPressed: () => unawaited(_remove(recent, appStrings.aiCleanupBodyRecent)),
          ),
      ],
    );
  }
}

/// The folders by name, each with a checkbox (all ticked); resolves to the
/// ticked ids, or null when cancelled.
class _AiCleanupDialog extends StatefulWidget {
  const _AiCleanupDialog({required this.folders, required this.body});

  final List<AiEmptyFolder> folders;
  final String body;

  @override
  State<_AiCleanupDialog> createState() => _AiCleanupDialogState();
}

class _AiCleanupDialogState extends State<_AiCleanupDialog> {
  late final Set<String> _chosen = widget.folders.map((folder) => folder.id).toSet();

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(appStrings.aiCleanupTitle),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(widget.body, style: TextStyle(color: _textSecondary, height: 1.45)),
            const SizedBox(height: 8),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 320),
              child: ListView(
                shrinkWrap: true,
                children: <Widget>[
                  for (final folder in widget.folders)
                    CheckboxListTile(
                      dense: true,
                      contentPadding: EdgeInsets.zero,
                      controlAffinity: ListTileControlAffinity.leading,
                      value: _chosen.contains(folder.id),
                      onChanged: (value) => setState(() {
                        if (value == true) {
                          _chosen.add(folder.id);
                        } else {
                          _chosen.remove(folder.id);
                        }
                      }),
                      title: Text(folder.name),
                      subtitle: Text(_aiFolderLabel(folder.path), style: TextStyle(color: _textMuted, fontSize: 12)),
                    ),
                ],
              ),
            ),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _chosen.isEmpty ? null : () => Navigator.of(context).pop(_chosen.toList()),
          child: Text(appStrings.aiCleanupRemove),
        ),
      ],
    );
  }
}
