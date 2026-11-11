part of 'main.dart';

/// "less than a minute", "about 2 min", "about 3 h".
String _formatEta(Duration eta) {
  final seconds = eta.inSeconds;
  if (seconds < 60) return appStrings.uploadEtaUnderMinute;
  if (seconds < 3600) return appStrings.uploadEtaMinutes((seconds / 60).round());
  return appStrings.uploadEtaHours((seconds / 3600).round());
}

/// The upload list in the corner: one summary line ("12 of 40 · 38 MB/s ·
/// about 2 min"), every file with its progress, pause, cancel and retry;
/// collapses to its header. Also offers to resume what a reload interrupted.
class _UploadPanel extends StatefulWidget {
  const _UploadPanel({required this.controller});

  final NeoCloudController controller;

  @override
  State<_UploadPanel> createState() => _UploadPanelState();
}

class _UploadPanelState extends State<_UploadPanel> {
  bool _expanded = true;
  int _announcedFailures = 0;
  bool _wasActive = false;
  bool _wasOffline = false;

  UploadQueue get _queue => widget.controller.uploads;
  UploadJournal get _journal => widget.controller.uploadJournal;

  @override
  void initState() {
    super.initState();
    _queue.addListener(_announce);
  }

  @override
  void dispose() {
    _queue.removeListener(_announce);
    super.dispose();
  }

  /// Screen readers hear when uploads start, finish, fail or lose the connection.
  void _announce() {
    final active = _queue.activeCount > 0;
    final failures = _queue.failedCount;
    final offline = _queue.waitingForConnection;
    final direction = Directionality.of(context);
    String? message;
    if (active && !_wasActive) {
      message = appStrings.uploadsActive(_queue.activeCount);
    } else if (!active && _wasActive) {
      message = failures > 0 ? appStrings.uploadAnnounceFinishedWithFailures(failures) : appStrings.uploadsDone;
    } else if (failures > _announcedFailures) {
      final failed = _queue.tasks.lastWhere((task) => task.status == UploadStatus.failed);
      message = appStrings.uploadAnnounceFailed(failed.source.name);
    } else if (offline && !_wasOffline) {
      message = appStrings.uploadWaitingForConnection;
    }
    _wasActive = active;
    _announcedFailures = failures;
    _wasOffline = offline;
    if (message != null) unawaited(SemanticsService.announce(message, direction));
  }

  String _statusLabel(UploadTask task) {
    if (task.waitingForConnection) return appStrings.uploadWaitingForConnection;
    if (task.paused && !task.finished) return appStrings.uploadPaused;
    switch (task.status) {
      case UploadStatus.waiting:
        return appStrings.uploadWaiting;
      case UploadStatus.uploading:
        return '${(task.progress * 100).round()} %';
      case UploadStatus.finalizing:
        return appStrings.uploadFinalizing;
      case UploadStatus.done:
        return appStrings.uploadDone;
      case UploadStatus.skipped:
        return appStrings.uploadSkipped;
      case UploadStatus.cancelled:
        return appStrings.cancel;
      case UploadStatus.failed:
        return task.error ?? appStrings.uploadFailed;
    }
  }

  /// "12 of 40 · 38 MB/s · about 2 min", or why nothing moves.
  String _summary() {
    final queue = _queue;
    final count = appStrings.uploadProgressCount(queue.finishedCount, queue.tasks.length);
    if (queue.activeCount == 0) return queue.failedCount > 0 ? appStrings.uploadAnnounceFinishedWithFailures(queue.failedCount) : appStrings.uploadsDone;
    if (queue.waitingForConnection) return '$count · ${appStrings.uploadWaitingForConnection}';
    if (queue.allPaused) return '$count · ${appStrings.uploadPaused}';
    if (queue.tasks.where((task) => !task.finished).every((task) => task.status == UploadStatus.finalizing)) {
      return '$count · ${appStrings.uploadFinalizing}';
    }
    final parts = <String>[count];
    final speed = queue.estimate.bytesPerSecond;
    if (speed != null && speed > 0) parts.add('${formatBytes(speed.round())}/s');
    final eta = queue.estimate.eta;
    if (eta != null) parts.add(_formatEta(eta));
    return parts.join(' · ');
  }

  Future<void> _resumeInterrupted() async {
    final result = await FilePicker.platform.pickFiles(allowMultiple: true, withReadStream: true);
    if (result == null) return;
    _journal.resume(<UploadSource>[
      for (final file in result.files)
        if (file.readStream != null) UploadSource(name: file.name, size: file.size, openRead: () => file.readStream!),
    ]);
  }

