part of 'main.dart';

// The right panel of the editor: parties, the settings of the selected
// field, and the palette of field types (a bottom sheet on phones).

class _SigningSidePanel extends StatelessWidget {
  const _SigningSidePanel({required this.state, required this.onAutodetect, this.closeOnPlace});

  final SigningEditorState state;
  final VoidCallback onAutodetect;

  /// Phones: closes the sheet once a tile is armed, so the page is reachable.
  final VoidCallback? closeOnPlace;

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: _bgCard,
      child: ListenableBuilder(
        listenable: state,
        builder: (context, _) {
          final selected = state.selected;
          final settings = selected != null && state.showSettings;
          return ListView(
            key: const ValueKey<String>('signing-side-panel'),
            padding: const EdgeInsets.all(16),
            children: <Widget>[
              _PartiesSection(state: state),
              const SizedBox(height: 16),
              Wrap(
                spacing: 8,
                runSpacing: 4,
                children: <Widget>[
                  ChoiceChip(
                    key: const ValueKey<String>('signing-tab-fields'),
                    label: Text(appStrings.signingFields),
                    selected: !settings,
                    showCheckmark: false,
                    onSelected: (_) => state.showSettingsTab(false),
                  ),
                  ChoiceChip(
                    key: const ValueKey<String>('signing-tab-settings'),
                    label: Text(appStrings.signingTabSettings),
                    selected: settings,
                    showCheckmark: false,
                    onSelected: selected == null ? null : (_) => state.showSettingsTab(true),
                  ),
                ],
              ),
              const SizedBox(height: 14),
              if (settings)
                _FieldInspector(key: ValueKey<String>('inspector-${selected.id}'), state: state, fieldId: selected.id)
              else ...<Widget>[
                Text(appStrings.signingPlaceHint, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4)),
                const SizedBox(height: 10),
                _Palette(state: state, closeOnPlace: closeOnPlace),
                const SizedBox(height: 16),
                OutlinedButton.icon(
                  key: const ValueKey<String>('signing-autodetect'),
                  onPressed: state.locked ? null : onAutodetect,
                  icon: const Icon(Icons.auto_fix_high_rounded, size: 18),
                  label: Text(appStrings.signingAutodetect),
                ),
              ],
            ],
          );
        },
      ),
    );
  }
}

// Parties

class _PartiesSection extends StatelessWidget {
  const _PartiesSection({required this.state});

  final SigningEditorState state;

  Future<void> _edit(BuildContext context, SigningParty party) async {
    final result = await showAdaptiveSheet<(String, String)>(
      context: context,
      builder: (dialogContext) => _PartyEditDialog(party: party),
    );
    if (result == null) return;
    state.renameParty(party.id, result.$1);
    state.setPartyEmail(party.id, result.$2.trim());
  }

  Future<void> _remove(BuildContext context, SigningParty party) => _confirmDelete(
    context,
    title: appStrings.signingDeletePartyTitle(party.name),
    message: appStrings.signingDeletePartyBody,
    confirmLabel: appStrings.signingDeleteParty,
    onConfirm: () async => state.deleteParty(party.id),
  );

