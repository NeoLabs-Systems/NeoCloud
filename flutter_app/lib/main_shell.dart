part of 'main.dart';

// The signed-in frame, ported from NeoRouter's main_shell.dart: a 276px
// sidebar with the brand card, grouped navigation and the account button on
// wide screens; an app bar with the logo and the same sidebar as a drawer
// below 980px; below 640px NeoAgent's phone frame (main_mobile_shell.dart).

class _NavGroup {
  const _NavGroup(this.group, this.sections);

  final SidebarGroup group;
  final List<AppSection> sections;
}

List<_NavGroup> _navigationGroups(NeoCloudController controller) {
  return <_NavGroup>[
    for (final group in SidebarGroup.values)
      if (group != SidebarGroup.settings && _groupSections(controller, group).isNotEmpty)
        _NavGroup(group, _groupSections(controller, group)),
  ];
}

class HomeView extends StatefulWidget {
  const HomeView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<HomeView> createState() => _HomeViewState();
}

class _HomeViewState extends State<HomeView> {
  /// The group whose pages are listed; follows navigation like NeoRouter.
  SidebarGroup? _expanded;
  AppSection? _lastSection;

  NeoCloudController get controller => widget.controller;

  /// Camera backup runs a pass every time the app comes back.
  late final AppLifecycleListener _lifecycle;

  void _resumeCameraBackup() {
    final camera = controller.cameraUploads;
    if (camera.supported && camera.settings.enabled) unawaited(camera.runNow(controller.client).catchError((Object _) {}));
  }

  @override
  void initState() {
    super.initState();
    _lifecycle = AppLifecycleListener(
      onResume: _resumeCameraBackup,
      // Polling for changes made elsewhere pauses while the app is hidden.
      onShow: () => controller.liveUpdates.setVisible(true),
      onHide: () => controller.liveUpdates.setVisible(false),
    );
    controller.uploads.resolveConflict = (conflict) async {
      if (!mounted) return null;
      return showConflictDialog(context, controller, conflict);
    };
  }

  @override
  void dispose() {
    _lifecycle.dispose();
    controller.uploads.resolveConflict = null;
    super.dispose();
  }

  void _syncExpanded() {
    if (_lastSection == controller.selectedSection) return;
    _lastSection = controller.selectedSection;
    // Settings has no sidebar group; the open group stays open, as in NeoRouter.
    final group = controller.selectedSection.group;
    if (group != SidebarGroup.settings) _expanded = group;
  }

