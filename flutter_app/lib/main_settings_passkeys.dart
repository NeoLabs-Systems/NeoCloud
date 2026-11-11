part of 'main.dart';

// Settings › Security › Security keys, ported from NeoAgent's account
// settings: the registered keys with rename and remove, and a name field
// with "Add security key". Keys made elsewhere stay listed and manageable on
// a device that cannot add one.

class _SecurityKeysSection extends StatefulWidget {
  const _SecurityKeysSection({required this.controller});

  final NeoCloudController controller;

  @override
  State<_SecurityKeysSection> createState() => _SecurityKeysSectionState();
}

class _SecurityKeysSectionState extends State<_SecurityKeysSection> with _PageLoad {
  final TextEditingController _label = TextEditingController();
  List<SecurityKeyItem>? _keys;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _label.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final keys = await _controller.client.securityKeys();
      if (mounted) setState(() => _keys = keys);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _addSecurityKey() async {
    final typed = _label.text.trim();
    final label = typed.isEmpty ? appStrings.passkeyDefaultName((_keys?.length ?? 0) + 1) : typed;
    var cancelled = false;
    await runAction(_controller, () async {
      final begin = await _controller.client.beginSecurityKeyRegistration();
      final options = begin['options'];
      if (options is! Map) throw WebAuthnException(appStrings.passkeyRegistrationFailed);
      final Map<String, dynamic> attestation;
      try {
        attestation = await _controller.webAuthn.createCredential(options.cast<String, dynamic>());
      } on WebAuthnException catch (error) {
        if (!error.cancelled) rethrow;
        cancelled = true;
        return;
      }
      final keys = await _controller.client.completeSecurityKeyRegistration(attestation, label);
      if (mounted) setState(() => _keys = keys);
    });
    if (!mounted || cancelled || pageError != null) return;
    _label.clear();
  }

  Future<void> _renameSecurityKey(SecurityKeyItem key) async {
    final nextLabel = await _promptText(
      context,
      title: appStrings.passkeyRenameTitle,
      label: appStrings.passkeyName,
      confirmLabel: appStrings.save,
      initialValue: key.label,
    );
    if (nextLabel == null || nextLabel.isEmpty || !mounted) return;
    await runAction(_controller, () async {
      final keys = await _controller.client.renameSecurityKey(key.id, nextLabel);
      if (mounted) setState(() => _keys = keys);
    });
  }

  Future<void> _removeSecurityKey(SecurityKeyItem key) => runAction(_controller, () async {
    final keys = await _controller.client.deleteSecurityKey(key.id);
    if (mounted) setState(() => _keys = keys);
  });

  /// Adding a key needs an authenticator this device can talk to.
  Widget _buildAddSecurityKey() {
    if (!_controller.supportsSecurityKeys) {
      return Text(appStrings.passkeyUnsupported, style: TextStyle(color: _textSecondary, height: 1.4));
    }
    return LayoutBuilder(
      builder: (context, constraints) {
        final nameField = TextField(
          controller: _label,
          decoration: InputDecoration(labelText: appStrings.passkeyNameOptional, hintText: appStrings.passkeyNameHint),
          onSubmitted: (_) => busy ? null : unawaited(_addSecurityKey()),
        );
        // Side by side the button leaves the name field unusably narrow
        // below this width, so the two stack instead.
        final stacked = constraints.maxWidth < 520;
        final addButton = FilledButton.icon(
          onPressed: busy ? null : () => unawaited(_addSecurityKey()),
          icon: busy
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.key),
          label: Text(appStrings.passkeyAdd),
          style: FilledButton.styleFrom(minimumSize: stacked ? const Size.fromHeight(48) : const Size(0, 48)),
        );
        if (stacked) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[nameField, const SizedBox(height: 12), addButton],
          );
        }
        return Row(
          children: <Widget>[Expanded(child: nameField), const SizedBox(width: 12), addButton],
        );
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final keys = _keys;
    return _SettingsGroup(
      title: appStrings.passkeysTitle,
      description: appStrings.passkeysDescription,
      trailing: keys == null
          ? null
          : _StatusPill(
              label: keys.isEmpty ? appStrings.passkeysNone : appStrings.passkeysRegistered(keys.length),
              color: keys.isEmpty ? _warning : _success,
            ),
      children: <Widget>[
        if (pageError != null)
          _SettingsBlock(child: _InlineError(message: pageError!, onDismiss: () => setState(() => pageError = null))),
        if (keys == null) const _LoadingPlaceholder(),
        for (final key in keys ?? const <SecurityKeyItem>[])
          _SecurityKeyCard(
            securityKey: key,
            busy: busy,
            onRename: () => unawaited(_renameSecurityKey(key)),
            onRemove: () => unawaited(_removeSecurityKey(key)),
          ),
        _SettingsBlock(child: _buildAddSecurityKey()),
      ],
    );
  }
}

class _SecurityKeyCard extends StatelessWidget {
  const _SecurityKeyCard({required this.securityKey, required this.busy, required this.onRename, required this.onRemove});

  final SecurityKeyItem securityKey;
  final bool busy;
  final VoidCallback onRename;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final locale = currentAppLanguage.code;
    final added = securityKey.createdAt == null ? appStrings.passkeyNotRecorded : formatDate(securityKey.createdAt, locale);
    final lastUsed = securityKey.lastUsedAt == null ? appStrings.passkeyNeverUsed : formatDate(securityKey.lastUsedAt, locale);
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(securityKey.backedUp ? Icons.cloud_sync : Icons.key, color: _textSecondary),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(securityKey.label, style: const TextStyle(fontWeight: FontWeight.w700)),
                const SizedBox(height: 4),
                Text(appStrings.passkeyAddedLastUsed(added, lastUsed), style: TextStyle(color: _textSecondary)),
              ],
            ),
          ),
          PopupMenuButton<String>(
            enabled: !busy,
            icon: Icon(Icons.more_horiz, color: _textSecondary),
            onSelected: (action) => action == 'rename' ? onRename() : onRemove(),
            itemBuilder: (context) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(value: 'rename', child: Text(appStrings.rename)),
              PopupMenuItem<String>(value: 'remove', child: Text(appStrings.passkeyRemove)),
            ],
          ),
        ],
      ),
    );
  }
}