  @override
  Widget build(BuildContext context) {
    final parties = state.parties;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.signingParties, style: _sectionTitleStyle()),
        const SizedBox(height: 8),
        for (final party in parties)
          Material(
            key: ValueKey<String>('signing-party-${party.id}'),
            color: party.id == state.activePartyId ? _bgTertiary : Colors.transparent,
            borderRadius: BorderRadius.circular(AppRadius.control),
            child: InkWell(
              borderRadius: BorderRadius.circular(AppRadius.control),
              onTap: () => state.setActiveParty(party.id),
              child: Padding(
                padding: const EdgeInsets.only(left: 10, top: 2, bottom: 2),
                child: Row(
                  children: <Widget>[
                    Container(width: 14, height: 14, decoration: BoxDecoration(color: state.colorOf(party.id), shape: BoxShape.circle)),
                    const SizedBox(width: 10),
                    if (party.isSelf) Padding(padding: const EdgeInsets.only(right: 6), child: Icon(Icons.person_rounded, size: 15, color: _textMuted)),
                    Expanded(
                      child: Text(
                        party.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: _textPrimary, fontWeight: party.id == state.activePartyId ? FontWeight.w600 : FontWeight.w400),
                      ),
                    ),
                    Text('${state.fieldCountOf(party.id)}', style: TextStyle(color: _textMuted, fontSize: 12)),
                    PopupMenuButton<String>(
                      key: ValueKey<String>('signing-party-menu-${party.id}'),
                      enabled: !state.locked,
                      icon: Icon(Icons.more_vert_rounded, size: 18, color: _textMuted),
                      onSelected: (value) {
                        if (value == 'edit') unawaited(_edit(context, party));
                        if (value == 'remove') unawaited(_remove(context, party));
                      },
                      itemBuilder: (context) => <PopupMenuEntry<String>>[
                        PopupMenuItem<String>(value: 'edit', child: Text(appStrings.signingRenameParty)),
                        if (parties.length > 1) PopupMenuItem<String>(value: 'remove', child: Text(appStrings.signingDeleteParty)),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ),
        const SizedBox(height: 4),
        Wrap(
          children: <Widget>[
            TextButton.icon(
              key: const ValueKey<String>('signing-add-party'),
              onPressed: state.canAddParty ? state.addParty : null,
              icon: const Icon(Icons.add_rounded, size: 18),
              label: Text(appStrings.signingAddParty),
            ),
            if (state.canAddSelf)
              TextButton.icon(
                key: const ValueKey<String>('signing-add-me'),
                onPressed: state.addSelfParty,
                icon: const Icon(Icons.person_add_alt_rounded, size: 18),
                label: Text(appStrings.signingAddMe),
              ),
          ],
        ),
      ],
    );
  }
}

class _PartyEditDialog extends StatefulWidget {
  const _PartyEditDialog({required this.party});

  final SigningParty party;

  @override
  State<_PartyEditDialog> createState() => _PartyEditDialogState();
}

class _PartyEditDialogState extends State<_PartyEditDialog> {
  late final TextEditingController _name = TextEditingController(text: widget.party.name);
  late final TextEditingController _email = TextEditingController(text: widget.party.email ?? '');

  @override
  void dispose() {
    _name.dispose();
    _email.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(appStrings.signingPartyEditTitle),
      content: SizedBox(
        width: 380,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            TextField(key: const ValueKey<String>('party-name'), controller: _name, autofocus: true, decoration: InputDecoration(labelText: appStrings.signingPartyName)),
            const SizedBox(height: 12),
            TextField(key: const ValueKey<String>('party-email'), controller: _email, keyboardType: TextInputType.emailAddress, decoration: InputDecoration(labelText: appStrings.signingPartyEmail)),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          key: const ValueKey<String>('party-save'),
          onPressed: () => Navigator.of(context).pop((_name.text, _email.text)),
          child: Text(appStrings.signingSave),
        ),
      ],
    );
  }
}

// The palette

class _Palette extends StatelessWidget {
  const _Palette({required this.state, this.closeOnPlace});

  final SigningEditorState state;
  final VoidCallback? closeOnPlace;

  @override
  Widget build(BuildContext context) {
    final touch = MediaQuery.sizeOf(context).width < 700;
    return GridView.count(
      crossAxisCount: 3,
      shrinkWrap: true,
      physics: const NeverScrollableScrollPhysics(),
      mainAxisSpacing: 8,
      crossAxisSpacing: 8,
      childAspectRatio: 1.05,
      children: <Widget>[
        for (final spec in FieldTypes.all)
          _PaletteTile(
            key: ValueKey<String>('palette-${spec.type.name}'),
            spec: spec,
            armed: state.armedType == spec.type,
            enabled: state.canAddField,
            longPressToDrag: touch,
            color: state.colorOf(state.activePartyId),
            onTap: () {
              state.arm(spec.type);
              if (state.armedType != null) closeOnPlace?.call();
            },
          ),
      ],
    );
  }
}

