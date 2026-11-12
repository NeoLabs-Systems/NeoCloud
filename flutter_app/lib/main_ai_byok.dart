part of 'main.dart';

/// NeoAgent's "bring your own key" settings: an account stores its own
/// per-provider API keys, or a custom OpenAI-compatible endpoint, instead of
/// relying on the server's. Keys are encrypted on the server and never shown
/// again once saved; only a "configured" state is displayed. NeoRouter comes
/// first, as its own row.
class ByokSettingsCard extends StatefulWidget {
  const ByokSettingsCard({super.key, required this.controller, this.onChanged});

  final NeoCloudController controller;

  /// After a key or endpoint was saved or removed, so the page reloads the
  /// models it offers.
  final VoidCallback? onChanged;

  @override
  State<ByokSettingsCard> createState() => _ByokSettingsCardState();
}

class _ByokSettingsCardState extends State<ByokSettingsCard> {
  List<Map<String, dynamic>>? _providers;
  bool _loading = false;
  String? _loadError;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final providers = await widget.controller.ai.byokProviders();
      if (!mounted) return;
      setState(() {
        _providers = providers;
        _loadError = null;
      });
    } catch (error) {
      if (mounted) setState(() => _loadError = _aiFriendlyError(widget.controller, error));
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _changed() async {
    await _load();
    widget.onChanged?.call();
  }

  @override
  Widget build(BuildContext context) {
    final providers = _providers;
    return _SettingsGroup(
      title: appStrings.aiByokTitle,
      description: appStrings.aiByokDescription,
      trailing: IconButton(
        tooltip: appStrings.refresh,
        onPressed: _loading ? null : () => unawaited(_load()),
        icon: _loading
            ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
            : const Icon(Icons.refresh_rounded, size: 20),
      ),
      children: <Widget>[
        NeoRouterConnectCard(controller: widget.controller, scope: 'user', onChanged: widget.onChanged),
        if (providers == null && _loadError != null)
          _SettingsBlock(child: _InlineError(message: _loadError!))
        else if (providers == null)
          const _LoadingPlaceholder()
        else if (providers.isEmpty)
          _SettingsBlock(child: _EmptyText(appStrings.aiByokNoProviders))
        else
          for (final provider in providers) _ByokProviderRow(controller: widget.controller, provider: provider, onChanged: _changed),
      ],
    );
  }
}

class _ByokProviderRow extends StatelessWidget {
  const _ByokProviderRow({required this.controller, required this.provider, required this.onChanged});

  final NeoCloudController controller;
  final Map<String, dynamic> provider;
  final Future<void> Function() onChanged;

  String get _id => provider['id']?.toString() ?? '';

  String get _label => provider['label']?.toString() ?? _id;

  bool get _supportsApiKey => provider['supportsApiKey'] == true;

  // A provider with no API key concept (e.g. Ollama) is only ever "yours"
  // by pointing it at a custom address -- same connect-a-URL flow as a
  // custom OpenAI-compatible endpoint.
  bool get _isUrlOnly => provider['isCustomEndpoint'] == true || !_supportsApiKey;

  @override
  Widget build(BuildContext context) {
    final description = provider['description']?.toString() ?? '';
    final configured = provider['configured'] == true;
    final customLabel = provider['customLabel']?.toString() ?? '';
    final baseUrl = provider['baseUrl']?.toString() ?? '';
    final title = _isUrlOnly && customLabel.isNotEmpty ? customLabel : _label;
    String? subtitle = description.isEmpty ? null : description;
    if (configured && _isUrlOnly && baseUrl.isNotEmpty) subtitle = baseUrl;
    final String editLabel;
    if (configured) {
      editLabel = appStrings.aiUpdate;
    } else {
      editLabel = _isUrlOnly ? appStrings.aiConnectEndpoint : appStrings.aiAddKey;
    }
    return _SettingsRow(
      label: title,
      description: subtitle,
      control: Wrap(
        spacing: 12,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: <Widget>[
          _StatusPill(label: configured ? appStrings.aiByokUsingYourKey : appStrings.aiNotSetUp, color: configured ? _success : _textMuted),
          OutlinedButton(onPressed: () => unawaited(_openEditDialog(context)), child: Text(editLabel)),
          if (configured)
            IconButton(tooltip: appStrings.aiRemove, onPressed: () => unawaited(_confirmRemove(context)), icon: const Icon(Icons.delete_outline, size: 18)),
        ],
      ),
    );
  }

