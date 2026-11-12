part of 'main.dart';

// Admin › AI › Model availability, NeoAgent's _AdminModelsTab: which models
// every account may pick, grouped by provider and collapsible, with search,
// a filter, bulk switches and a bar for unsaved changes.

// Catalog labels end with the provider, e.g. "Claude Sonnet (OpenRouter)";
// the list groups by provider, so rows drop that suffix.
final RegExp _adminAiProviderSuffix = RegExp(r'\s*\([^()]*\)$');

class _AdminAiModel {
  _AdminAiModel(this.meta)
    : name = _shortName(meta.displayLabel),
      searchText = '${meta.displayLabel}\n${meta.id}\n${_providerPickerLabel(meta.provider)}'.toLowerCase();

  static String _shortName(String label) {
    final stripped = label.replaceFirst(_adminAiProviderSuffix, '');
    return stripped.isEmpty ? label : stripped;
  }

  final ModelMeta meta;

  /// The label without its trailing provider name.
  final String name;

  /// Lower-cased label, id and provider, built once for search.
  final String searchText;

  String get id => meta.id;

  String get provider => meta.provider;

  bool matches(String lowerQuery) => searchText.contains(lowerQuery);
}

String _adminAiModelPrice(double? perMillion) {
  if (perMillion == null) return appStrings.aiPriceUnknown;
  if (perMillion == 0) return appStrings.aiPriceFree;
  var digits = 2;
  if (perMillion < 0.01) {
    digits = 4;
  } else if (perMillion < 1) {
    digits = 3;
  }
  return appStrings.aiPricePerMillion(perMillion.toStringAsFixed(digits));
}

/// What a model does besides text, as a tag; null for a plain text model.
String? _modelKindTag(ModelMeta model) {
  switch (model.kind) {
    case 'embedding':
      return appStrings.aiKindEmbedding;
    case 'transcription':
      return appStrings.aiKindTranscription;
  }
  return model.supportsVision ? appStrings.aiKindVision : null;
}

enum _AdminModelFilter { all, enabled, disabled }

/// One line of the availability list: a provider header, or a model below it.
class _AdminAiListEntry {
  const _AdminAiListEntry.provider(this.provider, this.models, this.expanded) : model = null;

  _AdminAiListEntry.model(_AdminAiModel this.model) : provider = model.provider, models = const <_AdminAiModel>[], expanded = true;

  final String provider;
  final List<_AdminAiModel> models;
  final bool expanded;
  final _AdminAiModel? model;
}

