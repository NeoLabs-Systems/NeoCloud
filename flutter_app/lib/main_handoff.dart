part of 'main.dart';

// Send to device: pick (or bring along) the files, pick one of the account's
// online devices, watch it arrive. The same sheet serves the phone's share
// menu, a selection of NeoCloud files and the command palette.

/// How the sheet was left.
enum HandoffOutcome { closed, saveToCloud }

/// Opens the sheet. [files] are local files to send, [nodes] files already in
/// NeoCloud; with neither, the sheet offers "Choose files". [offerSaveToCloud]
/// adds the share menu's second action.
Future<HandoffOutcome> showSendToDevice(
  BuildContext context,
  NeoCloudController controller, {
  List<UploadSource> files = const <UploadSource>[],
  List<FileNode> nodes = const <FileNode>[],
  bool offerSaveToCloud = false,
}) async {
  final outcome = await showAdaptiveSheet<HandoffOutcome>(
    context: context,
    builder: (_) => _SendToDeviceDialog(
      controller: controller,
      files: files,
      nodes: nodes,
      offerSaveToCloud: offerSaveToCloud,
    ),
  );
  // The first time: files can also be received while NeoCloud is closed. Not
  // on top of the folder picker that "Save to my NeoCloud" opens next.
  if (outcome != HandoffOutcome.saveToCloud && context.mounted) {
    unawaited(offerBackgroundReceiving(context, controller));
  }
  return outcome ?? HandoffOutcome.closed;
}

class _SendToDeviceDialog extends StatefulWidget {
  const _SendToDeviceDialog({
    required this.controller,
    required this.files,
    required this.nodes,
    required this.offerSaveToCloud,
  });

  final NeoCloudController controller;
  final List<UploadSource> files;
  final List<FileNode> nodes;
  final bool offerSaveToCloud;

  @override
  State<_SendToDeviceDialog> createState() => _SendToDeviceDialogState();
}

class _SendToDeviceDialogState extends State<_SendToDeviceDialog> {
  late List<UploadSource> _files = widget.files;
  HandoffDevice? _target;
  HandoffOffer? _offer;
  int _sent = 0;
  int _total = 0;
  bool _sending = false;
  String? _error;

  HandoffController get _handoff => widget.controller.handoff;

  @override
  void initState() {
    super.initState();
    unawaited(_handoff.refreshDevices());
  }

  int get _count => _files.length + widget.nodes.length;

  int get _bytes =>
      _files.fold<int>(0, (sum, file) => sum + file.size) +
      widget.nodes.fold<int>(0, (sum, node) => sum + node.size);

