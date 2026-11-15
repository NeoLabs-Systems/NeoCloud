part of 'main.dart';

// The signer's page for people who have a NeoCloud account: "use my saved
// details" at the start, the line that offers signing in to everyone else,
// and, at the end, where the signed copy was filed.

/// "You're signed in as … Use your saved details to sign faster." Shown on
/// the first step when this browser (or app) has a session. A link another
/// account already claimed just doesn't show it.
class _ClaimBanner extends StatefulWidget {
  const _ClaimBanner({required this.controller, required this.flow, required this.onUsed});

  final NeoCloudController controller;
  final PublicSigningController flow;

  /// The signer's name from the profile, to suggest in the consent step.
  final ValueChanged<String> onUsed;

  @override
  State<_ClaimBanner> createState() => _ClaimBannerState();
}

class _ClaimBannerState extends State<_ClaimBanner> {
  AccountUser? _user;
  bool _checked = false;
  bool _hidden = false;
  bool _busy = false;
  bool _used = false;
  int _filled = 0;
  String? _error;
  List<PrefillItem> _items = const <PrefillItem>[];

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(
      _controller.findSigningSession().then((user) {
        if (mounted) {
          setState(() {
            _user = user;
            _checked = true;
          });
          if (user != null) unawaited(_preview());
        }
      }),
    );
  }

  /// What "Use my details" would fill in, shown before the signer agrees: a
  /// sender names the fields, so the signer sees which saved detail goes where.
  Future<void> _preview() async {
    final token = _controller.signingToken;
    if (token == null) return;
    try {
      final result = await _controller.signingAccount.claim(token, preview: true);
      if (mounted) setState(() => _items = result.prefill);
    } on SigningException catch (error) {
      if (mounted && error.code == 'ALREADY_CLAIMED') setState(() => _hidden = true);
    }
  }

  String _lineFor(PrefillItem item) {
    final field = item.name.trim().isEmpty ? item.type : item.name.trim();
    if (item.imageId != null) return appStrings.signingClaimFillSignature(field);
    return appStrings.signingClaimFillLine(field, '${item.value ?? ''}');
  }

  Widget _fillList() {
    final shown = _items.take(6).toList(growable: false);
    return Padding(
      key: const ValueKey<String>('claim-fill-list'),
      padding: const EdgeInsets.only(top: 8, left: 30),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(appStrings.signingClaimWillFill, style: TextStyle(color: _textSecondary, fontSize: 12.5, fontWeight: FontWeight.w600)),
          for (final item in shown) Text(_lineFor(item), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 12.5, height: 1.4)),
          if (_items.length > shown.length) Text(appStrings.signingClaimFillMore(_items.length - shown.length), style: TextStyle(color: _textMuted, fontSize: 12.5)),
        ],
      ),
    );
  }

  String get _name {
    final user = _user!;
    return (user.displayName ?? '').trim().isEmpty ? user.username : user.displayName!.trim();
  }

  Future<void> _use() async {
    final token = _controller.signingToken;
    if (token == null) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await _controller.signingAccount.claim(token, apply: true);
      await widget.flow.applyProfileFill(result.applied);
      try {
        final profile = await _controller.signingAccount.profile();
        if (profile.displayName.trim().isNotEmpty) widget.onUsed(profile.displayName.trim());
      } catch (_) {
        // The name stays what the signer types.
      }
      if (mounted) {
        setState(() {
          _used = true;
          _filled = widget.flow.profileFilled.length;
        });
      }
    } on SigningException catch (error) {
      if (!mounted) return;
      if (error.code == 'ALREADY_CLAIMED') {
        setState(() => _hidden = true);
      } else {
        setState(() => _error = appStrings.signingClaimFailed);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!_checked || _hidden) return const SizedBox.shrink();
    if (_user == null) {
      return Align(
        alignment: Alignment.centerLeft,
        child: TextButton(
          key: const ValueKey<String>('claim-signin'),
          onPressed: _controller.signInThenReturnToSigning,
          style: TextButton.styleFrom(foregroundColor: _textSecondary, visualDensity: VisualDensity.compact, padding: const EdgeInsets.symmetric(horizontal: 4)),
          child: Text(appStrings.signingSignInHint),
        ),
      );
    }
    if (_used) {
      return Padding(
        padding: const EdgeInsets.only(bottom: AppSpacing.sm),
        child: _Notice(
          key: const ValueKey<String>('claim-done'),
          message: _filled > 0 ? appStrings.signingClaimFilled(_filled) : appStrings.signingClaimNothing,
          color: _filled > 0 ? _success : _warning,
          icon: _filled > 0 ? Icons.check_circle_outline_rounded : Icons.info_outline_rounded,
        ),
      );
    }
    return Container(
      key: const ValueKey<String>('claim-banner'),
      margin: const EdgeInsets.only(bottom: AppSpacing.sm),
      padding: const EdgeInsets.all(AppSpacing.sm),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Padding(padding: const EdgeInsets.only(top: 2), child: Icon(Icons.account_circle_outlined, size: 20, color: _textSecondary)),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(appStrings.signingClaimTitle(_name), style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
                    const SizedBox(height: 2),
                    Text(appStrings.signingClaimBody, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4)),
                  ],
                ),
              ),
            ],
          ),
          if (_items.isNotEmpty) _fillList(),
          if (_error != null) Padding(padding: const EdgeInsets.only(top: 6), child: Text(_error!, style: TextStyle(color: _danger, fontSize: 12.5))),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: <Widget>[
              OutlinedButton(
                key: const ValueKey<String>('claim-use'),
                onPressed: _busy ? null : () => unawaited(_use()),
                child: _busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.signingClaimUse),
              ),
              TextButton(key: const ValueKey<String>('claim-skip'), onPressed: _busy ? null : () => setState(() => _hidden = true), child: Text(appStrings.signingClaimSkip)),
            ],
          ),
        ],
      ),
    );
  }
}

