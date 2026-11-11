part of 'main.dart';

// Full-text search in the file list (docs/clients.md §8): the "File
// contents" chip, the match badge and the highlighted snippet.

/// A snippet with the matched terms in gold and heavier type; no fill
/// behind them, so the row stays calm.
class _SnippetText extends StatelessWidget {
  const _SnippetText({required this.snippet, this.fontSize = 12.5});

  final String snippet;
  final double fontSize;

  @override
  Widget build(BuildContext context) {
    final base = TextStyle(color: _textMuted, fontSize: fontSize);
    return Semantics(
      label: plainSnippet(snippet),
      excludeSemantics: true,
      child: Text.rich(
        TextSpan(
          style: base,
          children: <InlineSpan>[
            for (final part in parseSnippet(snippet))
              TextSpan(
                text: part.text,
                style: part.highlighted ? TextStyle(color: _accent, fontWeight: FontWeight.w600) : null,
              ),
          ],
        ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

/// "Name", "Content" or both: why a result matched.
class _MatchBadge extends StatelessWidget {
  const _MatchBadge({required this.match});

  final String match;

  String get _label {
    switch (match) {
      case 'content':
        return appStrings.searchMatchContent;
      case 'both':
        return appStrings.searchMatchBoth;
      default:
        return appStrings.searchMatchName;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.tag)),
      child: Text(_label, style: TextStyle(color: _textSecondary, fontSize: 11, fontWeight: FontWeight.w500)),
    );
  }
}

/// Searches inside files too; only shown when the server indexes them.
class _ContentSearchChip extends StatelessWidget {
  const _ContentSearchChip({required this.selected, required this.onChanged});

  final bool selected;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    return FilterChip(
      selected: selected,
      avatar: Icon(Icons.manage_search_rounded, size: 16, color: selected ? _textPrimary : _textMuted),
      label: Text(appStrings.searchFileContents),
      tooltip: appStrings.searchFileContentsHint,
      onSelected: onChanged,
    );
  }
}
