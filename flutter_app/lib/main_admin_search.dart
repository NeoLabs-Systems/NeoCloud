part of 'main.dart';

// Admin › Server › Full-text search (docs/clients.md §8): the switch, what
// it costs in privacy, and how far the index got.

class _AdminSearchGroup extends StatefulWidget {
  const _AdminSearchGroup({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminSearchGroup> createState() => _AdminSearchGroupState();
}

class _AdminSearchGroupState extends State<_AdminSearchGroup> with _PageLoad {
  SearchIndexStatus? _status;
  Timer? _poll;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  /// While files wait to be indexed the numbers move, so they refresh.
  Future<void> _load() async {
    try {
      final status = await _controller.client.adminSearchIndex();
      if (!mounted) return;
      setState(() => _status = status);
      if (status.enabled && status.pending > 0) {
        _poll ??= Timer.periodic(const Duration(seconds: 5), (_) => unawaited(_load()));
      } else {
        _poll?.cancel();
        _poll = null;
      }
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _setEnabled(bool enabled) => runAction(_controller, () async {
    await _controller.client.adminSetFullTextSearch(enabled);
    _controller.fullTextSearch = enabled;
    await _load();
  });

  Future<void> _rebuild() => runAction(_controller, () async {
    await _controller.client.adminRebuildSearchIndex();
    await _load();
  }, notice: appStrings.adminSearchRebuildStarted);

  Widget _value(String text) => Text(text, style: TextStyle(color: _textSecondary));

  Widget _tool(bool available, String yes) =>
      _StatusPill(label: available ? yes : appStrings.adminSearchToolMissing, color: available ? _success : _textMuted);

  @override
  Widget build(BuildContext context) {
    final status = _status;
    final numbers = NumberFormat.decimalPattern(currentAppLanguage.code);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.adminSearchTitle,
          description: appStrings.adminSearchDescription,
          trailing: IconButton(tooltip: appStrings.refresh, onPressed: () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded, size: 20)),
          children: <Widget>[
            if (status == null && pageError == null) const _LoadingPlaceholder(),
            if (status != null) ...<Widget>[
              _SettingsRow(
                label: appStrings.adminSearchToggle,
                description: appStrings.adminSearchPrivacy,
                control: Switch(value: status.enabled, onChanged: busy ? null : (value) => unawaited(_setEnabled(value))),
              ),
              _SettingsRow(
                label: appStrings.adminSearchState,
                control: _StatusPill(
                  label: !status.enabled
                      ? appStrings.adminSearchOff
                      : (status.pending > 0 ? appStrings.adminSearchIndexing : appStrings.adminSearchUpToDate),
                  color: !status.enabled ? _textMuted : (status.pending > 0 ? _info : _success),
                ),
              ),
              _SettingsRow(label: appStrings.adminSearchIndexed, control: _value(numbers.format(status.indexed))),
              _SettingsRow(label: appStrings.adminSearchPending, control: _value(numbers.format(status.pending))),
              _SettingsRow(
                label: appStrings.adminSearchFailed,
                description: status.failed > 0 ? appStrings.adminSearchFailedHint : null,
                control: _value(numbers.format(status.failed)),
              ),
              _SettingsRow(label: appStrings.adminSearchSize, control: _value(formatBytes(status.bytes))),
              _SettingsRow(
                label: appStrings.adminSearchPdf,
                description: status.pdftotext ? null : appStrings.adminSearchPdfFallback,
                control: _tool(status.pdftotext, appStrings.adminSearchToolPdftotext),
              ),
              _SettingsRow(label: appStrings.adminSearchOffice, control: _tool(status.office, appStrings.adminSearchToolAvailable)),
              _SettingsRow(
                label: appStrings.adminSearchRebuild,
                description: appStrings.adminSearchRebuildHint,
                control: OutlinedButton.icon(
                  onPressed: busy || !status.enabled ? null : () => unawaited(_rebuild()),
                  icon: const Icon(Icons.restart_alt_rounded, size: 18),
                  label: Text(appStrings.adminSearchRebuild),
                ),
              ),
            ],
          ],
        ),
      ],
    );
  }
}
