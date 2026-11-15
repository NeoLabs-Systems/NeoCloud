part of 'main.dart';

// The card that asks for one field at a time: progress, the field's input
// for its type, validation, and Back / Skip / Next.

class _FieldPanel extends StatelessWidget {
  const _FieldPanel({required this.flow, required this.maxHeight, this.fill = false});

  final PublicSigningController flow;
  final double maxHeight;

  /// Fill the height it is given (the side card); else only what it needs.
  final bool fill;

  @override
  Widget build(BuildContext context) {
    final field = flow.active;
    if (field == null) return const SizedBox.shrink();
    final strings = appStrings;
    final spec = FieldTypes.of(field.type);
    final title = field.name.isEmpty ? spec.label(strings) : field.name;
    final position = flow.activeIndex + 1;
    final total = flow.fieldCount;
    final issue = flow.visibleIssue(field);
    final problem = flow.fieldErrors[field.id] ?? issue?.message(strings);
    final isLast = position >= total;
    final empty = spec.isEmpty(flow.valueOf(field.id));

    return ConstrainedBox(
      constraints: BoxConstraints(maxHeight: math.max(220, maxHeight)),
      child: Column(
        mainAxisSize: fill ? MainAxisSize.max : MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Flexible(
            fit: fill ? FlexFit.tight : FlexFit.loose,
            child: SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(AppSpacing.md, AppSpacing.md, AppSpacing.md, AppSpacing.xs),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: Semantics(
                          liveRegion: true,
                          label: strings.signingPublicProgressLabel(position, total, title),
                          child: ExcludeSemantics(
                            child: Text(
                              strings.signingPublicProgress(position, total),
                              style: TextStyle(color: _textSecondary, fontSize: 13, fontWeight: FontWeight.w600),
                            ),
                          ),
                        ),
                      ),
                      TextButton.icon(
                        key: const ValueKey<String>('field-list-button'),
                        onPressed: () => unawaited(_showFieldList(context, flow)),
                        icon: const Icon(Icons.list_rounded, size: 18),
                        label: Text(strings.signingPublicAllFields),
                        style: TextButton.styleFrom(minimumSize: const Size(44, 44)),
                      ),
                    ],
                  ),
                  ExcludeSemantics(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(3),
                      child: LinearProgressIndicator(value: position / total, minHeight: 4, backgroundColor: _bgTertiary, color: _accent),
                    ),
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Expanded(
                        child: Text(title, style: TextStyle(color: _textPrimary, fontSize: 18, fontWeight: FontWeight.w700, height: 1.25)),
                      ),
                      const SizedBox(width: AppSpacing.xs),
                      Padding(
                        padding: const EdgeInsets.only(top: 3),
                        child: Text(
                          field.required ? strings.signingPublicRequired : strings.signingPublicOptional,
                          style: TextStyle(color: field.required ? _accent : _textMuted, fontSize: 12, fontWeight: FontWeight.w600),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  _FieldInput(key: ValueKey<String>('input-${field.id}'), flow: flow, field: field),
                  if (problem != null) ...<Widget>[
                    const SizedBox(height: AppSpacing.xs),
                    Semantics(
                      liveRegion: true,
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: <Widget>[
                          Padding(padding: const EdgeInsets.only(top: 2), child: Icon(Icons.error_outline_rounded, size: 16, color: _danger)),
                          const SizedBox(width: 6),
                          Expanded(child: Text(problem, key: const ValueKey<String>('field-problem'), style: TextStyle(color: _danger, fontSize: 13, height: 1.35))),
                        ],
                      ),
                    ),
                  ],
                  const SizedBox(height: AppSpacing.xs),
                  _PublicSaveStatus(flow: flow),
                ],
              ),
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(AppSpacing.md, 4, AppSpacing.md, AppSpacing.sm),
            child: Row(
              children: <Widget>[
                IconButton.outlined(
                  key: const ValueKey<String>('field-back'),
                  tooltip: strings.signingPublicBack,
                  onPressed: flow.canGoBack ? flow.back : null,
                  icon: const Icon(Icons.arrow_back_rounded),
                  constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
                ),
                if (flow.canSkip && empty) ...<Widget>[
                  const SizedBox(width: 4),
                  TextButton(
                    key: const ValueKey<String>('field-skip'),
                    onPressed: flow.next,
                    style: TextButton.styleFrom(minimumSize: const Size(56, 48)),
                    child: Text(strings.signingPublicSkip),
                  ),
                ],
                const SizedBox(width: AppSpacing.xs),
                Expanded(
                  child: FilledButton(
                    key: const ValueKey<String>('field-next'),
                    onPressed: flow.next,
                    style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                    child: Text(isLast ? strings.signingPublicReviewAction : strings.signingPublicNext),
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// One line under the input: saving, saved, or waiting for the connection.
class _PublicSaveStatus extends StatelessWidget {
  const _PublicSaveStatus({required this.flow});

  final PublicSigningController flow;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final String text;
    final IconData icon;
    final Color color;
    switch (flow.saveState) {
      case SaveState.saving:
        text = strings.signingPublicSaving;
        icon = Icons.sync_rounded;
        color = _textMuted;
      case SaveState.offline:
        text = strings.signingPublicOfflineTitle;
        icon = Icons.cloud_off_rounded;
        color = _warning;
      case SaveState.idle:
        if (flow.filledCount == 0) return const SizedBox.shrink();
        text = strings.signingPublicSavedAll;
        icon = Icons.check_rounded;
        color = _textMuted;
    }
    return ExcludeSemantics(
      excluding: flow.saveState == SaveState.idle,
      child: Row(
        children: <Widget>[
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 6),
          Flexible(child: Text(text, style: TextStyle(color: color, fontSize: 12))),
        ],
      ),
    );
  }
}

Future<void> _showFieldList(BuildContext context, PublicSigningController flow) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(appStrings.signingPublicAllFields),
      content: SizedBox(
        width: 420,
        child: ListenableBuilder(
          listenable: flow,
          builder: (context, _) => _FieldListBody(
            flow: flow,
            onPick: (index) {
              Navigator.of(dialogContext).pop();
              flow.go(index);
            },
          ),
        ),
      ),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.close))],
    ),
  );
}

