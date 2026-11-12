part of 'main.dart';

// Admin › Providers and Admin › Models, NeoAgent's two tabs: NeoRouter as
// the main provider and the other providers' keys; the server-wide switch
// and default budget, which models accounts may use, and this month's
// usage. Per-account models and budgets are in the user row's ⋯ menu
// (main_admin_ai_user.dart).

/// Admin › Providers: NeoRouter first, then every server-wide key or
/// address (NeoAgent's Providers tab).
class AdminAiProvidersTab extends StatelessWidget {
  const AdminAiProvidersTab({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        NeoRouterConnectCard(controller: controller, scope: 'server'),
        const SizedBox(height: 16),
        _AdminAiProvidersCard(controller: controller),
      ],
    );
  }
}

/// Admin › Models: the server-wide switch and default budget, which models
/// accounts may use, and this month's usage.
class AdminAiModelsTab extends StatefulWidget {
  const AdminAiModelsTab({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AdminAiModelsTab> createState() => _AdminAiModelsTabState();
}

class _AdminAiModelsTabState extends State<AdminAiModelsTab> with _PageLoad {
  final TextEditingController _budget = TextEditingController();
  AdminAiConfig? _config;

  /// The server has no AI routes (older than AI).
  bool _missing = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _budget.dispose();
    super.dispose();
  }

  void _show(AdminAiConfig config) {
    _config = config;
    final budget = config.monthlyTokens;
    _budget.text = budget == null ? '' : '$budget';
  }

  Future<void> _load() async {
    try {
      final config = await _controller.ai.adminConfig();
      if (!mounted) return;
      setState(() {
        _show(config);
        _missing = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _missing = _aiRouteMissing(error);
        pageError = _aiFriendlyError(_controller, error);
      });
    }
  }

  Future<void> _setEnabled(bool enabled) =>
      runAction(_controller, () async => _show(await _controller.ai.adminSaveConfig(enabled: enabled)));

  Future<void> _saveBudget() {
    final budget = _aiParseTokens(_budget.text, allowEmpty: true);
    if (budget == null) {
      setState(() => pageError = appStrings.aiBudgetInvalid);
      return Future<void>.value();
    }
    return runAction(_controller, () async => _show(await _controller.ai.adminSaveConfig(monthlyTokens: budget)), notice: appStrings.saved);
  }

  @override
  Widget build(BuildContext context) {
    if (_missing) {
      return _AiSetupCard(title: appStrings.aiAdminNotAvailableTitle, body: appStrings.aiAdminNotAvailableBody, icon: Icons.cloud_off_outlined);
    }
    final config = _config;
    if (config == null) {
      if (pageError == null) return const _LoadingPlaceholder();
      return _LoadError(
        message: pageError!,
        onRetry: () {
          setState(() => pageError = null);
          unawaited(_load());
        },
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.aiAdminTitle,
          description: appStrings.aiAdminDescription,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.aiAdminEnable,
              description: appStrings.aiAdminEnableDescription,
              control: Switch(value: config.enabled, onChanged: busy ? null : (value) => unawaited(_setEnabled(value))),
            ),
            _SettingsRow(
              label: appStrings.aiAdminBudget,
              description: appStrings.aiAdminBudgetDescription,
              fillControl: true,
              control: Row(
                children: <Widget>[
                  Expanded(
                    child: TextField(
                      controller: _budget,
                      keyboardType: TextInputType.number,
                      decoration: InputDecoration(isDense: true, hintText: appStrings.aiNoLimit),
                      onSubmitted: (_) => unawaited(_saveBudget()),
                    ),
                  ),
                  const SizedBox(width: 8),
                  OutlinedButton(onPressed: busy ? null : () => unawaited(_saveBudget()), child: Text(appStrings.save)),
                ],
              ),
            ),
          ],
        ),
        _AdminAiModelsGroup(controller: _controller),
        _AdminAiUsageGroup(controller: _controller),
      ],
    );
  }
}

