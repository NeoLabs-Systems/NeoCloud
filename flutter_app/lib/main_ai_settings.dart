part of 'main.dart';

// Settings › AI, after NeoAgent's Settings › Models (_ModelsSettingsPage):
// the opt-in with what leaves the server, one model per kind of work, the
// suggestions (Phase 2 switches), this month's usage and the account's own
// keys with NeoRouter first. Without a model it leads with NeoAgent's
// setup card; Admin › AI is one tap away for admins.

/// The default for `ai_naming_template`; the server builds names from it.
const String _aiDefaultNamingTemplate = '{date} {issuer} – {subject}';

String _aiNumber(int value) => NumberFormat.decimalPattern(currentAppLanguage.code).format(value);

extension _AiRoleText on AiRole {
  String get label => switch (this) {
    AiRole.text => appStrings.aiRoleText,
    AiRole.smart => appStrings.aiRoleSmart,
    AiRole.vision => appStrings.aiRoleVision,
    AiRole.embedding => appStrings.aiRoleEmbedding,
  };

  String get description => switch (this) {
    AiRole.text => appStrings.aiRoleTextDescription,
    AiRole.smart => appStrings.aiRoleSmartDescription,
    AiRole.vision => appStrings.aiRoleVisionDescription,
    AiRole.embedding => appStrings.aiRoleEmbeddingDescription,
  };
}

/// The suggestions, by their key in `ai_features`; unset ones follow the
/// server's defaults ([aiFeatureDefaults]).
List<(String, String, String)> get _aiFeatureRows => <(String, String, String)>[
  ('names', appStrings.aiFeatureNames, appStrings.aiFeatureNamesDescription),
  ('folders', appStrings.aiFeatureFolders, appStrings.aiFeatureFoldersDescription),
  ('tags', appStrings.aiFeatureTags, appStrings.aiFeatureTagsDescription),
  ('summaries', appStrings.aiFeatureSummaries, appStrings.aiFeatureSummariesDescription),
  ('onUpload', appStrings.aiFeatureOnUpload, appStrings.aiFeatureOnUploadDescription),
  ('autoApplyHighConfidence', appStrings.aiFeatureAutoApply, appStrings.aiFeatureAutoApplyDescription),
  ('newTags', appStrings.aiFeatureNewTags, appStrings.aiFeatureNewTagsDescription),
  ('newFolders', appStrings.aiFeatureNewFolders, appStrings.aiFeatureNewFoldersDescription),
];

class AiSettingsPage extends StatefulWidget {
  const AiSettingsPage({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<AiSettingsPage> createState() => _AiSettingsPageState();
}

class _AiSettingsPageState extends State<AiSettingsPage> {
  late final TextEditingController _template = TextEditingController(text: widget.controller.settings.aiNamingTemplate);
  AiStatus? _status;
  List<ModelMeta> _models = const <ModelMeta>[];
  String? _loadError;

  /// The server has no AI routes (older than AI).
  bool _missing = false;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  @override
  void dispose() {
    _template.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    try {
      final status = await _controller.ai.status();
      List<ModelMeta> models;
      try {
        models = await _controller.ai.models();
      } catch (_) {
        models = const <ModelMeta>[];
      }
      if (!mounted) return;
      setState(() {
        _status = status;
        _models = models;
        _loadError = null;
        _missing = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _missing = _aiRouteMissing(error);
        _loadError = _aiFriendlyError(_controller, error);
      });
    }
  }

  /// Saves [patch] like every other setting, then asks again what the
  /// models resolve to.
  Future<void> _save(Map<String, Object?> patch) async {
    await _controller.updateSettings(patch);
    await _load();
  }

  /// The server refuses an empty template; clearing the field goes back to
  /// the default.
  void _saveTemplate() {
    var value = _template.text.trim();
    if (value.isEmpty) {
      value = _aiDefaultNamingTemplate;
      _template.text = value;
    }
    if (value == _controller.settings.aiNamingTemplate) return;
    unawaited(_controller.updateSettings(<String, Object?>{'ai_naming_template': value}));
  }

  void _openAdmin(_AdminTab tab) {
    _controller._openAdminTab(tab);
    _controller.setSelectedSection(AppSection.admin);
  }

  @override
  Widget build(BuildContext context) {
    final status = _status;
    if (_missing) {
      return _AiSetupCard(title: appStrings.aiNotAvailableTitle, body: appStrings.aiNotAvailableBody, icon: Icons.cloud_off_outlined);
    }
    if (status == null) {
      if (_loadError == null) return const _LoadingPlaceholder();
      return _LoadError(
        message: _loadError!,
        onRetry: () {
          setState(() => _loadError = null);
          unawaited(_load());
        },
      );
    }
    final isAdmin = _controller.isAdmin;
    if (!status.enabled) {
      return _AiSetupCard(
        title: appStrings.aiDisabledTitle,
        body: appStrings.aiDisabledBody,
        adminHint: isAdmin ? appStrings.aiDisabledAdminHint : null,
        icon: Icons.power_settings_new_rounded,
        onOpenAdmin: isAdmin ? () => _openAdmin(_AdminTab.models) : null,
      );
    }
    final settings = _controller.settings;
    final noModel = !status.available && _models.where((model) => model.available).isEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (noModel) ...<Widget>[
          _AiSetupCard(
            title: appStrings.aiNoModelTitle,
            body: appStrings.aiNoModelBody,
            adminHint: isAdmin ? appStrings.aiNoModelAdminHint : null,
            icon: Icons.key_rounded,
            onOpenAdmin: isAdmin ? () => _openAdmin(_AdminTab.providers) : null,
          ),
          const SizedBox(height: 28),
        ],
        _SettingsGroup(
          title: appStrings.aiOptInTitle,
          children: <Widget>[
            _SettingsRow(
              label: appStrings.aiOptIn,
              description: appStrings.aiPrivacyNote,
              control: Switch(value: settings.aiOptIn, onChanged: (value) => unawaited(_save(<String, Object?>{'ai_opt_in': value}))),
            ),
          ],
        ),
        _SettingsGroup(
          title: appStrings.aiModelsTitle,
          description: appStrings.aiModelsDescription,
          children: <Widget>[for (final role in AiRole.values) _modelRow(role, status, settings)],
        ),
        _SettingsGroup(
          title: appStrings.aiFeaturesTitle,
          description: appStrings.aiFeaturesDescription,
          children: <Widget>[
            for (final (key, label, description) in _aiFeatureRows)
              _SettingsRow(
                label: label,
                description: description,
                control: Switch(
                  value: settings.aiFeatureOn(key),
                  onChanged: settings.aiOptIn
                      ? (value) => unawaited(
                          _controller.updateSettings(<String, Object?>{
                            'ai_features': <String, bool>{...settings.aiFeatures, key: value},
                          }),
                        )
                      : null,
                ),
              ),
            _SettingsRow(
              label: appStrings.aiNamingTemplate,
              description: appStrings.aiNamingTemplateDescription,
              fillControl: true,
              control: TextField(
                controller: _template,
                enabled: settings.aiOptIn,
                decoration: const InputDecoration(isDense: true, hintText: _aiDefaultNamingTemplate),
                onSubmitted: (_) => _saveTemplate(),
                onTapOutside: (_) => _saveTemplate(),
              ),
            ),
          ],
        ),
        if (settings.aiOptIn && status.available) ...<Widget>[
          _SettingsGroup(
            title: appStrings.aiReviewTitle,
            children: <Widget>[
              _SettingsRow(
                label: appStrings.aiReviewSettingsLabel,
                description: appStrings.aiReviewSettingsDescription(_controller.aiReview.counts.total),
                control: OutlinedButton.icon(
                  onPressed: () => _controller.setSelectedSection(AppSection.review),
                  icon: const Icon(Icons.auto_awesome_outlined, size: 17),
                  label: Text(appStrings.aiOpenReview),
                ),
              ),
            ],
          ),
          AiRulesCard(controller: _controller),
        ],
        _SettingsGroup(
          title: appStrings.aiUsageTitle,
          children: <Widget>[_SettingsBlock(child: _AiUsageMeter(used: status.budgetUsed, limit: status.budgetLimit))],
        ),
        ByokSettingsCard(controller: _controller, onChanged: () => unawaited(_load())),
      ],
    );
  }

