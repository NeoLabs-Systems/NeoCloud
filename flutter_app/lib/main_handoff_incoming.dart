part of 'main.dart';

// The receiving side: an offer from another device of the account asks
// "Accept or decline?". The web app also shows a browser notification when
// the person allowed it. The phone shows the sheet while the app is open.

class _HandoffHost extends StatefulWidget {
  const _HandoffHost({required this.controller, required this.child});

  final NeoCloudController controller;
  final Widget child;

  @override
  State<_HandoffHost> createState() => _HandoffHostState();
}

class _HandoffHostState extends State<_HandoffHost> {
  final BrowserNotice _notice = createBrowserNotice();
  final List<HandoffOffer> _queue = <HandoffOffer>[];
  bool _asking = false;

  HandoffController get _handoff => widget.controller.handoff;

  @override
  void initState() {
    super.initState();
    _handoff.onIncoming = _arrived;
  }

  @override
  void dispose() {
    if (_handoff.onIncoming == _arrived) _handoff.onIncoming = null;
    super.dispose();
  }

  void _arrived(HandoffOffer offer) {
    if (_handoff.notifyInBrowser && _notice.granted) {
      _notice.show(
        appStrings.handoffIncomingTitle(offer.fromName, offer.files.length),
        offer.files.map((file) => file.name).take(3).join(', '),
        tag: offer.id,
        onClick: () {},
      );
    }
    _queue.add(offer);
    unawaited(_askNext());
  }

  Future<void> _askNext() async {
    if (_asking || _queue.isEmpty) return;
    final navigatorContext = _navigatorKey.currentContext;
    if (navigatorContext == null) return;
    _asking = true;
    final messenger = ScaffoldMessenger.maybeOf(navigatorContext);
    final offer = _queue.removeAt(0);
    try {
      final accepted = await showAdaptiveSheet<bool>(
        context: navigatorContext,
        dismissible: false,
        builder: (dialogContext) => _IncomingDialog(offer: offer),
      );
      if (accepted == true) {
        await _receive(messenger, offer);
      } else {
        await widget.controller.client
            .declineHandoff(offer.id)
            .catchError((Object _) {});
      }
    } finally {
      _handoff.answered(offer);
      _asking = false;
      unawaited(_askNext());
    }
  }

  /// Accepts and saves. A browser downloads one file directly and several as
  /// one ZIP (browsers ask before a page starts many downloads); a phone saves
  /// each file through the system's save dialog. The server counts the send
  /// delivered when every byte was served.
  Future<void> _receive(
    ScaffoldMessengerState? messenger,
    HandoffOffer offer,
  ) async {
    final client = widget.controller.client;
    try {
      await client.acceptHandoff(offer.id);
      if (kIsWeb) {
        if (offer.files.length == 1) {
          await startDownload(
            client.handoffFileUri(offer.id, offer.files.first.id),
            fileName: offer.files.first.name,
          );
        } else {
          await startDownload(
            client.handoffZipUri(offer.id),
            fileName: 'NeoCloud files.zip',
          );
        }
      } else {
        for (final file in offer.files) {
          await startDownload(
            client.handoffFileUri(offer.id, file.id),
            fileName: file.name,
            headers: client.handoffDownloadHeaders,
          );
        }
        await client.completeHandoff(offer.id);
        messenger?.showSnackBar(
          SnackBar(content: Text(appStrings.handoffSaved)),
        );
      }
    } catch (_) {
      messenger?.showSnackBar(
        SnackBar(content: Text(appStrings.handoffSaveFailed)),
      );
    }
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

class _IncomingDialog extends StatelessWidget {
  const _IncomingDialog({required this.offer});

  final HandoffOffer offer;

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      icon: Icon(_platformIcon(offer.fromPlatform)),
      title: Text(
        appStrings.handoffIncomingTitle(offer.fromName, offer.files.length),
      ),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          for (final file in offer.files.take(6))
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Text(
                '${file.name} · ${formatBytes(file.size)}',
                overflow: TextOverflow.ellipsis,
              ),
            ),
          if (offer.files.length > 6)
            Text(
              '+${offer.files.length - 6}',
              style: TextStyle(color: _textSecondary),
            ),
        ],
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: Text(appStrings.handoffDecline),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(appStrings.handoffAccept),
        ),
      ],
    );
  }
}
