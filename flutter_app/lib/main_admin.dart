part of 'main.dart';

// The admin page, shaped like NeoRouter's AdminScreen: page title, a search
// that jumps to the tab holding a setting, a row of tab chips, then settings
// groups for the open tab.

enum _AdminTab { users, storage, backups, access, providers, models, server }

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
      case _AdminTab.providers:
        return appStrings.aiAdminTabProviders;
      case _AdminTab.models:
        return appStrings.aiAdminTabModels;
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
      case _AdminTab.providers:
        return Icons.key_outlined;
      case _AdminTab.models:
        return Icons.view_list_outlined;
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

/// One thing an admin might look for and the tab that has it (NeoRouter's
/// admin search).
class _AdminSearchEntry {
  const _AdminSearchEntry(this.title, this.tab, this.keywords);

  final String title;
  final _AdminTab tab;

  /// Extra terms, English and German, people search for.
  final String keywords;

  bool matches(String query) {
    final haystack = '${title.toLowerCase()} ${tab.label.toLowerCase()} $keywords';
    return query.split(RegExp(r'\s+')).where((word) => word.isNotEmpty).every(haystack.contains);
  }
}

List<_AdminSearchEntry> _adminSearchIndex() => <_AdminSearchEntry>[
  _AdminSearchEntry(appStrings.adminUsersTitle, _AdminTab.users, 'users accounts konten nutzer people email username admin role quota password delete'),
  _AdminSearchEntry(appStrings.adminStorageTitle, _AdminTab.storage, 'quota kontingent upload size trash papierkorb versions versionen recalculate'),
  _AdminSearchEntry(appStrings.backupTargets, _AdminTab.backups, 'backup sicherung sftp s3 webdav local restore verify schedule'),
  _AdminSearchEntry(appStrings.adminAllowSignup, _AdminTab.access, 'signup sign-up registration registrierung register open close'),
  _AdminSearchEntry(appStrings.aiAdminEnable, _AdminTab.models, 'ai ki llm suggestions vorschläge budget tokens usage nutzung'),
  _AdminSearchEntry(appStrings.aiProvidersTitle, _AdminTab.providers, 'ai ki providers anbieter keys schlüssel api openai anthropic google gemini openrouter ollama neorouter'),
  _AdminSearchEntry(appStrings.aiModelAvailability, _AdminTab.models, 'ai ki models modelle availability verfügbarkeit enable disable'),
  _AdminSearchEntry(appStrings.adminServerTitle, _AdminTab.server, 'version uptime database datenbank disk health node previews libreoffice'),
  _AdminSearchEntry(appStrings.adminSearchTitle, _AdminTab.server, 'search suche full-text volltext index rebuild'),
  _AdminSearchEntry(appStrings.adminMasterKey, _AdminTab.server, 'master key schlüssel encryption verschlüsselung export'),
  _AdminSearchEntry(appStrings.adminLogs, _AdminTab.server, 'logs protokoll errors fehler'),
];

class _AdminPanelState extends State<AdminPanel> {
  final TextEditingController _search = TextEditingController();

  /// Kept by the controller, so the address and a reload keep the tab.
  _AdminTab get _tab => widget.controller._adminTab;

  @override
  void dispose() {
    _search.dispose();
    super.dispose();
  }

  void _open(_AdminTab tab) {
    _search.clear();
    widget.controller._openAdminTab(tab);
  }