  Future<void> _openEditDialog(BuildContext context) async {
    final saved = await showAdaptiveSheet<bool>(
      context: context,
      builder: (_) => _ByokEditDialog(
        controller: controller,
        providerId: _id,
        providerLabel: _label,
        isCustomEndpoint: _isUrlOnly,
        supportsApiKey: _supportsApiKey,
        requiresBaseUrl: provider['requiresBaseUrl'] == true,
        supportsBaseUrl: provider['supportsBaseUrl'] == true,
        defaultBaseUrl: provider['defaultBaseUrl']?.toString() ?? '',
        initialBaseUrl: provider['baseUrl']?.toString() ?? '',
        initialLabel: provider['customLabel']?.toString() ?? '',
      ),
    );
    if (saved == true) await onChanged();
  }

  Future<void> _confirmRemove(BuildContext context) => _confirmDelete(
    context,
    title: appStrings.aiRemoveKeyTitle(_label),
    message: appStrings.aiRemoveKeyBody(_label),
    confirmLabel: appStrings.aiRemove,
    onConfirm: () async {
      final messenger = ScaffoldMessenger.of(context);
      try {
        await controller.ai.clearByok(_id);
        messenger.showSnackBar(SnackBar(content: Text(appStrings.aiKeyRemoved(_label))));
        await onChanged();
      } catch (error) {
        messenger.showSnackBar(SnackBar(content: Text(_aiFriendlyError(controller, error))));
      }
    },
  );
}

class _ByokEditDialog extends StatefulWidget {
  const _ByokEditDialog({
    required this.controller,
    required this.providerId,
    required this.providerLabel,
    required this.isCustomEndpoint,
    required this.supportsApiKey,
    required this.requiresBaseUrl,
    required this.supportsBaseUrl,
    required this.defaultBaseUrl,
    required this.initialBaseUrl,
    required this.initialLabel,
  });

  final NeoCloudController controller;
  final String providerId;
  final String providerLabel;
  final bool isCustomEndpoint;
  final bool supportsApiKey;
  final bool requiresBaseUrl;
  final bool supportsBaseUrl;
  final String defaultBaseUrl;
  final String initialBaseUrl;
  final String initialLabel;

  @override
  State<_ByokEditDialog> createState() => _ByokEditDialogState();
}

class _ByokEditDialogState extends State<_ByokEditDialog> {
  final TextEditingController _apiKeyController = TextEditingController();
  late final TextEditingController _baseUrlController = TextEditingController(
    text: widget.initialBaseUrl.isNotEmpty ? widget.initialBaseUrl : widget.defaultBaseUrl,
  );
  late final TextEditingController _labelController = TextEditingController(text: widget.initialLabel);
  bool _obscureKey = true;
  bool _saving = false;
  bool _testing = false;
  String? _testMessage;
  bool? _testOk;
  String? _errorMessage;

  bool get _baseUrlRequired => widget.requiresBaseUrl || !widget.supportsApiKey;

  @override
  void dispose() {
    _apiKeyController.dispose();
    _baseUrlController.dispose();
    _labelController.dispose();
    super.dispose();
  }

  Future<void> _test() async {
    final apiKey = _apiKeyController.text.trim();
    if (widget.supportsApiKey && apiKey.isEmpty) {
      setState(() {
        _testOk = false;
        _testMessage = appStrings.aiEnterKeyFirst;
      });
      return;
    }
    setState(() {
      _testing = true;
      _testMessage = null;
      _testOk = null;
    });
    try {
      final result = await widget.controller.ai.testByok(
        widget.providerId,
        apiKey: widget.supportsApiKey ? apiKey : null,
        baseUrl: widget.supportsBaseUrl ? _baseUrlController.text.trim() : null,
      );
      if (!mounted) return;
      final ok = result['ok'] == true;
      setState(() {
        _testOk = ok;
        _testMessage = ok
            ? (result['message']?.toString() ?? appStrings.aiConnectionGood)
            : (result['error']?.toString() ?? appStrings.aiCouldNotConnect);
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _testOk = false;
        _testMessage = _aiFriendlyError(widget.controller, error);
      });
    } finally {
      if (mounted) setState(() => _testing = false);
    }
  }