/// A token count typed by an admin: digits with optional separators. Empty
/// is 0 (no limit) when [allowEmpty]; null when it is not a count.
int? _aiParseTokens(String text, {required bool allowEmpty}) {
  final digits = text.trim().replaceAll(RegExp(r'[\s.,_]'), '');
  if (digits.isEmpty) return allowEmpty ? 0 : null;
  final value = int.tryParse(digits);
  return value == null || value < 0 ? null : value;
}

/// NeoAgent's section card: an uppercase eyebrow title, an optional line
/// of description and trailing actions, then its content.
class _AiEyebrowCard extends StatelessWidget {
  const _AiEyebrowCard({required this.title, required this.child, this.description, this.trailing});

  final String title;
  final String? description;
  final Widget? trailing;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return _AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  title.toUpperCase(),
                  style: TextStyle(fontSize: 12, fontWeight: FontWeight.w700, letterSpacing: 1.1, color: _textSecondary),
                ),
              ),
              if (trailing != null) ...<Widget>[const SizedBox(width: 10), trailing!],
            ],
          ),
          if (description != null) ...<Widget>[
            const SizedBox(height: 10),
            Text(description!, style: TextStyle(color: _textSecondary, height: 1.45)),
          ],
          const SizedBox(height: 16),
          child,
        ],
      ),
    );
  }
}

/// Null when [text] is an http(s) address with a host, otherwise the problem.
String? _aiUrlProblem(String text) {
  final uri = Uri.tryParse(text.trim());
  final isHttp = uri != null && (uri.scheme == 'http' || uri.scheme == 'https') && uri.host.isNotEmpty;
  return isHttp ? null : appStrings.aiUrlInvalid;
}

// ── Providers ─────────────────────────────────────────────────────────────────

