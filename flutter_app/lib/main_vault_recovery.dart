part of 'main.dart';

// Inside an unlocked vault: the banner (what vaults don't do, by design)
// and "Show recovery key" behind the passphrase.

/// Above the contents of an unlocked vault: what vaults don't do, and the
/// recovery key and lock actions.
class _VaultBanner extends StatelessWidget {
  const _VaultBanner({required this.controller, required this.vaultId, required this.folderName});

  final NeoCloudController controller;
  final String vaultId;
  final String folderName;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 8, 8, 8),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control)),
      child: Row(
        children: <Widget>[
          Icon(Icons.lock_outline_rounded, size: 17, color: _textSecondary),
          const SizedBox(width: 10),
          Expanded(child: Text(appStrings.vaultBanner, style: TextStyle(color: _textPrimary, fontSize: 13, height: 1.4))),
          const SizedBox(width: 8),
          PopupMenuButton<String>(
            tooltip: appStrings.details,
            icon: Icon(Icons.more_horiz_rounded, size: 20, color: _textMuted),
            onSelected: (action) {
              if (action == 'recovery') unawaited(showVaultRecoveryKeyDialog(context, controller, vaultId: vaultId, folderName: folderName));
              if (action == 'lock') controller.vaults.lock(vaultId);
            },
            itemBuilder: (context) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(value: 'recovery', child: Text(appStrings.vaultShowRecovery)),
              PopupMenuItem<String>(value: 'lock', child: Text(appStrings.vaultLock)),
            ],
          ),
        ],
      ),
    );
  }
}

/// "Show recovery key": asks for the passphrase again, then shows the words.
Future<void> showVaultRecoveryKeyDialog(
  BuildContext context,
  NeoCloudController controller, {
  required String vaultId,
  required String folderName,
}) {
  return showDialog<void>(
    context: context,
    builder: (_) => _ShowRecoveryKeyDialog(controller: controller, vaultId: vaultId, folderName: folderName),
  );
}

class _ShowRecoveryKeyDialog extends StatefulWidget {
  const _ShowRecoveryKeyDialog({required this.controller, required this.vaultId, required this.folderName});

  final NeoCloudController controller;
  final String vaultId;
  final String folderName;

  @override
  State<_ShowRecoveryKeyDialog> createState() => _ShowRecoveryKeyDialogState();
}

class _ShowRecoveryKeyDialogState extends State<_ShowRecoveryKeyDialog> {
  final TextEditingController _passphrase = TextEditingController();
  List<String>? _words;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _passphrase.dispose();
    super.dispose();
  }

  Future<void> _reveal() async {
    if (_passphrase.text.isEmpty || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final words = await widget.controller.vaults.recoveryWords(widget.vaultId, _passphrase.text);
      if (!mounted) return;
      setState(() {
        _busy = false;
        _words = words;
        if (words == null) _error = appStrings.vaultWrongPassphrase;
      });
      _passphrase.clear();
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
    final words = _words;
    return AlertDialog(
      title: Text(appStrings.vaultRecoveryTitle),
      content: SizedBox(
        width: 520,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (words == null) ...<Widget>[
                Text(appStrings.vaultShowRecoveryIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
                const SizedBox(height: AppSpacing.md),
                TextField(
                  controller: _passphrase,
                  autofocus: true,
                  obscureText: true,
                  enabled: !_busy,
                  onSubmitted: (_) => unawaited(_reveal()),
                  decoration: InputDecoration(labelText: appStrings.vaultPassphrase),
                ),
              ] else ...<Widget>[
                Text(appStrings.vaultRecoveryIntro, style: TextStyle(color: _textSecondary, height: 1.45)),
                const SizedBox(height: AppSpacing.md),
                _RecoveryWordsGrid(words: words),
                const SizedBox(height: AppSpacing.sm),
                _RecoveryWordActions(words: words, vaultName: widget.folderName, server: _serverLabel(widget.controller)),
                const SizedBox(height: AppSpacing.sm),
                _InlineNote(message: appStrings.vaultRecoveryDontStore, icon: Icons.warning_amber_rounded, color: _warning),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: words == null
          ? <Widget>[
              TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
              FilledButton(
                onPressed: _busy ? null : () => unawaited(_reveal()),
                child: _busy
                    ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                    : Text(appStrings.vaultShowRecovery),
              ),
            ]
          : <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close))],
    );
  }
}
