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

  /// A key signs in on its own, so adding one asks for the password again
  /// (and a code when 2FA is on), like the other confirmations; the dialog
  /// starts the registration and shows a wrong entry in place.
  Future<Map<String, dynamic>?> _confirmIdentity() async {
    final bool twoFactor;
    try {
      twoFactor = await _twoFactorEnabled(_controller);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
      return null;
    }
    if (!mounted) return null;
    return showAdaptiveSheet<Map<String, dynamic>>(
      context: context,
      builder: (_) => _ConfirmIdentityDialog<Map<String, dynamic>>(
        title: appStrings.passkeyAdd,
        message: appStrings.passkeyConfirmMessage,
        confirmLabel: appStrings.passkeyAdd,
        twoFactor: twoFactor,
        action: (password, code, _) async {
          final options = (await _controller.client.beginSecurityKeyRegistration(password, code))['options'];
          if (options is! Map) throw WebAuthnException(appStrings.passkeyRegistrationFailed);
          return options.cast<String, dynamic>();
        },
      ),
    );
  }

  Future<void> _addSecurityKey() async {
    final typed = _label.text.trim();
    final label = typed.isEmpty ? appStrings.passkeyDefaultName((_keys?.length ?? 0) + 1) : typed;
    setState(() => pageError = null);
    final options = await _confirmIdentity();
    if (options == null || !mounted) return;
    var cancelled = false;
    await runAction(_controller, () async {
      final Map<String, dynamic> attestation;
      try {
        attestation = await _controller.webAuthn.createCredential(options);
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

  /// Removing a key asks first, like every destructive action.
  Future<void> _removeSecurityKey(SecurityKeyItem key) => _confirmDelete(
    context,
    title: appStrings.settingsPasskeyRemoveTitle(key.label),
    message: appStrings.settingsPasskeyRemoveMessage,
    confirmLabel: appStrings.passkeyRemove,
    onConfirm: () => runAction(_controller, () async {
      final keys = await _controller.client.deleteSecurityKey(key.id);
      if (mounted) setState(() => _keys = keys);
    }),
  );

  /// Adding a key needs an authenticator this device can talk to. The
  /// phone app would need the native credential APIs: keys are added in a
  /// browser, and listed, renamed and removed here.
  Widget _buildAddSecurityKey() {
    if (!_controller.supportsSecurityKeys) {
      final text = _controller.usesDeviceLogin ? appStrings.passkeyAddInBrowser : appStrings.passkeyUnsupported;
      return Text(text, style: TextStyle(color: _textSecondary, height: 1.4));
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
        if (keys == null && pageError == null) const _LoadingPlaceholder(),
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
                Text(securityKey.label, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: _textPrimary)),
                const SizedBox(height: 3),
                Text(appStrings.passkeyAddedLastUsed(added, lastUsed), style: TextStyle(fontSize: 13, color: _textSecondary, height: 1.45)),
              ],
            ),
          ),
          PopupMenuButton<String>(
            enabled: !busy,
            tooltip: appStrings.details,
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
