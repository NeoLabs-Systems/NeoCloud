part of 'main.dart';

// AI help in a note (docs/notes.md): pick an action, or ask a question, and
// the answer comes back to look at first. AI never changes the note: the
// person inserts the answer, replaces the selection with it, takes the
// title or adds the tags.

class _NoteAiChoice {
  const _NoteAiChoice(this.action, this.question);

  final NoteAiAction action;
  final String? question;
}

Future<void> _showNoteAiMenu(
  BuildContext context, {
  required NeoCloudController controller,
  required Note note,
  required String selection,
  required ValueChanged<String> onInsert,
  required ValueChanged<String>? onReplace,
  required ValueChanged<String> onTitle,
  required ValueChanged<List<String>> onTags,
}) async {
  final choice = await showAdaptiveSheet<_NoteAiChoice>(
    context: context,
    builder: (dialogContext) => _AdaptiveDialog(
      icon: Icon(Icons.auto_awesome_outlined, color: _accent),
      title: Text(appStrings.notesAiTitle),
      content: SizedBox(width: 400, child: _NoteAiMenu(hasSelection: selection.trim().isNotEmpty, onPick: (choice) => Navigator.of(dialogContext).pop(choice))),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel))],
    ),
  );
  if (choice == null || !context.mounted) return;
  await showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _NoteAiAnswerDialog(
      controller: controller,
      note: note,
      choice: choice,
      selection: choice.action.onSelection ? selection : '',
      onInsert: onInsert,
      onReplace: choice.action.onSelection ? onReplace : null,
      onTitle: onTitle,
      onTags: onTags,
    ),
  );
}

class _NoteAiMenu extends StatefulWidget {
  const _NoteAiMenu({required this.hasSelection, required this.onPick});

  final bool hasSelection;
  final ValueChanged<_NoteAiChoice> onPick;

  @override
  State<_NoteAiMenu> createState() => _NoteAiMenuState();
}

class _NoteAiMenuState extends State<_NoteAiMenu> {
  final TextEditingController _question = TextEditingController();

  @override
  void dispose() {
    _question.dispose();
    super.dispose();
  }

  Widget _item(IconData icon, String label, NoteAiAction action) => ListTile(
    dense: true,
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(AppRadius.control)),
    leading: Icon(icon, size: 20, color: _accent),
    title: Text(label, style: TextStyle(fontSize: 14, color: _textPrimary)),
    onTap: () => widget.onPick(_NoteAiChoice(action, null)),
  );

  void _ask() {
    final question = _question.text.trim();
    if (question.isNotEmpty) widget.onPick(_NoteAiChoice(NoteAiAction.ask, question));
  }

  @override
  Widget build(BuildContext context) {
    final heading = TextStyle(fontSize: 12, color: _textSecondary, fontWeight: FontWeight.w600);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(padding: const EdgeInsets.fromLTRB(4, 0, 4, 4), child: Text(widget.hasSelection ? appStrings.notesAiOnSelection : appStrings.notesAiOnNoteText, style: heading)),
        _item(Icons.auto_fix_high_outlined, appStrings.notesAiImprove, NoteAiAction.improve),
        _item(Icons.compress_rounded, appStrings.notesAiShorten, NoteAiAction.shorten),
        _item(Icons.east_rounded, appStrings.notesAiContinue, NoteAiAction.continueWriting),
        Divider(height: 16, color: _border),
        Padding(padding: const EdgeInsets.fromLTRB(4, 0, 4, 4), child: Text(appStrings.notesAiOnWholeNote, style: heading)),
        _item(Icons.notes_rounded, appStrings.notesAiSummarize, NoteAiAction.summarize),
        _item(Icons.checklist_rounded, appStrings.notesAiActionItems, NoteAiAction.actionItems),
        _item(Icons.title_rounded, appStrings.notesAiSuggestTitle, NoteAiAction.title),
        _item(Icons.sell_outlined, appStrings.notesAiSuggestTags, NoteAiAction.tags),
        const SizedBox(height: 10),
        TextField(
          controller: _question,
          textInputAction: TextInputAction.send,
          onSubmitted: (_) => _ask(),
          decoration: InputDecoration(
            isDense: true,
            hintText: appStrings.notesAiAskHint,
            prefixIcon: Icon(Icons.auto_awesome_outlined, size: 18, color: _accent),
            suffixIcon: IconButton(tooltip: appStrings.notesAiAsk, onPressed: _ask, icon: const Icon(Icons.send_rounded, size: 18)),
          ),
        ),
      ],
    );
  }
}

/// Runs the action and shows its answer with what can be done with it.
class _NoteAiAnswerDialog extends StatefulWidget {
  const _NoteAiAnswerDialog({
    required this.controller,
    required this.note,
    required this.choice,
    required this.selection,
    required this.onInsert,
    required this.onReplace,
    required this.onTitle,
    required this.onTags,
  });

  final NeoCloudController controller;
  final Note note;
  final _NoteAiChoice choice;
  final String selection;
  final ValueChanged<String> onInsert;
  final ValueChanged<String>? onReplace;
  final ValueChanged<String> onTitle;
  final ValueChanged<List<String>> onTags;

