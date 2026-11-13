part of 'main.dart';

// Admin › Backups: targets with their last run, a form per target type, and
// the recent run history. While a run is in progress the tab polls.

class _AdminBackupsTab extends StatefulWidget {
  const _AdminBackupsTab({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminBackupsTab> createState() => _AdminBackupsTabState();
}

class _AdminBackupsTabState extends State<_AdminBackupsTab> with _PageLoad {
  List<Map<String, dynamic>>? _targets;
  List<Map<String, dynamic>> _runs = const <Map<String, dynamic>>[];
  Timer? _poll;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final data = await _controller.client.adminBackups();
      if (!mounted) return;
      final runs = ((data['runs'] as List?) ?? const []).map((e) => (e as Map).cast<String, dynamic>()).toList();
      setState(() {
        _targets = ((data['targets'] as List?) ?? const []).map((e) => (e as Map).cast<String, dynamic>()).toList();
        _runs = runs;
      });
      final running = runs.any((run) => run['status'] == 'running');
      _poll?.cancel();
      if (running) _poll = Timer(const Duration(seconds: 3), _load);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _act(Future<void> Function() action, {String? notice}) async {
    await runAction(_controller, action, notice: notice);
    await _load();
  }

  Future<void> _edit([Map<String, dynamic>? target]) async {
    final body = await showDialog<Map<String, Object?>>(
      context: context,
      builder: (_) => _BackupTargetDialog(target: target),
    );
    if (body == null) return;
    await _act(() => target == null
        ? _controller.client.adminCreateBackupTarget(body)
        : _controller.client.adminUpdateBackupTarget((target['id'] as num).toInt(), body));
  }

  String _typeLabel(String type) {
    switch (type) {
      case 'local':
        return appStrings.backupTypeLocal;
      case 'sftp':
        return appStrings.backupTypeSftp;
      case 's3':
        return appStrings.backupTypeS3;
      default:
        return appStrings.backupTypeWebdav;
    }
  }

  String _scheduleLabel(Map<String, dynamic> target) {
    final hour = '${target['hour']}'.padLeft(2, '0');
    switch (target['schedule']) {
      case 'daily':
        return '${appStrings.backupScheduleDaily} · $hour:00';
      case 'weekly':
        return '${appStrings.backupScheduleWeekly} · ${_weekdayName((target['weekday'] as num).toInt())} $hour:00';
      default:
        return appStrings.backupScheduleManual;
    }
  }

  DateTime? _date(Object? value) {
    final text = value?.toString();
    if (text == null || text.isEmpty) return null;
    return DateTime.tryParse('${text.replaceFirst(' ', 'T')}Z')?.toLocal();
  }

  /// "Backing up… 62 % · 13 GB of 21 GB" once the run knows its total.
  String _runningLabel(Map<String, dynamic> run) {
    final total = (run['bytesTotal'] as num?)?.toInt();
    final done = (run['bytesUploaded'] as num?)?.toInt() ?? 0;
    if (total == null || total <= 0) return appStrings.backupRunning;
    final percent = (done * 100 / total).clamp(0, 100).floor();
    return appStrings.backupRunningProgress(percent, formatBytes(done), formatBytes(total));
  }

  Widget _runStatus(Map<String, dynamic>? run) {
    final locale = currentAppLanguage.code;
    if (run == null) return Text(appStrings.backupNeverRun, style: TextStyle(color: _textMuted, fontSize: 12.5));
    switch (run['status']) {
      case 'running':
        return _StatusPill(label: _runningLabel(run), color: _info);
      // Status is a dot plus words, never colored text.
      // Wraps: on a phone the date and the count don't fit one line.
      case 'ok':
        return _wrappingStatus(
          appStrings.backupLastOk(formatDate(_date(run['finishedAt']), locale), (run['blobsUploaded'] as num?)?.toInt() ?? 0),
          _success,
        );
      default:
        return _wrappingStatus(appStrings.backupLastFailed('${run['error'] ?? ''}'), _danger);
    }
  }

  Widget _wrappingStatus(String label, Color color) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: <Widget>[
      Container(
        width: 7,
        height: 7,
        margin: const EdgeInsets.only(top: 6, right: 7),
        decoration: BoxDecoration(color: color, shape: BoxShape.circle),
      ),
      Expanded(
        child: Text(label, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 13)),
      ),
    ],
  );

