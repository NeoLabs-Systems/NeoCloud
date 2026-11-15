part of 'main.dart';

// Editing an office document (docs/office-editing.md): a full-screen page
// with the editor in it. The editor shows its own saving state. In a browser
// it is an iframe; on a phone it opens in the system browser, as the app
// cannot embed a web page it also has to hand the sign-in token to.

Future<void> showOfficeEditor(BuildContext context, NeoCloudController controller, FileNode node) async {
  await showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (_) => Dialog.fullscreen(
      backgroundColor: _bgPrimary,
      child: _OfficeEditorPage(controller: controller, node: node),
    ),
  );
  // Whatever the editor saved changes sizes, dates and versions.
  controller.notifyFilesChanged();
}

class _OfficeEditorPage extends StatefulWidget {
  const _OfficeEditorPage({required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  State<_OfficeEditorPage> createState() => _OfficeEditorPageState();
}

class _OfficeEditorPageState extends State<_OfficeEditorPage> {
  OfficeSession? _session;
  String? _error;
  bool _openedExternally = false;

  @override
  void initState() {
    super.initState();
    unawaited(_start());
  }

  Future<void> _start() async {
    setState(() => _error = null);
    try {
      final session = await widget.controller.office.startSession(widget.node);
      if (!mounted) return;
      setState(() => _session = session);
      if (!officeEditorEmbeds) await _openExternally(session);
    } catch (error) {
      if (mounted) setState(() => _error = widget.controller.friendlyError(error));
    }
  }

  Future<void> _openExternally(OfficeSession session) async {
    final opened = await launchUrl(session.linkWithToken, mode: LaunchMode.externalApplication);
    if (mounted) setState(() => _openedExternally = opened);
  }

  Widget _body() {
    final session = _session;
    final error = _error;
    if (error != null) {
      return Center(child: _LoadError(message: error, onRetry: () => unawaited(_start())));
    }
    if (session == null) return const _LoadingPlaceholder();
    if (officeEditorEmbeds) {
      return officeEditorFrame(editorUrl: session.editorUrl, accessToken: session.accessToken, accessTokenTtl: session.accessTokenTtl);
    }
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(
              _openedExternally ? appStrings.officeOpenedInBrowser : appStrings.officeOpenInBrowserHint,
              textAlign: TextAlign.center,
              style: TextStyle(color: _textSecondary, height: 1.45),
            ),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: () => unawaited(_openExternally(session)),
              icon: const Icon(Icons.open_in_new_rounded, size: 18),
              label: Text(appStrings.officeOpenInBrowser),
            ),
          ],
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final session = _session;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Container(
          height: 52,
          padding: const EdgeInsets.symmetric(horizontal: 12),
          decoration: BoxDecoration(color: _bgSecondary, border: Border(bottom: BorderSide(color: _border))),
          child: Row(
            children: <Widget>[
              Icon(Icons.description_outlined, size: 20, color: _textSecondary),
              const SizedBox(width: 10),
              Expanded(
                child: Text(widget.node.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w600, color: _textPrimary)),
              ),
              if (session != null && !session.canWrite) ...<Widget>[_StatusPill(label: appStrings.officeViewOnly, color: _warning), const SizedBox(width: 8)],
              TextButton.icon(
                onPressed: () => Navigator.of(context).pop(),
                icon: const Icon(Icons.close_rounded, size: 18),
                label: Text(appStrings.close),
              ),
            ],
          ),
        ),
        Expanded(child: _body()),
      ],
    );
  }
}