/// NeoAgent's _AdminProvidersTab: one bordered row per server-wide key or
/// address, with its status, the masked key and the actions.
class _AdminAiProvidersCard extends StatefulWidget {
  const _AdminAiProvidersCard({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminAiProvidersCard> createState() => _AdminAiProvidersCardState();
}

class _AdminAiProvidersCardState extends State<_AdminAiProvidersCard> with _PageLoad {
  List<AdminAiProvider>? _providers;
  bool _missing = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final providers = await _controller.ai.adminProviders();
      if (mounted) setState(() => _providers = providers);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _missing = _aiRouteMissing(error);
        pageError = _aiFriendlyError(_controller, error);
      });
    }
  }

  Future<void> _edit(AdminAiProvider provider) async {
    final saved = await showAdaptiveSheet<bool>(
      context: context,
      builder: (_) => _AdminAiProviderDialog(controller: _controller, provider: provider),
    );
    if (saved != true || !mounted) return;
    await runAction(_controller, _load, notice: appStrings.aiSavedName(provider.label));
  }

  /// An empty value clears the key or address on the server.
  Future<void> _clear(AdminAiProvider provider) => _confirmDelete(
    context,
    title: appStrings.aiClearTitle(provider.label),
    message: provider.isUrl ? appStrings.aiClearUrlBody : appStrings.aiClearKeyBody,
    confirmLabel: appStrings.clear,
    onConfirm: () => runAction(_controller, () async {
      await _controller.ai.adminSaveProvider(provider.key, '');
      await _load();
    }, notice: appStrings.aiCleared(provider.label)),
  );

  Widget _row(AdminAiProvider provider) {
    final configured = provider.configured;
    final String editLabel;
    if (provider.isUrl) {
      editLabel = configured ? appStrings.aiChange : appStrings.aiSetUrl;
    } else {
      editLabel = configured ? appStrings.aiReplace : appStrings.aiAddKey;
    }
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: _bgSecondary,
        borderRadius: BorderRadius.circular(AppRadius.tag),
        border: Border.all(color: _border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(provider.label, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 3),
                    Text(
                      provider.isUrl ? appStrings.aiEndpointUrl : appStrings.aiApiKey,
                      style: TextStyle(color: _textSecondary, fontSize: 12.5),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 10),
              _StatusPill(label: configured ? appStrings.aiSet : appStrings.aiNotSet, color: configured ? _success : _textMuted),
            ],
          ),
          if (configured && provider.hint.isNotEmpty) ...<Widget>[
            const SizedBox(height: 8),
            Text(provider.hint, maxLines: 1, overflow: TextOverflow.ellipsis, style: GoogleFonts.geistMono(fontSize: 12.5, color: _textSecondary)),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              OutlinedButton.icon(
                onPressed: busy ? null : () => unawaited(_edit(provider)),
                icon: Icon(configured ? Icons.edit_outlined : Icons.add_rounded, size: 18),
                label: Text(editLabel),
              ),
              if (configured)
                TextButton.icon(
                  onPressed: busy ? null : () => unawaited(_clear(provider)),
                  style: TextButton.styleFrom(foregroundColor: _danger),
                  icon: const Icon(Icons.delete_outline, size: 18),
                  label: Text(appStrings.clear),
                ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (_missing) {
      return _AiSetupCard(title: appStrings.aiAdminNotAvailableTitle, body: appStrings.aiAdminNotAvailableBody, icon: Icons.cloud_off_outlined);
    }
    final providers = _providers;
    final configured = providers?.where((p) => p.configured).length ?? 0;
    return _AiEyebrowCard(
      title: appStrings.aiProvidersTitle,
      description: appStrings.aiProvidersDescription,
      trailing: IconButton(
        tooltip: appStrings.refresh,
        onPressed: busy ? null : () => unawaited(_load()),
        icon: const Icon(Icons.refresh_rounded, size: 20),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (providers != null && providers.isNotEmpty)
            Align(
              alignment: Alignment.centerLeft,
              child: _SettingsScopePill(icon: Icons.vpn_key_outlined, label: appStrings.aiProvidersSetCount(configured, providers.length)),
            ),
          if (pageError != null || pageNotice != null) ...<Widget>[const SizedBox(height: 12), feedback()],
          const SizedBox(height: 14),
          if (providers == null && pageError == null) const _LoadingPlaceholder(),
          if (providers != null && providers.isEmpty) _EmptyText(appStrings.aiProvidersEmpty),
          for (final provider in providers ?? const <AdminAiProvider>[])
            Padding(padding: const EdgeInsets.only(bottom: 10), child: _row(provider)),
        ],
      ),
    );
  }
}

class _AdminAiProviderDialog extends StatefulWidget {
  const _AdminAiProviderDialog({required this.controller, required this.provider});

  final NeoCloudController controller;
  final AdminAiProvider provider;

  @override
  State<_AdminAiProviderDialog> createState() => _AdminAiProviderDialogState();
}