  @override
  Widget build(BuildContext context) {
    final targets = _targets;
    final locale = currentAppLanguage.code;
    final names = <int, String>{for (final t in targets ?? const <Map<String, dynamic>>[]) (t['id'] as num).toInt(): '${t['name']}'};
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (targets != null) feedback(),
        _SettingsGroup(
          title: appStrings.backupTargets,
          description: appStrings.backupTargetsDescription,
          trailing: FilledButton.icon(
            onPressed: busy ? null : () => unawaited(_edit()),
            icon: const Icon(Icons.add_rounded, size: 18),
            label: Text(appStrings.backupAddTarget),
          ),
          children: <Widget>[
            if (targets == null && pageError == null) const _LoadingPlaceholder(),
            if (targets == null && pageError != null)
              _SettingsBlock(
                child: _LoadError(
                  message: pageError!,
                  onRetry: () {
                    setState(() => pageError = null);
                    unawaited(_load());
                  },
                ),
              ),
            if (targets != null && targets.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.backupNoTargets)),
            for (final target in targets ?? const <Map<String, dynamic>>[])
              _SettingsBlock(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        Container(
                          width: 36,
                          height: 36,
                          decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(10)),
                          child: Icon(Icons.backup_outlined, size: 19, color: _textSecondary),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Wrap(
                                spacing: 8,
                                crossAxisAlignment: WrapCrossAlignment.center,
                                children: <Widget>[
                                  Text('${target['name']}', style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700, fontSize: 14.5)),
                                  _SettingsTag(label: _typeLabel('${target['type']}')),
                                ],
                              ),
                              const SizedBox(height: 3),
                              Text(
                                '${_scheduleLabel(target)} · ${appStrings.backupRetention}: ${target['retention']}',
                                style: TextStyle(color: _textSecondary, fontSize: 12.5),
                              ),
                              const SizedBox(height: 4),
                              _runStatus((target['lastRun'] as Map?)?.cast<String, dynamic>()),
                            ],
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: <Widget>[
                        FilledButton.icon(
                          onPressed: busy ? null : () => unawaited(_act(() => _controller.client.adminRunBackup((target['id'] as num).toInt()), notice: appStrings.backupStarted)),
                          icon: const Icon(Icons.play_arrow_rounded, size: 18),
                          label: Text(appStrings.backupRunNow),
                        ),
                        OutlinedButton(
                          onPressed: busy ? null : () => unawaited(_act(() => _controller.client.adminTestBackupTarget((target['id'] as num).toInt()), notice: appStrings.backupTestOk)),
                          child: Text(appStrings.backupTest),
                        ),
                        TextButton(onPressed: busy ? null : () => unawaited(_edit(target)), child: Text(appStrings.backupEditTarget)),
                        TextButton(
                          onPressed: busy
                              ? null
                              : () => unawaited(_confirmDelete(
                                  context,
                                  title: '${target['name']}',
                                  message: appStrings.backupDeleteMessage,
                                  onConfirm: () => _act(() => _controller.client.adminDeleteBackupTarget((target['id'] as num).toInt())),
                                )),
                          child: Text(appStrings.delete, style: TextStyle(color: _danger)),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
          ],
        ),
        _InlineNote(message: appStrings.backupRestoreHint, icon: Icons.restore_rounded),
        const SizedBox(height: 28),
        if (_runs.isNotEmpty)
          _SettingsGroup(
            title: appStrings.backupHistory,
            children: <Widget>[
              for (final run in _runs.take(15))
                _SettingsRow(
                  label: '${names[(run['targetId'] as num).toInt()] ?? '#${run['targetId']}'} · ${run['snapshot'] ?? ''}',
                  description: '${formatDate(_date(run['startedAt']), locale)} · ${formatBytes((run['bytesUploaded'] as num?)?.toInt() ?? 0)}',
                  control: _StatusPill(
                    label: switch (run['status']) {
                      'ok' => appStrings.adminBackupRunOk,
                      'running' => _runningLabel(run),
                      _ => appStrings.adminBackupRunFailed,
                    },
                    color: switch (run['status']) {
                      'ok' => _success,
                      'running' => _info,
                      _ => _danger,
                    },
                  ),
                ),
            ],
          ),
      ],
    );
  }
}

