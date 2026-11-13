part of 'main.dart';

// Settings › AI › File types: kinds of file AI leaves alone (setting
// `ai_skip_types`). One switch per category; "Choose formats" opens the
// category's formats as chips to switch single ones off. The categories
// and their formats come from the server (`fileTypes` of /api/ai/status).

String _aiFileTypeLabel(String id) => switch (id) {
  'images' => appStrings.aiFileTypeImages,
  'videos' => appStrings.aiFileTypeVideos,
  'documents' => appStrings.aiFileTypeDocuments,
  'spreadsheets' => appStrings.aiFileTypeSpreadsheets,
  'presentations' => appStrings.aiFileTypePresentations,
  'archives' => appStrings.aiFileTypeArchives,
  'audio' => appStrings.aiFileTypeAudio,
  'code' => appStrings.aiFileTypeCode,
  'other' => appStrings.aiFileTypeOther,
  _ => id,
};

/// The File types group; [enabled] is false until the person opted in.
class AiFileTypesGroup extends StatelessWidget {
  const AiFileTypesGroup({super.key, required this.controller, required this.categories, required this.enabled});

  final NeoCloudController controller;
  final List<AiFileTypeCategory> categories;
  final bool enabled;

  void _save(AiSkipTypes next) => unawaited(controller.updateSettings(<String, Object?>{'ai_skip_types': next.toJson()}));

  @override
  Widget build(BuildContext context) {
    final skip = controller.settings.aiSkipTypes;
    return _SettingsGroup(
      title: appStrings.aiFileTypesTitle,
      description: appStrings.aiFileTypesDescription,
      children: <Widget>[
        for (final category in categories)
          _AiFileTypeRow(
            key: ValueKey<String>(category.id),
            category: category,
            skip: skip,
            onChanged: enabled ? _save : null,
          ),
      ],
    );
  }
}

class _AiFileTypeRow extends StatefulWidget {
  const _AiFileTypeRow({super.key, required this.category, required this.skip, required this.onChanged});

  final AiFileTypeCategory category;
  final AiSkipTypes skip;
  final ValueChanged<AiSkipTypes>? onChanged;

  @override
  State<_AiFileTypeRow> createState() => _AiFileTypeRowState();
}

class _AiFileTypeRowState extends State<_AiFileTypeRow> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final category = widget.category;
    final skip = widget.skip;
    final onChanged = widget.onChanged;
    final formats = category.extensions;
    final on = formats.where((ext) => skip.formatOn(category, ext)).length;
    String? description;
    if (formats.isEmpty) {
      description = appStrings.aiFileTypeOtherDescription;
    } else if (on > 0 && on < formats.length) {
      description = appStrings.aiFileTypesSome(on, formats.length);
    }
    final control = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (formats.isNotEmpty)
          TextButton.icon(
            onPressed: () => setState(() => _open = !_open),
            icon: Icon(_open ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
            label: Text(appStrings.aiFileTypesChoose),
          ),
        const SizedBox(width: 4),
        Switch(
          value: skip.categoryOn(category),
          onChanged: onChanged == null ? null : (value) => onChanged(skip.withCategory(category, value)),
        ),
      ],
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _SettingsRow(label: _aiFileTypeLabel(category.id), description: description, control: control),
        if (_open && formats.isNotEmpty)
          Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
            child: Wrap(
              spacing: 6,
              runSpacing: 6,
              children: <Widget>[
                for (final ext in formats)
                  _AiFormatChip(
                    label: '.$ext',
                    selected: skip.formatOn(category, ext),
                    onTap: onChanged == null ? null : () => onChanged(skip.withFormat(category, ext, !skip.formatOn(category, ext))),
                  ),
              ],
            ),
          ),
      ],
    );
  }
}

/// A format that AI reads (selected, with a check) or leaves alone; the
/// pill style of the model picker's filter chips.
class _AiFormatChip extends StatelessWidget {
  const _AiFormatChip({required this.label, required this.selected, required this.onTap});

  final String label;
  final bool selected;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: selected ? _bgTertiary : Colors.transparent,
      shape: StadiumBorder(side: BorderSide(color: selected ? _borderLight : _border)),
      child: InkWell(
        onTap: onTap,
        customBorder: const StadiumBorder(),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (selected) ...<Widget>[Icon(Icons.check_rounded, size: 14, color: _textPrimary), const SizedBox(width: 5)],
              Text(
                label,
                style: TextStyle(
                  fontSize: 12.5,
                  fontWeight: FontWeight.w600,
                  color: selected ? _textPrimary : _textMuted,
                  decoration: selected ? null : TextDecoration.lineThrough,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
