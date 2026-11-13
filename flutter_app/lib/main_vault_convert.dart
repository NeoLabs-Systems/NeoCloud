part of 'main.dart';

// "Convert to vault…" on a folder (docs/clients.md §4): an explanation, the
// usual new-vault steps, then a progress sheet while this device encrypts
// and uploads every file (src/vault/vault_conversion.dart). The originals go
// to the trash only once everything is in the vault; a stopped conversion
// continues from the same menu entry.

VaultConversionStore _vaultConversions(NeoCloudController controller) => VaultConversionStore(controller._preferences);

/// Whether a conversion of [folderId] was started and not finished.
bool vaultConversionPending(NeoCloudController controller, String folderId) => _vaultConversions(controller).read(folderId) != null;

/// Converts [folder] into a vault, or continues an earlier conversion.
/// Resolves to the vault folder when it took the folder's place.
Future<FileNode?> showConvertToVault(BuildContext context, NeoCloudController controller, FileNode folder) async {
  final store = _vaultConversions(controller);
  var state = store.read(folder.id);
  try {
    if (state != null) {
      try {
        await controller.vaults.info(state.vaultId);
      } on ApiException catch (error) {
        // The partial vault was deleted: nothing to continue.
        if (error.statusCode != 404) rethrow;
        await store.remove(folder.id);
        state = null;
      }
    }
    if (!context.mounted) return null;
    if (state == null) {
      final check = await controller.client.vaultConversionCheck(folder.id);
      if (!context.mounted) return null;
      final go = await showAdaptiveSheet<bool>(context: context, builder: (_) => _VaultConvertIntroDialog(check: check));
      if (go != true || !context.mounted) return null;
      final vaultFolder = await showNewVaultDialog(context, controller, parentId: folder.parentId ?? 'root', presetName: folder.name);
      final vaultId = vaultFolder?.vaultId;
      if (vaultFolder == null || vaultId == null) return null;
      state = VaultConversionState(folderId: folder.id, folderName: folder.name, vaultId: vaultId, vaultFolderId: vaultFolder.id);
      await store.write(state);
    } else if (!controller.vaults.isUnlocked(state.vaultId)) {
      final answer = await showAdaptiveSheet<_VaultResumeAnswer>(
        context: context,
        builder: (_) => _VaultConvertResumeDialog(controller: controller, vaultId: state!.vaultId),
      );
      if (answer == _VaultResumeAnswer.startOver) {
        await store.remove(folder.id);
        if (!context.mounted) return null;
        return showConvertToVault(context, controller, folder);
      }
      if (answer != _VaultResumeAnswer.unlocked) return null;
    }
    if (!context.mounted) return null;
    final run = VaultConversion(
      backend: ClientVaultConversionBackend(controller.client),
      cipher: controller.vaults.conversionCipher(state.vaultId),
      store: store,
      state: state,
    );
    final result = await showAdaptiveSheet<FileNode>(
      context: context,
      dismissible: false,
      builder: (_) => _VaultConvertProgressDialog(run: run, folderName: folder.name),
    );
    controller.notifyFilesChanged();
    return result;
  } catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(controller.friendlyError(error))));
    }
    return null;
  }
}

String _vaultConvertProblem(String code) => switch (code) {
  'SHARED' => appStrings.vaultConvertProblemShared,
  'PUBLIC_LINK' => appStrings.vaultConvertProblemLink,
  'CONTAINS_VAULT' => appStrings.vaultConvertProblemContainsVault,
  'IN_VAULT' => appStrings.vaultConvertProblemInVault,
  _ => appStrings.vaultConvertProblemOther,
};

Widget _vaultConvertText(String text) => Text(text, style: TextStyle(color: _textSecondary, height: 1.45));

/// What happens, or why it can't: shown before the passphrase is chosen.
class _VaultConvertIntroDialog extends StatelessWidget {
  const _VaultConvertIntroDialog({required this.check});

  final VaultConversionCheck check;

