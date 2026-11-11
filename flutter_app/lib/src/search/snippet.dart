/// Full-text search snippets (docs/clients.md §8): the server marks each
/// matched term with U+0002 before and U+0003 after it.
library;

const String _start = '\u0002';
const String _end = '\u0003';

/// One run of a snippet; [highlighted] runs are the matched terms.
class SnippetPart {
  const SnippetPart(this.text, {this.highlighted = false});

  final String text;
  final bool highlighted;

  @override
  bool operator ==(Object other) => other is SnippetPart && other.text == text && other.highlighted == highlighted;

  @override
  int get hashCode => Object.hash(text, highlighted);

  @override
  String toString() => highlighted ? '[$text]' : text;
}

/// Splits [snippet] into plain and highlighted runs. Whitespace is folded
/// to single spaces; an unclosed mark runs to the end; stray end marks and
/// empty runs are dropped.
List<SnippetPart> parseSnippet(String snippet) {
  final parts = <SnippetPart>[];
  final buffer = StringBuffer();
  var highlighted = false;

  void flush() {
    if (buffer.isEmpty) return;
    final text = buffer.toString();
    buffer.clear();
    final last = parts.isEmpty ? null : parts.last;
    if (last != null && last.highlighted == highlighted) {
      parts[parts.length - 1] = SnippetPart(last.text + text, highlighted: highlighted);
    } else {
      parts.add(SnippetPart(text, highlighted: highlighted));
    }
  }

  final folded = snippet.replaceAll(RegExp(r'\s+'), ' ');
  for (final char in folded.split('')) {
    if (char == _start) {
      flush();
      highlighted = true;
    } else if (char == _end) {
      flush();
      highlighted = false;
    } else {
      buffer.write(char);
    }
  }
  flush();
  return parts;
}

/// The snippet without its marks, for screen readers and tooltips.
String plainSnippet(String snippet) => parseSnippet(snippet).map((part) => part.text).join();