class _AdminAiModelsGroup extends StatefulWidget {
  const _AdminAiModelsGroup({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminAiModelsGroup> createState() => _AdminAiModelsGroupState();
}

class _AdminAiModelsGroupState extends State<_AdminAiModelsGroup> with _PageLoad {
  final TextEditingController _search = TextEditingController();
  List<_AdminAiModel>? _models;
  Set<String> _savedDisabled = <String>{};
  Set<String> _disabled = <String>{};
  bool _savedAutoEnable = true;
  bool _autoEnable = true;
  _AdminModelFilter _filter = _AdminModelFilter.all;

  /// Providers whose group is open or shut against the default: shut when
  /// browsing, open while a search or filter narrows the list.
  final Set<String> _flipped = <String>{};

  NeoCloudController get _controller => widget.controller;

  bool get _dirty => !setEquals(_savedDisabled, _disabled) || _savedAutoEnable != _autoEnable;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final catalog = await _controller.ai.adminModels();
      if (!mounted) return;
      setState(() {
        _models = catalog.models.map(_AdminAiModel.new).toList()..sort(_compareModels);
        _reset(catalog);
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _aiFriendlyError(_controller, error));
    }
  }

  void _reset(AdminModelCatalog catalog) {
    _savedDisabled = catalog.disabledModels;
    _disabled = Set<String>.of(catalog.disabledModels);
    _savedAutoEnable = catalog.autoEnableNewModels;
    _autoEnable = catalog.autoEnableNewModels;
  }

  /// Provider A–Z, then cheapest first (unknown prices last), then name.
  static int _compareModels(_AdminAiModel a, _AdminAiModel b) {
    final byProvider = a.provider.compareTo(b.provider);
    if (byProvider != 0) return byProvider;
    final byCost = (a.meta.inputCostPerM ?? double.infinity).compareTo(b.meta.inputCostPerM ?? double.infinity);
    if (byCost != 0) return byCost;
    return a.searchText.compareTo(b.searchText);
  }

  int _enabledCount(Iterable<_AdminAiModel> models) => models.where((model) => !_disabled.contains(model.id)).length;

  /// Filters by the saved state, so a row does not vanish while it is edited.
  bool _isShown(_AdminAiModel model, String query) {
    if (query.isNotEmpty && !model.matches(query)) return false;
    return switch (_filter) {
      _AdminModelFilter.all => true,
      _AdminModelFilter.enabled => !_savedDisabled.contains(model.id),
      _AdminModelFilter.disabled => _savedDisabled.contains(model.id),
    };
  }

  void _setEnabled(Iterable<_AdminAiModel> models, bool enabled) {
    setState(() {
      pageNotice = null;
      for (final model in models) {
        if (enabled) {
          _disabled.remove(model.id);
        } else {
          _disabled.add(model.id);
        }
      }
    });
  }

  void _discard() {
    setState(() {
      _disabled = Set<String>.of(_savedDisabled);
      _autoEnable = _savedAutoEnable;
      pageError = null;
    });
  }

  Future<void> _save() => runAction(_controller, () async {
    final saved = await _controller.ai.adminSaveModels(_disabled.toList()..sort(), autoEnableNewModels: _autoEnable);
    if (mounted) setState(() => _reset(saved));
  }, notice: appStrings.aiModelsSaved);

  /// Search, filter and catalog changes start every group from its default.
  void _narrow(VoidCallback change) {
    setState(() {
      change();
      _flipped.clear();
    });
  }

  @override
  Widget build(BuildContext context) {
    final models = _models;
    return _SettingsGroup(
      title: appStrings.aiModelAvailability,
      description: appStrings.aiModelAvailabilityDescription,
      trailing: IconButton(tooltip: appStrings.refresh, onPressed: busy ? null : () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded, size: 20)),
      children: <Widget>[
        if (models == null && pageError == null) const _LoadingPlaceholder(),
        if (models == null && pageError != null) _SettingsBlock(child: _InlineError(message: pageError!)),
        if (models != null) ...<Widget>[
          _SettingsRow(
            label: appStrings.aiAutoEnable,
            description: appStrings.aiAutoEnableHint,
            control: Switch(
              value: _autoEnable,
              onChanged: busy
                  ? null
                  : (value) => setState(() {
                      pageNotice = null;
                      _autoEnable = value;
                    }),
            ),
          ),
          _SettingsBlock(child: _list(context, models)),
        ],
      ],
    );
  }

