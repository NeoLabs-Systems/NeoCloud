part of 'main.dart';

// The plain-text editor the viewer switches to for text, Markdown and code:
// a monospace field over the whole preview area. Saving uploads a new
// version (see _FileViewerState._save).

class _TextEditorPane extends StatelessWidget {
  const _TextEditorPane({required this.controller, required this.focusNode, required this.readOnly});

  final TextEditingController controller;
  final FocusNode focusNode;
  final bool readOnly;

  @override
  Widget build(BuildContext context) {
    return Container(
      color: _bgSecondary,
      padding: const EdgeInsets.fromLTRB(20, 16, 20, 16),
      child: TextField(
        controller: controller,
        focusNode: focusNode,
        readOnly: readOnly,
        autofocus: true,
        expands: true,
        maxLines: null,
        keyboardType: TextInputType.multiline,
        textAlignVertical: TextAlignVertical.top,
        autocorrect: false,
        enableSuggestions: false,
        smartDashesType: SmartDashesType.disabled,
        smartQuotesType: SmartQuotesType.disabled,
        style: GoogleFonts.geistMono(fontSize: 13, height: 1.5, color: _textPrimary),
        decoration: const InputDecoration(
          border: InputBorder.none,
          enabledBorder: InputBorder.none,
          focusedBorder: InputBorder.none,
          filled: false,
          isCollapsed: true,
        ),
      ),
    );
  }
}

/// "Discard your changes?" — true when the user lets them go.
Future<bool> _confirmDiscardChanges(BuildContext context) async {
  final discard = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(appStrings.editorDiscardTitle),
      content: SizedBox(
        width: 400,
        child: Text(appStrings.editorDiscardMessage, style: TextStyle(color: _textSecondary, height: 1.45)),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.editorKeepEditing)),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          style: FilledButton.styleFrom(backgroundColor: _danger, foregroundColor: Colors.white),
          child: Text(appStrings.editorDiscard),
        ),
      ],
    ),
  );
  return discard ?? false;
}

/// The file changed on the server while it was being edited: true to save
/// over that version anyway (it stays in the version history).
Future<bool> _confirmOverwrite(BuildContext context, String name) async {
  final overwrite = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(appStrings.editorConflictTitle),
      content: SizedBox(
        width: 420,
        child: Text(appStrings.editorConflictMessage(name), style: TextStyle(color: _textSecondary, height: 1.45)),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(dialogContext).pop(false), child: Text(appStrings.cancel)),
        FilledButton(onPressed: () => Navigator.of(dialogContext).pop(true), child: Text(appStrings.editorOverwrite)),
      ],
    ),
  );
  return overwrite ?? false;
}
