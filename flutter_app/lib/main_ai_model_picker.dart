part of 'main.dart';

// NeoAgent's model picker (main_model_picker.dart): a button showing the
// chosen model, opening a searchable list grouped by provider, with an
// "Auto" entry on top. On a phone the list opens as a bottom sheet. Provider
// marks are neutral and gold marks the selection only, as everywhere else.

class _ModelPickerOption {
  const _ModelPickerOption({
    required this.value,
    required this.label,
    this.group = '',
    this.subtitle,
    this.icon,
    this.isAuto = false,
    this.priceTier,
    this.isByok = false,
    this.adminGranted = false,
  });

  final String value;
  final String label;
  final String group;
  final String? subtitle;
  final IconData? icon;
  final bool isAuto;

  /// 'free' | 'cheap' | 'medium' | 'expensive' | null
  final String? priceTier;

  /// Runs on the account's own key.
  final bool isByok;

  /// An admin granted this account a model that is off for others.
  final bool adminGranted;
}

// ─── Provider helpers ─────────────────────────────────────────────────────────

IconData _providerPickerIcon(String provider) {
  final p = provider.toLowerCase();
  if (p.contains('neorouter')) return Icons.alt_route_rounded;
  if (p.contains('google') || p.contains('gemini')) return Icons.auto_awesome_rounded;
  if (p.contains('openai') || p.contains('gpt')) return Icons.bolt_rounded;
  if (p.contains('anthropic') || p.contains('claude')) return Icons.menu_book_rounded;
  if (p.contains('openrouter')) return Icons.hub_rounded;
  if (p.contains('ollama')) return Icons.device_hub_rounded;
  return Icons.memory_rounded;
}

String _providerPickerLabel(String id) {
  final labels = <String, String>{
    'anthropic': 'Anthropic',
    'openai': 'OpenAI',
    'openai-compatible': appStrings.aiProviderCustom,
    'google': 'Google',
    'gemini': 'Google',
    'ollama': 'Ollama',
    'openrouter': 'OpenRouter',
    'neorouter': 'NeoRouter',
  };
  return labels[id.toLowerCase()] ?? (id.isEmpty ? appStrings.aiOther : id);
}

// ─── Option builders ──────────────────────────────────────────────────────────

_ModelPickerOption _autoModelOption() =>
    _ModelPickerOption(value: 'auto', label: appStrings.aiAuto, subtitle: appStrings.aiAutoSubtitle, icon: Icons.auto_awesome_outlined, isAuto: true);

List<_ModelPickerOption> _modelPickerOptions(List<ModelMeta> models, {bool allowAuto = false}) {
  return <_ModelPickerOption>[
    if (allowAuto) _autoModelOption(),
    ...models.map((m) {
      final parts = <String>[if (m.provider.isNotEmpty) _providerPickerLabel(m.provider), if (m.purpose.isNotEmpty) m.purpose];
      return _ModelPickerOption(
        value: m.id,
        label: m.displayLabel,
        group: m.provider,
        subtitle: parts.isEmpty ? null : parts.join(' · '),
        icon: _providerPickerIcon(m.provider),
        priceTier: m.priceTier,
        isByok: m.isByok,
        adminGranted: m.adminGranted,
      );
    }),
  ];
}

/// The models offered for [role]: Auto, then the ones that can do the work.
List<_ModelPickerOption> _roleModelOptions(AiRole role, List<ModelMeta> models) => _modelPickerOptions(modelsForRole(role, models), allowAuto: true);

// ─────────────────────────────────────────────────────────────────────────────
// Model Picker Button — drop-in trigger that opens the list
// ─────────────────────────────────────────────────────────────────────────────

class _ModelPickerButton extends StatelessWidget {
  const _ModelPickerButton({required this.value, required this.options, required this.onChanged, required this.dialogTitle});

  final String value;
  final List<_ModelPickerOption> options;
  final ValueChanged<String> onChanged;
  final String dialogTitle;