  Widget _list(BuildContext context, List<_AdminAiModel> catalog) {
    final query = _search.text.trim().toLowerCase();
    final shown = catalog.where((model) => _isShown(model, query)).toList();
    final groups = <String, List<_AdminAiModel>>{};
    for (final model in shown) {
      groups.putIfAbsent(model.provider, () => <_AdminAiModel>[]).add(model);
    }
    final openByDefault = query.isNotEmpty || _filter != _AdminModelFilter.all || groups.length == 1;
    final entries = <_AdminAiListEntry>[];
    for (final group in groups.entries) {
      final expanded = openByDefault != _flipped.contains(group.key);
      entries.add(_AdminAiListEntry.provider(group.key, group.value, expanded));
      if (expanded) entries.addAll(group.value.map(_AdminAiListEntry.model));
    }
    final anyExpanded = entries.any((entry) => entry.model != null);
    final changes = _savedDisabled.difference(_disabled).length + _disabled.difference(_savedDisabled).length;
    final narrowed = query.isNotEmpty || _filter != _AdminModelFilter.all;
    // Every line is one fixed height, so the panel fits its lines up to a cap
    // and the list lays out without measuring rows.
    const lineHeight = 52.0;
    final maxListHeight = (MediaQuery.sizeOf(context).height * 0.62).clamp(360.0, 760.0);
    final listHeight = entries.isEmpty ? lineHeight * 3 : (entries.length * lineHeight + 2).clamp(0.0, maxListHeight);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Wrap(
          spacing: 12,
          runSpacing: 10,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 360),
              child: _SearchField(
                controller: _search,
                hintText: appStrings.aiSearchModelsOrProviders,
                onChanged: (_) => _narrow(() {}),
                onClear: () => _narrow(_search.clear),
              ),
            ),
            _SettingsChoice<_AdminModelFilter>(
              value: _filter,
              options: <(_AdminModelFilter, String)>[
                (_AdminModelFilter.all, appStrings.aiAll),
                (_AdminModelFilter.enabled, appStrings.aiFilterEnabled),
                (_AdminModelFilter.disabled, appStrings.aiFilterDisabled),
              ],
              onChanged: (value) => _narrow(() => _filter = value),
            ),
          ],
        ),
        const SizedBox(height: 12),
        Wrap(
          spacing: 4,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Text(appStrings.aiShownCount(shown.length), style: TextStyle(color: _textMuted, fontSize: 13)),
            const SizedBox(width: 8),
            TextButton(
              onPressed: shown.isEmpty || _enabledCount(shown) == shown.length ? null : () => _setEnabled(shown, true),
              child: Text(narrowed ? appStrings.aiEnableShown : appStrings.aiEnableAll),
            ),
            TextButton(
              onPressed: _enabledCount(shown) == 0 ? null : () => _setEnabled(shown, false),
              child: Text(narrowed ? appStrings.aiDisableShown : appStrings.aiDisableAll),
            ),
            if (groups.length > 1)
              TextButton.icon(
                onPressed: () => setState(() {
                  _flipped.clear();
                  if (anyExpanded == openByDefault) _flipped.addAll(groups.keys);
                }),
                icon: Icon(anyExpanded ? Icons.unfold_less : Icons.unfold_more, size: 18),
                label: Text(anyExpanded ? appStrings.aiCollapseAll : appStrings.aiExpandAll),
              ),
          ],
        ),
        const SizedBox(height: 10),
        Container(
          height: listHeight,
          decoration: BoxDecoration(border: Border.all(color: _border), borderRadius: BorderRadius.circular(AppRadius.control)),
          clipBehavior: Clip.antiAlias,
          child: entries.isEmpty
              ? Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(
                      catalog.isEmpty ? appStrings.aiNoModelsYet : appStrings.aiNoModelMatches(_search.text.trim()),
                      textAlign: TextAlign.center,
                      style: TextStyle(color: _textSecondary, height: 1.45),
                    ),
                  ),
                )
              : ListView.builder(
                  itemExtent: lineHeight,
                  itemCount: entries.length,
                  itemBuilder: (context, index) {
                    final entry = entries[index];
                    final model = entry.model;
                    if (model != null) {
                      return _AdminAiModelRow(
                        model: model,
                        enabled: !_disabled.contains(model.id),
                        onChanged: (enabled) => _setEnabled(<_AdminAiModel>[model], enabled),
                      );
                    }
                    return _AdminAiProviderHeader(
                      provider: entry.provider,
                      enabledCount: _enabledCount(entry.models),
                      total: entry.models.length,
                      expanded: entry.expanded,
                      first: index == 0,
                      onToggleExpanded: () => setState(() {
                        if (!_flipped.remove(entry.provider)) _flipped.add(entry.provider);
                      }),
                      onSetAll: (enabled) => _setEnabled(entry.models, enabled),
                    );
                  },
                ),
        ),
        const SizedBox(height: 14),
        Wrap(
          spacing: 12,
          runSpacing: 10,
          alignment: WrapAlignment.end,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            if (_dirty) ...<Widget>[
              _StatusPill(label: appStrings.aiUnsavedChanges(changes), color: _warning),
              TextButton(onPressed: busy ? null : _discard, child: Text(appStrings.aiDiscard)),
            ],
            FilledButton(onPressed: _dirty && !busy ? () => unawaited(_save()) : null, child: Text(appStrings.aiSaveChanges)),
          ],
        ),
        if (pageError != null || pageNotice != null) ...<Widget>[const SizedBox(height: 12), feedback()],
      ],
    );
  }
}