  Widget _interruptedBanner() {
    final count = _journal.interrupted.length;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 10, 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          _StatusPill(label: appStrings.uploadInterrupted(count), color: _warning),
          const SizedBox(height: 4),
          Text(appStrings.uploadInterruptedHint, style: TextStyle(color: _textMuted, fontSize: 12)),
          const SizedBox(height: 8),
          Row(
            children: <Widget>[
              FilledButton(onPressed: () => unawaited(_resumeInterrupted()), child: Text(appStrings.uploadResumeReselect)),
              const SizedBox(width: 8),
              TextButton(onPressed: _journal.dismiss, child: Text(appStrings.uploadDismiss)),
            ],
          ),
        ],
      ),
    );
  }

  Widget _taskRow(UploadTask task) {
    final queue = _queue;
    final failed = task.status == UploadStatus.failed;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 6, 6, 6),
      child: Row(
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  task.source.folder.isEmpty ? task.source.name : '${task.source.folder}/${task.source.name}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: _textPrimary, fontSize: 13),
                ),
                const SizedBox(height: 5),
                ClipRRect(
                  borderRadius: BorderRadius.circular(AppRadius.pill),
                  child: LinearProgressIndicator(
                    minHeight: 4,
                    value: (task.status == UploadStatus.uploading && task.sentBytes == 0 && !task.paused) || task.status == UploadStatus.finalizing
                        ? null
                        : (task.finished ? 1 : task.progress),
                    backgroundColor: _bgTertiary,
                    valueColor: AlwaysStoppedAnimation<Color>(failed ? _danger : (task.finished || task.paused ? _textSecondary : _accent)),
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  '${formatBytes(task.source.size)} · ${_statusLabel(task)}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: failed ? _danger : _textMuted, fontSize: 12),
                ),
              ],
            ),
          ),
          if (task.paused && !task.finished)
            IconButton(tooltip: appStrings.uploadResume, onPressed: () => queue.resume(task), icon: const Icon(Icons.play_arrow_rounded, size: 18))
          else if (task.canPause)
            IconButton(tooltip: appStrings.uploadPause, onPressed: () => queue.pause(task), icon: const Icon(Icons.pause_rounded, size: 18)),
          if (failed)
            IconButton(tooltip: appStrings.retry, onPressed: () => queue.retry(task), icon: const Icon(Icons.refresh_rounded, size: 18))
          else if (!task.finished)
            IconButton(tooltip: appStrings.uploadCancel, onPressed: () => queue.cancel(task), icon: const Icon(Icons.close_rounded, size: 18)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: Listenable.merge(<Listenable>[_queue, _journal]),
      builder: (context, _) {
        final queue = _queue;
        final interrupted = _journal.interrupted.isNotEmpty;
        if (!queue.hasTasks && !interrupted) return const SizedBox.shrink();
        final active = queue.activeCount;
        final width = math.min(380.0, MediaQuery.sizeOf(context).width - 40);
        // The panel floats above the Scaffold, so it brings its own Material
        // for the buttons and ink inside it.
        return Material(
          type: MaterialType.transparency,
          child: _GlassSurface(
            width: width,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                if (interrupted) _interruptedBanner(),
                if (interrupted && queue.hasTasks) Divider(height: 1, color: _border),
                if (queue.hasTasks) ...<Widget>[
                  Padding(
                    padding: const EdgeInsets.fromLTRB(16, 8, 6, 8),
                    child: Row(
                      children: <Widget>[
                        if (active > 0 && !queue.allPaused && !queue.waitingForConnection)
                          const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                        else
                          Container(
                            width: 8,
                            height: 8,
                            margin: const EdgeInsets.all(4),
                            decoration: BoxDecoration(
                              shape: BoxShape.circle,
                              color: active == 0 ? (queue.failedCount > 0 ? _danger : _success) : _warning,
                            ),
                          ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Semantics(
                            liveRegion: true,
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: <Widget>[
                                Text(
                                  active > 0 ? appStrings.uploadsActive(active) : appStrings.uploadsTitle,
                                  style: TextStyle(fontWeight: FontWeight.w600, color: _textPrimary, fontSize: 13.5),
                                ),
                                Text(_summary(), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textMuted, fontSize: 12)),
                              ],
                            ),
                          ),
                        ),
                        if (active > 0)
                          queue.allPaused
                              ? IconButton(tooltip: appStrings.uploadResumeAll, onPressed: queue.resumeAll, icon: const Icon(Icons.play_arrow_rounded, size: 20))
                              : IconButton(tooltip: appStrings.uploadPauseAll, onPressed: queue.pauseAll, icon: const Icon(Icons.pause_rounded, size: 20)),
                        IconButton(
                          tooltip: appStrings.details,
                          onPressed: () => setState(() => _expanded = !_expanded),
                          icon: Icon(_expanded ? Icons.expand_more_rounded : Icons.expand_less_rounded, size: 20),
                        ),
                        if (active == 0)
                          IconButton(tooltip: appStrings.uploadClearFinished, onPressed: queue.clearFinished, icon: const Icon(Icons.close_rounded, size: 18)),
                      ],
                    ),
                  ),
                  if (_expanded) ...<Widget>[
                    Divider(height: 1, color: _border),
                    ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 280),
                      child: ListView.builder(
                        shrinkWrap: true,
                        padding: const EdgeInsets.symmetric(vertical: 6),
                        itemCount: queue.tasks.length,
                        itemBuilder: (context, index) => _taskRow(queue.tasks[queue.tasks.length - 1 - index]),
                      ),
                    ),
                  ],
                ],
              ],
            ),
          ),
        );
      },
    );
  }
}