/// Every field of the signer with its state; tapping one jumps to it.
class _FieldListBody extends StatelessWidget {
  const _FieldListBody({required this.flow, required this.onPick});

  final PublicSigningController flow;
  final ValueChanged<int> onPick;

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    return ConstrainedBox(
      constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(context).height * 0.55),
      child: ListView.builder(
        shrinkWrap: true,
        itemCount: flow.fieldCount,
        itemBuilder: (context, index) {
          final field = flow.fields[index];
          final spec = FieldTypes.of(field.type);
          final title = field.name.isEmpty ? spec.label(strings) : field.name;
          final empty = spec.isEmpty(flow.valueOf(field.id));
          final broken = flow.issueFor(field) != null && !empty;
          final done = !empty && !broken;
          final page = field.areas.isEmpty ? null : field.areas.first.page + 1;
          return Semantics(
            button: true,
            selected: index == flow.activeIndex,
            label: '$title, ${done ? strings.signingPublicStateDone : strings.signingPublicStateOpen}',
            child: ListTile(
              key: ValueKey<String>('list-field-${field.id}'),
              minTileHeight: 52,
              contentPadding: EdgeInsets.zero,
              selected: index == flow.activeIndex,
              selectedColor: _textPrimary,
              selectedTileColor: _accentMuted,
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control)),
              leading: Icon(spec.icon, size: 20, color: _textSecondary),
              title: Text(title, maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text(<String>[
                if (page != null) strings.signingPublicDocumentPage(page),
                field.required ? strings.signingPublicRequired : strings.signingPublicOptional,
              ].join(' · ')),
              trailing: _StatusPill(label: done ? strings.signingPublicStateDone : strings.signingPublicStateOpen, color: done ? _success : _textMuted),
              onTap: () => onPick(index),
            ),
          );
        },
      ),
    );
  }
}