  @override
  Widget build(BuildContext context) {
    final controller = widget.controller;
    final query = _search.text.trim().toLowerCase();
    final results = query.isEmpty ? const <_AdminSearchEntry>[] : _adminSearchIndex().where((entry) => entry.matches(query)).toList();
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.adminTitle, subtitle: appStrings.adminSubtitle),
        _AdminSafetyCard(controller: controller, onOpenBackups: () => _open(_AdminTab.backups)),
        TextField(
          controller: _search,
          onChanged: (_) => setState(() {}),
          onSubmitted: (_) {
            if (results.isNotEmpty) _open(results.first.tab);
          },
          textInputAction: TextInputAction.search,
          decoration: InputDecoration(
            isDense: true,
            hintText: appStrings.adminSearchHint,
            prefixIcon: Icon(Icons.search, size: 18, color: _textMuted),
            suffixIcon: _search.text.isEmpty
                ? null
                : IconButton(
                    tooltip: appStrings.clear,
                    icon: Icon(Icons.close, size: 18, color: _textMuted),
                    onPressed: () => setState(_search.clear),
                  ),
          ),
        ),
        if (query.isNotEmpty)
          results.isEmpty
              ? _EmptyCard(title: appStrings.paletteNothing, subtitle: appStrings.adminSearchHint, icon: Icons.search_off_rounded)
              : _SettingsCardList(
                  children: <Widget>[
                    for (final entry in results)
                      ListTile(
                        leading: Icon(entry.tab.icon, color: _accent),
                        title: Text(entry.title, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: _textPrimary)),
                        subtitle: Text(entry.tab.label, style: TextStyle(color: _textMuted, fontSize: 12)),
                        trailing: Icon(Icons.chevron_right_rounded, color: _textMuted),
                        onTap: () => _open(entry.tab),
                      ),
                  ],
                )
        else
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: _AdminTab.values.map((tab) => _AdminTabChip(tab: tab, active: tab == _tab, onTap: () => _open(tab))).toList(growable: false),
          ),
        if (query.isEmpty)
          KeyedSubtree(
          key: ValueKey<_AdminTab>(_tab),
          child: switch (_tab) {
            _AdminTab.users => _AdminUsersTab(controller: controller),
            _AdminTab.storage => _AdminStorageTab(controller: controller),
            _AdminTab.backups => _AdminBackupsTab(controller: controller),
            _AdminTab.access => _AdminAccessTab(controller: controller),
            _AdminTab.providers => AdminAiProvidersTab(controller: controller),
            _AdminTab.models => AdminAiModelsTab(controller: controller),
            _AdminTab.server => _AdminServerTab(controller: controller),
          },
        ),
      ],
    );
  }
}

