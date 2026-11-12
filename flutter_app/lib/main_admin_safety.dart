part of 'main.dart';

// Admin › first-run safety: a checklist (master key exported, backup target
// set up, backup verified) and a banner that stays until the admin confirms
// the master key is stored offline. The server has no setting for that
// confirmation, so it is kept per admin on this device, like whether the
// card is folded. It starts folded once everything is done.

/// Bumped when the master key is confirmed, so the checklist redraws.
final ValueNotifier<int> _adminSafetyRevision = ValueNotifier<int>(0);

String _masterKeyAckKey(NeoCloudController controller) => 'admin.masterKeyAck.${controller.user?.id ?? 0}';

bool _masterKeyAcknowledged(NeoCloudController controller) => controller._preferences.getBool(_masterKeyAckKey(controller)) ?? false;

String _safetyFoldedKey(NeoCloudController controller) => 'admin.safetyFolded.${controller.user?.id ?? 0}';

/// Asks for the admin's password (and a code when 2FA is on), shows
/// NEOCLOUD_MASTER_KEY with copy and download, and records "I stored it"
/// when ticked.
Future<void> _exportMasterKey(BuildContext context, NeoCloudController controller) async {
  final twoFactor = await _twoFactorEnabled(controller);
  if (!context.mounted) return;
  final key = await showAdaptiveSheet<String>(
    context: context,
    builder: (_) => _ConfirmIdentityDialog<String>(
      title: appStrings.adminMasterKey,
      confirmLabel: appStrings.adminShowMasterKey,
      twoFactor: twoFactor,
      action: (password, code, _) => controller.client.adminMasterKey(password, code),
    ),
  );
  if (key == null || !context.mounted) return;
  final stored = await showDialog<bool>(
    context: context,
    barrierDismissible: false,
    builder: (_) => _MasterKeyDialog(masterKey: key, acknowledged: _masterKeyAcknowledged(controller)),
  );
  if (stored == true) {
    await controller._preferences.setBool(_masterKeyAckKey(controller), true);
    _adminSafetyRevision.value += 1;
  }
}

class _MasterKeyDialog extends StatefulWidget {
  const _MasterKeyDialog({required this.masterKey, required this.acknowledged});

  final String masterKey;
  final bool acknowledged;

  @override
  State<_MasterKeyDialog> createState() => _MasterKeyDialogState();
}

class _MasterKeyDialogState extends State<_MasterKeyDialog> {
  late bool _stored = widget.acknowledged;
  bool _copied = false;

  String get _line => 'NEOCLOUD_MASTER_KEY=${widget.masterKey}';

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(appStrings.adminMasterKey),
      content: SizedBox(
        width: 540,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            _InlineNote(message: appStrings.adminMasterKeyDescription, icon: Icons.warning_amber_rounded, color: _warning),
            const SizedBox(height: 14),
            _AppCard(
              padding: const EdgeInsets.all(14),
              child: SelectableText(_line, style: GoogleFonts.geistMono(fontSize: 12.5, color: _textPrimary)),
            ),
            const SizedBox(height: AppSpacing.sm),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[
                OutlinedButton.icon(
                  onPressed: () async {
                    await Clipboard.setData(ClipboardData(text: widget.masterKey));
                    if (mounted) setState(() => _copied = true);
                  },
                  icon: Icon(_copied ? Icons.check_rounded : Icons.copy_rounded, size: 16),
                  label: Text(_copied ? appStrings.copied : appStrings.copy),
                ),
                TextButton.icon(
                  onPressed: () => unawaited(FilePicker.platform
                      .saveFile(fileName: 'neocloud-master-key.txt', bytes: Uint8List.fromList(utf8.encode('$_line\n')))
                      .catchError((Object _) => null)),
                  icon: const Icon(Icons.download_rounded, size: 16),
                  label: Text(appStrings.securityKeyDownload),
                ),
              ],
            ),
            CheckboxListTile(
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              value: _stored,
              onChanged: (value) => setState(() => _stored = value ?? false),
              title: Text(appStrings.securityKeyStored, style: const TextStyle(fontSize: 14)),
            ),
          ],
        ),
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(_stored), child: Text(appStrings.close))],
    );
  }
}