  Future<void> _chooseFiles() async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: true,
      withReadStream: true,
    );
    if (result == null) return;
    setState(
      () => _files = <UploadSource>[
        for (final file in result.files)
          if (file.readStream != null)
            UploadSource(
              name: file.name,
              size: file.size,
              openRead: () => file.readStream!,
            ),
      ],
    );
  }

  Future<void> _send(HandoffDevice target) async {
    setState(() {
      _target = target;
      _sending = true;
      _error = null;
      _sent = 0;
      _total = _bytes;
    });
    try {
      final offer = await _handoff.send(
        target,
        files: _files,
        nodeIds: <String>[for (final node in widget.nodes) node.id],
        progress: (sent, total) {
          if (mounted) setState(() => _sent = sent);
        },
      );
      if (mounted) setState(() => _offer = offer);
    } catch (error) {
      if (mounted) setState(() => _error = _sendError(error));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  String _sendError(Object error) {
    if (error is ApiException) {
      if (error.code == 'SEND_TOO_LARGE' || error.code == 'TOO_MANY_FILES') {
        return appStrings.handoffTooLarge;
      }
      if (error.code == 'DEVICE_NOT_FOUND' || error.code == 'DEVICE_NOT_RECEIVING') {
        return appStrings.handoffDeviceGone;
      }
    }
    return appStrings.handoffFailed;
  }

  /// The send as the server last reported it (it changes while the other device answers).
  HandoffOffer? _current() {
    final offer = _offer;
    if (offer == null) return null;
    for (final known in _handoff.sent) {
      if (known.id == offer.id) return known;
    }
    return offer;
  }

  String _status(HandoffOffer offer) {
    final name = offer.toName;
    switch (offer.status) {
      case HandoffStatus.delivered:
        return appStrings.handoffDelivered(name);
      case HandoffStatus.declined:
        return appStrings.handoffDeclined(name);
      case HandoffStatus.cancelled:
        return appStrings.handoffCancelled;
      case HandoffStatus.expired:
        return appStrings.handoffExpired;
      case HandoffStatus.uploading:
      case HandoffStatus.pending:
      case HandoffStatus.accepted:
        return appStrings.handoffWaiting(name);
    }
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: !_sending,
      child: ListenableBuilder(
        listenable: _handoff,
        builder: (context, _) {
          final offer = _current();
          return _AdaptiveDialog(
            icon: const Icon(Icons.send_to_mobile_rounded),
            title: Text(appStrings.handoffTitle),
            content: SizedBox(
              width: 420,
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  if (_count > 0)
                    Text(
                      appStrings.handoffSummary(_count, formatBytes(_bytes)),
                      style: TextStyle(color: _textSecondary),
                    ),
                  if (offer != null)
                    _sentView(offer)
                  else if (_sending)
                    _progressView()
                  else
                    ..._chooser(),
                  if (_error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 12),
                      child: Text(_error!, style: TextStyle(color: _danger)),
                    ),
                ],
              ),
            ),
            actions: <Widget>[
              if (offer == null && widget.offerSaveToCloud && !_sending)
                OutlinedButton(
                  onPressed: () =>
                      Navigator.of(context).pop(HandoffOutcome.saveToCloud),
                  child: Text(appStrings.handoffSaveToCloud),
                ),
              if (_sending)
                TextButton(
                  onPressed: () => unawaited(_cancel()),
                  child: Text(appStrings.handoffCancelSend),
                )
              else
                TextButton(
                  onPressed: () =>
                      Navigator.of(context).pop(HandoffOutcome.closed),
                  child: Text(appStrings.close),
                ),
            ],
          );
        },
      ),
    );
  }

  Future<void> _cancel() async {
    final offer = _offer;
    if (offer != null) await _handoff.cancel(offer);
    if (mounted) Navigator.of(context).pop(HandoffOutcome.closed);
  }

  List<Widget> _chooser() {
    final targets = _handoff.targets;
    return <Widget>[
      if (_count == 0)
        Padding(
          padding: const EdgeInsets.only(top: 12),
          child: OutlinedButton.icon(
            onPressed: () => unawaited(_chooseFiles()),
            icon: const Icon(Icons.attach_file_rounded),
            label: Text(appStrings.handoffChooseFiles),
          ),
        ),
      if (_count > 0) ...<Widget>[
        Padding(
          padding: const EdgeInsets.only(top: 16, bottom: 4),
          child: Text(
            appStrings.handoffPickDevice,
            style: const TextStyle(fontWeight: FontWeight.w700),
          ),
        ),
        if (targets.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Text(
              appStrings.handoffNoDevices,
              style: TextStyle(color: _textSecondary, height: 1.4),
            ),
          )
        else
          for (final device in targets)
            ListTile(
              contentPadding: EdgeInsets.zero,
              leading: Icon(_platformIcon(device.platform), color: _accent),
              title: Text(device.name),
              subtitle: Text(_kindLabel(device.kind)),
              onTap: () => unawaited(_send(device)),
            ),
      ],
    ];
  }

  Widget _progressView() {
    final fraction = _total == 0 ? null : (_sent / _total).clamp(0.0, 1.0);
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(appStrings.handoffSending(_target?.name ?? '')),
          const SizedBox(height: 8),
          LinearProgressIndicator(value: fraction),
        ],
      ),
    );
  }

  Widget _sentView(HandoffOffer offer) {
    final done = offer.finished;
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Row(
        children: <Widget>[
          if (offer.status == HandoffStatus.delivered)
            Icon(Icons.check_circle_rounded, color: _accent)
          else if (done)
            Icon(Icons.info_outline_rounded, color: _textSecondary)
          else
            const SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          const SizedBox(width: 12),
          Expanded(child: Text(_status(offer))),
        ],
      ),
    );
  }
}

String _kindLabel(String kind) {
  switch (kind) {
    case 'phone':
      return appStrings.handoffKindPhone;
    case 'desktop':
      return appStrings.handoffKindDesktop;
    case 'browser':
      return appStrings.handoffKindBrowser;
  }
  return appStrings.handoffKindApp;
}
