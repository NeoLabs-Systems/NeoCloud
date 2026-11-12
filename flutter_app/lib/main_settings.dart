part of 'main.dart';

// Settings, laid out like NeoAgent's: a 268px page rail beside the open page
// on wide screens, the page list then the page on phones. The rail, page
// header and the group / row / choice building blocks are NeoAgent's.

enum SettingsScope { account, preferences }

enum SettingsPage { profile, security, devices, sharedLinks, storage, imports, general, cameraUpload }

extension SettingsScopeInfo on SettingsScope {
  String get label => this == SettingsScope.account ? appStrings.settingsScopeAccount : appStrings.pageGeneral.toUpperCase();
}

extension SettingsPageInfo on SettingsPage {
  SettingsScope get scope => this == SettingsPage.general || this == SettingsPage.cameraUpload ? SettingsScope.preferences : SettingsScope.account;

  bool get requiresBrowserSession => this == SettingsPage.profile || this == SettingsPage.security ||
      this == SettingsPage.devices || this == SettingsPage.imports;

  /// Camera upload is available only on supported devices.
  bool visibleFor(NeoCloudController controller) {
    switch (this) {
      case SettingsPage.profile:
      case SettingsPage.security:
      case SettingsPage.devices:
      case SettingsPage.imports:
        return true;
      case SettingsPage.cameraUpload:
        return controller.cameraUploads.supported;
      case SettingsPage.sharedLinks:
      case SettingsPage.storage:
      case SettingsPage.general:
        return true;
    }
  }

  String get label {
    switch (this) {
      case SettingsPage.profile:
        return appStrings.pageProfile;
      case SettingsPage.security:
        return appStrings.pageSecurity;
      case SettingsPage.devices:
        return appStrings.pageDevices;
      case SettingsPage.sharedLinks:
        return appStrings.pageSharedLinks;
      case SettingsPage.storage:
        return appStrings.pageStorage;
      case SettingsPage.imports:
        return appStrings.pageImport;
      case SettingsPage.general:
        return appStrings.pageGeneral;
      case SettingsPage.cameraUpload:
        return appStrings.pageCameraUpload;
    }
  }

  String get description {
    switch (this) {
      case SettingsPage.profile:
        return appStrings.pageProfileDescription;
      case SettingsPage.security:
        return appStrings.pageSecurityDescription;
      case SettingsPage.devices:
        return appStrings.pageDevicesDescription;
      case SettingsPage.sharedLinks:
        return appStrings.pageSharedLinksDescription;
      case SettingsPage.storage:
        return appStrings.pageStorageDescription;
      case SettingsPage.imports:
        return appStrings.pageImportDescription;
      case SettingsPage.general:
        return appStrings.pageGeneralDescription;
      case SettingsPage.cameraUpload:
        return appStrings.pageCameraUploadDescription;
    }
  }

  IconData get icon {
    switch (this) {
      case SettingsPage.profile:
        return Icons.person_outline_rounded;
      case SettingsPage.security:
        return Icons.lock_outline_rounded;
      case SettingsPage.devices:
        return Icons.devices_outlined;
      case SettingsPage.sharedLinks:
        return Icons.link_rounded;
      case SettingsPage.storage:
        return Icons.cloud_outlined;
      case SettingsPage.imports:
        return Icons.move_to_inbox_outlined;
      case SettingsPage.general:
        return Icons.tune_rounded;
      case SettingsPage.cameraUpload:
        return Icons.photo_camera_outlined;
    }
  }
}

class SettingsPanel extends StatelessWidget {
  const SettingsPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final visible = SettingsPage.values.where((page) => page.visibleFor(controller)).toList();
        final selected = controller.settingsPage;
        final page = visible.contains(selected) && !(controller.usesDeviceLogin && selected!.requiresBrowserSession) ? selected : null;
        if (constraints.maxWidth >= 760) {
          final shown = page ?? visible.firstWhere((item) => !controller.usesDeviceLogin || !item.requiresBrowserSession);
          return Row(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Container(
                width: 268,
                decoration: BoxDecoration(color: _bgSecondary, border: Border(right: BorderSide(color: _border))),
                child: _SettingsNav(controller: controller, selected: shown, compact: false),
              ),
              Expanded(
                child: _SettingsPageView(key: ValueKey<SettingsPage>(shown), controller: controller, page: shown),
              ),
            ],
          );
        }
        if (page == null) return _SettingsNav(controller: controller, selected: null, compact: true);
        // On a phone a page sits on top of the list: back returns to it.
        return PopScope(
          canPop: false,
          onPopInvokedWithResult: (didPop, _) {
            if (!didPop) controller.setSettingsPage(null);
          },
          child: _SettingsPageView(
            key: ValueKey<SettingsPage>(page),
            controller: controller,
            page: page,
            onBack: () => controller.setSettingsPage(null),
          ),
        );
      },
    );
  }
}