  Future<void> _save() async {
    final apiKey = _apiKeyController.text.trim();
    if (widget.supportsApiKey && apiKey.isEmpty) {
      setState(() => _errorMessage = appStrings.aiKeyRequired);
      return;
    }
    final baseUrl = _baseUrlController.text.trim();
    if (_baseUrlRequired && baseUrl.isEmpty) {
      setState(() => _errorMessage = appStrings.aiBaseUrlRequired);
      return;
    }
    setState(() {
      _saving = true;
      _errorMessage = null;
    });
    try {
      await widget.controller.ai.saveByok(
        widget.providerId,
        apiKey: widget.supportsApiKey ? apiKey : '',
        baseUrl: widget.supportsBaseUrl ? baseUrl : null,
        label: widget.isCustomEndpoint ? _labelController.text.trim() : null,
      );
      if (mounted) Navigator.of(context).pop(true);
    } catch (error) {
      if (mounted) setState(() => _errorMessage = _aiFriendlyError(widget.controller, error));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final busy = _saving || _testing;
    return _AdaptiveDialog(
      title: Text(widget.isCustomEndpoint ? appStrings.aiCustomEndpointTitle : appStrings.aiYourKeyTitle(widget.providerLabel)),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              widget.isCustomEndpoint ? appStrings.aiCustomEndpointBody : appStrings.aiYourKeyBody(widget.providerLabel),
              style: TextStyle(color: _textSecondary, height: 1.4),
            ),
            const SizedBox(height: 16),
            if (widget.isCustomEndpoint) ...<Widget>[
              TextField(
                controller: _labelController,
                decoration: InputDecoration(labelText: appStrings.aiNameOptional, hintText: appStrings.aiNameHint),
              ),
              const SizedBox(height: 12),
            ],
            if (widget.supportsBaseUrl) ...<Widget>[
              TextField(
                controller: _baseUrlController,
                keyboardType: TextInputType.url,
                autocorrect: false,
                decoration: InputDecoration(
                  labelText: _baseUrlRequired ? appStrings.aiBaseUrl : appStrings.aiBaseUrlOptional,
                  hintText: widget.defaultBaseUrl.isNotEmpty ? widget.defaultBaseUrl : 'https://api.example.com/v1',
                  helperText: appStrings.aiBaseUrlHelper,
                  helperMaxLines: 2,
                ),
              ),
              const SizedBox(height: 12),
            ],
            if (widget.supportsApiKey)
              TextField(
                controller: _apiKeyController,
                obscureText: _obscureKey,
                autocorrect: false,
                enableSuggestions: false,
                decoration: InputDecoration(
                  labelText: appStrings.aiApiKey,
                  hintText: 'sk-…',
                  suffixIcon: IconButton(
                    tooltip: _obscureKey ? appStrings.aiShow : appStrings.aiHide,
                    icon: Icon(_obscureKey ? Icons.visibility_outlined : Icons.visibility_off_outlined),
                    onPressed: () => setState(() => _obscureKey = !_obscureKey),
                  ),
                ),
              ),
            const SizedBox(height: 6),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: busy ? null : () => unawaited(_test()),
                icon: _testing
                    ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                    : const Icon(Icons.wifi_tethering_rounded, size: 16),
                label: Text(appStrings.aiTestConnection),
              ),
            ),
            if (_testMessage != null) Text(_testMessage!, style: TextStyle(color: _testOk == true ? _success : _danger, fontSize: 12.5)),
            if (_errorMessage != null) ...<Widget>[const SizedBox(height: 10), _InlineError(message: _errorMessage!)],
            const SizedBox(height: 10),
            Text(appStrings.aiByokLimitsNote, style: TextStyle(color: _textMuted, fontSize: 12)),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: busy ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: busy ? null : () => unawaited(_save()),
          child: _saving ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.save),
        ),
      ],
    );
  }
}
