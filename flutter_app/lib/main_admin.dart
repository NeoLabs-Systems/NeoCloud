part of 'main.dart';

// The admin page, shaped like NeoAgent's AdminPanel: page title, a row of tab
// chips, then section cards for the open tab.

enum _AdminTab { users, storage, backups, access, server }

extension _AdminTabInfo on _AdminTab {
  String get label {
    switch (this) {
      case _AdminTab.users:
        return appStrings.adminTabUsers;
      case _AdminTab.storage:
        return appStrings.adminTabStorage;
      case _AdminTab.backups:
        return appStrings.adminTabBackups;
      case _AdminTab.access:
        return appStrings.adminTabAccess;
      case _AdminTab.server:
        return appStrings.adminTabServer;
    }
  }

  IconData get icon {
    switch (this) {
      case _AdminTab.users:
        return Icons.group_outlined;
      case _AdminTab.storage:
        return Icons.storage_rounded;
      case _AdminTab.backups:
        return Icons.backup_outlined;
      case _AdminTab.access:
        return Icons.key_outlined;
      case _AdminTab.server:
        return Icons.dns_outlined;
    }
  }
}

class AdminPanel extends StatefulWidget {
  const AdminPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AdminPanel> createState() => _AdminPanelState();
}

class _AdminPanelState extends State<AdminPanel> {
  _AdminTab _tab = _AdminTab.users;

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.adminTitle, subtitle: appStrings.adminSubtitle),
        _AdminSafetyCard(controller: controller, onOpenBackups: () => setState(() => _tab = _AdminTab.backups)),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: _AdminTab.values
              .map((tab) => _AdminTabChip(tab: tab, active: tab == _tab, onTap: () => setState(() => _tab = tab)))
              .toList(growable: false),
        ),
        KeyedSubtree(
          key: ValueKey<_AdminTab>(_tab),
          child: switch (_tab) {
            _AdminTab.users => _AdminUsersTab(controller: controller),
            _AdminTab.storage => _AdminStorageTab(controller: controller),
            _AdminTab.backups => _AdminBackupsTab(controller: controller),
            _AdminTab.access => _AdminAccessTab(controller: controller),
            _AdminTab.server => _AdminServerTab(controller: controller),
          },
        ),
      ],
    );
  }
}

class _AdminTabChip extends StatelessWidget {
  const _AdminTabChip({required this.tab, required this.active, required this.onTap});