/// The banner and checklist at the top of the admin page; folds to its
/// heading and progress.
class _AdminSafetyCard extends StatefulWidget {
  const _AdminSafetyCard({required this.controller, required this.onOpenBackups});

  final NeoCloudController controller;
  final VoidCallback onOpenBackups;

  @override
  State<_AdminSafetyCard> createState() => _AdminSafetyCardState();
}

class _AdminSafetyCardState extends State<_AdminSafetyCard> {
  List<Map<String, dynamic>>? _targets;
  bool _busy = false;
  String? _error;
  String? _notice;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _adminSafetyRevision.addListener(_changed);
    unawaited(_load());
  }

  @override
  void dispose() {
    _adminSafetyRevision.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _load() async {
    try {
      final data = await _controller.client.adminBackups();
      final targets = ((data['targets'] as List?) ?? const []).map((e) => (e as Map).cast<String, dynamic>()).toList();
      if (mounted) setState(() => _targets = targets);
    } catch (_) {
      // The checklist then shows the master key item only.
      if (mounted) setState(() => _targets = const <Map<String, dynamic>>[]);
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      await action();
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _verify(Map<String, dynamic> target) => _run(() async {
    final result = await _controller.client.adminVerifyBackup((target['id'] as num).toInt());
    if (!mounted) return;
    setState(() {
      if (result['ok'] == true) {
        _notice = appStrings.securityVerifyOk;
      } else {
        _error = appStrings.securityVerifyFailed('${result['error'] ?? ''}');
      }
    });
    await _load();
  });

  Map<String, dynamic>? _map(Object? value) => (value as Map?)?.cast<String, dynamic>();

  /// Folded as the admin left it; otherwise folded once everything is done.
  bool _folded(bool allDone) => _controller._preferences.getBool(_safetyFoldedKey(_controller)) ?? allDone;

  void _setFolded(bool folded) {
    unawaited(_controller._preferences.setBool(_safetyFoldedKey(_controller), folded));
    setState(() {});
  }

  Widget _header({required int done, required int total, required bool folded}) {
    final allDone = done == total;
    return InkWell(
      borderRadius: BorderRadius.circular(AppRadius.control),
      onTap: () => _setFolded(!folded),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 4),
        child: Row(
          children: <Widget>[
            Expanded(child: Text(appStrings.securityChecklistTitle, style: _sectionTitleStyle())),
            _StatusPill(
              label: allDone ? appStrings.adminSafetyAllDone : appStrings.adminSafetyProgress(done, total),
              color: allDone ? _success : _warning,
            ),
            const SizedBox(width: 8),
            Icon(
              folded ? Icons.expand_more_rounded : Icons.expand_less_rounded,
              size: 20,
              color: _textMuted,
              semanticLabel: folded ? appStrings.adminSafetyExpand : appStrings.adminSafetyCollapse,
            ),
          ],
        ),
      ),
    );
  }

  Widget _item({required bool done, required String title, required String hint, Widget? action}) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 8),
      child: Row(
        children: <Widget>[
          Icon(done ? Icons.check_circle_rounded : Icons.radio_button_unchecked_rounded, size: 20, color: done ? _success : _textMuted),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: TextStyle(color: _textPrimary, fontSize: 14, fontWeight: FontWeight.w500)),
                const SizedBox(height: 2),
                Text(hint, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
              ],
            ),
          ),
          const SizedBox(width: 12),
          if (done) _StatusPill(label: appStrings.securityDone, color: _success) else ?action,
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final targets = _targets;
    if (targets == null) return const SizedBox.shrink();
    final keyDone = _masterKeyAcknowledged(_controller);
    final hasTarget = targets.isNotEmpty;
    // Servers with backup verification report lastVerification per target.
    final canVerify = targets.any((target) => target.containsKey('lastVerification'));
    final verified = targets.any((target) => _map(target['lastVerification'])?['ok'] == true);
    final ran = targets.any((target) => _map(target['lastRun'])?['status'] == 'ok');
    final backupDone = canVerify ? verified : ran;
    final done = <bool>[keyDone, hasTarget, backupDone].where((item) => item).length;
    final folded = _folded(done == 3) && _error == null && _notice == null;
    if (folded) {
      return _AppCard(padding: const EdgeInsets.fromLTRB(20, 10, 16, 10), child: _header(done: done, total: 3, folded: true));
    }
    final verifyTarget = targets.where((target) => _map(target['lastRun'])?['status'] == 'ok').firstOrNull ?? targets.firstOrNull;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (!keyDone) ...<Widget>[
          _Notice(message: appStrings.securityBanner, color: _warning, icon: Icons.warning_amber_rounded),
          const SizedBox(height: AppSpacing.sm),
        ],
        _AppCard(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 12),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              _header(done: done, total: 3, folded: false),
              const SizedBox(height: 2),
              Text(appStrings.securityChecklistSubtitle, style: TextStyle(color: _textSecondary, fontSize: 13)),
              const SizedBox(height: 8),
              _item(
                done: keyDone,
                title: appStrings.securityMasterKeyItem,
                hint: appStrings.securityMasterKeyItemHint,
                action: FilledButton(
                  onPressed: _busy ? null : () => unawaited(_run(() => _exportMasterKey(context, _controller))),
                  child: Text(appStrings.securityExportKey),
                ),
              ),
              _item(
                done: hasTarget,
                title: appStrings.securityBackupTargetItem,
                hint: appStrings.securityBackupTargetItemHint,
                action: OutlinedButton(onPressed: widget.onOpenBackups, child: Text(appStrings.securitySetUpBackup)),
              ),
              _item(
                done: backupDone,
                title: canVerify ? appStrings.securityBackupVerifiedItem : appStrings.securityBackupRanItem,
                hint: appStrings.securityBackupVerifiedItemHint,
                action: canVerify && verifyTarget != null && ran
                    ? OutlinedButton(onPressed: _busy ? null : () => unawaited(_verify(verifyTarget)), child: Text(appStrings.securityVerifyNow))
                    : _StatusPill(label: appStrings.securityOpen, color: _warning),
              ),
              if (_error != null) ...<Widget>[const SizedBox(height: 6), _InlineError(message: _error!)],
              if (_notice != null) ...<Widget>[const SizedBox(height: 6), _InlineSuccess(message: _notice!)],
            ],
          ),
        ),
      ],
    );
  }
}