  _ModelPickerOption get _current => options.firstWhere(
    (o) => o.value == value,
    orElse: () => _ModelPickerOption(value: value, label: value.contains('::') ? value.split('::').last : value),
  );

  @override
  Widget build(BuildContext context) {
    final current = _current;
    final iconColor = current.isAuto ? _accent : _textSecondary;
    final iconData = current.isAuto ? Icons.auto_awesome_outlined : (current.icon ?? Icons.memory_rounded);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: () => unawaited(_openPicker(context)),
        borderRadius: BorderRadius.circular(AppRadius.input),
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
          decoration: BoxDecoration(
            color: _bgSecondary,
            borderRadius: BorderRadius.circular(AppRadius.input),
            border: Border.all(color: _border),
          ),
          child: Row(
            children: <Widget>[
              Container(
                width: 28,
                height: 28,
                decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(8)),
                child: Icon(iconData, size: 15, color: iconColor),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  current.label,
                  style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: _textPrimary),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: 6),
              Icon(Icons.unfold_more_rounded, size: 16, color: _textMuted),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _openPicker(BuildContext context) async {
    final picked = await showAdaptiveSheet<String>(
      context: context,
      builder: (sheetContext) =>
          _ModelPickerDialog(title: dialogTitle, options: options, currentValue: value, onChanged: (v) => Navigator.of(sheetContext).pop(v)),
    );
    if (picked != null && picked != value) onChanged(picked);
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Model Picker Dialog (a bottom sheet on phones)
// ─────────────────────────────────────────────────────────────────────────────

class _ModelPickerDialog extends StatelessWidget {
  const _ModelPickerDialog({required this.title, required this.options, required this.currentValue, required this.onChanged});

  final String title;
  final List<_ModelPickerOption> options;
  final String currentValue;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final sheet = ModalRoute.of(context) is ModalBottomSheetRoute;
    final body = Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: EdgeInsets.fromLTRB(20, sheet ? 0 : 16, 10, 12),
          child: Row(
            children: <Widget>[
              Expanded(child: Text(title, style: _displayTitleStyle(18))),
              IconButton(
                tooltip: appStrings.close,
                onPressed: () => Navigator.of(context).pop(),
                icon: Icon(Icons.close_rounded, size: 20, color: _textSecondary),
              ),
            ],
          ),
        ),
        Flexible(
          child: _ModelPickerList(options: options, currentValue: currentValue, onChanged: onChanged, autofocus: !sheet),
        ),
      ],
    );
    if (sheet) {
      return SizedBox(height: MediaQuery.sizeOf(context).height * 0.8, child: body);
    }
    return Dialog(
      backgroundColor: _bgCard,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.panel),
        side: BorderSide(color: _borderLight),
      ),
      clipBehavior: Clip.antiAlias,
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: 520, minWidth: 300, maxHeight: MediaQuery.sizeOf(context).height * 0.76),
        child: body,
      ),
    );
  }
}

/// The picker's model list on its own: search, provider filter and the
/// grouped rows. Shows the models that can do [role], with Auto on top.
class ModelPickerPanel extends StatelessWidget {
  const ModelPickerPanel({super.key, required this.models, required this.role, required this.value, required this.onChanged});

  final List<ModelMeta> models;
  final AiRole role;
  final String value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) => _ModelPickerList(options: _roleModelOptions(role, models), currentValue: value, onChanged: onChanged);
}

/// Search, provider filter and the grouped model rows. Scales to catalogs
/// with hundreds of models across many providers.
class _ModelPickerList extends StatefulWidget {
  const _ModelPickerList({required this.options, required this.currentValue, required this.onChanged, this.autofocus = false});

  final List<_ModelPickerOption> options;
  final String currentValue;
  final ValueChanged<String> onChanged;
  final bool autofocus;

  @override
  State<_ModelPickerList> createState() => _ModelPickerListState();
}

class _ModelPickerListState extends State<_ModelPickerList> {
  /// The provider filter only appears once there are this many providers;
  /// below that the group headers are enough to scan the list.
  static const int _providerFilterThreshold = 3;