/// The page list: a rail beside the open page on wide screens, the whole
/// screen on a phone.
class _SettingsNav extends StatelessWidget {
  const _SettingsNav({required this.controller, required this.selected, required this.compact});

  final NeoCloudController controller;
  final SettingsPage? selected;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: compact ? _pagePadding(context) : const EdgeInsets.fromLTRB(14, 26, 14, 24),
      children: <Widget>[
        Padding(
          padding: EdgeInsets.fromLTRB(compact ? 2 : 10, 0, 0, 6),
          child: Text(
            appStrings.settingsTitle,
            style: compact
                ? _displayTitleStyle(26)
                : TextStyle(fontSize: 20, fontWeight: FontWeight.w700, letterSpacing: -0.3, color: _textPrimary),
          ),
        ),
        // NeoAgent leads its phone settings with the QR scanner: approving
        // a browser or a desktop sign-in is what the phone is for.
        if (compact && qrScannerSupported && controller.usesDeviceLogin) ...<Widget>[
          const SizedBox(height: 16),
          _ScanQrTile(controller: controller),
        ],
        for (final scope in SettingsScope.values)
          ..._scopeSection(scope, SettingsPage.values.where((page) => page.scope == scope && page.visibleFor(controller)).toList()),
        const SizedBox(height: 22),
        if (compact)
          _SettingsCardList(
            children: <Widget>[
              _SettingsListTile(icon: Icons.logout_rounded, title: _signOutLabel, onTap: () => unawaited(_signOut(context))),
            ],
          )
        else
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 4),
            child: OutlinedButton.icon(
              onPressed: () => unawaited(_signOut(context)),
              icon: const Icon(Icons.logout_rounded, size: 18),
              label: Text(_signOutLabel),
            ),
          ),
      ],
    );
  }

  String get _signOutLabel => controller.usesDeviceLogin ? appStrings.disconnect : appStrings.signOut;

  /// A device asks first: disconnecting needs a new approval.
  Future<void> _signOut(BuildContext context) async {
    if (!controller.usesDeviceLogin) return controller.logout();
    await _confirmDelete(
      context,
      title: appStrings.disconnectTitle,
      message: appStrings.disconnectMessage(controller.connectedServer),
      confirmLabel: appStrings.disconnect,
      onConfirm: controller.logout,
    );
  }

  void _openPage(SettingsPage page) {
    if (controller.usesDeviceLogin && page.requiresBrowserSession) {
      final route = AppRoute('settings', AppRoute.slug(page.name));
      unawaited(launchUrl(controller.client.uri('/app${route.location}'), mode: LaunchMode.externalApplication));
      return;
    }
    controller.setSettingsPage(page);
  }

  List<Widget> _scopeSection(SettingsScope scope, List<SettingsPage> pages) {
    final header = Padding(
      padding: EdgeInsets.fromLTRB(compact ? 4 : 10, 22, 4, 8),
      child: Text(
        scope.label.toUpperCase(),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: GoogleFonts.geistMono(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 1.4, color: _textMuted),
      ),
    );
    if (!compact) {
      return <Widget>[
        header,
        for (final page in pages)
          _SettingsNavItem(page: page, selected: page == selected, onTap: () => _openPage(page)),
      ];
    }
    return <Widget>[
      header,
      _SettingsCardList(
        children: <Widget>[
          for (final page in pages)
            _SettingsListTile(icon: page.icon, title: page.label, onTap: () => _openPage(page)),
        ],
      ),
    ];
  }
}

class _SettingsPageView extends StatelessWidget {
  const _SettingsPageView({super.key, required this.controller, required this.page, this.onBack});

  final NeoCloudController controller;
  final SettingsPage page;