/// The input that suits the field's type.
class _FieldInput extends StatelessWidget {
  const _FieldInput({super.key, required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  Widget build(BuildContext context) {
    switch (field.type) {
      case FieldType.signature:
      case FieldType.initials:
        return _SignatureInput(flow: flow, field: field);
      case FieldType.image:
      case FieldType.stamp:
      case FieldType.file:
        return _UploadInput(flow: flow, field: field);
      case FieldType.checkbox:
        return _CheckboxInput(flow: flow, field: field);
      case FieldType.radio:
      case FieldType.multiple:
        return _ChoiceInput(flow: flow, field: field);
      case FieldType.select:
        return _SelectInput(flow: flow, field: field);
      case FieldType.text:
      case FieldType.number:
      case FieldType.phone:
      case FieldType.date:
      case FieldType.cells:
      case FieldType.heading:
        return _TypedInput(flow: flow, field: field);
    }
  }
}

/// Text, number, phone, date and character boxes: one text field each.
class _TypedInput extends StatefulWidget {
  const _TypedInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  State<_TypedInput> createState() => _TypedInputState();
}

class _TypedInputState extends State<_TypedInput> {
  late final TextEditingController _text = TextEditingController(text: _initialText());
  final FocusNode _focus = FocusNode();

  SigningField get _field => widget.field;
  String get _dateFormat => DateFormats.resolve(_field.prefs.format, currentAppLanguage.code);

  String _initialText() {
    final value = widget.flow.valueOf(widget.field.id);
    if (value == null) return '';
    if (widget.field.type == FieldType.date) {
      final date = parseSigningIsoDate('$value');
      return date == null ? '$value' : formatSigningDate(date, DateFormats.resolve(widget.field.prefs.format, currentAppLanguage.code));
    }
    return '$value';
  }

  @override
  void initState() {
    super.initState();
    _focus.addListener(() {
      if (!_focus.hasFocus) widget.flow.touch(_field);
    });
  }

  @override
  void dispose() {
    _text.dispose();
    _focus.dispose();
    super.dispose();
  }

  Future<void> _pickDate() async {
    final current = parseSigningIsoDate('${widget.flow.valueOf(_field.id) ?? ''}');
    final now = DateTime.now();
    final picked = await showDatePicker(
      context: context,
      initialDate: current ?? now,
      firstDate: DateTime(1900),
      lastDate: DateTime(2200),
      helpText: appStrings.signingPublicPickDate,
    );
    if (picked == null || !mounted) return;
    _text.text = formatSigningDate(picked, _dateFormat);
    widget.flow.setValue(_field, toSigningIsoDate(picked), immediate: true);
    widget.flow.touch(_field);
  }

  /// Fills the box with today's date (the picker also opens on it).
  void _useToday() {
    final now = DateTime.now();
    _text.text = formatSigningDate(now, _dateFormat);
    widget.flow.setValue(_field, toSigningIsoDate(now), immediate: true);
    widget.flow.touch(_field);
  }

  TextInputType get _keyboard {
    switch (_field.type) {
      case FieldType.number:
        return const TextInputType.numberWithOptions(decimal: true, signed: true);
      case FieldType.phone:
        return TextInputType.phone;
      case FieldType.date:
        return TextInputType.datetime;
      case FieldType.text:
        if (_field.prefs.multiline) return TextInputType.multiline;
        return _field.prefs.format == 'email' ? TextInputType.emailAddress : TextInputType.text;
      default:
        return TextInputType.text;
    }
  }

  List<TextInputFormatter> get _formatters {
    switch (_field.type) {
      case FieldType.number:
        return <TextInputFormatter>[FilteringTextInputFormatter.allow(RegExp(r'[0-9.,\- ]'))];
      case FieldType.phone:
        return <TextInputFormatter>[FilteringTextInputFormatter.allow(RegExp(r'[0-9+()\-./ ]'))];
      case FieldType.date:
        return <TextInputFormatter>[FilteringTextInputFormatter.allow(RegExp(r'[0-9./\-]'))];
      case FieldType.cells:
        return <TextInputFormatter>[LengthLimitingTextInputFormatter(_field.prefs.length ?? defaultCellCount)];
      default:
        return const <TextInputFormatter>[];
    }
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final multiline = _field.type == FieldType.text && _field.prefs.multiline;
    final isDate = _field.type == FieldType.date;
    final isCells = _field.type == FieldType.cells;
    final hint = isDate ? _dateHint(_dateFormat) : _field.prefs.placeholder ?? strings.signingPublicTypeHere;
    final isLast = widget.flow.activeIndex >= widget.flow.fieldCount - 1;
    final input = TextField(
      key: const ValueKey<String>('typed-input'),
      controller: _text,
      focusNode: _focus,
      autofocus: true,
      minLines: multiline ? 3 : 1,
      maxLines: multiline ? 6 : 1,
      keyboardType: _keyboard,
      textInputAction: multiline ? TextInputAction.newline : (isLast ? TextInputAction.done : TextInputAction.next),
      inputFormatters: _formatters,
      textCapitalization: _field.type == FieldType.text ? TextCapitalization.sentences : TextCapitalization.none,
      style: isCells ? const TextStyle(letterSpacing: 6, fontFeatures: <FontFeature>[FontFeature.tabularFigures()]) : null,
      decoration: InputDecoration(
        hintText: hint,
        helperText: isDate ? strings.signingPublicDateFormat(_dateHint(_dateFormat)) : (isCells ? strings.signingPublicCellsHint(_field.prefs.length ?? defaultCellCount) : null),
        suffixIcon: isDate
            ? IconButton(
                key: const ValueKey<String>('date-picker-button'),
                tooltip: strings.signingPublicPickDate,
                onPressed: _pickDate,
                icon: const Icon(Icons.calendar_today_rounded, size: 20),
                constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
              )
            : null,
      ),
      onChanged: (text) => widget.flow.setValue(_field, text),
      onSubmitted: multiline ? null : (_) => widget.flow.next(),
    );
    if (!isDate) return input;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        input,
        const SizedBox(height: 4),
        TextButton.icon(
          key: const ValueKey<String>('date-today-button'),
          onPressed: _useToday,
          icon: const Icon(Icons.today_rounded, size: 18),
          label: Text(strings.signingPublicToday),
        ),
      ],
    );
  }
}

