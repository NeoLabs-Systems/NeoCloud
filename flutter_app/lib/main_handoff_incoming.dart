part of 'main.dart';

// The receiving side: an offer from another device of the account asks
// "Accept or decline?". The web app also shows a browser notification when
// the person allowed it. The phone shows the sheet while the app is open; with
// the app closed or in the background, Android's receiving service (if the
// person turned it on) asks in a notification instead (src/handoff/background).

class _HandoffHost extends StatefulWidget {
  const _HandoffHost({required this.controller, required this.child});

  final NeoCloudController controller;
  final Widget child;

  @override
  State<_HandoffHost> createState() => _HandoffHostState();
}

class _HandoffHostState extends State<_HandoffHost> with WidgetsBindingObserver {
  final BrowserNotice _notice = createBrowserNotice();
  final List<HandoffOffer> _queue = <HandoffOffer>[];
  bool _asking = false;

  /// The offer whose dialog is up.
  String? _showing;

  HandoffController get _handoff => widget.controller.handoff;

  @override
  void initState() {
    super.initState();
    _handoff.onIncoming = _arrived;
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    if (_handoff.onIncoming == _arrived) _handoff.onIncoming = null;
    super.dispose();
  }

  /// The app is on screen (unknown counts: tests, desktop).
  bool get _visible => (WidgetsBinding.instance.lifecycleState ?? AppLifecycleState.resumed) == AppLifecycleState.resumed;

  /// Coming back, what still waits is asked about here: whatever a
  /// notification's button answered meanwhile is gone after the refresh.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) unawaited(_askWaiting());
  }

  Future<void> _askWaiting() async {
    await _handoff.refresh();
    for (final offer in _handoff.incoming) {
      if (offer.id != _showing && !_queue.any((queued) => queued.id == offer.id)) _queue.add(offer);
    }
    unawaited(_askNext());
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
    // In the background the notification asks; it stays in the controller's
    // list and is asked here once the app is back.
    if (!_visible) return;
    _queue.add(offer);
    unawaited(_askNext());
  }

  Future<void> _askNext() async {
    if (_asking || !_visible) return;
    // Offers answered elsewhere (a notification's button) are not asked again.
    _queue.removeWhere((queued) => !_handoff.incoming.any((known) => known.id == queued.id));
    if (_queue.isEmpty) return;
    final navigatorContext = _navigatorKey.currentContext;
    if (navigatorContext == null) return;
    _asking = true;
    final messenger = ScaffoldMessenger.maybeOf(navigatorContext);
    final offer = _queue.removeAt(0);
    _showing = offer.id;
    _handoff.background.dismissOffer(offer.id);
    try {
      final accepted = await showAdaptiveSheet<bool>(
        context: navigatorContext,
        dismissible: false,
        builder: (dialogContext) => _IncomingDialog(offer: offer),
      );
      if (accepted == true) {
        await _receive(messenger, offer);
        // The first time files are accepted: they could also arrive with the app closed.
        final after = _navigatorKey.currentContext;
        if (after != null && after.mounted) await offerBackgroundReceiving(after, widget.controller);
      } else {
        await widget.controller.client
            .declineHandoff(offer.id)
            .catchError((Object _) {});
      }
    } finally {
      _handoff.answered(offer);
      _showing = null;
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