  /// Set on a phone, where the page covers the list.
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: _pagePadding(context),
      children: <Widget>[
        Align(
          alignment: Alignment.topLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 880),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                if (onBack != null)
                  Align(
                    alignment: Alignment.centerLeft,
                    child: TextButton.icon(
                      onPressed: onBack,
                      icon: const Icon(Icons.arrow_back_ios_new_rounded, size: 16),
                      label: Text(appStrings.settingsTitle),
                    ),
                  ),
                _SettingsPageHeader(controller: controller, page: page),
                const SizedBox(height: 26),
                _settingsPageBody(controller, page),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

Widget _settingsPageBody(NeoCloudController controller, SettingsPage page) {
  switch (page) {
    case SettingsPage.profile:
      return _ProfileSettingsPage(controller: controller);
    case SettingsPage.security:
      return _SecuritySettingsPage(controller: controller);
    case SettingsPage.devices:
      return _DevicesSettingsPage(controller: controller);
    case SettingsPage.sharedLinks:
      return _SharedLinksSettingsPage(controller: controller);
    case SettingsPage.storage:
      return _StorageSettingsPage(controller: controller);
    case SettingsPage.imports:
      return _ImportSettingsPage(controller: controller);
    case SettingsPage.general:
      return _GeneralSettingsPage(controller: controller);
    case SettingsPage.cameraUpload:
      return _CameraUploadSettingsPage(controller: controller);
  }
}

/// Scope, title and description of a page, plus whether changes are saved.
class _SettingsPageHeader extends StatelessWidget {
  const _SettingsPageHeader({required this.controller, required this.page});

  final NeoCloudController controller;
  final SettingsPage page;

  @override
  Widget build(BuildContext context) {
    final compact = MediaQuery.sizeOf(context).width < 760;
    final heading = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(page.scope.label.toUpperCase(), style: _sectionEyebrowStyle()),
        const SizedBox(height: 8),
        Text(page.label, style: _displayTitleStyle(compact ? 24 : 30)),
        const SizedBox(height: 8),
        Text(page.description, style: TextStyle(color: _textSecondary, height: 1.5)),
      ],
    );
    final indicators = Column(
      crossAxisAlignment: compact ? CrossAxisAlignment.start : CrossAxisAlignment.end,
      children: <Widget>[
        _SettingsScopePill(icon: Icons.cloud_outlined, label: appStrings.settingsAccountHint),
        const SizedBox(height: 10),
        _SettingsSaveStatus(controller: controller),
      ],
    );
    if (compact) {
      return Column(crossAxisAlignment: CrossAxisAlignment.start, children: <Widget>[heading, const SizedBox(height: 14), indicators]);
    }
    return Row(
      crossAxisAlignment: CrossAxisAlignment.end,
      children: <Widget>[Expanded(child: heading), const SizedBox(width: 24), indicators],
    );
  }
}

/// Settings save as they change; this says so, and shows a save in flight.
class _SettingsSaveStatus extends StatelessWidget {
  const _SettingsSaveStatus({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final saving = controller.isSavingSettings;
    final failed = controller.settingsSaveError != null;
    final String label;
    if (saving) {
      label = appStrings.saving;
    } else if (failed) {
      label = appStrings.settingsSaveFailed;
    } else {
      label = appStrings.settingsSaveHint;
    }
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        if (saving)
          SizedBox.square(dimension: 12, child: CircularProgressIndicator(strokeWidth: 1.6, color: _textMuted))
        else
          Icon(failed ? Icons.error_outline_rounded : Icons.info_outline_rounded, size: 15, color: failed ? _danger : _textSecondary),
        const SizedBox(width: 6),
        Flexible(child: Text(label, style: TextStyle(color: _textMuted, fontSize: 12.5))),
      ],
    );
  }
}

class _SettingsNavItem extends StatelessWidget {
  const _SettingsNavItem({
    required this.page,
    required this.selected,
    required this.onTap,
  });

  final SettingsPage page;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: Material(
        color: selected ? _bgTertiary : Colors.transparent,
        borderRadius: BorderRadius.circular(10),
        child: InkWell(
          borderRadius: BorderRadius.circular(10),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 10),
            child: Row(
              children: <Widget>[
                Icon(
                  page.icon,
                  size: 18,
                  color: selected ? _accent : _textMuted,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    page.label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontSize: 14,
                      fontWeight: selected ? FontWeight.w600 : FontWeight.w500,
                      color: selected ? _textPrimary : _textSecondary,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SettingsScopePill extends StatelessWidget {
  const _SettingsScopePill({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
      decoration: BoxDecoration(
        color: _bgTertiary,
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: _border),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          Icon(icon, size: 14, color: _textSecondary),
          const SizedBox(width: 7),
          Flexible(
            child: Text(
              label,
              style: TextStyle(color: _textSecondary, fontSize: 12.5),
            ),
          ),
        ],
      ),
    );
  }
}

/// A titled block of settings: a heading and optional help line, then its
/// rows on one surface, separated by hairlines.
class _SettingsGroup extends StatelessWidget {
  const _SettingsGroup({
    required this.title,
    required this.children,
    this.description,
    this.trailing,
  });

  final String title;
  final String? description;
  final Widget? trailing;
  final List<Widget> children;


  @override
  Widget build(BuildContext context) {
    final body = Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          _SettingsHeading(
            title: title,
            description: description,
            trailing: trailing,
          ),
          if (children.isNotEmpty) _SettingsCardList(children: children),
        ],
      ),
    );
    return body;
  }
}