  final TextEditingController _searchCtrl = TextEditingController();
  String _query = '';

  /// The provider group shown alone, or null for every provider.
  String? _provider;

  @override
  void dispose() {
    _searchCtrl.dispose();
    super.dispose();
  }

  List<_ModelPickerOption> get _filtered {
    final q = _query.toLowerCase();
    return widget.options.where((o) {
      if (_provider != null && (o.isAuto || o.group != _provider)) return false;
      if (q.isEmpty) return true;
      return o.label.toLowerCase().contains(q) ||
          (o.subtitle?.toLowerCase().contains(q) ?? false) ||
          o.group.toLowerCase().contains(q) ||
          o.value.toLowerCase().contains(q);
    }).toList();
  }

  @override
  Widget build(BuildContext context) {
    final providers = <String>[];
    for (final opt in widget.options) {
      if (!opt.isAuto && opt.group.isNotEmpty && !providers.contains(opt.group)) providers.add(opt.group);
    }
    final filtered = _filtered;
    final autoOption = filtered.where((o) => o.isAuto).firstOrNull;
    final regularOptions = filtered.where((o) => !o.isAuto).toList();

    final groups = <String>[];
    final grouped = <String, List<_ModelPickerOption>>{};
    for (final opt in regularOptions) {
      if (!grouped.containsKey(opt.group)) {
        groups.add(opt.group);
        grouped[opt.group] = <_ModelPickerOption>[];
      }
      grouped[opt.group]!.add(opt);
    }
    final hasGroups = groups.any((g) => g.isNotEmpty);
    final noModels = widget.options.every((o) => o.isAuto);

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(14, 0, 14, 12),
          child: _SearchField(
            controller: _searchCtrl,
            hintText: appStrings.aiSearchModels,
            onChanged: (v) => setState(() => _query = v.trim()),
            onClear: () => setState(() {
              _searchCtrl.clear();
              _query = '';
            }),
          ),
        ),
        if (providers.length >= _providerFilterThreshold)
          SizedBox(
            height: 46,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 12),
              children: <Widget>[
                _ProviderFilterChip(label: appStrings.aiAll, selected: _provider == null, onTap: () => setState(() => _provider = null)),
                for (final provider in providers)
                  _ProviderFilterChip(
                    label: _providerPickerLabel(provider),
                    selected: _provider == provider,
                    onTap: () => setState(() => _provider = provider),
                  ),
              ],
            ),
          ),
        Divider(height: 1, thickness: 1, color: _border),
        Flexible(
          child: filtered.isEmpty
              ? Padding(
                  padding: const EdgeInsets.all(36),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      Icon(Icons.search_off_rounded, size: 36, color: _textMuted),
                      const SizedBox(height: 12),
                      Text(appStrings.aiNoResults(_query), style: TextStyle(color: _textSecondary, fontSize: 14)),
                    ],
                  ),
                )
              : ListView(
                  padding: const EdgeInsets.symmetric(vertical: 6),
                  shrinkWrap: true,
                  children: <Widget>[
                    if (autoOption != null) ...<Widget>[
                      _PickerRow(option: autoOption, selected: widget.currentValue == autoOption.value, onTap: () => widget.onChanged(autoOption.value)),
                      if (regularOptions.isNotEmpty) Divider(height: 1, indent: 14, endIndent: 14, color: _border),
                    ],
                    if (noModels && _query.isEmpty)
                      Padding(
                        padding: const EdgeInsets.fromLTRB(18, 14, 18, 14),
                        child: Text(appStrings.aiNoModelsForRole, style: TextStyle(color: _textMuted, fontSize: 13)),
                      ),
                    for (final group in groups) ...<Widget>[
                      if (hasGroups && group.isNotEmpty) _PickerGroupHeader(label: _providerPickerLabel(group)),
                      for (final opt in grouped[group]!)
                        _PickerRow(option: opt, selected: widget.currentValue == opt.value, onTap: () => widget.onChanged(opt.value)),
                    ],
                  ],
                ),
        ),
      ],
    );
  }
}