class _PaletteTile extends StatelessWidget {
  const _PaletteTile({super.key, required this.spec, required this.armed, required this.enabled, required this.longPressToDrag, required this.color, required this.onTap, this.label});

  final FieldTypeSpec spec;

  /// In place of the type's own name.
  final String? label;
  final bool armed;
  final bool enabled;
  final bool longPressToDrag;
  final Color color;
  final VoidCallback onTap;

  Widget _face({bool lifted = false}) => Container(
    decoration: BoxDecoration(
      color: lifted ? _bgCard.withValues(alpha: 0.92) : _bgSecondary,
      borderRadius: BorderRadius.circular(AppRadius.control),
      border: Border.all(color: armed ? _accent : _border, width: armed ? 2 : 1),
    ),
    padding: const EdgeInsets.all(8),
    child: Stack(
      children: <Widget>[
        Positioned(right: 0, top: 0, child: Icon(Icons.drag_indicator_rounded, size: 14, color: _textMuted)),
        Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(spec.icon, size: 24, color: color),
              const SizedBox(height: 6),
              Text(label ?? spec.label(appStrings), maxLines: 2, textAlign: TextAlign.center, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary, fontSize: 12, height: 1.2)),
            ],
          ),
        ),
      ],
    ),
  );

  @override
  Widget build(BuildContext context) {
    final tile = Semantics(
      button: true,
      label: label ?? spec.label(appStrings),
      child: InkWell(borderRadius: BorderRadius.circular(AppRadius.control), onTap: enabled ? onTap : null, child: _face()),
    );
    if (!enabled) return Opacity(opacity: 0.5, child: tile);
    final feedback = Material(color: Colors.transparent, child: SizedBox(width: 96, height: 84, child: _face(lifted: true)));
    if (longPressToDrag) {
      return LongPressDraggable<FieldType>(data: spec.type, dragAnchorStrategy: pointerDragAnchorStrategy, feedback: feedback, child: tile);
    }
    return Draggable<FieldType>(data: spec.type, dragAnchorStrategy: pointerDragAnchorStrategy, feedback: feedback, child: tile);
  }
}

// The inspector

class _FieldInspector extends StatefulWidget {
  const _FieldInspector({super.key, required this.state, required this.fieldId});

  final SigningEditorState state;
  final String fieldId;

  @override
  State<_FieldInspector> createState() => _FieldInspectorState();
}

class _FieldInspectorState extends State<_FieldInspector> {
  late final TextEditingController _name;
  late final TextEditingController _default;

  SigningEditorState get _state => widget.state;

  @override
  void initState() {
    super.initState();
    final field = _state.fieldById(widget.fieldId)!;
    _name = TextEditingController(text: field.name);
    _default = TextEditingController(text: field.defaultValue == null ? '' : '${field.defaultValue}');
  }

  @override
  void dispose() {
    _name.dispose();
    _default.dispose();
    super.dispose();
  }

  void _update(SigningField Function(SigningField field) change, {String? coalesce}) =>
      _state.updateField(widget.fieldId, change, coalesce: coalesce);

  void _setPref(String key, Object? value) => _update((field) => field.copyWith(prefs: field.prefs.set(key, value)), coalesce: 'pref:$key');