/// "A copy was saved to Signed documents in your cloud" with the buttons to
/// get there, for a signed-in signer once the document is complete.
class _SavedCopyNote extends StatefulWidget {
  const _SavedCopyNote({required this.controller, required this.flow});

  final NeoCloudController controller;
  final PublicSigningController flow;

  @override
  State<_SavedCopyNote> createState() => _SavedCopyNoteState();
}

class _SavedCopyNoteState extends State<_SavedCopyNote> {
  String? _nodeId;
  bool _asked = false;
  int _tries = 0;
  Timer? _retry;

  @override
  void initState() {
    super.initState();
    unawaited(_look());
  }

  @override
  void didUpdateWidget(_SavedCopyNote old) {
    super.didUpdateWidget(old);
    if (_nodeId == null && widget.flow.finalReady && !_asked) unawaited(_look());
  }

  @override
  void dispose() {
    _retry?.cancel();
    super.dispose();
  }

  /// The payload may say it; otherwise the inbox of finished requests does.
  Future<void> _look() async {
    final fromPayload = widget.flow.view?.savedNodeId;
    if (fromPayload != null) {
      if (mounted) setState(() => _nodeId = fromPayload);
      return;
    }
    if (!widget.flow.finalReady) return;
    _asked = true;
    if (await widget.controller.findSigningSession() == null) return;
    try {
      final page = await widget.controller.signingAccount.inbox(done: true);
      final mine = page.items.where((item) => item.partyId == widget.flow.view?.party.id && item.savedNodeId != null).firstOrNull;
      if (mounted && mine != null) setState(() => _nodeId = mine.savedNodeId);
      if (mine == null && _tries++ < 4) {
        // The server files the copy a moment after the document completes.
        _retry = Timer(const Duration(seconds: 4), () {
          _asked = false;
          if (mounted) unawaited(_look());
        });
      }
    } catch (_) {
      // Not signed in here, or an older server: no note.
    }
  }

  Future<void> _open() async {
    final controller = widget.controller;
    final id = _nodeId;
    if (id == null) return;
    controller.finishSigning();
    try {
      final node = await controller.client.getNode(id);
      controller.openFolder(node.parentId);
    } catch (_) {
      // Opening NeoCloud is enough.
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_nodeId == null) return const SizedBox.shrink();
    return Column(
      key: const ValueKey<String>('saved-copy'),
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _Notice(message: appStrings.signingSavedCopy, color: _success, icon: Icons.cloud_done_outlined),
        const SizedBox(height: 8),
        OutlinedButton(key: const ValueKey<String>('saved-copy-open'), onPressed: () => unawaited(_open()), child: Text(appStrings.signingSavedCopyOpen)),
        const SizedBox(height: 4),
        TextButton(key: const ValueKey<String>('saved-copy-neocloud'), onPressed: widget.controller.finishSigning, child: Text(appStrings.signingOpenNeoCloud)),
      ],
    );
  }
}
