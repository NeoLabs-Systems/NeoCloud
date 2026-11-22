part of 'main.dart';

// The pieces of Notes: a note's card, its colors, the read view that ticks
// checklists, and the text controller that shows Markdown as it is typed
// (headings larger, **bold** bold, done tasks struck through), the marks
// themselves kept faint so the text stays plain Markdown.

class _NoteTint {
  const _NoteTint(this.background, this.border, this.dot);

  final Color background;
  final Color border;
  final Color dot;
}

/// A note color as a quiet card tint, for the current theme.
_NoteTint _noteTint(NoteColor color) {
  final dark = _appBrightness == Brightness.dark;
  Color hex(int value) => Color(value);
  switch (color) {
    case NoteColor.plain:
      return _NoteTint(_bgCard, _border, _textMuted);
    case NoteColor.red:
      return dark ? _NoteTint(hex(0xFF33201D), hex(0x40E08A79), hex(0xFFE08A79)) : _NoteTint(hex(0xFFFBE4E0), hex(0x33B4483C), hex(0xFFB4483C));
    case NoteColor.orange:
      return dark ? _NoteTint(hex(0xFF33251A), hex(0x40E0A070), hex(0xFFE0A070)) : _NoteTint(hex(0xFFFCEBDB), hex(0x33B86A2C), hex(0xFFB86A2C));
    case NoteColor.amber:
      return dark ? _NoteTint(hex(0xFF2E2818), hex(0x47E1B052), hex(0xFFE1B052)) : _NoteTint(hex(0xFFFAF0D6), hex(0x40B07D2B), hex(0xFFB07D2B));
    case NoteColor.green:
      return dark ? _NoteTint(hex(0xFF1C2A1E), hex(0x407DBF86), hex(0xFF7DBF86)) : _NoteTint(hex(0xFFE5F2E3), hex(0x334F8A55), hex(0xFF4F8A55));
    case NoteColor.teal:
      return dark ? _NoteTint(hex(0xFF18292A), hex(0x406EBEB4), hex(0xFF6EBEB4)) : _NoteTint(hex(0xFFDFF1EE), hex(0x332E7D73), hex(0xFF2E7D73));
    case NoteColor.blue:
      return dark ? _NoteTint(hex(0xFF1B2433), hex(0x407896D2), hex(0xFF7896D2)) : _NoteTint(hex(0xFFE2EAF7), hex(0x333C5F9E), hex(0xFF3C5F9E));
    case NoteColor.purple:
      return dark ? _NoteTint(hex(0xFF272233), hex(0x40AA8CD2), hex(0xFFAA8CD2)) : _NoteTint(hex(0xFFEEE6F6), hex(0x33704E9E), hex(0xFF704E9E));
    case NoteColor.gray:
      return dark ? _NoteTint(hex(0xFF232825), hex(0x40A9B2A2), hex(0xFFA9B2A2)) : _NoteTint(hex(0xFFEDEDE8), hex(0x33686E60), hex(0xFF686E60));
  }
}

String _noteColorLabel(NoteColor color) {
  switch (color) {
    case NoteColor.plain:
      return appStrings.notesColorNone;
    case NoteColor.red:
      return appStrings.notesColorRed;
    case NoteColor.orange:
      return appStrings.notesColorOrange;
    case NoteColor.amber:
      return appStrings.notesColorAmber;
    case NoteColor.green:
      return appStrings.notesColorGreen;
    case NoteColor.teal:
      return appStrings.notesColorTeal;
    case NoteColor.blue:
      return appStrings.notesColorBlue;
    case NoteColor.purple:
      return appStrings.notesColorPurple;
    case NoteColor.gray:
      return appStrings.notesColorGray;
  }
}

/// The color swatches; the chosen color, or null when dismissed.
Future<NoteColor?> _pickNoteColor(BuildContext context, NoteColor current) {
  return showAdaptiveSheet<NoteColor>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      title: Text(appStrings.notesColor),
      content: SizedBox(
        width: 360,
        child: Wrap(
          spacing: 10,
          runSpacing: 10,
          children: <Widget>[
            for (final color in NoteColor.values)
              Tooltip(
                message: _noteColorLabel(color),
                child: Semantics(
                  label: _noteColorLabel(color),
                  selected: color == current,
                  button: true,
                  child: InkWell(
                    customBorder: const CircleBorder(),
                    onTap: () => Navigator.of(dialogContext).pop(color),
                    child: Container(
                      width: 44,
                      height: 44,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: _noteTint(color).background,
                        border: Border.all(color: color == current ? _accent : _noteTint(color).border, width: color == current ? 2.5 : 1.5),
                      ),
                      child: color == NoteColor.plain ? Icon(Icons.format_color_reset_outlined, size: 18, color: _textMuted) : null,
                    ),
                  ),
                ),
              ),
          ],
        ),
      ),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
    ),
  );
}

