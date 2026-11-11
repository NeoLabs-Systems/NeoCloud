part of 'main.dart';

// The import jobs under the form: live progress while one is moving (a poll
// every two seconds), counts, speed, errors and the controls.

const Duration _importPollInterval = Duration(seconds: 2);

extension _ImportStatusInfo on ImportStatus {
  String get label {
    switch (this) {
      case ImportStatus.queued:
        return appStrings.importStatusQueued;
      case ImportStatus.scanning:
        return appStrings.importStatusScanning;
      case ImportStatus.running:
        return appStrings.importStatusRunning;
      case ImportStatus.paused:
        return appStrings.importStatusPaused;
      case ImportStatus.done:
        return appStrings.importStatusDone;
      case ImportStatus.failed:
        return appStrings.importStatusFailed;
      case ImportStatus.cancelled:
        return appStrings.importStatusCancelled;
    }
  }

  Color get color {
    switch (this) {
      case ImportStatus.queued:
      case ImportStatus.scanning:
      case ImportStatus.running:
        return _info;
      case ImportStatus.paused:
        return _warning;
      case ImportStatus.done:
        return _success;
      case ImportStatus.failed:
        return _danger;
      case ImportStatus.cancelled:
        return _textMuted;
    }
  }
}

class _ImportJobList extends StatefulWidget {
  const _ImportJobList({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<_ImportJobList> createState() => _ImportJobListState();
}

class _ImportJobListState extends State<_ImportJobList> with _PageLoad {
  List<ImportJob>? _jobs;
  final ImportSpeedMeters _meters = ImportSpeedMeters();
  final Set<String> _expanded = <String>{};
  Timer? _poll;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(reload());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> reload() async {
    try {
      final jobs = await _controller.client.imports();
      if (!mounted) return;
      _meters.sample(jobs, DateTime.now());
      final wasActive = _jobs?.any((job) => job.active) ?? false;
      setState(() => _jobs = jobs);
      final active = jobs.any((job) => job.active);
      if (active) {
        _poll ??= Timer.periodic(_importPollInterval, (_) => unawaited(reload()));
      } else {
        _poll?.cancel();
        _poll = null;
        // Finished files are in the tree now.
        if (wasActive) _controller.notifyFilesChanged();
      }
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _resume(ImportJob job) async {
    try {
      await _controller.client.resumeImport(job.id);
    } on ApiException catch (error) {
      if (error.code != 'PASSWORD_REQUIRED' || !mounted) rethrow;
      final password = await _promptText(
        context,
        title: appStrings.importResume,
        label: appStrings.importAppPassword,
        confirmLabel: appStrings.importResume,
        obscure: true,
      );
      if (password == null || password.isEmpty) return;
      await _controller.client.resumeImport(job.id, password: password);
    }
  }

  Future<void> _act(Future<void> Function() action) => runAction(_controller, () async {
    await action();
    await reload();
  });

  String _sourceLabel(ImportJob job) {
    final host = Uri.tryParse(job.url)?.host ?? job.url;
    return '${job.username}@$host${job.sourcePath == '/' ? '' : job.sourcePath}';
  }

  List<Widget> _actions(ImportJob job) {
    final status = job.status;
    return <Widget>[
      if (job.active) OutlinedButton(onPressed: busy ? null : () => unawaited(_act(() => _controller.client.pauseImport(job.id))), child: Text(appStrings.importPause)),
      if (status == ImportStatus.paused || status == ImportStatus.failed || status == ImportStatus.cancelled)
        OutlinedButton(onPressed: busy ? null : () => unawaited(_act(() => _resume(job))), child: Text(appStrings.importResume)),
      if (!job.finished)
        TextButton(
          onPressed: busy
              ? null
              : () => unawaited(_confirmDelete(
                  context,
                  title: appStrings.importCancelTitle,
                  message: appStrings.importCancelMessage,
                  confirmLabel: appStrings.importCancel,
                  onConfirm: () => _act(() => _controller.client.cancelImport(job.id)),
                )),
          child: Text(appStrings.importCancel),
        ),
      if (status == ImportStatus.done)
        FilledButton.icon(
          onPressed: () => _controller.openFolder(job.targetFolderId),
          icon: const Icon(Icons.folder_open_rounded, size: 18),
          label: Text(appStrings.importOpenFolder),
        ),
      if (job.finished)
        IconButton(
          tooltip: appStrings.importRemove,
          onPressed: busy ? null : () => unawaited(_act(() => _controller.client.deleteImport(job.id))),
          icon: Icon(Icons.close_rounded, size: 18, color: _textMuted),
        ),
    ];
  }

  Widget _errors(ImportJob job) {
    final open = _expanded.contains(job.id);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton.icon(
            onPressed: () => setState(() => open ? _expanded.remove(job.id) : _expanded.add(job.id)),
            icon: Icon(open ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
            label: Text(open ? appStrings.importHideErrors : appStrings.importShowErrors(job.errors.length)),
          ),
        ),
        if (open)
          Container(
            constraints: const BoxConstraints(maxHeight: 220),
            decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.control)),
            child: ListView(
              shrinkWrap: true,
              padding: const EdgeInsets.all(12),
              children: <Widget>[
                for (final error in job.errors)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        SelectableText(error.path, style: GoogleFonts.geistMono(fontSize: 12, color: _textPrimary)),
                        Text(error.message, style: TextStyle(fontSize: 12.5, color: _textSecondary)),
                      ],
                    ),
                  ),
              ],
            ),
          ),
      ],
    );
  }

  Widget _job(ImportJob job) {
    final fraction = importFraction(job);
    final details = <String>[importCountsLine(job)];
    if (job.status == ImportStatus.running) {
      final speed = formatTransferSpeed(_meters.speedOf(job.id));
      if (speed != null) details.add(speed);
      final eta = _meters.etaOf(job.id);
      if (eta != null) details.add(_formatEta(eta));
    }
    final problems = <String>[
      if (job.skippedFiles > 0) appStrings.importSkipped(job.skippedFiles),
      if (job.failedFiles > 0) appStrings.importFailed(job.failedFiles),
    ];
    return _SettingsBlock(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(_sourceLabel(job), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w500, color: _textPrimary)),
                    const SizedBox(height: 2),
                    Text(
                      appStrings.importInto(job.targetPath == '/' || job.targetPath.isEmpty ? appStrings.home : job.targetPath),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 13, color: _textSecondary),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              _StatusPill(label: job.status.label, color: job.status.color),
            ],
          ),
          const SizedBox(height: 12),
          ClipRRect(
            borderRadius: BorderRadius.circular(AppRadius.pill),
            child: LinearProgressIndicator(minHeight: 4, value: job.active ? fraction : (fraction ?? 0)),
          ),
          const SizedBox(height: 8),
          Text(details.join(' · '), style: TextStyle(fontSize: 13, color: _textSecondary)),
          if (problems.isNotEmpty) ...<Widget>[
            const SizedBox(height: 4),
            Text(problems.join(' · '), style: TextStyle(fontSize: 13, color: _textMuted)),
          ],
          if (job.errors.isNotEmpty) _errors(job),
          const SizedBox(height: 10),
          Wrap(spacing: 8, runSpacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: _actions(job)),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final jobs = _jobs;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.importJobsTitle,
          trailing: IconButton(tooltip: appStrings.refresh, onPressed: () => unawaited(reload()), icon: const Icon(Icons.refresh_rounded, size: 20)),
          children: <Widget>[
            if (jobs == null && pageError == null) const _LoadingPlaceholder(),
            if (jobs != null && jobs.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.importJobsEmpty)),
            for (final job in jobs ?? const <ImportJob>[]) _job(job),
          ],
        ),
      ],
    );
  }
}
