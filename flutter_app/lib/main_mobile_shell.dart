part of 'main.dart';

// The shell on phones (below 640px), NeoAgent's phone frame
// (main_app_shell.dart): a slim top bar, the sections of the open group as a
// chip row under it, and one bottom tab per sidebar group, in place of the
// drawer. Photos sits right after Files, like the main tab of a gallery app;
// it also carries camera backup's status (the old Camera tab), whose
// settings stay under Settings › Camera upload. 640–980px keeps the drawer.

enum _PhoneTab { files, photos, settings, admin }

List<_PhoneTab> _phoneTabs(NeoCloudController controller) => <_PhoneTab>[
  _PhoneTab.files,
  _PhoneTab.photos,
  _PhoneTab.settings,
  if (controller.isAdmin) _PhoneTab.admin,
];

_PhoneTab _currentPhoneTab(NeoCloudController controller) {
  final section = controller.selectedSection;
  if (section == AppSection.photos) return _PhoneTab.photos;
  if (section == AppSection.settings) return _PhoneTab.settings;
  if (section == AppSection.admin) return _PhoneTab.admin;
  return _PhoneTab.files;
}

void _openPhoneTab(NeoCloudController controller, _PhoneTab tab) {
  switch (tab) {
    case _PhoneTab.files:
      controller.setSelectedSection(AppSection.files);
    case _PhoneTab.photos:
      controller.setSelectedSection(AppSection.photos);
    case _PhoneTab.settings:
      controller.setSelectedSection(AppSection.settings);
    case _PhoneTab.admin:
      controller.setSelectedSection(AppSection.admin);
  }
}

extension _PhoneTabInfo on _PhoneTab {
  String get label => switch (this) {
    _PhoneTab.files => SidebarGroup.files.label,
    _PhoneTab.photos => SidebarGroup.photos.label,
    _PhoneTab.settings => SidebarGroup.settings.label,
    _PhoneTab.admin => SidebarGroup.admin.label,
  };

  // NeoAgent's tab icons: the sidebar group's outlined icon, one glyph for
  // both states; the colour carries the selection. Settings is NeoAgent's
  // sliders.
  IconData get icon => switch (this) {
    _PhoneTab.files => SidebarGroup.files.icon,
    _PhoneTab.photos => SidebarGroup.photos.icon,
    _PhoneTab.settings => Icons.tune,
    _PhoneTab.admin => SidebarGroup.admin.icon,
  };
}

/// The phone's header (NeoAgent's _MobileTopBar): the mark and name, then
/// camera upload's status and the QR scanner; below it the chips of the
/// open group.
class _MobileTopBar extends StatelessWidget {
  const _MobileTopBar({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final group = controller.selectedSection.group;
    final sections = _groupSections(controller, group);
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 8, 4),
          child: Row(
            children: <Widget>[
              const NeoCloudMark(size: 30),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  appStrings.appName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: GoogleFonts.geist(fontSize: 16, fontWeight: FontWeight.w700, color: _textPrimary, letterSpacing: -0.4),
                ),
              ),
              // On Photos, backup's status includes "off · Turn on".
              if (controller.selectedSection == AppSection.photos)
                Padding(padding: const EdgeInsets.only(right: 8), child: _PhotosBackupStatus(controller: controller))
              else
                _CameraStatusChip(controller: controller),
              if (qrScannerSupported && controller.usesDeviceLogin)
                IconButton(
                  tooltip: appStrings.scanTitle,
                  onPressed: () => unawaited(_scanQrCode(context, controller)),
                  icon: Icon(Icons.qr_code_scanner_rounded, color: _textSecondary),
                ),
            ],
          ),
        ),
        if (sections.length > 1)
          _MobileSectionChips(
            controller: controller,
            sections: sections,
            selected: controller.selectedSection,
            onSelect: controller.setSelectedSection,
          ),
      ],
    );
  }
}

/// The sections of the open group as a horizontally scrolling chip row
/// (NeoAgent's _MobileSectionChips): every page of the group stays one tap
/// away and visible.
class _MobileSectionChips extends StatelessWidget {
  const _MobileSectionChips({required this.controller, required this.sections, required this.selected, required this.onSelect});

  final NeoCloudController controller;
  final List<AppSection> sections;
  final AppSection selected;
  final ValueChanged<AppSection> onSelect;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      // Grows with the text so a large font scale doesn't clip the chips.
      height: 36 + 16 * MediaQuery.textScalerOf(context).scale(1),
      child: ListView.separated(
        key: const ValueKey<String>('mobile-section-chips'),
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
        itemCount: sections.length,
        separatorBuilder: (_, _) => const SizedBox(width: 8),
        itemBuilder: (context, index) {
          final section = sections[index];
          return _SectionChip(section: section, active: section == selected, badge: _sectionBadge(controller, section), onTap: () => onSelect(section));
        },
      ),
    );
  }
}

class _SectionChip extends StatelessWidget {
  const _SectionChip({required this.section, required this.active, required this.onTap, this.badge});

  final AppSection section;
  final bool active;
  final Widget? badge;
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
              Icon(section.icon, size: 15, color: active ? _accent : _textMuted),
              const SizedBox(width: 7),
              Text(
                section.label,
                style: GoogleFonts.geist(fontSize: 12.5, fontWeight: FontWeight.w600, color: active ? _accentHover : _textSecondary),
              ),
              if (badge != null) ...<Widget>[const SizedBox(width: 7), badge!],
            ],
          ),
        ),
      ),
    );
  }
}

/// One tab per phone destination (NeoAgent's _MobileTabBar).
class _MobileTabBar extends StatelessWidget {
  const _MobileTabBar({required this.controller});

  final NeoCloudController controller;

  @override
  Widget build(BuildContext context) {
    final tabs = _phoneTabs(controller);
    final selected = _currentPhoneTab(controller);
    return Container(
      decoration: BoxDecoration(
        color: _bgPrimary,
        border: Border(top: BorderSide(color: _border)),
      ),
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(8, 8, 8, 6),
          child: Row(
            children: tabs
                .map(
                  (tab) => Expanded(
                    child: _MobileTab(
                      tab: tab,
                      active: tab == selected,
                      onTap: () => _openPhoneTab(controller, tab),
                    ),
                  ),
                )
                .toList(growable: false),
          ),
        ),
      ),
    );
  }
}

class _MobileTab extends StatelessWidget {
  const _MobileTab({required this.tab, required this.active, required this.onTap});

  final _PhoneTab tab;
  final bool active;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = active ? _accent : _textMuted;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 7),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Icon(tab.icon, size: 21, color: color),
              const SizedBox(height: 5),
              Text(
                tab.label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: GoogleFonts.geist(
                  fontSize: 10.5,
                  fontWeight: FontWeight.w600,
                  color: color,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