/// A note in the grid: title, a few lines, the checklist's progress and
/// tags; ⋯ (or a long press) for pin, archive, color and trash.
class _NoteCardView extends StatelessWidget {
  const _NoteCardView({required this.card, required this.onTap, required this.actions});

  final NoteCard card;
  final VoidCallback onTap;
  final List<_FileAction> actions;

  @override
  Widget build(BuildContext context) {
    final tint = _noteTint(card.color);
    final radius = BorderRadius.circular(16);
    final locale = currentAppLanguage.code;
    return Semantics(
      button: true,
      label: card.title,
      child: Material(
        color: tint.background,
        shape: RoundedRectangleBorder(borderRadius: radius, side: BorderSide(color: tint.border)),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          onLongPress: () => unawaited(_showFileActionSheet(context, title: card.title, actions: actions)),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 6, 12),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Expanded(
                      child: Padding(
                        padding: const EdgeInsets.only(top: 6),
                        child: Text(card.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, height: 1.3, color: _textPrimary)),
                      ),
                    ),
                    if (card.encrypted) Padding(padding: const EdgeInsets.only(top: 8, left: 4), child: Icon(Icons.lock_outline_rounded, size: 15, color: _textMuted, semanticLabel: appStrings.notesEncrypted)),
                    if (card.pinned) Padding(padding: const EdgeInsets.only(top: 8, left: 4), child: Icon(Icons.push_pin, size: 15, color: _accent, semanticLabel: appStrings.notesPinned)),
                    SizedBox(width: 36, height: 32, child: _FileActionsMenu(actions: actions, title: card.title)),
                  ],
                ),
                Padding(
                  padding: const EdgeInsets.only(right: 10),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      if (card.snippet.isNotEmpty) ...<Widget>[
                        const SizedBox(height: 6),
                        Text(card.snippet, maxLines: 8, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, height: 1.5, color: _textSecondary)),
                      ],
                      if (card.checklist.total > 0) ...<Widget>[
                        const SizedBox(height: 10),
                        Row(
                          children: <Widget>[
                            Expanded(
                              child: ClipRRect(
                                borderRadius: BorderRadius.circular(AppRadius.pill),
                                child: LinearProgressIndicator(
                                  minHeight: 4,
                                  value: card.checklist.done / card.checklist.total,
                                  backgroundColor: _border,
                                  valueColor: AlwaysStoppedAnimation<Color>(_success),
                                ),
                              ),
                            ),
                            const SizedBox(width: 8),
                            Text('${card.checklist.done}/${card.checklist.total}', style: TextStyle(fontSize: 11, color: _textSecondary)),
                          ],
                        ),
                      ],
                      const SizedBox(height: 10),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        crossAxisAlignment: WrapCrossAlignment.center,
                        children: <Widget>[
                          for (final tag in card.tags.take(4)) _TagPill(tag),
                          if (card.notebook.isNotEmpty) Text(card.notebook, style: TextStyle(fontSize: 11, color: _textMuted)),
                          if (card.modifiedAt != null) Text(formatDate(card.modifiedAt, locale), style: TextStyle(fontSize: 11, color: _textMuted)),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _TagPill extends StatelessWidget {
  const _TagPill(this.tag, {this.onRemove});

  final String tag;
  final VoidCallback? onRemove;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: EdgeInsets.fromLTRB(9, 3, onRemove == null ? 9 : 4, 3),
      decoration: BoxDecoration(color: _textPrimary.withValues(alpha: 0.07), borderRadius: BorderRadius.circular(AppRadius.pill)),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Text('#$tag', style: TextStyle(fontSize: 12, color: _textSecondary)),
          if (onRemove != null)
            InkWell(
              onTap: onRemove,
              customBorder: const CircleBorder(),
              child: Padding(padding: const EdgeInsets.all(2), child: Icon(Icons.close_rounded, size: 13, color: _textMuted, semanticLabel: appStrings.notesRemoveTag(tag))),
            ),
        ],
      ),
    );
  }
}