  Widget _modelRow(AiRole role, AiStatus status, UserSettings settings) {
    final roleStatus = status.roles[role.name];
    var description = role.description;
    if (roleStatus != null && !roleStatus.ready) {
      description = '$description ${appStrings.aiRoleNotReady}';
    } else if (roleStatus?.requestedUnavailable ?? false) {
      description = '$description ${appStrings.aiRoleRequestedUnavailable}';
    }
    return _SettingsRow(
      label: role.label,
      description: description,
      fillControl: true,
      control: _ModelPickerButton(
        value: selectedModel(role, settings.aiModels),
        options: _roleModelOptions(role, _models),
        dialogTitle: appStrings.aiSelectModel(role.label),
        onChanged: (next) => unawaited(_save(<String, Object?>{role.settingKey: next})),
      ),
    );
  }
}

/// This month's tokens against the budget, as a bar when there is one.
class _AiUsageMeter extends StatelessWidget {
  const _AiUsageMeter({required this.used, required this.limit});

  final int used;
  final int? limit;

  @override
  Widget build(BuildContext context) {
    final limit = this.limit;
    if (limit == null) return Text(appStrings.aiUsageUnlimited(_aiNumber(used)), style: TextStyle(color: _textSecondary));
    final share = (used / limit).clamp(0.0, 1.0);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(appStrings.aiUsageOf(_aiNumber(used), _aiNumber(limit)), style: TextStyle(color: _textSecondary)),
        const SizedBox(height: 10),
        ClipRRect(
          borderRadius: BorderRadius.circular(AppRadius.pill),
          child: LinearProgressIndicator(value: share, minHeight: 6, backgroundColor: _bgTertiary, color: share >= 1 ? _danger : _accent),
        ),
      ],
    );
  }
}

/// NeoAgent's _ModelSetupCard: why AI cannot run yet, and for admins the
/// way to Admin › AI.
class _AiSetupCard extends StatelessWidget {
  const _AiSetupCard({required this.title, required this.body, required this.icon, this.adminHint, this.onOpenAdmin});

  final String title;
  final String body;
  final IconData icon;
  final String? adminHint;
  final VoidCallback? onOpenAdmin;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: _warning.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(AppRadius.card),
        border: Border.all(color: _warning.withValues(alpha: 0.3)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Icon(icon, color: _warning, size: 20),
              const SizedBox(width: 10),
              Expanded(
                child: Text(title, style: TextStyle(color: _textPrimary, fontSize: 16, fontWeight: FontWeight.w700)),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(body, style: TextStyle(color: _textSecondary, height: 1.45)),
          if (adminHint != null) ...<Widget>[
            const SizedBox(height: 6),
            Text(adminHint!, style: TextStyle(color: _textMuted, height: 1.45)),
          ],
          if (onOpenAdmin != null) ...<Widget>[
            const SizedBox(height: 14),
            FilledButton.icon(
              onPressed: onOpenAdmin,
              icon: const Icon(Icons.admin_panel_settings_outlined, size: 18),
              label: Text(appStrings.aiOpenAdmin),
            ),
          ],
        ],
      ),
    );
  }
}