class _ChoiceRow extends StatelessWidget {
  const _ChoiceRow({super.key, required this.label, required this.selected, required this.multiple, required this.onTap});

  final String label;
  final bool selected;
  final bool multiple;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final IconData icon;
    if (multiple) {
      icon = selected ? Icons.check_box_rounded : Icons.check_box_outline_blank_rounded;
    } else {
      icon = selected ? Icons.radio_button_checked_rounded : Icons.radio_button_unchecked_rounded;
    }
    return Semantics(
      checked: selected,
      inMutuallyExclusiveGroup: !multiple,
      button: true,
      label: label,
      excludeSemantics: true,
      onTap: onTap,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(AppRadius.control),
        child: Container(
          constraints: const BoxConstraints(minHeight: 48),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(AppRadius.control),
            color: selected ? _accentMuted : null,
            border: Border.all(color: selected ? _accent : _border),
          ),
          child: Row(
            children: <Widget>[
              Icon(icon, color: selected ? _accent : _textMuted),
              const SizedBox(width: 10),
              Expanded(child: Text(label, style: TextStyle(color: _textPrimary, height: 1.3))),
            ],
          ),
        ),
      ),
    );
  }
}

/// Radio (one of the options) and multiple (any of them).
class _ChoiceInput extends StatelessWidget {
  const _ChoiceInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  Widget build(BuildContext context) {
    final multiple = field.type == FieldType.multiple;
    final options = field.prefs.options;
    final value = flow.valueOf(field.id);
    final chosen = value is List ? value.map((entry) => '$entry').toSet() : <String>{if (value != null) '$value'};
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        for (final option in options)
          Padding(
            padding: const EdgeInsets.only(bottom: 6),
            child: _ChoiceRow(
              key: ValueKey<String>('choice-$option'),
              label: option,
              selected: chosen.contains(option),
              multiple: multiple,
              onTap: () {
                if (!multiple) {
                  flow.setValue(field, chosen.contains(option) ? null : option, immediate: true);
                  return;
                }
                final next = <String>{...chosen};
                if (!next.remove(option)) next.add(option);
                flow.setValue(field, options.where(next.contains).toList(growable: false), immediate: true);
              },
            ),
          ),
      ],
    );
  }
}