/// The note as it reads: headings, lists, quotes, code, and checklists
/// that tick with a tap.
class _NoteReadView extends StatelessWidget {
  const _NoteReadView({required this.body, required this.onToggleTask});

  final String body;
  final ValueChanged<int> onToggleTask;

  @override
  Widget build(BuildContext context) {
    final blocks = parseNoteBlocks(body);
    if (body.trim().isEmpty) {
      return Text(appStrings.notesEmptyNote, style: TextStyle(fontSize: 16, color: _textMuted));
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[for (final block in blocks) _block(block)],
    );
  }

  Widget _block(NoteBlock block) {
    final base = TextStyle(fontSize: 16, height: 1.65, color: _textPrimary.withValues(alpha: 0.92));
    switch (block.kind) {
      case NoteBlockKind.heading1:
        return Padding(padding: const EdgeInsets.only(top: 14, bottom: 4), child: _inline(block.text, base.copyWith(fontSize: 26, fontWeight: FontWeight.w700, height: 1.25)));
      case NoteBlockKind.heading2:
        return Padding(padding: const EdgeInsets.only(top: 12, bottom: 2), child: _inline(block.text, base.copyWith(fontSize: 21, fontWeight: FontWeight.w600, height: 1.3)));
      case NoteBlockKind.heading3:
        return Padding(padding: const EdgeInsets.only(top: 8), child: _inline(block.text, base.copyWith(fontSize: 18, fontWeight: FontWeight.w600)));
      case NoteBlockKind.task:
        return InkWell(
          borderRadius: BorderRadius.circular(8),
          onTap: () => onToggleTask(block.line),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 3),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Padding(padding: const EdgeInsets.only(top: 4, right: 10), child: _TaskBox(done: block.done)),
                Expanded(child: _inline(block.text, block.done ? base.copyWith(color: _textMuted, decoration: TextDecoration.lineThrough) : base)),
              ],
            ),
          ),
        );
      case NoteBlockKind.bullet:
      case NoteBlockKind.numbered:
        return Padding(
          padding: const EdgeInsets.only(left: 4),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              SizedBox(width: 24, child: Text(block.kind == NoteBlockKind.bullet ? '•' : '${block.number}.', style: base.copyWith(color: _textSecondary))),
              Expanded(child: _inline(block.text, base)),
            ],
          ),
        );
      case NoteBlockKind.quote:
        return Container(
          margin: const EdgeInsets.symmetric(vertical: 4),
          padding: const EdgeInsets.fromLTRB(14, 2, 0, 2),
          decoration: BoxDecoration(border: Border(left: BorderSide(color: _borderLight, width: 3))),
          child: _inline(block.text, base.copyWith(color: _textSecondary, fontStyle: FontStyle.italic)),
        );
      case NoteBlockKind.code:
        return Container(
          margin: const EdgeInsets.symmetric(vertical: 6),
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control)),
          child: SelectableText(block.text, style: GoogleFonts.geistMono(fontSize: 13, height: 1.5, color: _textPrimary)),
        );
      case NoteBlockKind.divider:
        return Padding(padding: const EdgeInsets.symmetric(vertical: 12), child: Divider(height: 1, color: _borderLight));
      case NoteBlockKind.blank:
        return const SizedBox(height: 10);
      case NoteBlockKind.paragraph:
        return _inline(block.text, base);
    }
  }

  Widget _inline(String text, TextStyle style) {
    return Text.rich(TextSpan(
      style: style,
      children: <InlineSpan>[
        for (final run in parseInline(text))
          TextSpan(
            text: run.text,
            style: TextStyle(
              fontWeight: run.bold ? FontWeight.w700 : null,
              fontStyle: run.italic ? FontStyle.italic : null,
              decoration: run.strike ? TextDecoration.lineThrough : (run.link != null ? TextDecoration.underline : null),
              color: run.link != null ? _accent : null,
              fontFamily: run.code ? GoogleFonts.geistMono().fontFamily : null,
              backgroundColor: run.code ? _bgTertiary : null,
            ),
            recognizer: run.link == null ? null : (TapGestureRecognizer()..onTap = () => unawaited(_openNoteLink(run.link!))),
          ),
      ],
    ));
  }
}