String _weekdayName(int day) {
  // DateTime weekdays run Monday=1..Sunday=7; the server uses Sunday=0.
  final date = DateTime(2024, 1, 7 + day);
  return DateFormat.EEEE(currentAppLanguage.code).format(date);
}

/// Add or edit a target: name, type, the type's connection fields, and the
/// schedule. Secrets are never shown again; leaving them empty keeps them.
class _BackupTargetDialog extends StatefulWidget {
  const _BackupTargetDialog({this.target});

  final Map<String, dynamic>? target;

  @override
  State<_BackupTargetDialog> createState() => _BackupTargetDialogState();
}

class _BackupTargetDialogState extends State<_BackupTargetDialog> {
  late String _type = '${widget.target?['type'] ?? 'local'}';
  late String _schedule = '${widget.target?['schedule'] ?? 'daily'}';
  late int _hour = (widget.target?['hour'] as num?)?.toInt() ?? 3;
  late int _weekday = (widget.target?['weekday'] as num?)?.toInt() ?? 0;
  late bool _pathStyle = (widget.target?['config'] as Map?)?['forcePathStyle'] == true;
  final Map<String, TextEditingController> _fields = <String, TextEditingController>{};

  bool get _editing => widget.target != null;
  Map<String, dynamic> get _config => ((widget.target?['config'] as Map?) ?? const {}).cast<String, dynamic>();

  TextEditingController _field(String key, [String? initial]) {
    return _fields.putIfAbsent(key, () => TextEditingController(text: initial ?? '${_config[key] ?? ''}'));
  }

  @override
  void dispose() {
    for (final controller in _fields.values) {
      controller.dispose();
    }
    super.dispose();
  }

