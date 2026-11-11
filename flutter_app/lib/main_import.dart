part of 'main.dart';

// Settings › Import: move files over from Nextcloud or another WebDAV
// server (docs/clients.md §7). This file is the form; the job list with its
// live progress is main_import_jobs.dart.

/// Where Nextcloud explains app passwords.
const String _nextcloudAppPasswordDocs =
    'https://docs.nextcloud.com/server/latest/user_manual/en/session_management.html#managing-devices';

class _ImportSettingsPage extends StatefulWidget {
  const _ImportSettingsPage({required this.controller});

  final NeoCloudController controller;

  @override
  State<_ImportSettingsPage> createState() => _ImportSettingsPageState();
}

class _ImportSettingsPageState extends State<_ImportSettingsPage> with _PageLoad {
  final TextEditingController _url = TextEditingController();
  final TextEditingController _username = TextEditingController();
  final TextEditingController _password = TextEditingController();
  final TextEditingController _sourcePath = TextEditingController(text: '/');
  final GlobalKey<_ImportJobListState> _jobs = GlobalKey<_ImportJobListState>();

  /// The NeoCloud folder files go to; Home when null.
  (String, String)? _target;
  ImportProbe? _probe;

  NeoCloudController get _controller => widget.controller;

  @override
  void dispose() {
    _url.dispose();
    _username.dispose();
    _password.dispose();
    _sourcePath.dispose();
    super.dispose();
  }

  bool get _complete => _url.text.trim().isNotEmpty && _username.text.trim().isNotEmpty && _password.text.isNotEmpty;

  Map<String, Object?> get _body => <String, Object?>{
    'source': 'nextcloud',
    'url': normalizeBackendUrl(_url.text),
    'username': _username.text.trim(),
    'password': _password.text,
    'sourcePath': _sourcePath.text.trim().isEmpty ? '/' : _sourcePath.text.trim(),
    'targetFolderId': ?_target?.$1,
  };

  /// A new probe is needed once anything it was made with changes.
  void _changed(String _) {
    _controller.clearInlineError();
    setState(() => _probe = null);
  }

  Future<void> _pickTarget() async {
    final picked = await showFolderPicker(
      context,
      _controller,
      title: appStrings.importTarget,
      confirmLabel: appStrings.syncChooseThisFolder,
      hideVaults: true,
    );
    if (picked != null && mounted) setState(() => _target = picked);
  }

  Future<void> _test() => runAction(_controller, () async {
    final probe = await _controller.client.testImport(_body);
    if (mounted) setState(() => _probe = probe);
  });

  Future<void> _start() => runAction(_controller, () async {
    await _controller.client.startImport(_body);
    _password.clear();
    if (mounted) setState(() => _probe = null);
    await _jobs.currentState?.reload();
  }, notice: appStrings.importStarted);

  Future<void> _explainAppPassword() => showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      backgroundColor: _bgCard,
      title: Text(appStrings.importAppPasswordHelpTitle),
      content: SizedBox(
        width: 440,
        child: Text(appStrings.importAppPasswordHelp, style: TextStyle(color: _textSecondary, height: 1.5)),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => unawaited(launchUrl(Uri.parse(_nextcloudAppPasswordDocs), webOnlyWindowName: '_blank')),
          child: Text(appStrings.importAppPasswordDocs),
        ),
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close)),
      ],
    ),
  );

  Widget _field(TextEditingController controller, String label, {String? hint, bool obscure = false, TextInputType? keyboard}) {
    return TextField(
      controller: controller,
      obscureText: obscure,
      autocorrect: false,
      keyboardType: keyboard,
      onChanged: _changed,
      decoration: InputDecoration(labelText: label, hintText: hint),
    );
  }

  Widget _probeResult(ImportProbe probe) {
    if (!probe.ok) return _InlineError(message: probe.error?.isNotEmpty == true ? probe.error! : appStrings.importTestFailed);
    final usage = _controller.storage;
    final shortfall = usage == null ? null : importShortfall(probe.bytes, usage);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _InlineSuccess(message: appStrings.importTestFound(NumberFormat.decimalPattern(currentAppLanguage.code).format(probe.files), formatBytes(probe.bytes))),
        if (shortfall != null) ...<Widget>[
          const SizedBox(height: AppSpacing.sm),
          _InlineNote(message: appStrings.importQuotaWarning(formatBytes(shortfall)), icon: Icons.warning_amber_rounded, color: _warning),
        ],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final probe = _probe;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.importNextcloudTitle,
          description: appStrings.importNextcloudDescription,
          children: <Widget>[
            _SettingsBlock(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  _field(_url, appStrings.importServerUrl, hint: 'https://cloud.example.com', keyboard: TextInputType.url),
                  const SizedBox(height: 12),
                  _field(_username, appStrings.accountUsername),
                  const SizedBox(height: 12),
                  _field(_password, appStrings.importAppPassword, obscure: true),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: () => unawaited(_explainAppPassword()),
                      icon: const Icon(Icons.help_outline_rounded, size: 16),
                      label: Text(appStrings.importAppPasswordHelpLink),
                    ),
                  ),
                ],
              ),
            ),
            _SettingsRow(
              label: appStrings.importSource,
              description: appStrings.importSourceHint,
              fillControl: true,
              control: TextField(controller: _sourcePath, autocorrect: false, onChanged: _changed, decoration: const InputDecoration(isDense: true)),
            ),
            _SettingsRow(
              label: appStrings.importTarget,
              description: _target?.$2 ?? appStrings.importTargetHint,
              control: OutlinedButton.icon(
                onPressed: () => unawaited(_pickTarget()),
                icon: const Icon(Icons.folder_outlined, size: 18),
                label: Text(appStrings.importChooseFolder),
              ),
            ),
            _SettingsBlock(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  if (probe != null) ...<Widget>[_probeResult(probe), const SizedBox(height: AppSpacing.md)],
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: <Widget>[
                      OutlinedButton(
                        onPressed: busy || !_complete ? null : () => unawaited(_test()),
                        child: busy && probe == null
                            ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                            : Text(appStrings.importTest),
                      ),
                      FilledButton.icon(
                        onPressed: busy || !_complete || probe?.ok == false ? null : () => unawaited(_start()),
                        icon: const Icon(Icons.download_rounded, size: 18),
                        label: Text(appStrings.importStart),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
        _ImportJobList(key: _jobs, controller: _controller),
      ],
    );
  }
}