/// A collapsible provider group: how many of its listed models are on, and
/// one checkbox for all of them.
class _AdminAiProviderHeader extends StatelessWidget {
  const _AdminAiProviderHeader({
    required this.provider,
    required this.enabledCount,
    required this.total,
    required this.expanded,
    required this.first,
    required this.onToggleExpanded,
    required this.onSetAll,
  });

  final String provider;
  final int enabledCount;
  final int total;
  final bool expanded;
  final bool first;
  final VoidCallback onToggleExpanded;
  final ValueChanged<bool> onSetAll;

  @override
  Widget build(BuildContext context) {
    final bool? allOn = enabledCount == total ? true : (enabledCount == 0 ? false : null);
    return Material(
      color: _bgSecondary,
      child: InkWell(
        onTap: onToggleExpanded,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 8),
          decoration: BoxDecoration(border: Border(top: first ? BorderSide.none : BorderSide(color: _border))),
          child: Row(
            children: <Widget>[
              Icon(expanded ? Icons.expand_more : Icons.chevron_right, color: _textSecondary),
              const SizedBox(width: 4),
              Checkbox(tristate: true, value: allOn, onChanged: (_) => onSetAll(allOn != true)),
              const SizedBox(width: 4),
              Icon(_providerPickerIcon(provider), size: 17, color: _textSecondary),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  _providerPickerLabel(provider),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700),
                ),
              ),
              const SizedBox(width: 8),
              Text(appStrings.aiEnabledCount(enabledCount, total), style: TextStyle(color: _textMuted, fontSize: 12.5)),
              const SizedBox(width: 8),
            ],
          ),
        ),
      ),
    );
  }
}

class _AdminAiModelRow extends StatelessWidget {
  const _AdminAiModelRow({required this.model, required this.enabled, required this.onChanged});

  final _AdminAiModel model;
  final bool enabled;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    final cost = model.meta.inputCostPerM;
    final kind = _modelKindTag(model.meta);
    final narrow = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    return InkWell(
      onTap: () => onChanged(!enabled),
      child: Padding(
        padding: EdgeInsets.only(left: narrow ? 12 : 38, right: 8),
        child: Row(
          children: <Widget>[
            Checkbox(value: enabled, onChanged: (value) => onChanged(value == true)),
            const SizedBox(width: 6),
            Expanded(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    model.name,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: enabled ? _textPrimary : _textMuted, fontWeight: FontWeight.w600),
                  ),
                  if (model.meta.modelId != model.name)
                    Text(
                      model.meta.modelId,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: GoogleFonts.geistMono(fontSize: 11.5, color: _textMuted),
                    ),
                ],
              ),
            ),
            if (kind != null) ...<Widget>[const SizedBox(width: 8), _ModelTag(kind)],
            if (cost != null && !narrow) ...<Widget>[
              const SizedBox(width: 12),
              Text(_adminAiModelPrice(cost), style: TextStyle(color: enabled ? _textSecondary : _textMuted, fontSize: 12.5, fontWeight: FontWeight.w600)),
            ],
          ],
        ),
      ),
    );
  }
}