  @override
  State<_NoteAiAnswerDialog> createState() => _NoteAiAnswerDialogState();
}

class _NoteAiAnswerDialogState extends State<_NoteAiAnswerDialog> {
  NoteAiAnswer? _answer;
  String? _error;
  final Set<String> _pickedTags = <String>{};

  @override
  void initState() {
    super.initState();
    unawaited(_run());
  }

  Future<void> _run() async {
    setState(() {
      _answer = null;
      _error = null;
    });
    try {
      final answer = await widget.controller.client.noteAi(widget.note.id, widget.choice.action, selection: widget.selection, question: widget.choice.question);
      if (!mounted) return;
      setState(() {
        _answer = answer;
        _pickedTags
          ..clear()
          ..addAll((answer.tags ?? const <String>[]).where((tag) => !widget.note.tags.contains(tag)));
      });
    } catch (error) {
      if (mounted) setState(() => _error = _aiFriendlyError(widget.controller, error));
    }
  }

  String get _heading {
    switch (widget.choice.action) {
      case NoteAiAction.summarize:
        return appStrings.notesAiSummarize;
      case NoteAiAction.actionItems:
        return appStrings.notesAiActionItems;
      case NoteAiAction.improve:
        return appStrings.notesAiImprove;
      case NoteAiAction.shorten:
        return appStrings.notesAiShorten;
      case NoteAiAction.continueWriting:
        return appStrings.notesAiContinue;
      case NoteAiAction.ask:
        return widget.choice.question ?? appStrings.notesAiAsk;
      case NoteAiAction.title:
        return appStrings.notesAiSuggestTitle;
      case NoteAiAction.tags:
        return appStrings.notesAiSuggestTags;
    }
  }

  void _done(VoidCallback apply) {
    apply();
    Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final answer = _answer;
    final text = answer?.text ?? '';
    final actions = <Widget>[TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close))];
    if (answer != null) {
      actions.insert(0, TextButton(onPressed: _run, child: Text(appStrings.notesAiTryAgain)));
      if (answer.title != null) actions.add(FilledButton(onPressed: () => _done(() => widget.onTitle(answer.title!)), child: Text(appStrings.notesUseTitle)));
      if (answer.tags != null && answer.tags!.isNotEmpty) {
        actions.add(FilledButton(onPressed: _pickedTags.isEmpty ? null : () => _done(() => widget.onTags(_pickedTags.toList())), child: Text(appStrings.notesAiAddTags)));
      }
      if (text.isNotEmpty) {
        actions.add(TextButton(
          onPressed: () async {
            await Clipboard.setData(ClipboardData(text: text));
            if (context.mounted) ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.copied)));
          },
          child: Text(appStrings.copy),
        ));
        if (widget.onReplace != null) actions.add(OutlinedButton(onPressed: () => _done(() => widget.onReplace!(text)), child: Text(appStrings.notesAiReplace)));
        actions.add(FilledButton(onPressed: () => _done(() => widget.onInsert(text)), child: Text(appStrings.notesAiInsert)));
      }
    }
    return _AdaptiveDialog(
      icon: Icon(Icons.auto_awesome_outlined, color: _accent),
      title: Text(_heading, maxLines: 2, overflow: TextOverflow.ellipsis),
      content: SizedBox(width: 520, child: _content(answer)),
      actions: actions,
    );
  }

  Widget _content(NoteAiAnswer? answer) {
    if (_error != null) return Text(_error!, style: TextStyle(color: _danger, height: 1.45));
    if (answer == null) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 24),
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: <Widget>[
            const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 12),
            Text(appStrings.notesAiWorking, style: TextStyle(color: _textSecondary)),
          ],
        ),
      );
    }
    final footer = answer.model == null
        ? const SizedBox.shrink()
        : Padding(padding: const EdgeInsets.only(top: 12), child: Text(appStrings.notesAiBy(answer.model!), style: TextStyle(fontSize: 12, color: _textMuted)));
    if (answer.title != null) {
      return Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[Text(answer.title!, style: _displayTitleStyle(20)), footer],
      );
    }
    if (answer.tags != null) {
      if (answer.tags!.isEmpty) return Text(appStrings.notesAiNoTags, style: TextStyle(color: _textSecondary));
      return Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              for (final tag in answer.tags!)
                FilterChip(
                  label: Text('#$tag'),
                  selected: _pickedTags.contains(tag) || widget.note.tags.contains(tag),
                  onSelected: widget.note.tags.contains(tag) ? null : (on) => setState(() => on ? _pickedTags.add(tag) : _pickedTags.remove(tag)),
                ),
            ],
          ),
          footer,
        ],
      );
    }
    final text = answer.text ?? '';
    if (text.trim().isEmpty) return Text(appStrings.notesAiNothing, style: TextStyle(color: _textSecondary));
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Container(
          constraints: const BoxConstraints(maxHeight: 360),
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(color: _bgPrimary, borderRadius: BorderRadius.circular(AppRadius.control), border: Border.all(color: _border)),
          child: SingleChildScrollView(child: _NoteReadView(body: text, onToggleTask: (_) {})),
        ),
        footer,
      ],
    );
  }
}