  final _AdminTab tab;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final radius = BorderRadius.circular(999);
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
          decoration: BoxDecoration(
            borderRadius: radius,
            color: active ? _accentMuted : _bgTertiary,
            border: Border.all(color: active ? _accent.withValues(alpha: 0.3) : _border),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(tab.icon, size: 15, color: active ? _accent : _textMuted),
              const SizedBox(width: 7),
              Text(
                tab.label,
                style: GoogleFonts.geist(fontSize: 12.5, fontWeight: FontWeight.w600, color: active ? _accentHover : _textSecondary),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _AdminUsersTab extends StatefulWidget {
  const _AdminUsersTab({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminUsersTab> createState() => _AdminUsersTabState();
}

class _AdminUsersTabState extends State<_AdminUsersTab> with _PageLoad {
  final TextEditingController _search = TextEditingController();
  List<AdminUser>? _users;
  int _defaultQuota = 0;
  Timer? _debounce;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _search.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final users = await _controller.client.adminUsers(_search.text.trim());
      final config = await _controller.client.adminStorageConfig();
      if (!mounted) return;
      setState(() {
        _users = users;
        _defaultQuota = (config['defaultQuotaBytes'] as num?)?.toInt() ?? 0;
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _act(Future<void> Function() action) => runAction(_controller, () async {
    await action();
    await _load();
    await _controller.refreshAccount();
  });

  Future<void> _editQuota(AdminUser user) async {
    var mode = user.quotaMode;
    final custom = TextEditingController(text: user.quotaMode == 'custom' ? formatBytes(user.quotaBytes).replaceAll(' ', '') : '');
    final result = await showDialog<(String, String?)>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          backgroundColor: _bgCard,
          title: Text('${appStrings.adminSetQuota} · ${user.username}'),
          content: SizedBox(
            width: 420,
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                _SettingsChoice<String>(
                  value: mode,
                  options: <(String, String)>[
                    ('default', appStrings.adminQuotaModeDefault),
                    ('custom', appStrings.adminQuotaModeCustom),
                    ('unlimited', appStrings.adminQuotaModeUnlimited),
                  ],
                  onChanged: (value) => setDialogState(() => mode = value),
                ),
                if (mode == 'default') ...<Widget>[
                  const SizedBox(height: 12),
                  Text(
                    appStrings.adminQuotaDefault(_defaultQuota < 0 ? appStrings.unlimited : formatBytes(_defaultQuota)),
                    style: TextStyle(color: _textSecondary),
                  ),
                ],
                if (mode == 'custom') ...<Widget>[
                  const SizedBox(height: 14),
                  TextField(
                    controller: custom,
                    autofocus: true,
                    decoration: InputDecoration(labelText: appStrings.adminSetQuota, hintText: appStrings.adminQuotaCustomHint),
                  ),
                ],
              ],
            ),
          ),
          actions: <Widget>[
            OutlinedButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(appStrings.cancel)),
            FilledButton(
              onPressed: () => Navigator.of(dialogContext).pop((mode, mode == 'custom' ? custom.text.trim() : null)),
              child: Text(appStrings.save),
            ),
          ],
        ),
      ),
    );
    custom.dispose();
    if (result == null) return;
    await _act(() => _controller.client.adminSetQuota(user.id, result.$1, bytes: result.$2));
  }

  Future<void> _resetPassword(AdminUser user) async {
    final password = await _promptText(
      context,
      title: appStrings.adminResetPassword,
      label: appStrings.newPassword,
      confirmLabel: appStrings.adminResetPassword,
      obscure: true,
    );
    if (password == null || password.isEmpty) return;
    await runAction(_controller, () => _controller.client.adminResetPassword(user.id, password), notice: appStrings.passwordChanged);
  }

  String _quotaLabel(AdminUser user) {
    if (user.quotaBytes < 0) return appStrings.unlimited;
    final used = formatBytes(user.usedBytes);
    return appStrings.storageUsedOf(used, formatBytes(user.quotaBytes));
  }

  @override
  Widget build(BuildContext context) {
    final users = _users;
    final ownId = _controller.user?.id;
    final locale = currentAppLanguage.code;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SectionCard(
          title: appStrings.adminUsersTitle,
          trailing: IconButton(tooltip: appStrings.refresh, onPressed: _load, icon: const Icon(Icons.refresh_rounded, size: 20)),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              _SearchField(
                controller: _search,
                hintText: appStrings.adminSearchUsers,
                onChanged: (_) {
                  setState(() {});
                  _debounce?.cancel();
                  _debounce = Timer(const Duration(milliseconds: 250), _load);
                },
                onClear: () {
                  _search.clear();
                  _load();
                },
              ),
              const SizedBox(height: 14),
              if (users == null) const _LoadingPlaceholder(),
              for (final user in users ?? const <AdminUser>[]) ...<Widget>[
                Container(
                  padding: const EdgeInsets.all(14),
                  margin: const EdgeInsets.only(bottom: 10),
                  decoration: BoxDecoration(
                    color: _bgSecondary,
                    borderRadius: BorderRadius.circular(AppRadius.tag),
                    border: Border.all(color: _border),
                  ),
                  child: Row(
                    children: <Widget>[
                      CircleAvatar(
                        radius: 17,
                        backgroundColor: user.isAdmin ? _accentMuted : _bgTertiary,
                        child: Text(
                          user.username.characters.first.toUpperCase(),
                          style: TextStyle(color: user.isAdmin ? _accent : _textPrimary, fontWeight: FontWeight.w700, fontSize: 13),
                        ),
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
                                Text(user.username, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700)),
                                if (user.isAdmin) _SettingsTag(label: appStrings.roleAdmin),
                              ],
                            ),
                            const SizedBox(height: 3),
                            Text(
                              '${user.email ?? '—'} · ${appStrings.adminFilesCount(user.fileCount)} · ${formatDate(user.lastLogin, locale)}',
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(color: _textSecondary, fontSize: 12.5),
                            ),
                            const SizedBox(height: 8),
                            Row(
                              children: <Widget>[
                                Expanded(
                                  child: ClipRRect(
                                    borderRadius: BorderRadius.circular(999),
                                    child: LinearProgressIndicator(
                                      minHeight: 5,
                                      value: user.quotaBytes <= 0 ? 0 : (user.usedBytes / user.quotaBytes).clamp(0, 1).toDouble(),
                                      backgroundColor: _bgTertiary,
                                      valueColor: AlwaysStoppedAnimation<Color>(_textSecondary),
                                    ),
                                  ),
                                ),
                                const SizedBox(width: 10),
                                Text(_quotaLabel(user), style: TextStyle(color: _textMuted, fontSize: 12)),
                              ],
                            ),
                          ],
                        ),
                      ),
                      const SizedBox(width: 8),
                      PopupMenuButton<String>(
                        tooltip: appStrings.details,
                        icon: Icon(Icons.more_horiz_rounded, color: _textMuted),
                        color: _bgCard,
                        onSelected: (action) {
                          switch (action) {
                            case 'quota':
                              unawaited(_editQuota(user));
                            case 'admin':
                              unawaited(_act(() => _controller.client.adminSetAdmin(user.id, !user.isAdmin)));
                            case 'password':
                              unawaited(_resetPassword(user));
                            case 'delete':
                              unawaited(_confirmDelete(
                                context,
                                title: appStrings.adminDeleteUser,
                                message: appStrings.adminDeleteUserMessage(user.username),
                                onConfirm: () => _act(() => _controller.client.adminDeleteUser(user.id)),
                              ));
                          }
                        },
                        itemBuilder: (context) => <PopupMenuEntry<String>>[
                          PopupMenuItem<String>(value: 'quota', child: Text(appStrings.adminSetQuota)),
                          if (user.id != ownId)
                            PopupMenuItem<String>(
                              value: 'admin',
                              child: Text(user.isAdmin ? appStrings.adminRemoveAdmin : appStrings.adminMakeAdmin),
                            ),
                          PopupMenuItem<String>(value: 'password', child: Text(appStrings.adminResetPassword)),
                          if (!user.isAdmin)
                            PopupMenuItem<String>(
                              value: 'delete',
                              child: Text(appStrings.adminDeleteUser, style: TextStyle(color: _danger)),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ],
    );
  }
}

class _AdminStorageTab extends StatefulWidget {
  const _AdminStorageTab({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminStorageTab> createState() => _AdminStorageTabState();
}

class _AdminStorageTabState extends State<_AdminStorageTab> with _PageLoad {
  final TextEditingController _quota = TextEditingController();
  final TextEditingController _maxUpload = TextEditingController();
  final TextEditingController _trashDays = TextEditingController();
  final TextEditingController _versions = TextEditingController();
  bool _loaded = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    for (final controller in <TextEditingController>[_quota, _maxUpload, _trashDays, _versions]) {
      controller.dispose();
    }
    super.dispose();
  }

  String _sizeText(num? bytes) {
    final value = bytes?.toInt() ?? 0;
    if (value < 0) return 'unlimited';
    if (value == 0) return '0';
    return formatBytes(value).replaceAll(' ', '');
  }

  void _fill(Map<String, dynamic> config) {
    _quota.text = _sizeText(config['defaultQuotaBytes'] as num?);
    _maxUpload.text = _sizeText(config['maxUploadBytes'] as num?);
    _trashDays.text = '${config['trashRetentionDays'] ?? 30}';
    _versions.text = '${config['versionRetention'] ?? 10}';
  }

  Future<void> _load() async {
    try {
      final config = await _controller.client.adminStorageConfig();
      if (!mounted) return;
      setState(() {
        _fill(config);
        _loaded = true;
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _save() => runAction(_controller, () async {
    final quota = _quota.text.trim().toLowerCase();
    final config = await _controller.client.adminUpdateStorageConfig(<String, Object?>{
      'defaultQuotaBytes': quota == 'unlimited' ? 'unlimited' : quota,
      'maxUploadBytes': _maxUpload.text.trim(),
      'trashRetentionDays': int.tryParse(_trashDays.text.trim()) ?? -1,
      'versionRetention': int.tryParse(_versions.text.trim()) ?? -1,
    });
    _fill(config);
    await _controller.refreshAccount();
  }, notice: appStrings.saved);

  Widget _field(TextEditingController controller) =>
      TextField(controller: controller, decoration: const InputDecoration(isDense: true));

  @override
  Widget build(BuildContext context) {
    if (!_loaded && pageError == null) return const _LoadingPlaceholder();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.adminStorageTitle,
          trailing: FilledButton(onPressed: busy ? null : () => unawaited(_save()), child: Text(appStrings.save)),
          children: <Widget>[
            _SettingsRow(label: appStrings.adminDefaultQuota, description: appStrings.adminDefaultQuotaDescription, fillControl: true, control: _field(_quota)),
            _SettingsRow(label: appStrings.adminMaxUpload, description: appStrings.adminMaxUploadDescription, fillControl: true, control: _field(_maxUpload)),
            _SettingsRow(label: appStrings.adminTrashDays, description: appStrings.adminTrashDaysDescription, fillControl: true, control: _field(_trashDays)),
            _SettingsRow(label: appStrings.adminVersions, fillControl: true, control: _field(_versions)),
            _SettingsRow(
              label: appStrings.adminRecalculate,
              control: OutlinedButton(
                onPressed: busy ? null : () => unawaited(runAction(_controller, _controller.client.adminRecalculate, notice: appStrings.saved)),
                child: Text(appStrings.adminRecalculate),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _AdminAccessTab extends StatefulWidget {
  const _AdminAccessTab({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminAccessTab> createState() => _AdminAccessTabState();
}

class _AdminAccessTabState extends State<_AdminAccessTab> with _PageLoad {
  bool? _allowSignup;
  List<Map<String, dynamic>>? _keys;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final allow = await _controller.client.adminAllowSignup();
      final keys = await _controller.client.adminSignupKeys();
      if (!mounted) return;
      setState(() {
        _allowSignup = allow;
        _keys = keys;
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _act(Future<void> Function() action) => runAction(_controller, () async {
    await action();
    await _load();
  });

  String _keyStatus(Map<String, dynamic> key) {
    switch (key['status']) {
      case 'used':
        return appStrings.adminKeyUsed((key['usedBy'] as Map?)?['username']?.toString() ?? '—');
      case 'revoked':
        return appStrings.adminKeyRevoked;
      default:
        return appStrings.adminKeyUnused;
    }
  }

  @override
  Widget build(BuildContext context) {
    final keys = _keys;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.adminAccessTitle,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.adminAllowSignup,
              description: appStrings.adminAllowSignupDescription,
              control: Switch(
                value: _allowSignup ?? true,
                onChanged: _allowSignup == null || busy ? null : (value) => unawaited(_act(() => _controller.client.adminSetAllowSignup(value))),
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.adminSignupKeys,
          trailing: FilledButton.icon(
            onPressed: busy ? null : () => unawaited(_act(_controller.client.adminCreateSignupKey)),
            icon: const Icon(Icons.add_rounded, size: 18),
            label: Text(appStrings.adminCreateKey),
          ),
          children: <Widget>[
            if (keys == null) const _LoadingPlaceholder(),
            if (keys != null && keys.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.adminNoKeys)),
            for (final key in keys ?? const <Map<String, dynamic>>[])
              _SettingsRow(
                label: key['code']?.toString() ?? '',
                description: _keyStatus(key),
                control: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    IconButton(
                      tooltip: appStrings.copy,
                      onPressed: () => Clipboard.setData(ClipboardData(text: key['code']?.toString() ?? '')),
                      icon: const Icon(Icons.copy_rounded, size: 18),
                    ),
                    if (key['status'] == 'unused')
                      TextButton(
                        onPressed: () => unawaited(_act(() => _controller.client.adminRevokeSignupKey((key['id'] as num).toInt()))),
                        child: Text(appStrings.adminRevoke),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ],
    );
  }
}

class _AdminServerTab extends StatefulWidget {
  const _AdminServerTab({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminServerTab> createState() => _AdminServerTabState();
}

class _AdminServerTabState extends State<_AdminServerTab> with _PageLoad {
  Map<String, dynamic>? _health;
  List<Map<String, dynamic>>? _logs;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final health = await _controller.client.adminHealth();
      final logs = await _controller.client.adminLogs();
      if (!mounted) return;
      setState(() {
        _health = health;
        _logs = logs;
      });
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _showMasterKey() => runAction(_controller, () => _exportMasterKey(context, _controller));

  String _uptime(num? seconds) {
    final total = seconds?.toInt() ?? 0;
    final days = total ~/ 86400;
    final hours = (total % 86400) ~/ 3600;
    final minutes = (total % 3600) ~/ 60;
    if (days > 0) return '${days}d ${hours}h';
    if (hours > 0) return '${hours}h ${minutes}m';
    return '${minutes}m';
  }

  @override
  Widget build(BuildContext context) {
    final health = _health;
    final logs = _logs ?? const <Map<String, dynamic>>[];
    final disk = (health?['storageDisk'] as Map?)?.cast<String, dynamic>();
    Widget value(String text) => Text(text, style: TextStyle(color: _textSecondary));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        if (health == null && pageError == null) const _LoadingPlaceholder(),
        if (health != null)
          _SettingsGroup(
            title: appStrings.adminServerTitle,
            trailing: IconButton(tooltip: appStrings.refresh, onPressed: _load, icon: const Icon(Icons.refresh_rounded, size: 20)),
            children: <Widget>[
              _SettingsRow(label: appStrings.adminVersion, control: value('${health['version']} · Node ${health['node']}')),
              _SettingsRow(label: appStrings.adminUptime, control: value(_uptime(health['uptimeSeconds'] as num?))),
              _SettingsRow(
                label: appStrings.adminDatabase,
                control: _databaseStatus(health),
              ),
              _SettingsRow(label: appStrings.adminAccounts, control: value('${health['users']}')),
              _SettingsRow(label: appStrings.adminFiles, control: value('${health['files']}')),
              _SettingsRow(label: appStrings.adminStored, control: value(formatBytes((health['usedBytes'] as num?)?.toInt() ?? 0))),
              _SettingsRow(
                label: appStrings.adminPreviews,
                control: value(appStrings.adminPreviewsValue(
                  (health['previews'] as Map?)?['officePreview'] == true ? appStrings.yes : appStrings.noLibreOffice,
                )),
              ),
              if (disk != null)
                _SettingsRow(
                  label: appStrings.adminDiskFree,
                  control: value(
                    '${formatBytes((disk['freeBytes'] as num).toInt())} / ${formatBytes((disk['totalBytes'] as num).toInt())}',
                  ),
                ),
            ],
          ),
        _AdminSearchGroup(controller: _controller),
        _SettingsGroup(
          title: appStrings.adminMasterKey,
          description: appStrings.adminMasterKeyDescription,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.adminMasterKey,
              control: OutlinedButton.icon(
                onPressed: busy ? null : () => unawaited(_showMasterKey()),
                icon: const Icon(Icons.key_rounded, size: 18),
                label: Text(appStrings.adminShowMasterKey),
              ),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.adminLogs,
          children: <Widget>[
            _SettingsBlock(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 360),
                child: SingleChildScrollView(
                  reverse: true,
                  child: SelectableText(
                    logs.reversed.take(150).toList().reversed.map((entry) => '${entry['timestamp'] ?? ''}  ${entry['message'] ?? ''}').join('\n'),
                    style: GoogleFonts.geistMono(fontSize: 11.5, height: 1.5, color: _textSecondary),
                  ),
                ),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