/// Admin › Server › Database: "pending" right after a start is neutral,
/// only "damaged" (or damaged blobs found by the scrub) is red.
Widget _databaseStatus(Map<String, dynamic> health) {
  final state = '${health['database']}';
  final (String label, Color color) = switch (state) {
    'ok' => (appStrings.securityDatabaseOk, _success),
    'pending' => (appStrings.securityDatabasePending, _textMuted),
    'damaged' => (appStrings.securityDatabaseDamaged, _danger),
    _ => (state, _textMuted),
  };
  final checkedAt = DateTime.tryParse('${health['databaseCheckedAt'] ?? ''}')?.toLocal();
  final damaged = (health['damagedBlobs'] as num?)?.toInt() ?? 0;
  return Column(
    crossAxisAlignment: CrossAxisAlignment.end,
    mainAxisSize: MainAxisSize.min,
    children: <Widget>[
      _StatusPill(label: label, color: color),
      if (checkedAt != null)
        Text(appStrings.securityDatabaseCheckedAt(formatDate(checkedAt, currentAppLanguage.code)), style: TextStyle(color: _textMuted, fontSize: 12)),
      if (damaged > 0) ...<Widget>[
        const SizedBox(height: 4),
        _StatusPill(label: appStrings.securityDamagedBlobs(damaged), color: _danger),
      ],
    ],
  );
}