class _ProviderFilterChip extends StatelessWidget {
  const _ProviderFilterChip({required this.label, required this.selected, required this.onTap});

  final String label;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(right: 6),
      child: Material(
        color: selected ? _bgTertiary : Colors.transparent,
        shape: StadiumBorder(side: BorderSide(color: selected ? _borderLight : _border)),
        child: InkWell(
          onTap: onTap,
          customBorder: const StadiumBorder(),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Center(
              child: Text(
                label,
                style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: selected ? _textPrimary : _textSecondary),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────────────────────────────────────
// Sub-widgets
// ─────────────────────────────────────────────────────────────────────────────

/// Free, $, $$ or $$$ as a small neutral tag.
class _PriceTierChip extends StatelessWidget {
  const _PriceTierChip({required this.tier});

  final String tier;

  @override
  Widget build(BuildContext context) {
    final text = switch (tier) {
      'free' => appStrings.aiPriceFree,
      'cheap' => r'$',
      'medium' => r'$$',
      'expensive' => r'$$$',
      _ => '',
    };
    if (text.isEmpty) return const SizedBox.shrink();
    return _ModelTag(text);
  }
}

/// A small neutral tag on a model row (BYOK, Granted, price, kind).
class _ModelTag extends StatelessWidget {
  const _ModelTag(this.text, {this.tooltip});

  final String text;
  final String? tooltip;

  @override
  Widget build(BuildContext context) {
    final tag = Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
      decoration: BoxDecoration(
        color: _bgTertiary,
        borderRadius: BorderRadius.circular(5),
        border: Border.all(color: _border),
      ),
      child: Text(
        text,
        style: TextStyle(fontSize: 10.5, fontWeight: FontWeight.w700, color: _textSecondary, letterSpacing: 0.2),
      ),
    );
    final tooltip = this.tooltip;
    return tooltip == null ? tag : Tooltip(message: tooltip, child: tag);
  }
}

class _PickerGroupHeader extends StatelessWidget {
  const _PickerGroupHeader({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 6),
      child: Row(
        children: <Widget>[
          Text(
            label.toUpperCase(),
            style: GoogleFonts.geistMono(fontSize: 10.5, fontWeight: FontWeight.w600, color: _textMuted, letterSpacing: 1.2),
          ),
          const SizedBox(width: 10),
          Expanded(child: Container(height: 1, color: _border)),
        ],
      ),
    );
  }
}

class _PickerRow extends StatelessWidget {
  const _PickerRow({required this.option, required this.selected, required this.onTap});

  final _ModelPickerOption option;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final iconColor = option.isAuto || selected ? _accent : _textSecondary;
    final iconData = option.isAuto ? Icons.auto_awesome_outlined : (option.icon ?? Icons.memory_rounded);
    return Material(
      color: selected ? _bgTertiary : Colors.transparent,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
          child: Row(
            children: <Widget>[
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  color: _bgTertiary,
                  borderRadius: BorderRadius.circular(10),
                  border: Border.all(color: selected ? _accent.withValues(alpha: 0.5) : _border),
                ),
                child: Icon(iconData, size: 17, color: iconColor),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      option.label,
                      style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: _textPrimary),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                    if (option.subtitle != null) ...<Widget>[
                      const SizedBox(height: 2),
                      Text(
                        option.subtitle!,
                        style: TextStyle(fontSize: 12, color: _textMuted),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ],
                ),
              ),
              const SizedBox(width: 8),
              if (option.isByok) ...<Widget>[_ModelTag(appStrings.aiByokTag), const SizedBox(width: 6)],
              if (option.adminGranted) ...<Widget>[_ModelTag(appStrings.aiGranted, tooltip: appStrings.aiGrantedHint), const SizedBox(width: 6)],
              if (option.priceTier != null) ...<Widget>[_PriceTierChip(tier: option.priceTier!), const SizedBox(width: 6)],
              SizedBox(width: 20, child: selected ? Icon(Icons.check_circle_rounded, size: 18, color: _accent) : null),
            ],
          ),
        ),
      ),
    );
  }
}