class _SelectInput extends StatelessWidget {
  const _SelectInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  Widget build(BuildContext context) {
    final options = field.prefs.options;
    final value = flow.valueOf(field.id);
    return DropdownButtonFormField<String>(
      key: const ValueKey<String>('select-input'),
      initialValue: options.contains('$value') ? '$value' : null,
      isExpanded: true,
      hint: Text(appStrings.signingPublicChoose),
      items: <DropdownMenuItem<String>>[
        for (final option in options) DropdownMenuItem<String>(value: option, child: Text(option, maxLines: 2, overflow: TextOverflow.ellipsis)),
      ],
      onChanged: (picked) => flow.setValue(field, picked, immediate: true),
    );
  }
}

class _CheckboxInput extends StatelessWidget {
  const _CheckboxInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  Widget build(BuildContext context) {
    final checked = flow.valueOf(field.id) == true;
    final label = field.name.isEmpty ? FieldTypes.of(field.type).label(appStrings) : field.name;
    return _ChoiceRow(
      key: const ValueKey<String>('checkbox-input'),
      label: label,
      selected: checked,
      multiple: true,
      onTap: () => flow.setValue(field, !checked, immediate: true),
    );
  }
}

/// Signature and initials: the signature dialog (draw, type or upload).
class _SignatureInput extends StatelessWidget {
  const _SignatureInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  bool get _initials => field.type == FieldType.initials;

