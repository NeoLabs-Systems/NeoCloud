part of 'main.dart';

// Settings › AI › Sorting rules: the account's own rules, tried before any
// model. A rule names a folder and the conditions a file must meet (type,
// issuer, extension, a name pattern); a match is suggested as a move
// labelled "Rule", never as AI.

class AiRulesCard extends StatefulWidget {
  const AiRulesCard({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AiRulesCard> createState() => _AiRulesCardState();
}

class _AiRulesCardState extends State<AiRulesCard> {
  List<AiRule>? _rules;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final rules = await _controller.ai.rules();
      if (mounted) {
        setState(() {
          _rules = rules;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  Future<void> _edit([AiRule? rule]) async {
    final saved = await showAdaptiveSheet<AiRule>(
      context: context,
      builder: (_) => _AiRuleDialog(controller: _controller, rule: rule),
    );
    if (saved != null) await _load();
  }

  Future<void> _setEnabled(AiRule rule, bool enabled) async {
    try {
      await _controller.ai.updateRule(rule.id, <String, Object?>{'enabled': enabled});
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
    }
  }

  Future<void> _delete(AiRule rule) => _confirmDelete(
    context,
    title: appStrings.aiRuleDeleteTitle,
    message: appStrings.aiRuleDeleteBody(rule.name),
    onConfirm: () async {
      try {
        await _controller.ai.deleteRule(rule.id);
        await _load();
      } catch (error) {
        if (mounted) setState(() => _error = _aiFriendlyError(_controller, error));
      }
    },
  );

  /// "Invoice · Telekom · *.pdf" – what a file must be.
  String _conditions(AiRule rule) {
    return <String>[
      if (rule.type != null) _aiDocTypeLabel(rule.type!),
      if (rule.issuer != null) rule.issuer!,
      if (rule.extension != null) '.${rule.extension}',
      if (rule.namePattern != null) rule.namePattern!,
    ].join(' · ');
  }

  @override
  Widget build(BuildContext context) {
    final rules = _rules;
    return _SettingsGroup(
      title: appStrings.aiRulesTitle,
      description: appStrings.aiRulesDescription,
      trailing: OutlinedButton.icon(
        onPressed: () => unawaited(_edit()),
        icon: const Icon(Icons.add_rounded, size: 18),
        label: Text(appStrings.aiRuleAdd),
      ),
      children: <Widget>[
        if (_error != null) _SettingsBlock(child: _InlineError(message: _error!)),
        if (rules == null && _error == null) const _LoadingPlaceholder(),
        if (rules != null && rules.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.aiRulesEmpty)),
        for (final rule in rules ?? const <AiRule>[])
          InkWell(
            onTap: () => unawaited(_edit(rule)),
            child: Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 8, 12),
              child: Row(
                children: <Widget>[
                  Icon(Icons.rule_rounded, size: 18, color: rule.enabled ? _textSecondary : _textMuted),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(rule.name, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: rule.enabled ? _textPrimary : _textMuted)),
                        const SizedBox(height: 3),
                        Text(
                          appStrings.aiRuleSummary(_conditions(rule), _aiFolderLabel(rule.targetPath)),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 12.5, color: _textSecondary, height: 1.4),
                        ),
                      ],
                    ),
                  ),
                  Switch(value: rule.enabled, onChanged: (value) => unawaited(_setEnabled(rule, value))),
                  IconButton(
                    tooltip: appStrings.delete,
                    onPressed: () => unawaited(_delete(rule)),
                    icon: Icon(Icons.delete_outline_rounded, size: 19, color: _textMuted),
                  ),
                ],
              ),
            ),
          ),
      ],
    );
  }
}

/// Adds or edits one rule: a name, the conditions and the folder.
class _AiRuleDialog extends StatefulWidget {
  const _AiRuleDialog({required this.controller, this.rule});

  final NeoCloudController controller;
  final AiRule? rule;

  @override
  State<_AiRuleDialog> createState() => _AiRuleDialogState();
}