  Widget _sectionSwitcher({required bool slide}) {
    return AnimatedSwitcher(
      duration: Duration(milliseconds: slide ? 220 : 160),
      switchInCurve: slide ? Curves.easeOutCubic : Curves.easeOut,
      switchOutCurve: slide ? Curves.easeInCubic : Curves.easeIn,
      transitionBuilder: (child, animation) {
        final fade = FadeTransition(opacity: animation, child: child);
        if (!slide) return fade;
        return SlideTransition(
          position: Tween<Offset>(begin: const Offset(0, 0.014), end: Offset.zero).animate(animation),
          child: fade,
        );
      },
      layoutBuilder: (current, previous) => Stack(children: <Widget>[?current]),
      child: KeyedSubtree(
        key: ValueKey<AppSection>(controller.selectedSection),
        child: _SectionBody(controller: controller),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    _syncExpanded();
    final wide = MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet;
    final phone = MediaQuery.sizeOf(context).width < AppBreakpoints.mobile;
    // The undo bar and upload panel float above the phone's tab bar.
    final floatingBottom = phone ? 76 + MediaQuery.paddingOf(context).bottom : 20.0;
    final sidebar = _Sidebar(
      controller: controller,
      groups: _navigationGroups(controller),
      expanded: _expanded,
      onToggle: (group) => setState(() => _expanded = _expanded == group ? null : group),
    );
    return _ControlSurfaceBackdrop(
      child: _GlobalShortcuts(
        controller: controller,
        child: Stack(
        children: <Widget>[
          Positioned.fill(
            child: Scaffold(
              backgroundColor: Colors.transparent,
              appBar: wide || phone
                  ? null
                  : AppBar(
                      backgroundColor: _bgPrimary,
                      surfaceTintColor: Colors.transparent,
                      scrolledUnderElevation: 0,
                      titleSpacing: 0,
                      title: const _BrandLockup(logoSize: 26),
                      actions: <Widget>[
                        _CameraStatusChip(controller: controller),
                      ],
                      shape: Border(bottom: BorderSide(color: _border)),
                    ),
              drawer: wide || phone ? null : Drawer(backgroundColor: _bgSecondary, shape: const RoundedRectangleBorder(), child: sidebar),
              bottomNavigationBar: phone ? _MobileTabBar(controller: controller) : null,
              body: phone
                  // Phones, like NeoAgent: a slim top bar instead of the app
                  // bar, the tab bar below, pages sliding in a touch.
                  ? SafeArea(
                      bottom: false,
                      child: Column(
                        children: <Widget>[
                          _MobileTopBar(controller: controller),
                          Expanded(child: _sectionSwitcher(slide: true)),
                        ],
                      ),
                    )
                  : Row(
                      children: <Widget>[
                        if (wide) sidebar,
                        Expanded(child: _sectionSwitcher(slide: false)),
                      ],
                    ),
            ),
          ),
          Positioned(left: 0, right: 0, bottom: floatingBottom, child: Center(child: _UndoBar(center: controller.undo))),
          Positioned(right: 20, bottom: floatingBottom, child: _UploadPanel(controller: controller)),
        ],
      ),
      ),
    );
  }
}

/// The count beside a section in the sidebar and the phone's chips: the
/// suggestions waiting in Review.
Widget? _sectionBadge(NeoCloudController controller, AppSection section) {
  if (section != AppSection.review) return null;
  final count = controller.aiReview.counts.total;
  return count > 0 ? _CountBadge(count: count) : null;
}

/// A small neutral count, like NeoRouter's nav counters.
class _CountBadge extends StatelessWidget {
  const _CountBadge({required this.count});

  final int count;

  @override
  Widget build(BuildContext context) {
    return Container(
      constraints: const BoxConstraints(minWidth: 20),
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.pill), border: Border.all(color: _border)),
      child: Text(
        count > 999 ? '999+' : '$count',
        textAlign: TextAlign.center,
        style: GoogleFonts.geist(fontSize: 11, fontWeight: FontWeight.w600, color: _textSecondary),
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({required this.controller, required this.groups, required this.expanded, required this.onToggle});

  final NeoCloudController controller;
  final List<_NavGroup> groups;
  final SidebarGroup? expanded;
  final ValueChanged<SidebarGroup> onToggle;

  @override
  Widget build(BuildContext context) {
    void go(AppSection section) {
      controller.setSelectedSection(section);
      if (Scaffold.maybeOf(context)?.isDrawerOpen == true) Navigator.of(context).pop();
    }

    final current = controller.selectedSection;
    final items = <Widget>[];
    for (final nav in groups) {
      final parent = nav.sections.first;
      final hasChildren = nav.sections.length > 1;
      final open = hasChildren && expanded == nav.group;
      // As in NeoRouter and NeoAgent: the group lights up with its first page.
      final active = current == parent;
      items.add(_SidebarButton(
        label: nav.group.label,
        icon: nav.group.icon,
        active: active,
        trailing: hasChildren
            ? Icon(open ? Icons.expand_less : Icons.expand_more, size: 16, color: active ? _textSecondary : _textMuted)
            : null,
        onTap: hasChildren ? () => onToggle(nav.group) : () => go(parent),
      ));
      if (hasChildren && open) {
        items.add(_SidebarSubnav(
          children: <Widget>[
            for (final section in nav.sections)
              _SidebarButton(
                label: section.label,
                icon: section.icon,
                compact: true,
                active: current == section,
                trailing: _sectionBadge(controller, section),
                onTap: () => go(section),
              ),
          ],
        ));
      }
    }

    return Container(
      width: 276,
      decoration: BoxDecoration(color: _bgSecondary, border: Border(right: BorderSide(color: _border))),
      child: SafeArea(
        child: Column(
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(18, 18, 16, 14),
              child: Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(18),
                  border: Border.all(color: _border),
                  color: _bgTertiary,
                ),
                child: Row(
                  children: <Widget>[
                    const _BrandLockup(logoSize: 38, showName: false),
                    const SizedBox(width: 11),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: <Widget>[
                          Text(
                            appStrings.appName,
                            style: GoogleFonts.geist(fontSize: 17, fontWeight: FontWeight.w700, color: _textPrimary, letterSpacing: -0.35),
                          ),
                          const SizedBox(height: 3),
                          Text(
                            appStrings.appEyebrow,
                            style: GoogleFonts.geistMono(fontSize: 9.5, fontWeight: FontWeight.w600, color: _textMuted, letterSpacing: 1.8),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
            Expanded(child: ListView(padding: const EdgeInsets.fromLTRB(12, 2, 12, 10), children: items)),
            if (controller.storage != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 14),
                child: _StorageMeter(usage: controller.storage!, onTap: () {
                  controller.setSettingsPage(SettingsPage.storage);
                  go(AppSection.settings);
                }),
              ),
            Padding(
              padding: const EdgeInsets.fromLTRB(14, 0, 14, 14),
              child: _SidebarSettingsButton(
                label: controller.accountLabel.isEmpty ? appStrings.navSettings : controller.accountLabel,
                active: current == AppSection.settings,
                onTap: () => go(AppSection.settings),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// How full the account is, above the account button. Neutral until nearly
/// full, like NeoRouter's usage bars.
class _StorageMeter extends StatelessWidget {
  const _StorageMeter({required this.usage, required this.onTap});

  final StorageUsage usage;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final used = formatBytes(usage.usedBytes);
    final label = usage.unlimited ? appStrings.storageUsedUnlimited(used) : appStrings.storageUsedOf(used, formatBytes(usage.quotaBytes));
    final fraction = usage.fraction;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(AppRadius.control),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 2),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(Icons.cloud_outlined, size: 15, color: _textMuted),
                const SizedBox(width: 7),
                Text(appStrings.storageLabel, style: TextStyle(color: _textSecondary, fontSize: 12.5, fontWeight: FontWeight.w500)),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    label,
                    textAlign: TextAlign.right,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: _textMuted, fontSize: 12),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            ClipRRect(
              borderRadius: BorderRadius.circular(AppRadius.pill),
              child: LinearProgressIndicator(
                minHeight: 6,
                value: usage.unlimited ? 0 : fraction,
                backgroundColor: _bgTertiary,
                valueColor: AlwaysStoppedAnimation<Color>(fraction >= 0.9 ? _warning : _textSecondary),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _SidebarSettingsButton extends StatelessWidget {
  const _SidebarSettingsButton({required this.label, required this.active, required this.onTap});

  final String label;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final initial = label.trim().isEmpty ? 'N' : label.trim().characters.first.toUpperCase();
    final radius = BorderRadius.circular(18);
    return Material(
      color: active ? _accentMuted : _bgTertiary,
      borderRadius: radius,
      child: InkWell(
        borderRadius: radius,
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.fromLTRB(10, 10, 12, 10),
          decoration: BoxDecoration(
            borderRadius: radius,
            border: Border.all(color: active ? _accent.withValues(alpha: 0.35) : _border),
          ),
          child: Row(
            children: <Widget>[
              Container(
                width: 30,
                height: 30,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: active ? _accent : _bgSecondary,
                  border: Border.all(color: active ? _accent : _borderLight),
                ),
                alignment: Alignment.center,
                child: Text(
                  initial,
                  style: TextStyle(color: active ? _onAccent : _textPrimary, fontWeight: FontWeight.w700, fontSize: 12),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: GoogleFonts.geist(color: _textPrimary, fontSize: 12.5, fontWeight: FontWeight.w600, letterSpacing: -0.1),
                    ),
                    Text(appStrings.navSettings, style: TextStyle(color: _textMuted, fontSize: 12)),
                  ],
                ),
              ),
              Icon(Icons.settings_outlined, size: 17, color: active ? _accent : _textSecondary),
            ],
          ),
        ),
      ),
    );
  }
}

class _SectionBody extends StatelessWidget {
  const _SectionBody({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    switch (controller.selectedSection) {
      case AppSection.files:
        return FilesPanel(controller: controller);
      case AppSection.review:
        return AiReviewPage(controller: controller);
      case AppSection.shared:
        return controller.openSharedFolderId == null
            ? SharedWithMePanel(controller: controller)
            : FilesPanel(key: const ValueKey<String>('shared-folder'), controller: controller);
      case AppSection.favorites:
        return FavoritesPanel(controller: controller);
      case AppSection.recent:
        return RecentPanel(controller: controller);
      case AppSection.trash:
        return TrashPanel(controller: controller);
      case AppSection.settings:
        return SettingsPanel(controller: controller);
      case AppSection.admin:
        return controller.isAdmin ? AdminPanel(controller: controller) : FilesPanel(controller: controller);
    }
  }
}
