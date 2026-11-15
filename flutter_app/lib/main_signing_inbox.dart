part of 'main.dart';

// Signing for an account that is also asked to sign: the "To sign" and
// "Signed by me" tabs of the Signing page (requests sent to the account's
// verified address), the count on the sidebar, and the session helpers the
// signer's page uses to offer the account's saved details.

extension NeoCloudSigningAccount on NeoCloudController {
  /// Refreshes the count on the Signing entry; quiet when it can't.
  Future<void> refreshSigningTodo() async {
    if (!isAuthenticated) return;
    try {
      final page = await signingAccount.inbox(done: false);
      setSigningTodo(page.todo);
    } catch (_) {
      // Older servers have no inbox; the badge just stays away.
    }
  }

  /// The account signed in on this instance, for a signing page: the app's
  /// own session, else the browser's cookie session (asked once per page).
  Future<AccountUser?> findSigningSession() async {
    if (user != null) return user;
    if (signingSessionChecked) return signingSession;
    signingSessionChecked = true;
    if (usesDeviceLogin) return null;
    try {
      final status = await client.authStatus();
      if (status['authenticated'] == true && status['user'] is Map) {
        signingSession = AccountUser.fromJson((status['user'] as Map).cast<String, dynamic>());
      }
    } catch (_) {
      // Signed out as far as we can tell.
    }
    return signingSession;
  }

  /// "Have an account? Sign in": leaves the signing page for the sign-in and
  /// comes back to this link afterwards. The link only travels in memory.
  void signInThenReturnToSigning() {
    final token = signingToken;
    if (token == null) return;
    _requestedRoute = AppRoute(AppRoute.signSection, token);
    finishSigning();
  }
}

enum _SigningTab { mine, todo, done }

/// "My documents" · "To sign" · "Signed by me".
class _SigningTabs extends StatelessWidget {
  const _SigningTabs({required this.tab, required this.todo, required this.onSelect});

  final _SigningTab tab;
  final int todo;
  final ValueChanged<_SigningTab> onSelect;

  @override
  Widget build(BuildContext context) {
    Widget chip(_SigningTab value, String label, {int count = 0}) => ChoiceChip(
      key: ValueKey<String>('signing-tab-${value.name}'),
      label: Row(mainAxisSize: MainAxisSize.min, children: <Widget>[Text(label), if (count > 0) ...<Widget>[const SizedBox(width: 8), _CountBadge(count: count)]]),
      selected: tab == value,
      showCheckmark: false,
      onSelected: (_) => onSelect(value),
    );
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: <Widget>[
        chip(_SigningTab.mine, appStrings.signingTabMine),
        chip(_SigningTab.todo, appStrings.signingTabTodo, count: todo),
        chip(_SigningTab.done, appStrings.signingTabDone),
      ],
    );
  }
}

/// The requests addressed to this account; [done] picks the signed ones.
class _SigningInboxView extends StatefulWidget {
  const _SigningInboxView({super.key, required this.controller, required this.done});

  final NeoCloudController controller;
  final bool done;

  @override
  State<_SigningInboxView> createState() => _SigningInboxViewState();
}

