part of 'main.dart';

// A locked vault: the passphrase card shown instead of its contents, and
// "Forgot passphrase?" — the recovery words plus a new passphrase re-wrap
// the same vault key (PUT /api/vaults/:id/key), so nothing is re-encrypted.

/// Shown instead of a locked vault's contents.
class _VaultUnlockCard extends StatefulWidget {
  const _VaultUnlockCard({required this.controller, required this.vaultId, required this.folderName});

  final NeoCloudController controller;
  final String vaultId;
  final String folderName;

  @override
  State<_VaultUnlockCard> createState() => _VaultUnlockCardState();
}

class _VaultUnlockCardState extends State<_VaultUnlockCard> {
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
      if (!unlocked) {
        setState(() {
          _busy = false;
          _error = appStrings.vaultWrongPassphrase;
        });
        _passphrase.clear();
      }
    } catch (error) {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = widget.controller.friendlyError(error);
        });
      }
    }
  }

  Future<void> _forgot() async {
    await showAdaptiveSheet<void>(
      context: context,
      dismissible: false,
      builder: (_) => _VaultRecoverDialog(controller: widget.controller, vaultId: widget.vaultId),
    );
  }

  @override
  Widget build(BuildContext context) {
    return _EntranceMotion(
      child: _AppCard(
        padding: const EdgeInsets.symmetric(vertical: 40, horizontal: 20),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 380),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                Center(
                  child: Container(
                    width: 48,
                    height: 48,
                    decoration: BoxDecoration(color: _accentMuted, shape: BoxShape.circle),
                    child: Icon(Icons.lock_rounded, size: 22, color: _accent),
                  ),
                ),
                const SizedBox(height: 16),
                Text(appStrings.vaultLockedTitle(widget.folderName), textAlign: TextAlign.center, style: _displayTitleStyle(18)),
                const SizedBox(height: 6),
                Text(
                  appStrings.vaultLockedSubtitle,
                  textAlign: TextAlign.center,
                  style: TextStyle(color: _textSecondary, fontSize: 14, height: 1.45),
                ),
                const SizedBox(height: 18),
                TextField(
                  controller: _passphrase,
                  autofocus: true,
                  obscureText: true,
                  enabled: !_busy,
                  onSubmitted: (_) => unawaited(_unlock()),
                  decoration: InputDecoration(labelText: appStrings.vaultPassphrase),
                ),
                if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
                const SizedBox(height: AppSpacing.md),
                FilledButton.icon(
                  onPressed: _busy ? null : () => unawaited(_unlock()),
                  icon: _busy
                      ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.lock_open_rounded, size: 18),
                  label: Text(_busy ? appStrings.vaultUnlocking : appStrings.vaultUnlock),
                ),
                const SizedBox(height: AppSpacing.xs),
                TextButton(onPressed: _busy ? null : () => unawaited(_forgot()), child: Text(appStrings.vaultForgot)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _VaultRecoverDialog extends StatefulWidget {
  const _VaultRecoverDialog({required this.controller, required this.vaultId});

  final NeoCloudController controller;
  final String vaultId;

  @override
  State<_VaultRecoverDialog> createState() => _VaultRecoverDialogState();
}

class _VaultRecoverDialogState extends State<_VaultRecoverDialog> {
  final TextEditingController _words = TextEditingController();
  final _PassphraseDraft _passphrase = _PassphraseDraft();
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _words.addListener(() => setState(() => _error = null));
  }

  @override
  void dispose() {
    _words.dispose();
    _passphrase.dispose();
    super.dispose();
  }

  /// The key the typed words stand for, or why they don't.
  (Uint8List?, String?) _parse() {
    final words = splitRecoveryInput(_words.text);
    if (words.isEmpty) return (null, null);
    try {
      return (decodeRecoveryWords(words), null);
    } on RecoveryWordsException catch (error) {
      switch (error.problem) {
        case RecoveryWordsProblem.wrongCount:
          return (null, appStrings.vaultRecoverWordCount(words.length));
        case RecoveryWordsProblem.unknownWord:
          return (null, appStrings.vaultRecoverUnknownWord('${error.position}'));
        case RecoveryWordsProblem.checksum:
          return (null, appStrings.vaultRecoverChecksum);
      }
    }
  }

  Future<void> _submit() async {
    final (key, problem) = _parse();
    final passphraseProblem = _passphrase.problem;
    if (key == null || passphraseProblem != null) {
      setState(() => _error = problem ?? passphraseProblem ?? appStrings.vaultRecoverWordCount(0));
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.controller.vaults.recover(widget.vaultId, key, _passphrase.passphrase.text);
      if (!mounted) return;
      Navigator.of(context).pop();
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.vaultRecovered)));
    } on VaultRecoveryMismatch {
      if (mounted) {
        setState(() {
          _busy = false;
          _error = appStrings.vaultRecoverMismatch;
        });
      }
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
    final (key, problem) = _parse();
    return _AdaptiveDialog(
      title: Text(appStrings.vaultRecoverTitle),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(appStrings.vaultRecoverIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: _words,
                autofocus: true,
                enabled: !_busy,
                minLines: 3,
                maxLines: 5,
                autocorrect: false,
                enableSuggestions: false,
                style: GoogleFonts.geistMono(fontSize: 13, color: _textPrimary),
                decoration: InputDecoration(labelText: appStrings.vaultRecoverWords, alignLabelWithHint: true),
              ),
              const SizedBox(height: 6),
              if (key != null)
                _StatusPill(label: appStrings.vaultRecoverWordsOk, color: _success)
              else if (problem != null)
                Text(problem, style: TextStyle(color: _textMuted, fontSize: 12.5)),
              const SizedBox(height: AppSpacing.md),
              _PassphraseFields(draft: _passphrase, enabled: !_busy && key != null, label: appStrings.vaultNewPassphrase),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _busy || key == null ? null : () => unawaited(_submit()),
          child: _busy
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(appStrings.vaultRecoverSubmit),
        ),
      ],
    );
  }
}