Future<void> _openNoteLink(String link) async {
  final uri = Uri.tryParse(link);
  if (uri == null || !(uri.scheme == 'https' || uri.scheme == 'http' || uri.scheme == 'mailto')) return;
  await launchUrl(uri, mode: LaunchMode.externalApplication);
}

class _TaskBox extends StatelessWidget {
  const _TaskBox({required this.done});

  final bool done;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 18,
      height: 18,
      decoration: BoxDecoration(
        color: done ? _accent : Colors.transparent,
        borderRadius: BorderRadius.circular(5),
        border: done ? null : Border.all(color: _textSecondary, width: 1.5),
      ),
      child: done ? Icon(Icons.check_rounded, size: 14, color: _onAccent) : null,
    );
  }
}

/// Shows Markdown while it is typed: the text stays exactly what is saved,
/// only its style changes.
class _MarkdownEditingController extends TextEditingController {
  _MarkdownEditingController({super.text});

  static final RegExp _inlineMarks = RegExp(r'(\*\*[^*\n]+\*\*|~~[^~\n]+~~|`[^`\n]+`|\*[^*\s\n][^*\n]*\*)');

  @override
  TextSpan buildTextSpan({required BuildContext context, TextStyle? style, required bool withComposing}) {
    final base = style ?? const TextStyle();
    final faint = TextStyle(color: _textMuted.withValues(alpha: 0.7));
    final composing = value.composing;
    // While an IME composes, keep the plain text so the underline is right.
    if (withComposing && composing.isValid && !composing.isCollapsed) return TextSpan(style: base, text: text);
    final children = <InlineSpan>[];
    final lines = text.split('\n');
    for (var index = 0; index < lines.length; index += 1) {
      final line = lines[index];
      final heading = RegExp(r'^(#{1,3})\s').firstMatch(line);
      final task = RegExp(r'^(\s*[-*+]\s+\[([ xX])\]\s?)').firstMatch(line);
      final list = RegExp(r'^(\s*([-*+]|\d+[.)])\s)').firstMatch(line);
      var lineStyle = base;
      var marker = '';
      if (heading != null) {
        final level = heading.group(1)!.length;
        lineStyle = base.copyWith(fontSize: level == 1 ? 26 : (level == 2 ? 21 : 18), fontWeight: FontWeight.w700, height: 1.4);
        marker = heading.group(0)!;
      } else if (task != null) {
        marker = task.group(1)!;
        if (task.group(2) != ' ') lineStyle = base.copyWith(color: _textMuted, decoration: TextDecoration.lineThrough);
      } else if (line.startsWith('>')) {
        lineStyle = base.copyWith(color: _textSecondary, fontStyle: FontStyle.italic);
        marker = line.startsWith('> ') ? '> ' : '>';
      } else if (list != null) {
        marker = list.group(1)!;
      }
      if (marker.isNotEmpty) {
        final markerStyle = task != null ? TextStyle(color: _accent, fontWeight: FontWeight.w600, decoration: TextDecoration.none) : faint;
        children.add(TextSpan(text: marker, style: lineStyle.merge(markerStyle)));
      }
      final rest = line.substring(marker.length);
      var position = 0;
      for (final match in _inlineMarks.allMatches(rest)) {
        if (match.start > position) children.add(TextSpan(text: rest.substring(position, match.start), style: lineStyle));
        final token = match.group(0)!;
        final width = token.startsWith('**') || token.startsWith('~~') ? 2 : 1;
        final inner = token.substring(width, token.length - width);
        final innerStyle = token.startsWith('**')
            ? lineStyle.copyWith(fontWeight: FontWeight.w700)
            : token.startsWith('~~')
            ? lineStyle.copyWith(decoration: TextDecoration.lineThrough)
            : token.startsWith('`')
            ? lineStyle.copyWith(fontFamily: GoogleFonts.geistMono().fontFamily, backgroundColor: _bgTertiary)
            : lineStyle.copyWith(fontStyle: FontStyle.italic);
        children.add(TextSpan(text: token.substring(0, width), style: lineStyle.merge(faint)));
        children.add(TextSpan(text: inner, style: innerStyle));
        children.add(TextSpan(text: token.substring(token.length - width), style: lineStyle.merge(faint)));
        position = match.end;
      }
      if (position < rest.length) children.add(TextSpan(text: rest.substring(position), style: lineStyle));
      if (index < lines.length - 1) children.add(TextSpan(text: '\n', style: lineStyle));
    }
    return TextSpan(style: base, children: children);
  }
}