class _SigningInboxViewState extends State<_SigningInboxView> {
  List<InboxItem>? _items;
  String? _error;
  String? _opening;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _controller.signingRevision.addListener(_changed);
    unawaited(_load());
  }

  @override
  void dispose() {
    _controller.signingRevision.removeListener(_changed);
    super.dispose();
  }

  void _changed() => unawaited(_load(quiet: true));

  Future<void> _load({bool quiet = false}) async {
    try {
      final page = await _controller.signingAccount.inbox(done: widget.done);
      if (!mounted) return;
      if (!widget.done) _controller.setSigningTodo(page.todo);
      setState(() {
        _items = page.items;
        _error = null;
      });
    } catch (error) {
      if (mounted && !quiet) setState(() => _error = _controller.friendlyError(error));
    }
  }

  Future<void> _open(InboxItem item) async {
    if (_opening != null) return;
    setState(() => _opening = item.partyId);
    try {
      final url = await _controller.signingAccount.openInbox(item.partyId);
      if (mounted) _controller.openSigningLink(url);
    } catch (error) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(signingErrorText(_controller, error, appStrings.signingInboxOpenFailed))));
    } finally {
      if (mounted) setState(() => _opening = null);
    }
  }

  Future<void> _openSaved(InboxItem item) async {
    try {
      final node = await _controller.client.getNode(item.savedNodeId!);
      _controller.openFolder(node.parentId);
    } catch (error) {
      if (mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(signingErrorText(_controller, error, appStrings.signingInboxOpenFailed))));
    }
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    if (_error != null) return _LoadError(message: _error!, onRetry: () => unawaited(_load()));
    if (items == null) return const _LoadingPlaceholder();
    if (items.isEmpty) {
      return _EmptyCard(
        icon: widget.done ? Icons.task_alt_rounded : Icons.edit_note_rounded,
        title: widget.done ? appStrings.signingInboxEmptyDoneTitle : appStrings.signingInboxEmptyTodoTitle,
        subtitle: widget.done ? appStrings.signingInboxEmptyDoneBody : appStrings.signingInboxEmptyTodoBody,
      );
    }
    return _EntranceMotion(
      child: _AppCard(
        padding: EdgeInsets.zero,
        child: Column(
          children: <Widget>[
            for (var i = 0; i < items.length; i++) ...<Widget>[
              if (i > 0) Divider(height: 1, color: _border),
              _InboxRow(
                key: ValueKey<String>('inbox-row-${items[i].partyId}'),
                item: items[i],
                done: widget.done,
                busy: _opening == items[i].partyId,
                onOpen: () => unawaited(_open(items[i])),
                onOpenSaved: items[i].savedNodeId == null ? null : () => unawaited(_openSaved(items[i])),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _InboxRow extends StatelessWidget {
  const _InboxRow({super.key, required this.item, required this.done, required this.busy, required this.onOpen, this.onOpenSaved});

  final InboxItem item;
  final bool done;
  final bool busy;
  final VoidCallback onOpen;
  final VoidCallback? onOpenSaved;

  @override
  Widget build(BuildContext context) {
    final language = currentAppLanguage.code;
    final waiting = item.isWaiting;
    final expires = item.expiresAt;
    final signedAt = item.completedAt;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Row(
        children: <Widget>[
          Icon(done ? Icons.task_alt_rounded : Icons.description_outlined, color: done ? _success : _textMuted, size: 26),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(item.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w500, fontSize: 14)),
                const SizedBox(height: 4),
                Wrap(
                  spacing: 12,
                  runSpacing: 2,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: <Widget>[
                    if (!done) _StatusPill(label: waiting ? appStrings.signingInboxWaiting : appStrings.signingInboxReady, color: waiting ? _textMuted : _success),
                    if (item.creatorName.isNotEmpty) Text(appStrings.signingInboxFrom(item.creatorName), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                    if (!done && expires != null) Text(appStrings.signingExpiresOn(DateFormat.yMMMd(language).format(expires.toLocal())), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                    if (done && signedAt != null) Text(appStrings.signingInboxSignedOn(DateFormat.yMMMd(language).format(signedAt.toLocal())), style: TextStyle(color: _textMuted, fontSize: 12.5)),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(width: 12),
          if (!done)
            OutlinedButton(
              key: ValueKey<String>('inbox-open-${item.partyId}'),
              onPressed: busy ? null : onOpen,
              child: busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.signingInboxOpen),
            )
          else if (onOpenSaved != null)
            OutlinedButton(key: ValueKey<String>('inbox-saved-${item.partyId}'), onPressed: onOpenSaved, child: Text(appStrings.signingInboxOpenSaved)),
        ],
      ),
    );
  }
}