/// The heading over a group of settings, also used over content that brings
/// its own layout, like the messaging channel grid.
class _SettingsHeading extends StatelessWidget {
  const _SettingsHeading({required this.title, this.description, this.trailing});

  final String title;
  final String? description;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(2, 0, 0, 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: <Widget>[
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: _sectionTitleStyle()),
                if (description != null) ...<Widget>[
                  const SizedBox(height: 3),
                  Text(description!, style: TextStyle(fontSize: 13, color: _textSecondary, height: 1.45)),
                ],
              ],
            ),
          ),
          if (trailing != null) ...<Widget>[const SizedBox(width: 12), trailing!],
        ],
      ),
    );
  }
}

/// Rows on a single card, split by hairlines.
class _SettingsCardList extends StatelessWidget {
  const _SettingsCardList({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return _AppCard(
      padding: EdgeInsets.zero,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          for (var i = 0; i < children.length; i++) ...<Widget>[
            if (i > 0) Divider(height: 1, thickness: 1, color: _border),
            children[i],
          ],
        ],
      ),
    );
  }
}

/// One setting: label and help on the left, its control on the right. On a
/// narrow screen the control moves under the label.
class _SettingsRow extends StatelessWidget {
  const _SettingsRow({
    required this.label,
    this.description,
    this.control,
    this.fillControl = false,
    this.tag,
  });

  final String label;
  final String? description;
  final Widget? control;

  /// Gives the control a fixed width beside the label and the full width
  /// under it. Pickers and text fields need this; switches and buttons size
  /// themselves.
  final bool fillControl;

  /// Short qualifier next to the label, such as "This device".
  final String? tag;

  @override
  Widget build(BuildContext context) {
    final text = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Wrap(
          spacing: 8,
          runSpacing: 4,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Text(
              label,
              style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: _textPrimary),
            ),
            if (tag != null) _SettingsTag(label: tag!),
          ],
        ),
        if (description != null) ...<Widget>[
          const SizedBox(height: 4),
          Text(
            description!,
            style: TextStyle(fontSize: 13, color: _textSecondary, height: 1.45),
          ),
        ],
      ],
    );
    final row = Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 16),
      child: LayoutBuilder(
        builder: (context, constraints) {
          final control = this.control;
          if (control == null) return text;
          if (constraints.maxWidth < 560) {
            return Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                text,
                const SizedBox(height: 12),
                if (fillControl)
                  control
                else
                  Align(alignment: Alignment.centerLeft, child: control),
              ],
            );
          }
          return Row(
            children: <Widget>[
              Expanded(child: text),
              const SizedBox(width: 24),
              if (fillControl)
                SizedBox(width: 300, child: control)
              else
                control,
            ],
          );
        },
      ),
    );
    return row;
  }
}
/// Free-form content inside a group, for forms and lists.
class _SettingsBlock extends StatelessWidget {
  const _SettingsBlock({required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => Padding(padding: const EdgeInsets.all(20), child: child);
}

/// Small neutral tag next to a label (NeoRouter's MetaPill).
class _SettingsTag extends StatelessWidget {
  const _SettingsTag({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.tag)),
      child: Text(label, style: TextStyle(color: _textSecondary, fontSize: 12, fontWeight: FontWeight.w500)),
    );
  }
}

/// A row that opens something, for the page list on a phone.
class _SettingsListTile extends StatelessWidget {
  const _SettingsListTile({required this.icon, required this.title, required this.onTap});

  final IconData icon;
  final String title;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        child: Row(
          children: <Widget>[
            Container(
              width: 32,
              height: 32,
              decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(9)),
              child: Icon(icon, size: 17, color: _textSecondary),
            ),
            const SizedBox(width: 12),
            Expanded(child: Text(title, style: TextStyle(fontSize: 15, fontWeight: FontWeight.w500, color: _textPrimary))),
            Icon(Icons.chevron_right_rounded, size: 20, color: _textMuted),
          ],
        ),
      ),
    );
  }
}

/// A one-of-a-few choice; the theme gives it NeoRouter's look.
class _SettingsChoice<T extends Object> extends StatelessWidget {
  const _SettingsChoice({required this.value, required this.options, required this.onChanged});

  final T value;
  final List<(T, String)> options;
  final ValueChanged<T>? onChanged;

  @override
  Widget build(BuildContext context) {
    final onChanged = this.onChanged;
    return SegmentedButton<T>(
      segments: <ButtonSegment<T>>[
        for (final (option, label) in options) ButtonSegment<T>(value: option, label: Text(label)),
      ],
      selected: <T>{value},
      showSelectedIcon: false,
      onSelectionChanged: onChanged == null ? null : (selection) => onChanged(selection.first),
    );
  }
}