  @override
  Widget build(BuildContext context) {
    final field = _state.fieldById(widget.fieldId);
    if (field == null) return const SizedBox.shrink();
    final spec = FieldTypes.of(field.type);
    final locked = _state.locked;
    final problems = fieldProblems(field);
    final isHeading = !spec.signerFillable;
    final takesDefault = !spec.isImageValue && field.type != FieldType.checkbox && field.type != FieldType.date && field.type != FieldType.multiple;
    final canBeReadonly = spec.signerFillable && !spec.isImageValue;
    return Container(
      key: const ValueKey<String>('signing-inspector'),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: _bgSecondary, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(spec.icon, size: 18, color: _state.colorOf(field.partyId)),
              const SizedBox(width: 8),
              Expanded(child: Text('${spec.label(appStrings)} · ${appStrings.signingFieldSettings}', style: _sectionTitleStyle())),
              IconButton(key: const ValueKey<String>('signing-field-duplicate'), tooltip: appStrings.signingDuplicateField, visualDensity: VisualDensity.compact, onPressed: locked ? null : _state.duplicateSelected, icon: const Icon(Icons.copy_rounded, size: 18)),
              IconButton(key: const ValueKey<String>('signing-field-delete'), tooltip: appStrings.signingDeleteField, visualDensity: VisualDensity.compact, onPressed: locked ? null : _state.deleteSelected, icon: Icon(Icons.delete_outline_rounded, size: 18, color: _danger)),
            ],
          ),
          const SizedBox(height: 10),
          DropdownButtonFormField<String>(
            key: ValueKey<String>('field-party-${field.partyId}'),
            initialValue: field.partyId,
            isExpanded: true,
            decoration: InputDecoration(labelText: appStrings.signingFieldParty, isDense: true),
            items: <DropdownMenuItem<String>>[
              for (final party in _state.parties)
                DropdownMenuItem<String>(
                  value: party.id,
                  child: Row(children: <Widget>[Container(width: 10, height: 10, decoration: BoxDecoration(color: _state.colorOf(party.id), shape: BoxShape.circle)), const SizedBox(width: 8), Flexible(child: Text(party.name, overflow: TextOverflow.ellipsis))]),
                ),
            ],
            onChanged: locked ? null : (value) => value == null ? null : _state.setFieldParty(field.id, value),
          ),
          const SizedBox(height: 10),
          if (!isHeading)
            TextField(
              key: const ValueKey<String>('field-name'),
              controller: _name,
              readOnly: locked,
              maxLength: 100,
              buildCounter: (context, {required currentLength, required isFocused, maxLength}) => null,
              decoration: InputDecoration(labelText: appStrings.signingFieldLabel, isDense: true),
              onChanged: (text) => _update((field) => field.copyWith(name: text), coalesce: 'name'),
            ),
          if (!isHeading) ...<Widget>[
            SwitchListTile(
              key: const ValueKey<String>('field-required'),
              dense: true,
              contentPadding: EdgeInsets.zero,
              title: Text(appStrings.signingRequired, style: TextStyle(color: _textPrimary, fontSize: 14)),
              value: field.required && !field.readonly,
              onChanged: locked || field.readonly ? null : (value) => _update((field) => field.copyWith(required: value)),
            ),
            if (canBeReadonly)
              SwitchListTile(
                key: const ValueKey<String>('field-readonly'),
                dense: true,
                contentPadding: EdgeInsets.zero,
                title: Text(appStrings.signingReadonly, style: TextStyle(color: _textPrimary, fontSize: 14)),
                value: field.readonly,
                onChanged: locked ? null : (value) => _update((field) => field.copyWith(readonly: value, required: value ? false : field.required)),
              ),
          ],
          if (takesDefault) ...<Widget>[
            const SizedBox(height: 6),
            if (spec.hasOptions && field.type != FieldType.multiple)
              _ChoiceDefault(field: field, locked: locked, onChanged: (value) => _update((field) => field.copyWith(defaultValue: value)))
            else
              TextField(
                key: const ValueKey<String>('field-default'),
                controller: _default,
                readOnly: locked,
                minLines: isHeading ? 2 : 1,
                maxLines: isHeading || field.prefs.multiline ? 4 : 1,
                keyboardType: field.type == FieldType.number ? const TextInputType.numberWithOptions(decimal: true, signed: true) : null,
                decoration: InputDecoration(labelText: isHeading ? appStrings.signingFieldHeading : appStrings.signingDefaultValue, isDense: true),
                onChanged: (text) => _update((field) => field.copyWith(defaultValue: text.isEmpty ? null : text), coalesce: 'default'),
              ),
          ],
          for (final pref in spec.prefs) ...<Widget>[
            const SizedBox(height: 8),
            _PrefControl(key: ValueKey<String>('pref-${field.id}-${pref.key}'), pref: pref, field: field, locked: locked, onChanged: (value) => _setPref(pref.key, value)),
          ],
          if (problems.isNotEmpty) ...<Widget>[
            const SizedBox(height: 10),
            for (final problem in problems)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Icon(Icons.error_outline_rounded, size: 16, color: _danger),
                    const SizedBox(width: 6),
                    Expanded(child: Text(problem.message(appStrings), key: ValueKey<String>('problem-${problem.name}'), style: TextStyle(color: _danger, fontSize: 12.5, height: 1.35))),
                  ],
                ),
              ),
          ],
        ],
      ),
    );
  }
}