/// NeoRouter's pill-shaped tab chip: neutral, the open tab raised.
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
            color: active ? _bgTertiary : Colors.transparent,
            border: Border.all(color: active ? _borderLight : _border),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(tab.icon, size: 15, color: active ? _textPrimary : _textMuted),
              const SizedBox(width: 7),
              Text(
                tab.label,
                style: GoogleFonts.geist(fontSize: 12.5, fontWeight: FontWeight.w600, color: active ? _textPrimary : _textSecondary),
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

  Future<void> _editAi(AdminUser user) async {
    final saved = await showAdaptiveSheet<bool>(
      context: context,
      builder: (_) => _AdminAiUserDialog(controller: _controller, user: user),
    );
    if (saved == true && mounted) setState(() => pageNotice = appStrings.aiAdminUserSaved(user.username));
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
    final used = formatBytes(user.usedBytes);
    if (user.quotaBytes < 0) return appStrings.storageUsedUnlimited(used);
    return appStrings.storageUsedOf(used, formatBytes(user.quotaBytes));
  }

  void _onMenu(AdminUser user, String action) {
    switch (action) {
      case 'quota':
        unawaited(_editQuota(user));
      case 'ai':
        unawaited(_editAi(user));
      case 'admin':
        unawaited(_act(() => _controller.client.adminSetAdmin(user.id, !user.isAdmin)));
      case 'password':
        unawaited(_resetPassword(user));
      case 'disable':
        unawaited(_act(() => _controller.client.adminSetDisabled(user.id, !user.disabled)));
      case 'signOut':
        unawaited(_confirmDelete(
          context,
          title: appStrings.adminSignOutEverywhereTitle(user.username),
          message: appStrings.adminSignOutEverywhereMessage,
          confirmLabel: appStrings.adminSignOutEverywhere,
          onConfirm: () => runAction(
            _controller,
            () => _controller.client.adminSignOutEverywhere(user.id),
            notice: appStrings.adminSignedOutEverywhere(user.username),
          ),
        ));
      case 'delete':
        unawaited(_confirmDelete(
          context,
          title: appStrings.adminDeleteUser,
          message: appStrings.adminDeleteUserMessage(user.username),
          onConfirm: () => _act(() => _controller.client.adminDeleteUser(user.id)),
        ));
    }
  }

  /// One account as a settings row, like NeoRouter's user list: initial,
  /// name and details, role tag, then the actions menu.
  Widget _userRow(AdminUser user, {required bool self, required String locale}) {
    final details = <String>[
      ?user.email,
      appStrings.adminFilesCount(user.fileCount),
      _quotaLabel(user),
      if (user.lastLogin != null) formatDate(user.lastLogin, locale),
    ];
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Row(
        children: <Widget>[
          Container(
            width: 32,
            height: 32,
            alignment: Alignment.center,
            decoration: BoxDecoration(shape: BoxShape.circle, color: _bgTertiary, border: Border.all(color: _borderLight)),
            child: Text(
              user.username.isEmpty ? '?' : user.username.characters.first.toUpperCase(),
              style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w700, fontSize: 12),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  self ? appStrings.adminUserYou(user.username) : user.username,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: _textPrimary),
                ),
                const SizedBox(height: 2),
                Text(details.join(' · '), maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: _textSecondary, fontSize: 13)),
              ],
            ),
          ),
          const SizedBox(width: 8),
          // NeoRouter shows "Disabled" before the role; status is a dot and a word.
          if (user.disabled) ...<Widget>[_StatusPill(label: appStrings.adminUserDisabled, color: _danger), const SizedBox(width: 12)],
          _SettingsTag(label: user.isAdmin ? appStrings.roleAdmin : appStrings.roleUser),
          PopupMenuButton<String>(
            tooltip: appStrings.details,
            enabled: !busy,
            icon: Icon(Icons.more_vert_rounded, color: _textMuted),
            onSelected: (action) => _onMenu(user, action),
            itemBuilder: (context) => <PopupMenuEntry<String>>[
              PopupMenuItem<String>(value: 'quota', child: Text(appStrings.adminSetQuota)),
              PopupMenuItem<String>(value: 'ai', child: Text(appStrings.aiAdminUserMenu)),
              if (!self)
                PopupMenuItem<String>(value: 'admin', child: Text(user.isAdmin ? appStrings.adminRemoveAdmin : appStrings.adminMakeAdmin)),
              PopupMenuItem<String>(value: 'password', child: Text(appStrings.adminResetPassword)),
              if (!self) PopupMenuItem<String>(value: 'disable', child: Text(user.disabled ? appStrings.adminEnableUser : appStrings.adminDisableUser)),
              if (!self) PopupMenuItem<String>(value: 'signOut', child: Text(appStrings.adminSignOutEverywhere)),
              if (!user.isAdmin) PopupMenuItem<String>(value: 'delete', child: Text(appStrings.adminDeleteUser, style: TextStyle(color: _danger))),
            ],
          ),
        ],
      ),
    );
  }

  void _retry() {
    setState(() => pageError = null);
    unawaited(_load());
  }

  @override
  Widget build(BuildContext context) {
    final users = _users;
    final ownId = _controller.user?.id;
    final locale = currentAppLanguage.code;
    if (users == null && pageError != null) return _LoadError(message: pageError!, onRetry: _retry);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        _SettingsGroup(
          title: appStrings.adminUsersTitle,
          description: users == null ? null : appStrings.adminUsersCount(users.length),
          trailing: IconButton(tooltip: appStrings.refresh, onPressed: busy ? null : () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded, size: 20)),
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 14, 16, 14),
              child: _SearchField(
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
            ),
            if (users == null) const _LoadingPlaceholder(),
            if (users != null && users.isEmpty) _SettingsBlock(child: _EmptyText(appStrings.paletteNothing)),
            for (final user in users ?? const <AdminUser>[]) _userRow(user, self: user.id == ownId, locale: locale),
          ],
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
    // Without the current values a Save would overwrite them with blanks.
    if (!_loaded) {
      return _LoadError(
        message: pageError!,
        onRetry: () {
          setState(() => pageError = null);
          unawaited(_load());
        },
      );
    }
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

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final allow = await _controller.client.adminAllowSignup();
      if (!mounted) return;
      setState(() => _allowSignup = allow);
    } catch (error) {
      if (mounted) setState(() => pageError = _controller.friendlyError(error));
    }
  }

  Future<void> _act(Future<void> Function() action) => runAction(_controller, () async {
    await action();
    await _load();
  });

  @override
  Widget build(BuildContext context) {
    if (_allowSignup == null) {
      if (pageError == null) return const _LoadingPlaceholder();
      return _LoadError(
        message: pageError!,
        onRetry: () {
          setState(() => pageError = null);
          unawaited(_load());
        },
      );
    }
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
                value: _allowSignup!,
                onChanged: busy ? null : (value) => unawaited(_act(() => _controller.client.adminSetAllowSignup(value))),
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
    if (health == null && pageError != null) {
      return _LoadError(
        message: pageError!,
        onRetry: () {
          setState(() => pageError = null);
          unawaited(_load());
        },
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        feedback(),
        if (health == null) const _LoadingPlaceholder(),
        if (health != null)
          _SettingsGroup(
            title: appStrings.adminServerTitle,
            trailing: IconButton(tooltip: appStrings.refresh, onPressed: () => unawaited(_load()), icon: const Icon(Icons.refresh_rounded, size: 20)),
            children: <Widget>[
              _SettingsRow(
                label: appStrings.adminVersion,
                control: Text('${health['version']} · Node ${health['node']}', style: GoogleFonts.geistMono(color: _textSecondary, fontSize: 13)),
              ),
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