class _AdminAiProviderDialogState extends State<_AdminAiProviderDialog> {
  late final TextEditingController _value = TextEditingController(text: widget.provider.isUrl ? widget.provider.hint : '');
  bool _obscured = true;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _value.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final provider = widget.provider;
    final value = _value.text.trim();
    String? problem;
    if (value.isEmpty) {
      problem = provider.isUrl ? appStrings.aiEnterAddress : appStrings.aiPasteAKey;
    } else if (provider.isUrl) {
      problem = _aiUrlProblem(value);
    }
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.controller.ai.adminSaveProvider(provider.key, value);
      if (mounted) Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = _aiFriendlyError(widget.controller, error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final provider = widget.provider;
    final error = _error;
    final String hint;
    if (provider.isUrl) {
      hint = 'https://…';
    } else if (provider.configured) {
      hint = appStrings.aiPasteNewKey(provider.hint);
    } else {
      hint = appStrings.aiPasteTheKey;
    }
    return _AdaptiveDialog(
      title: Text(provider.label),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              provider.isUrl ? appStrings.aiProviderUrlBody(provider.label) : appStrings.aiProviderKeyBody,
              style: TextStyle(color: _textSecondary, height: 1.4),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _value,
              autofocus: true,
              obscureText: !provider.isUrl && _obscured,
              autocorrect: false,
              enableSuggestions: false,
              keyboardType: provider.isUrl ? TextInputType.url : TextInputType.visiblePassword,
              onSubmitted: (_) {
                if (!_saving) unawaited(_save());
              },
              decoration: InputDecoration(
                labelText: provider.isUrl ? appStrings.aiUrl : appStrings.aiApiKey,
                hintText: hint,
                suffixIcon: provider.isUrl
                    ? null
                    : IconButton(
                        tooltip: _obscured ? appStrings.aiShow : appStrings.aiHide,
                        onPressed: () => setState(() => _obscured = !_obscured),
                        icon: Icon(_obscured ? Icons.visibility_outlined : Icons.visibility_off_outlined),
                      ),
              ),
            ),
            if (error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: error)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(false), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _saving ? null : () => unawaited(_save()),
          child: _saving ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.save),
        ),
      ],
    );
  }
}

// ── Usage ─────────────────────────────────────────────────────────────────────

/// This month's AI use: totals, then the busiest accounts, features and
/// models. Metadata only; the server never keeps what was sent.
class _AdminAiUsageGroup extends StatefulWidget {
  const _AdminAiUsageGroup({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminAiUsageGroup> createState() => _AdminAiUsageGroupState();
}

class _AdminAiUsageGroupState extends State<_AdminAiUsageGroup> {
  AiUsageSummary? _usage;
  String? _error;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final usage = await widget.controller.ai.adminUsage();
      if (mounted) {
        setState(() {
          _usage = usage;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(widget.controller, error));
    }
  }

  Widget _value(String text) => Text(text, style: TextStyle(color: _textSecondary));

  String _lineLabel(AiUsageLine line) => line.detail.isEmpty ? line.label : '${line.label} · ${_providerPickerLabel(line.detail)}';

  Widget _lines(String title, List<AiUsageLine> lines) {
    return _SettingsRow(
      label: title,
      control: Column(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: <Widget>[
          for (final line in lines.take(5))
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Text(
                '${_lineLabel(line)}  ${appStrings.aiTokenCount(_aiNumber(line.tokens))}',
                style: TextStyle(color: _textSecondary, fontSize: 13),
              ),
            ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final usage = _usage;
    return _SettingsGroup(
      title: appStrings.aiUsageTitle,
      description: appStrings.aiAdminUsageDescription,
      trailing: IconButton(tooltip: appStrings.refresh, onPressed: () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded, size: 20)),
      children: <Widget>[
        if (usage == null && _error == null) const _LoadingPlaceholder(),
        if (usage == null && _error != null) _SettingsBlock(child: _InlineError(message: _error!)),
        if (usage != null && usage.calls == 0) _SettingsBlock(child: _EmptyText(appStrings.aiUsageNone)),
        if (usage != null && usage.calls > 0) ...<Widget>[
          _SettingsRow(label: appStrings.aiUsageRequests, control: _value(_aiNumber(usage.calls))),
          _SettingsRow(label: appStrings.aiUsageTokens, control: _value(_aiNumber(usage.tokens))),
          _SettingsRow(label: appStrings.aiUsageErrors, control: _value(_aiNumber(usage.errors))),
          if (usage.users.isNotEmpty) _lines(appStrings.aiUsageAccounts, usage.users),
          if (usage.features.isNotEmpty) _lines(appStrings.aiUsageFeatures, usage.features),
          if (usage.models.isNotEmpty) _lines(appStrings.aiUsageModels, usage.models),
        ],
      ],
    );
  }
}
