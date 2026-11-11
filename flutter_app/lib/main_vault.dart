part of 'main.dart';

// Vault folders (docs/clients.md §4): created with a passphrase that never
// leaves the device and a recovery key the user writes down, unlocked per
// session (main_vault_unlock.dart). Building blocks: main_vault_widgets.dart.

/// Creates a vault folder in [parentId] in three steps: name and
/// passphrase, recovery key, check of three recovery words. Resolves to the
/// new folder, already unlocked.
Future<FileNode?> showNewVaultDialog(BuildContext context, NeoCloudController controller, {required String parentId}) {
  return showDialog<FileNode>(
    context: context,
    barrierDismissible: false,
    builder: (_) => _NewVaultDialog(controller: controller, parentId: parentId),
  );
}

enum _NewVaultStep { details, recovery, confirm }

class _NewVaultDialog extends StatefulWidget {
  const _NewVaultDialog({required this.controller, required this.parentId});

  final NeoCloudController controller;
  final String parentId;

  @override
  State<_NewVaultDialog> createState() => _NewVaultDialogState();
}

class _NewVaultDialogState extends State<_NewVaultDialog> {
  final TextEditingController _name = TextEditingController();
  final _PassphraseDraft _passphrase = _PassphraseDraft();
  final Uint8List _key = newVaultKey();
  late final List<String> _words = encodeRecoveryWords(_key);
  late final List<int> _checkPositions = pickRecoveryCheckPositions();
  late final List<TextEditingController> _checks = List<TextEditingController>.generate(3, (_) => TextEditingController());
  _NewVaultStep _step = _NewVaultStep.details;
  bool _saved = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _passphrase.dispose();
    for (final check in _checks) {
      check.dispose();
    }
    super.dispose();
  }

  void _go(_NewVaultStep step) => setState(() {
    _error = null;
    _step = step;
  });

  void _next() {
    switch (_step) {
      case _NewVaultStep.details:
        final problem = _name.text.trim().isEmpty ? appStrings.vaultNameRequired : _passphrase.problem;
        if (problem != null) {
          setState(() => _error = problem);
          return;
        }
        _go(_NewVaultStep.recovery);
      case _NewVaultStep.recovery:
        _go(_NewVaultStep.confirm);
      case _NewVaultStep.confirm:
        unawaited(_create());
    }
  }

  void _back() {
    switch (_step) {
      case _NewVaultStep.details:
        Navigator.of(context).pop();
      case _NewVaultStep.recovery:
        _go(_NewVaultStep.details);
      case _NewVaultStep.confirm:
        _go(_NewVaultStep.recovery);
    }
  }

  bool get _checksMatch {
    for (var i = 0; i < _checkPositions.length; i++) {
      if (recoveryWordIndex(_checks[i].text) != bip39Words.indexOf(_words[_checkPositions[i]])) return false;
    }
    return true;
  }

  Future<void> _create() async {
    if (!_checksMatch) {
      setState(() => _error = appStrings.vaultRecoveryCheckWrong);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final folder = await widget.controller.vaults.create(
        parentId: widget.parentId,
        name: _name.text.trim(),
        passphrase: _passphrase.passphrase.text,
        key: _key,
      );
      widget.controller.notifyFilesChanged();
      if (mounted) Navigator.of(context).pop(folder);
    } catch (error) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = widget.controller.friendlyError(error);
        });
      }
    }
  }

  List<Widget> _details() {
    return <Widget>[
      Text(appStrings.vaultIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
      const SizedBox(height: AppSpacing.md),
      TextField(
        controller: _name,
        autofocus: true,
        enabled: !_busy,
        decoration: InputDecoration(labelText: appStrings.folderName),
      ),
      const SizedBox(height: AppSpacing.sm),
      _PassphraseFields(draft: _passphrase, enabled: !_busy),
      const SizedBox(height: AppSpacing.md),
      _InlineNote(message: appStrings.vaultWarningCallout, icon: Icons.warning_amber_rounded, color: _warning),
    ];
  }

  List<Widget> _recovery() {
    return <Widget>[
      Text(appStrings.vaultRecoveryIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
      const SizedBox(height: AppSpacing.md),
      _RecoveryWordsGrid(words: _words),
      const SizedBox(height: AppSpacing.sm),
      _RecoveryWordActions(words: _words, vaultName: _name.text.trim(), server: _serverLabel(widget.controller)),
      const SizedBox(height: AppSpacing.sm),
      _InlineNote(message: appStrings.vaultRecoveryDontStore, icon: Icons.warning_amber_rounded, color: _warning),
      const SizedBox(height: AppSpacing.xs),
      CheckboxListTile(
        contentPadding: EdgeInsets.zero,
        controlAffinity: ListTileControlAffinity.leading,
        value: _saved,
        onChanged: (value) => setState(() => _saved = value ?? false),
        title: Text(appStrings.vaultRecoverySaved, style: const TextStyle(fontSize: 14)),
      ),
    ];
  }

  List<Widget> _confirm() {
    return <Widget>[
      Text(appStrings.vaultRecoveryCheckIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
      const SizedBox(height: AppSpacing.md),
      for (var i = 0; i < _checkPositions.length; i++) ...<Widget>[
        TextField(
          controller: _checks[i],
          autofocus: i == 0,
          enabled: !_busy,
          autocorrect: false,
          enableSuggestions: false,
          style: GoogleFonts.geistMono(fontSize: 13.5, color: _textPrimary),
          onChanged: (_) => setState(() => _error = null),
          onSubmitted: (_) => i == _checkPositions.length - 1 ? _next() : null,
          decoration: InputDecoration(labelText: appStrings.vaultRecoveryWordNumber('${_checkPositions[i] + 1}')),
        ),
        const SizedBox(height: AppSpacing.sm),
      ],
      _InlineNote(message: appStrings.vaultWarningCallout, icon: Icons.warning_amber_rounded, color: _warning),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final (String stepLabel, List<Widget> body) = switch (_step) {
      _NewVaultStep.details => (appStrings.vaultStepDetails, _details()),
      _NewVaultStep.recovery => (appStrings.vaultStepRecovery, _recovery()),
      _NewVaultStep.confirm => (appStrings.vaultStepConfirm, _confirm()),
    };
    final canContinue = !_busy && (_step != _NewVaultStep.recovery || _saved);
    return AlertDialog(
      title: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(appStrings.newVault),
          const SizedBox(height: 4),
          Text(stepLabel, style: TextStyle(color: _textMuted, fontSize: 12.5, fontWeight: FontWeight.w500)),
        ],
      ),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              ...body,
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: _busy ? null : _back,
          child: Text(_step == _NewVaultStep.details ? appStrings.cancel : appStrings.vaultBack),
        ),
        FilledButton(
          onPressed: canContinue ? _next : null,
          child: _busy
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(_step == _NewVaultStep.confirm ? appStrings.vaultCreate : appStrings.vaultNext),
        ),
      ],
    );
  }
}