/// The default of a select or radio field: one of its own choices.
class _ChoiceDefault extends StatelessWidget {
  const _ChoiceDefault({required this.field, required this.locked, required this.onChanged});

  final SigningField field;
  final bool locked;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) {
    final options = field.prefs.options;
    final current = field.defaultValue is String && options.contains(field.defaultValue) ? field.defaultValue as String : null;
    return DropdownButtonFormField<String?>(
      key: ValueKey<String>('field-default-choice-${options.join('|')}-$current'),
      initialValue: current,
      isExpanded: true,
      decoration: InputDecoration(labelText: appStrings.signingDefaultValue, isDense: true),
      items: <DropdownMenuItem<String?>>[
        DropdownMenuItem<String?>(value: null, child: Text(appStrings.signingColorNone)),
        for (final option in options) DropdownMenuItem<String?>(value: option, child: Text(option, overflow: TextOverflow.ellipsis)),
      ],
      onChanged: locked ? null : onChanged,
    );
  }
}

const List<String> _inkColors = <String>['#111111', '#1d4ed8', '#b91c1c', '#15803d', '#a16207', '#7e22ce'];

class _PrefControl extends StatelessWidget {
  const _PrefControl({super.key, required this.pref, required this.field, required this.locked, required this.onChanged});

  final PrefSpec pref;
  final SigningField field;
  final bool locked;
  final ValueChanged<Object?> onChanged;

  @override
  Widget build(BuildContext context) {
    final label = pref.label(appStrings);
    final value = field.prefs[pref.key];
    switch (pref.kind) {
      case PrefKind.toggle:
        return SwitchListTile(
          dense: true,
          contentPadding: EdgeInsets.zero,
          title: Text(label, style: TextStyle(color: _textPrimary, fontSize: 14)),
          value: value == true,
          onChanged: locked ? null : (on) => onChanged(on ? true : null),
        );
      case PrefKind.integer:
      case PrefKind.decimal:
        final integer = pref.kind == PrefKind.integer;
        return TextFormField(
          initialValue: value is num ? (integer ? '${value.toInt()}' : formatDecimal(value.toDouble())) : '',
          readOnly: locked,
          keyboardType: TextInputType.numberWithOptions(decimal: !integer, signed: pref.min == null || pref.min! < 0),
          decoration: InputDecoration(labelText: label, isDense: true, hintText: pref.min != null && pref.max != null ? '${pref.min} – ${pref.max}' : null),
          onChanged: (text) {
            final trimmed = text.trim();
            if (trimmed.isEmpty) return onChanged(null);
            final number = integer ? int.tryParse(trimmed) : parseDecimal(trimmed);
            if (number != null) onChanged(number);
          },
        );
      case PrefKind.choice:
        final current = pref.choices.any((choice) => choice.value == value) ? value as String : null;
        return DropdownButtonFormField<String?>(
          initialValue: current,
          isExpanded: true,
          decoration: InputDecoration(labelText: label, isDense: true),
          items: <DropdownMenuItem<String?>>[
            DropdownMenuItem<String?>(value: null, child: Text(appStrings.signingColorNone)),
            for (final choice in pref.choices) DropdownMenuItem<String?>(value: choice.value, child: Text(choice.label(appStrings))),
          ],
          onChanged: locked ? null : onChanged,
        );
      case PrefKind.text:
        return TextFormField(
          initialValue: value is String ? value : '',
          readOnly: locked,
          maxLength: 100,
          buildCounter: (context, {required currentLength, required isFocused, maxLength}) => null,
          decoration: InputDecoration(labelText: label, isDense: true),
          onChanged: (text) => onChanged(text.isEmpty ? null : text),
        );
      case PrefKind.color:
        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(label, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
            const SizedBox(height: 6),
            Wrap(
              spacing: 8,
              children: <Widget>[
                _Swatch(color: null, selected: value == null, onTap: locked ? null : () => onChanged(null)),
                for (final hex in _inkColors) _Swatch(color: parseHexColor(hex), selected: value == hex, onTap: locked ? null : () => onChanged(hex)),
              ],
            ),
          ],
        );
      case PrefKind.options:
        return _OptionsEditor(options: field.prefs.options, locked: locked, onChanged: onChanged);
    }
  }
}

