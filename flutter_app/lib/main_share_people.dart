part of 'main.dart';

// "Share with people" in the share dialog: give other accounts on this
// server access to a file or folder. The owner types the exact email address
// or username; there are deliberately no suggestions, no autocomplete and no
// list of accounts, so nobody can browse who has an account here. An
// unknown name gets the same answer as any name that can't be added.

class _SharePeopleSection extends StatefulWidget {
  const _SharePeopleSection({required this.controller, required this.nodeId, required this.isFolder});

  final NeoCloudController controller;
  final String nodeId;
  final bool isFolder;

  @override
  State<_SharePeopleSection> createState() => _SharePeopleSectionState();
}

class _SharePeopleSectionState extends State<_SharePeopleSection> {
  final TextEditingController _recipient = TextEditingController();
  MemberPermission _permission = MemberPermission.read;
  List<ShareMember>? _members;
  bool _busy = false;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _recipient.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final members = await _controller.client.members(widget.nodeId);
      if (mounted) setState(() => _members = members);
    } catch (error) {
      if (mounted) {
        setState(() {
          _members = const <ShareMember>[];
          _error = _controller.friendlyError(error);
        });
      }
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
      _controller.notifyFilesChanged();
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _add() async {
    final recipient = _recipient.text.trim();
    if (recipient.isEmpty || _busy) return;
    await _run(() async {
      final member = await _controller.client.addMember(widget.nodeId, recipient, _permission);
      if (!mounted) return;
      setState(() {
        _members = <ShareMember>[...?_members, member];
        _recipient.clear();
      });
    });
  }

  Future<void> _change(ShareMember member, MemberPermission permission) => _run(() async {
    final updated = await _controller.client.updateMember(widget.nodeId, member.id, permission);
    if (!mounted) return;
    setState(() => _members = <ShareMember>[for (final m in _members ?? const <ShareMember>[]) m.id == member.id ? updated : m]);
  });

  Future<void> _remove(ShareMember member) => _run(() async {
    await _controller.client.removeMember(widget.nodeId, member.id);
    if (!mounted) return;
    setState(() => _members = (_members ?? const <ShareMember>[]).where((m) => m.id != member.id).toList());
  });

  Widget _field() {
    return TextField(
      controller: _recipient,
      enabled: !_busy,
      // No suggestions of any kind: the name must be known already.
      autocorrect: false,
      enableSuggestions: false,
      autofillHints: null,
      keyboardType: TextInputType.text,
      textInputAction: TextInputAction.done,
      onSubmitted: (_) => unawaited(_add()),
      onChanged: (_) {
        if (_error != null) setState(() => _error = null);
      },
      decoration: InputDecoration(
        hintText: appStrings.memberRecipientHint,
        prefixIcon: Icon(Icons.person_add_alt_outlined, size: 18, color: _textMuted),
      ),
    );
  }

  Widget _permissionChoice() {
    if (!widget.isFolder) return Text(appStrings.memberCanView, style: TextStyle(color: _textSecondary, fontSize: 13));
    return SegmentedButton<MemberPermission>(
      segments: <ButtonSegment<MemberPermission>>[
        ButtonSegment<MemberPermission>(value: MemberPermission.read, label: Text(appStrings.memberCanView)),
        ButtonSegment<MemberPermission>(value: MemberPermission.write, label: Text(appStrings.memberCanEdit)),
      ],
      selected: <MemberPermission>{_permission},
      showSelectedIcon: false,
      onSelectionChanged: _busy ? null : (choice) => setState(() => _permission = choice.first),
    );
  }

  Widget _memberRow(ShareMember member) {
    final permissionLabel = member.permission.canWrite ? appStrings.memberCanEdit : appStrings.memberCanView;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        children: <Widget>[
          CircleAvatar(
            radius: 15,
            backgroundColor: _bgTertiary,
            child: Text(member.label.characters.first.toUpperCase(), style: TextStyle(color: _textSecondary, fontSize: 13)),
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(member.label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 13.5)),
                if (member.displayName != null)
                  Text(member.recipient, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12)),
              ],
            ),
          ),
          if (member.expired) ...<Widget>[_StatusPill(label: appStrings.memberExpired, color: _warning), const SizedBox(width: 8)],
          PopupMenuButton<String>(
            enabled: !_busy,
            tooltip: appStrings.memberChange,
            onSelected: (choice) {
              if (choice == 'remove') {
                unawaited(_remove(member));
              } else {
                unawaited(_change(member, MemberPermission.fromApi(choice)));
              }
            },
            itemBuilder: (context) => <PopupMenuEntry<String>>[
              CheckedPopupMenuItem<String>(value: 'read', checked: !member.permission.canWrite, child: Text(appStrings.memberCanView)),
              if (widget.isFolder)
                CheckedPopupMenuItem<String>(value: 'write', checked: member.permission.canWrite, child: Text(appStrings.memberCanEdit)),
              const PopupMenuDivider(),
              PopupMenuItem<String>(value: 'remove', child: Text(appStrings.memberRemove, style: TextStyle(color: _danger))),
            ],
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: <Widget>[
                  Text(permissionLabel, style: TextStyle(color: _textSecondary, fontSize: 13)),
                  Icon(Icons.arrow_drop_down_rounded, size: 20, color: _textMuted),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final members = _members;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        _SectionTitle(appStrings.sharePeopleTitle),
        const SizedBox(height: 4),
        Text(appStrings.sharePeopleHelp, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
        const SizedBox(height: AppSpacing.sm),
        Row(
          children: <Widget>[
            Expanded(child: _field()),
            const SizedBox(width: 8),
            FilledButton(
              onPressed: _busy ? null : () => unawaited(_add()),
              child: Text(appStrings.memberAdd),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        Align(alignment: Alignment.centerLeft, child: _permissionChoice()),
        if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), _InlineError(message: _error!)],
        const SizedBox(height: AppSpacing.sm),
        if (members == null) const _LoadingPlaceholder(),
        if (members != null && members.isEmpty)
          Text(appStrings.membersEmpty, style: TextStyle(color: _textMuted, fontSize: 12.5)),
        if (members != null) for (final member in members) _memberRow(member),
      ],
    );
  }
}