  Future<void> _open(BuildContext context) async {
    final result = await showSignatureDialog(
      context,
      initials: _initials,
      offerInitials: !_initials && flow.hasEmptyInitials,
      initialName: flow.signerName,
    );
    if (result == null) return;
    flow.setUpload(field, bytes: result.png, fileName: _initials ? 'initials.png' : 'signature.png', method: result.method, typedText: result.typedText);
    if (result.useForInitials) {
      for (final other in flow.fields.where((candidate) => candidate.type == FieldType.initials && FieldTypes.of(candidate.type).isEmpty(flow.valueOf(candidate.id)))) {
        flow.setUpload(other, bytes: result.png, fileName: 'initials.png', method: result.method, typedText: result.typedText);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final value = flow.valueOf(field.id);
    if (value is UploadedValue) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Semantics(
            image: true,
            label: strings.signingPublicSignatureSaved,
            child: Container(
              key: const ValueKey<String>('signature-preview'),
              height: 110,
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
              child: _RemoteImage(flow: flow, fieldId: field.id, revision: value),
            ),
          ),
          const SizedBox(height: AppSpacing.xs),
          Row(
            children: <Widget>[
              OutlinedButton(
                key: const ValueKey<String>('signature-change'),
                onPressed: () => unawaited(_open(context)),
                style: OutlinedButton.styleFrom(minimumSize: const Size(88, 44)),
                child: Text(strings.signingPublicChange),
              ),
              const SizedBox(width: AppSpacing.xs),
              TextButton(
                key: const ValueKey<String>('signature-remove'),
                onPressed: () => flow.clearValue(field),
                style: TextButton.styleFrom(minimumSize: const Size(88, 44)),
                child: Text(strings.signingPublicRemove),
              ),
            ],
          ),
        ],
      );
    }
    return Semantics(
      button: true,
      label: _initials ? strings.signingPublicAddInitials : strings.signingPublicAddSignature,
      excludeSemantics: true,
      onTap: () => unawaited(_open(context)),
      child: InkWell(
        key: const ValueKey<String>('signature-add'),
        onTap: () => unawaited(_open(context)),
        borderRadius: BorderRadius.circular(AppRadius.control),
        child: Container(
          height: 96,
          alignment: Alignment.center,
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(AppRadius.control),
            border: Border.all(color: _accent, width: 1.5),
            color: _accentMuted,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(Icons.draw_rounded, color: _accent),
              const SizedBox(width: 8),
              Flexible(
                child: Text(
                  _initials ? strings.signingPublicAddInitials : strings.signingPublicAddSignature,
                  style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Image, stamp and file fields: choose a picture or file from the device
/// (on a phone the system picker offers the camera too).
class _UploadInput extends StatefulWidget {
  const _UploadInput({required this.flow, required this.field});

  final PublicSigningController flow;
  final SigningField field;

  @override
  State<_UploadInput> createState() => _UploadInputState();
}

class _UploadInputState extends State<_UploadInput> {
  static const int _maxImageBytes = 8 * 1024 * 1024;
  static const int _maxFileBytes = 10 * 1024 * 1024;

  String? _problem;

  bool get _isFile => widget.field.type == FieldType.file;

  static String _mimeOf(String name, {required bool image}) {
    final extension = name.contains('.') ? name.split('.').last.toLowerCase() : '';
    const known = <String, String>{
      'png': 'image/png',
      'jpg': 'image/jpeg',
      'jpeg': 'image/jpeg',
      'webp': 'image/webp',
      'gif': 'image/gif',
      'heic': 'image/heic',
      'pdf': 'application/pdf',
      'txt': 'text/plain',
      'doc': 'application/msword',
      'docx': 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    };
    return known[extension] ?? (image ? 'image/png' : 'application/octet-stream');
  }

  static String _size(int bytes) => '${(bytes / (1024 * 1024)).toStringAsFixed(0)} MB';

  Future<void> _pick() async {
    final strings = appStrings;
    setState(() => _problem = null);
    try {
      final result = await FilePicker.platform.pickFiles(type: _isFile ? FileType.any : FileType.image, withData: true);
      final file = result?.files.single;
      final bytes = file?.bytes;
      if (file == null || bytes == null || !mounted) return;
      final limit = _isFile ? _maxFileBytes : _maxImageBytes;
      if (bytes.length > limit) {
        setState(() => _problem = strings.signingPublicTooLarge(_size(limit)));
        return;
      }
      widget.flow.setUpload(
        widget.field,
        bytes: bytes,
        fileName: file.name,
        contentType: _mimeOf(file.name, image: !_isFile),
      );
    } catch (_) {
      if (mounted) setState(() => _problem = strings.signingPublicPickFailed);
    }
  }

  @override
  Widget build(BuildContext context) {
    final strings = appStrings;
    final value = widget.flow.valueOf(widget.field.id);
    final problem = _problem;
    final has = value is UploadedValue;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (has && value.isImage)
          Container(
            key: const ValueKey<String>('upload-preview'),
            height: 130,
            margin: const EdgeInsets.only(bottom: AppSpacing.xs),
            padding: const EdgeInsets.all(8),
            decoration: BoxDecoration(color: Colors.white, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
            child: _RemoteImage(flow: widget.flow, fieldId: widget.field.id, revision: value),
          ),
        if (has && !value.isImage)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.xs),
            child: Row(
              children: <Widget>[
                Icon(Icons.insert_drive_file_outlined, color: _textSecondary),
                const SizedBox(width: 8),
                Expanded(child: Text(value.fileName ?? strings.signingPublicUploaded, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textPrimary))),
              ],
            ),
          ),
        Row(
          children: <Widget>[
            Expanded(
              child: OutlinedButton.icon(
                key: const ValueKey<String>('upload-pick'),
                onPressed: _pick,
                style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(52)),
                icon: Icon(_isFile ? Icons.attach_file_rounded : Icons.add_photo_alternate_outlined),
                label: Text(has ? strings.signingPublicReplaceFile : (_isFile ? strings.signingPublicChooseFile : strings.signingPublicChoosePicture)),
              ),
            ),
            if (has) ...<Widget>[
              const SizedBox(width: AppSpacing.xs),
              TextButton(
                key: const ValueKey<String>('upload-remove'),
                onPressed: () => widget.flow.clearValue(widget.field),
                style: TextButton.styleFrom(minimumSize: const Size(72, 52)),
                child: Text(strings.signingPublicRemove),
              ),
            ],
          ],
        ),
        if (problem != null)
          Padding(padding: const EdgeInsets.only(top: AppSpacing.xs), child: Text(problem, style: TextStyle(color: _danger, fontSize: 13))),
      ],
    );
  }
}

/// `DD.MM.YYYY` as a person reads it: `TT.MM.JJJJ` in German.
String _dateHint(String format) {
  if (currentAppLanguage != AppLanguage.german) return format;
  return format.replaceAll('DD', 'TT').replaceAll('YYYY', 'JJJJ');
}