  Widget _step(IconData icon, String text) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Icon(icon, size: 18, color: _accent),
        const SizedBox(width: AppSpacing.sm),
        Expanded(child: _vaultConvertText(text)),
      ],
    ),
  );

  @override
  Widget build(BuildContext context) {
    final blocking = check.problems.where((problem) => problem != 'QUOTA').toList();
    final noRoom = check.problems.contains('QUOTA');
    final allowed = blocking.isEmpty && !noRoom;
    return _AdaptiveDialog(
      icon: Icon(allowed ? Icons.lock_outline_rounded : Icons.block_rounded),
      title: Text(allowed ? appStrings.vaultConvertTitle(check.name) : appStrings.vaultConvertRefusedTitle),
      content: SizedBox(
        width: 520,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (allowed) ...<Widget>[
              _vaultConvertText(appStrings.vaultConvertIntro),
              const SizedBox(height: AppSpacing.md),
              _step(Icons.sync_lock_rounded, appStrings.vaultConvertStepCopy),
              _step(Icons.key_rounded, appStrings.vaultConvertStepPassphrase),
              _step(Icons.delete_outline_rounded, appStrings.vaultConvertStepTrash),
              const SizedBox(height: AppSpacing.xs),
              Text(
                appStrings.vaultConvertSummary(check.files, formatBytes(check.bytes)),
                style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: AppSpacing.sm),
              _InlineNote(message: appStrings.vaultConvertTrashSpace),
            ] else ...<Widget>[
              for (final problem in blocking) ...<Widget>[_InlineError(message: _vaultConvertProblem(problem)), const SizedBox(height: AppSpacing.xs)],
              if (noRoom)
                _InlineError(message: appStrings.vaultConvertNoSpace(formatBytes(check.vaultBytes), formatBytes(check.freeBytes ?? 0))),
            ],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(false), child: Text(allowed ? appStrings.cancel : appStrings.close)),
        if (allowed) FilledButton(onPressed: () => Navigator.of(context).pop(true), child: Text(appStrings.vaultConvertStart)),
      ],
    );
  }
}

enum _VaultResumeAnswer { unlocked, startOver }

/// A conversion started earlier, whose vault this session hasn't unlocked.
class _VaultConvertResumeDialog extends StatefulWidget {
  const _VaultConvertResumeDialog({required this.controller, required this.vaultId});

  final NeoCloudController controller;
  final String vaultId;

  @override
  State<_VaultConvertResumeDialog> createState() => _VaultConvertResumeDialogState();
}

class _VaultConvertResumeDialogState extends State<_VaultConvertResumeDialog> {
  final TextEditingController _passphrase = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _passphrase.dispose();
    super.dispose();
  }

  Future<void> _unlock() async {
    if (_passphrase.text.isEmpty || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final unlocked = await widget.controller.vaults.unlock(widget.vaultId, _passphrase.text);
      if (!mounted) return;
      if (unlocked) {
        Navigator.of(context).pop(_VaultResumeAnswer.unlocked);
        return;
      }
      _passphrase.clear();
      setState(() {
        _busy = false;
        _error = appStrings.vaultWrongPassphrase;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = widget.controller.friendlyError(error);
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      icon: const Icon(Icons.lock_rounded),
      title: Text(appStrings.vaultConvertStoppedTitle),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            _vaultConvertText(appStrings.vaultConvertResumeIntro),
            const SizedBox(height: AppSpacing.md),
            TextField(
              controller: _passphrase,
              autofocus: true,
              obscureText: true,
              enabled: !_busy,
              onSubmitted: (_) => unawaited(_unlock()),
              decoration: InputDecoration(labelText: appStrings.vaultPassphrase),
            ),
            if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
            const SizedBox(height: AppSpacing.sm),
            Text(appStrings.vaultConvertForgetNote, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(_VaultResumeAnswer.startOver), child: Text(appStrings.vaultConvertForget)),
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _busy ? null : () => unawaited(_unlock()),
          child: _busy
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(appStrings.vaultConvertContinue),
        ),
      ],
    );
  }
}

/// Runs [run] and shows where it stands. Closes with the vault folder once
/// it replaced the original.
class _VaultConvertProgressDialog extends StatefulWidget {
  const _VaultConvertProgressDialog({required this.run, required this.folderName});

  final VaultConversion run;
  final String folderName;

  @override
  State<_VaultConvertProgressDialog> createState() => _VaultConvertProgressDialogState();
}

class _VaultConvertProgressDialogState extends State<_VaultConvertProgressDialog> {
  bool _stopping = false;

  VaultConversion get _run => widget.run;

  @override
  void initState() {
    super.initState();
    _run.addListener(_changed);
    unawaited(_run.run());
  }