class _AiRuleDialogState extends State<_AiRuleDialog> {
  late final TextEditingController _name = TextEditingController(text: widget.rule?.name ?? '');
  late final TextEditingController _issuer = TextEditingController(text: widget.rule?.issuer ?? '');
  late final TextEditingController _extension = TextEditingController(text: widget.rule?.extension ?? '');
  late final TextEditingController _pattern = TextEditingController(text: widget.rule?.namePattern ?? '');
  late String? _type = widget.rule?.type;
  late String? _folderId = widget.rule?.targetFolderId;
  late String _folderLabel = widget.rule == null ? '' : _aiFolderLabel(widget.rule!.targetPath);
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _issuer.dispose();
    _extension.dispose();
    _pattern.dispose();
    super.dispose();
  }

  String? _text(TextEditingController controller) {
    final value = controller.text.trim();
    return value.isEmpty ? null : value;
  }

  Future<void> _pickFolder() async {
    final target = await showFolderPicker(
      context,
      widget.controller,
      title: appStrings.aiRuleTarget,
      confirmLabel: appStrings.aiRuleChooseFolder,
      hideVaults: true,
    );
    if (target == null || !mounted) return;
    setState(() {
      _folderId = target.$1;
      _folderLabel = target.$2;
    });
  }

  Future<void> _save() async {
    final name = _text(_name);
    final folderId = _folderId;
    final rule = AiRule(
      id: widget.rule?.id ?? 0,
      name: name ?? '',
      type: _type,
      issuer: _text(_issuer),
      extension: _text(_extension)?.replaceFirst(RegExp(r'^\.'), ''),
      namePattern: _text(_pattern),
      targetFolderId: folderId ?? '',
      enabled: widget.rule?.enabled ?? true,
    );
    if (name == null) return setState(() => _error = appStrings.aiRuleNeedsName);
    if (rule.type == null && rule.issuer == null && rule.extension == null && rule.namePattern == null) {
      return setState(() => _error = appStrings.aiRuleNeedsCondition);
    }
    if (folderId == null) return setState(() => _error = appStrings.aiRuleNeedsFolder);
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final ai = widget.controller.ai;
      final saved = widget.rule == null ? await ai.createRule(rule) : await ai.updateRule(rule.id, rule.toJson());
      if (mounted) Navigator.of(context).pop(saved);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = error is ApiException && error.code == 'AI_TARGET_NOT_ALLOWED'
            ? appStrings.aiErrorTargetNotAllowed
            : _aiFriendlyError(widget.controller, error);
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(widget.rule == null ? appStrings.aiRuleAdd : appStrings.aiRuleEdit),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            TextField(controller: _name, autofocus: widget.rule == null, decoration: InputDecoration(labelText: appStrings.aiRuleName)),
            const SizedBox(height: 18),
            Text(appStrings.aiRuleWhen, style: _sectionTitleStyle()),
            const SizedBox(height: 10),
            DropdownButtonFormField<String?>(
              initialValue: _type,
              decoration: InputDecoration(labelText: appStrings.aiFieldType),
              items: <DropdownMenuItem<String?>>[
                DropdownMenuItem<String?>(child: Text(appStrings.aiRuleAnyType)),
                for (final type in _aiDocTypes) DropdownMenuItem<String?>(value: type, child: Text(_aiDocTypeLabel(type))),
              ],
              onChanged: _saving ? null : (value) => setState(() => _type = value),
            ),
            const SizedBox(height: 12),
            TextField(controller: _issuer, decoration: InputDecoration(labelText: appStrings.aiFieldIssuer, hintText: 'Telekom')),
            const SizedBox(height: 12),
            Row(
              children: <Widget>[
                Expanded(child: TextField(controller: _extension, decoration: InputDecoration(labelText: appStrings.aiRuleExtension, hintText: 'pdf'))),
                const SizedBox(width: 12),
                Expanded(
                  flex: 2,
                  child: TextField(controller: _pattern, decoration: InputDecoration(labelText: appStrings.aiRulePattern, hintText: 'Rechnung*')),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Text(appStrings.aiRuleTarget, style: _sectionTitleStyle()),
            const SizedBox(height: 10),
            OutlinedButton.icon(
              onPressed: _saving ? null : () => unawaited(_pickFolder()),
              icon: const Icon(Icons.folder_outlined, size: 18),
              label: Align(
                alignment: Alignment.centerLeft,
                child: Text(_folderLabel.isEmpty ? appStrings.aiRuleChooseFolder : _folderLabel, maxLines: 1, overflow: TextOverflow.ellipsis),
              ),
            ),
            if (_error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: _error!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _saving ? null : () => unawaited(_save()),
          child: _saving ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent)) : Text(appStrings.save),
        ),
      ],
    );
  }
}
