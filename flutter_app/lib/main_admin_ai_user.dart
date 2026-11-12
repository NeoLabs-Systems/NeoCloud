part of 'main.dart';

/// Admin › Users › ⋯ › AI: one account's monthly token budget, and the
/// models it may use although they are switched off for everyone (NeoAgent's
/// _AdminOpsUserModelsDialog). Only those models are listed: a model already
/// on for everyone needs no grant. The budget opens on the account's current
/// setting: the server default, its own number, or no limit.
class _AdminAiUserDialog extends StatefulWidget {
  const _AdminAiUserDialog({required this.controller, required this.user});

  final NeoCloudController controller;
  final AdminUser user;

  @override
  State<_AdminAiUserDialog> createState() => _AdminAiUserDialogState();
}

class _AdminAiUserDialogState extends State<_AdminAiUserDialog> {
  final TextEditingController _search = TextEditingController();
  final TextEditingController _budget = TextEditingController();
  List<ModelMeta> _models = const <ModelMeta>[];
  Set<String> _granted = <String>{};

  /// `default`, `custom` or `unlimited`, as loaded until changed.
  late String _budgetMode = _modeOf(widget.user.aiMonthlyTokens);

  /// The budget as loaded, to save it only when it changed.
  int? _loadedBudget;
  int? _defaultBudget;
  bool _loading = true;
  bool _saving = false;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  /// null follows the server default, 0 is no limit.
  static String _modeOf(int? monthlyTokens) {
    if (monthlyTokens == null) return 'default';
    return monthlyTokens == 0 ? 'unlimited' : 'custom';
  }

  @override
  void initState() {
    super.initState();
    if (_budgetMode == 'custom') _budget.text = '${widget.user.aiMonthlyTokens}';
    _search.addListener(() => setState(() {}));
    unawaited(_load());
  }

  @override
  void dispose() {
    _search.dispose();
    _budget.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final ai = _controller.ai;
      final catalog = await ai.adminModels();
      final account = await ai.adminUserAi(widget.user.id);
      final granted = account.modelIds;
      AdminAiConfig? config;
      try {
        config = await ai.adminConfig();
      } catch (_) {
        // Only the hint below the budget needs it.
      }
      if (!mounted) return;
      final disabled = catalog.disabledModels;
      final models =
          catalog.models.where((model) => disabled.contains(model.id) || disabled.contains(model.modelId) || granted.contains(model.id)).toList()
            ..sort((a, b) {
              final byGrant = (granted.contains(a.id) ? 0 : 1) - (granted.contains(b.id) ? 0 : 1);
              return byGrant != 0 ? byGrant : a.displayLabel.toLowerCase().compareTo(b.displayLabel.toLowerCase());
            });
      setState(() {
        _models = models;
        _granted = Set<String>.of(granted);
        _defaultBudget = config?.monthlyTokens;
        _loadedBudget = account.monthlyTokens;
        _budgetMode = _modeOf(account.monthlyTokens);
        if (_budgetMode == 'custom') _budget.text = '${account.monthlyTokens}';
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = _aiFriendlyError(_controller, error);
      });
    }
  }

  Future<void> _save() async {
    int? budget;
    if (_budgetMode == 'unlimited') budget = 0;
    if (_budgetMode == 'custom') {
      budget = _aiParseTokens(_budget.text, allowEmpty: false);
      if (budget == null || budget == 0) {
        setState(() => _error = appStrings.aiBudgetInvalid);
        return;
      }
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await _controller.ai.adminSaveUserModels(widget.user.id, _granted.toList()..sort());
      if (budget != _loadedBudget) await _controller.ai.adminSaveUserBudget(widget.user.id, budget);
      if (mounted) Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = _aiFriendlyError(_controller, error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final query = _search.text.trim().toLowerCase();
    final visible = query.isEmpty
        ? _models
        : _models.where((model) => model.displayLabel.toLowerCase().contains(query) || model.id.toLowerCase().contains(query)).toList();
    final defaultBudget = _defaultBudget;
    return _AdaptiveDialog(
      title: Text(appStrings.aiAdminUserTitle(widget.user.username)),
      content: SizedBox(
        width: 480,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            if (_loading)
              const _LoadingPlaceholder()
            else ...<Widget>[
              Text(appStrings.aiAdminBudget, style: _sectionTitleStyle()),
              const SizedBox(height: 10),
              _SettingsChoice<String>(
                value: _budgetMode,
                options: <(String, String)>[
                  ('default', appStrings.aiUserBudgetDefault),
                  ('custom', appStrings.aiUserBudgetCustom),
                  ('unlimited', appStrings.aiNoLimit),
                ],
                onChanged: _saving ? null : (mode) => setState(() => _budgetMode = mode),
              ),
              const SizedBox(height: 10),
              if (_budgetMode == 'default')
                Text(
                  appStrings.aiUserBudgetDefaultValue(defaultBudget == null ? appStrings.aiNoLimit : _aiNumber(defaultBudget)),
                  style: TextStyle(color: _textSecondary),
                ),
              if (_budgetMode == 'custom')
                TextField(
                  controller: _budget,
                  autofocus: true,
                  keyboardType: TextInputType.number,
                  decoration: InputDecoration(labelText: appStrings.aiTokensPerMonth, hintText: '500000'),
                ),
              const SizedBox(height: 22),
              Text(appStrings.aiExtraModels, style: _sectionTitleStyle()),
              const SizedBox(height: 6),
              Text(appStrings.aiExtraModelsIntro(widget.user.username), style: TextStyle(color: _textSecondary, height: 1.45)),
              const SizedBox(height: 12),
              if (_models.isEmpty)
                _EmptyText(appStrings.aiExtraModelsEmpty)
              else ...<Widget>[
                if (_models.length > 8) ...<Widget>[
                  _SearchField(controller: _search, hintText: appStrings.aiSearchModels, onChanged: (_) {}, onClear: _search.clear),
                  const SizedBox(height: 8),
                ],
                ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 320),
                  child: ListView(
                    shrinkWrap: true,
                    children: <Widget>[
                      for (final model in visible)
                        CheckboxListTile(
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                          controlAffinity: ListTileControlAffinity.leading,
                          value: _granted.contains(model.id),
                          onChanged: _saving
                              ? null
                              : (value) => setState(() {
                                  if (value == true) {
                                    _granted.add(model.id);
                                  } else {
                                    _granted.remove(model.id);
                                  }
                                }),
                          title: Text(model.displayLabel, maxLines: 1, overflow: TextOverflow.ellipsis),
                          subtitle: Text(_providerPickerLabel(model.provider), style: TextStyle(color: _textMuted, fontSize: 12)),
                        ),
                    ],
                  ),
                ),
              ],
            ],
            if (_error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(false), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _loading || _saving ? null : () => unawaited(_save()),
          child: _saving ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.save),
        ),
      ],
    );
  }
}