class _Swatch extends StatelessWidget {
  const _Swatch({required this.color, required this.selected, required this.onTap});

  final Color? color;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      customBorder: const CircleBorder(),
      onTap: onTap,
      child: Container(
        width: 26,
        height: 26,
        decoration: BoxDecoration(color: color ?? _bgCard, shape: BoxShape.circle, border: Border.all(color: selected ? _accent : _border, width: selected ? 2.5 : 1)),
        child: color == null ? Icon(Icons.block_rounded, size: 14, color: _textMuted) : null,
      ),
    );
  }
}

/// The choices of a select, radio or multiple field, one text box each.
class _OptionsEditor extends StatefulWidget {
  const _OptionsEditor({required this.options, required this.locked, required this.onChanged});

  final List<String> options;
  final bool locked;
  final ValueChanged<Object?> onChanged;

  @override
  State<_OptionsEditor> createState() => _OptionsEditorState();
}

class _OptionsEditorState extends State<_OptionsEditor> {
  late List<TextEditingController> _controllers = <TextEditingController>[for (final option in widget.options) TextEditingController(text: option)];

  @override
  void dispose() {
    for (final controller in _controllers) {
      controller.dispose();
    }
    super.dispose();
  }

  void _emit() => widget.onChanged(_controllers.map((controller) => controller.text).toList(growable: false));

  void _add() {
    setState(() => _controllers = <TextEditingController>[..._controllers, TextEditingController(text: appStrings.signingChoiceLabel('${_controllers.length + 1}'))]);
    _emit();
  }

  void _remove(int index) {
    setState(() {
      _controllers[index].dispose();
      _controllers = <TextEditingController>[
        for (final (i, controller) in _controllers.indexed)
          if (i != index) controller,
      ];
    });
    _emit();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.signingPrefOptions, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
        for (final (index, controller) in _controllers.indexed)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Row(
              children: <Widget>[
                Expanded(
                  child: TextField(
                    key: ValueKey<String>('option-$index'),
                    controller: controller,
                    readOnly: widget.locked,
                    maxLength: 100,
                    buildCounter: (context, {required currentLength, required isFocused, maxLength}) => null,
                    decoration: const InputDecoration(isDense: true),
                    onChanged: (_) => _emit(),
                  ),
                ),
                IconButton(
                  key: ValueKey<String>('option-remove-$index'),
                  tooltip: appStrings.signingRemoveChoice,
                  visualDensity: VisualDensity.compact,
                  onPressed: widget.locked ? null : () => _remove(index),
                  icon: const Icon(Icons.close_rounded, size: 18),
                ),
              ],
            ),
          ),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            key: const ValueKey<String>('option-add'),
            onPressed: widget.locked || _controllers.length >= 50 ? null : _add,
            icon: const Icon(Icons.add_rounded, size: 18),
            label: Text(appStrings.signingAddChoice),
          ),
        ),
      ],
    );
  }
}