  Widget _input(String key, String label, {bool secret = false, String? hint, int lines = 1}) {
    final secretSet = secret && _config['${key}Set'] == true;
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: TextField(
        controller: _field(key, secret ? '' : null),
        obscureText: secret && lines == 1,
        maxLines: lines,
        decoration: InputDecoration(
          labelText: label,
          hintText: secretSet ? appStrings.backupSecretKept : hint,
          isDense: true,
        ),
      ),
    );
  }

  List<Widget> _typeFields() {
    switch (_type) {
      case 'local':
        return <Widget>[_input('path', appStrings.backupPath, hint: appStrings.backupPathHint)];
      case 'sftp':
        return <Widget>[
          _input('host', appStrings.backupHost),
          _input('port', appStrings.backupPort, hint: '22'),
          _input('username', appStrings.backupUsername),
          _input('password', appStrings.backupPassword, secret: true),
          _input('privateKey', appStrings.backupPrivateKey, secret: true, lines: 3),
          _input('path', appStrings.backupPath, hint: '/backups/neocloud'),
        ];
      case 's3':
        return <Widget>[
          _input('endpoint', appStrings.backupEndpoint, hint: 'https://s3.eu-central-1.amazonaws.com'),
          _input('region', appStrings.backupRegion, hint: 'eu-central-1'),
          _input('bucket', appStrings.backupBucket),
          _input('accessKeyId', appStrings.backupAccessKey),
          _input('secretAccessKey', appStrings.backupSecretKey, secret: true),
          _input('prefix', appStrings.backupPrefix, hint: 'neocloud'),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            value: _pathStyle,
            onChanged: (value) => setState(() => _pathStyle = value),
            title: Text(appStrings.backupPathStyle, style: const TextStyle(fontSize: 13.5)),
          ),
        ];
      default:
        return <Widget>[
          _input('url', appStrings.backupUrl, hint: 'https://cloud.example.com/remote.php/dav/files/me/NeoCloud'),
          _input('username', appStrings.backupUsername),
          _input('password', appStrings.backupPassword, secret: true),
        ];
    }
  }

  Map<String, Object?> _body() {
    final config = <String, Object?>{
      for (final entry in _fields.entries)
        if (entry.key != 'name' && entry.key != 'retention' && entry.value.text.trim().isNotEmpty) entry.key: entry.value.text.trim(),
      if (_type == 's3') 'forcePathStyle': _pathStyle,
    };
    return <String, Object?>{
      'name': _field('name', '${widget.target?['name'] ?? ''}').text.trim(),
      if (!_editing) 'type': _type,
      'config': config,
      'schedule': _schedule,
      'hour': _hour,
      'weekday': _weekday,
      'retention': int.tryParse(_field('retention', '${widget.target?['retention'] ?? 7}').text.trim()) ?? 7,
    };
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      backgroundColor: _bgCard,
      title: Text(_editing ? appStrings.backupEditTarget : appStrings.backupAddTarget),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: TextField(
                  controller: _field('name', '${widget.target?['name'] ?? ''}'),
                  decoration: InputDecoration(labelText: appStrings.backupName, isDense: true),
                ),
              ),
              if (!_editing) ...<Widget>[
                Text(appStrings.backupType, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
                const SizedBox(height: 6),
                _SettingsChoice<String>(
                  value: _type,
                  options: <(String, String)>[
                    ('local', appStrings.backupTypeLocal),
                    ('sftp', appStrings.backupTypeSftp),
                    ('s3', 'S3'),
                    ('webdav', appStrings.backupTypeWebdav),
                  ],
                  onChanged: (value) => setState(() {
                    _type = value;
                    for (final key in _fields.keys.where((k) => k != 'name' && k != 'retention').toList()) {
                      _fields.remove(key)?.dispose();
                    }
                  }),
                ),
                const SizedBox(height: 14),
              ],
              ..._typeFields(),
              const SizedBox(height: 4),
              Text(appStrings.backupSchedule, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
              const SizedBox(height: 6),
              _SettingsChoice<String>(
                value: _schedule,
                options: <(String, String)>[
                  ('manual', appStrings.backupScheduleManual),
                  ('daily', appStrings.backupScheduleDaily),
                  ('weekly', appStrings.backupScheduleWeekly),
                ],
                onChanged: (value) => setState(() => _schedule = value),
              ),
              if (_schedule != 'manual') ...<Widget>[
                const SizedBox(height: 12),
                Row(
                  children: <Widget>[
                    Expanded(
                      child: DropdownButtonFormField<int>(
                        initialValue: _hour,
                        decoration: InputDecoration(labelText: appStrings.backupHour, isDense: true),
                        items: <DropdownMenuItem<int>>[
                          for (var h = 0; h < 24; h++) DropdownMenuItem<int>(value: h, child: Text('${'$h'.padLeft(2, '0')}:00')),
                        ],
                        onChanged: (value) => setState(() => _hour = value ?? _hour),
                      ),
                    ),
                    if (_schedule == 'weekly') ...<Widget>[
                      const SizedBox(width: 12),
                      Expanded(
                        child: DropdownButtonFormField<int>(
                          initialValue: _weekday,
                          decoration: InputDecoration(labelText: appStrings.backupWeekday, isDense: true),
                          items: <DropdownMenuItem<int>>[
                            for (var d = 0; d < 7; d++) DropdownMenuItem<int>(value: d, child: Text(_weekdayName(d))),
                          ],
                          onChanged: (value) => setState(() => _weekday = value ?? _weekday),
                        ),
                      ),
                    ],
                  ],
                ),
              ],
              const SizedBox(height: 12),
              TextField(
                controller: _field('retention', '${widget.target?['retention'] ?? 7}'),
                keyboardType: TextInputType.number,
                decoration: InputDecoration(labelText: appStrings.backupRetention, isDense: true),
              ),
            ],
          ),
        ),
      ),
      actions: <Widget>[
        OutlinedButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(onPressed: () => Navigator.of(context).pop(_body()), child: Text(appStrings.save)),
      ],
    );
  }
}