  @override
  void dispose() {
    _run.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  void _start({bool finishWithoutFailed = false}) {
    setState(() => _stopping = false);
    unawaited(_run.run(finishWithoutFailed: finishWithoutFailed));
  }

  void _stop() {
    setState(() => _stopping = true);
    _run.cancel();
  }

  String _interruption() => switch (_run.stopReason) {
    VaultConversionStop.offline => appStrings.vaultConvertOffline,
    VaultConversionStop.quota => appStrings.vaultConvertQuota,
    VaultConversionStop.changing => appStrings.vaultConvertChanging,
    VaultConversionStop.verify => appStrings.vaultConvertVerifyFailed,
    _ => appStrings.vaultConvertError(_run.error is ApiException ? (_run.error! as ApiException).message : '${_run.error}'),
  };

  Widget _progress() {
    final total = _run.totalBytes;
    final fraction = total <= 0 ? (_run.totalFiles <= 0 ? null : _run.doneFiles / _run.totalFiles) : _run.doneBytes / total;
    final finishing = _run.phase == VaultConversionPhase.finishing;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        LinearProgressIndicator(value: finishing ? null : fraction?.clamp(0.0, 1.0)),
        const SizedBox(height: AppSpacing.sm),
        Text(
          finishing
              ? appStrings.vaultConvertFinishing
              : appStrings.vaultConvertProgressFiles(math.min(_run.doneFiles, _run.totalFiles), _run.totalFiles),
          style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600),
        ),
        if (!finishing) ...<Widget>[
          const SizedBox(height: 2),
          Text(
            appStrings.vaultConvertProgressBytes(formatBytes(math.min(_run.doneBytes, total)), formatBytes(total)),
            style: TextStyle(color: _textSecondary, fontSize: 13),
          ),
          if (_run.currentPath.isNotEmpty) ...<Widget>[
            const SizedBox(height: 2),
            Text(_run.currentPath, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12.5)),
          ],
        ],
        const SizedBox(height: AppSpacing.md),
        _InlineNote(message: appStrings.vaultConvertKeepOpen),
      ],
    );
  }

  Widget _failures() {
    const shown = 8;
    final failures = _run.failures;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _vaultConvertText(appStrings.vaultConvertFailedBody(failures.length)),
        const SizedBox(height: AppSpacing.sm),
        for (final failure in failures.take(shown))
          Text(failure.path, maxLines: 1, overflow: TextOverflow.ellipsis, style: GoogleFonts.geistMono(fontSize: 12.5, color: _textPrimary)),
        if (failures.length > shown) Text('…', style: TextStyle(color: _textMuted)),
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final navigator = Navigator.of(context);
    final close = TextButton(onPressed: () => navigator.pop(), child: Text(appStrings.close));
    final (IconData icon, String title, Widget body, List<Widget> actions) = switch (_run.phase) {
      VaultConversionPhase.ready || VaultConversionPhase.copying || VaultConversionPhase.finishing => (
        Icons.sync_lock_rounded,
        appStrings.vaultConvertProgressTitle(widget.folderName),
        _progress(),
        <Widget>[
          TextButton(
            onPressed: _stopping || _run.phase == VaultConversionPhase.finishing ? null : _stop,
            child: Text(appStrings.vaultConvertStop),
          ),
        ],
      ),
      VaultConversionPhase.done => (
        Icons.verified_user_outlined,
        appStrings.vaultConvertDoneTitle(widget.folderName),
        Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            _vaultConvertText(appStrings.vaultConvertDoneBody),
            if (_run.failures.isNotEmpty) ...<Widget>[
              const SizedBox(height: AppSpacing.sm),
              _InlineNote(message: appStrings.vaultConvertDoneSkipped(_run.failures.length), icon: Icons.warning_amber_rounded, color: _warning),
            ],
          ],
        ),
        <Widget>[close, FilledButton(onPressed: () => navigator.pop(_run.result), child: Text(appStrings.vaultConvertOpenVault))],
      ),
      VaultConversionPhase.cancelled => (
        Icons.pause_circle_outline_rounded,
        appStrings.vaultConvertStoppedTitle,
        _vaultConvertText(appStrings.vaultConvertStoppedBody),
        <Widget>[close, FilledButton(onPressed: _start, child: Text(appStrings.vaultConvertContinue))],
      ),
      VaultConversionPhase.interrupted => (
        Icons.pause_circle_outline_rounded,
        appStrings.vaultConvertStoppedTitle,
        _InlineNote(message: _interruption(), icon: Icons.warning_amber_rounded, color: _warning),
        <Widget>[close, FilledButton(onPressed: _start, child: Text(appStrings.vaultConvertContinue))],
      ),
      VaultConversionPhase.review => (
        Icons.warning_amber_rounded,
        appStrings.vaultConvertFailedTitle,
        _failures(),
        <Widget>[
          TextButton(onPressed: () => navigator.pop(), child: Text(appStrings.vaultConvertLater)),
          OutlinedButton(onPressed: () => _start(finishWithoutFailed: true), child: Text(appStrings.vaultConvertFinishAnyway)),
          FilledButton(onPressed: _start, child: Text(appStrings.vaultConvertRetry)),
        ],
      ),
      VaultConversionPhase.refused => (
        Icons.block_rounded,
        appStrings.vaultConvertRefusedTitle,
        Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (_run.stopReason == VaultConversionStop.verify) _InlineError(message: appStrings.vaultConvertVerifyFailed),
            for (final problem in _run.problems) ...<Widget>[_InlineError(message: _vaultConvertProblem(problem)), const SizedBox(height: AppSpacing.xs)],
            const SizedBox(height: AppSpacing.xs),
            _vaultConvertText(appStrings.vaultConvertStoppedBody),
          ],
        ),
        <Widget>[close],
      ),
    };
    return PopScope(
      canPop: !_run.running,
      child: _AdaptiveDialog(
        icon: Icon(icon),
        title: Text(title),
        content: SizedBox(width: 520, child: body),
        actions: actions,
      ),
    );
  }
}
