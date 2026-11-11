part of 'main.dart';

/// Flat placeholder rows (or tiles) while a slow folder loads; shown only
/// after a short delay so fast folders never flash it.
Widget _skeletonSliver({required bool grid, required int columns, required double tileExtent}) {
  Widget bar(double width, double height) => Container(
    width: width,
    height: height,
    decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(AppRadius.tag)),
  );
  if (grid) {
    return SliverGrid(
      gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: columns, mainAxisSpacing: 12, crossAxisSpacing: 12, mainAxisExtent: tileExtent),
      delegate: SliverChildBuilderDelegate(
        (context, index) => Container(
          padding: const EdgeInsets.all(14),
          decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[Expanded(child: bar(double.infinity, double.infinity)), const SizedBox(height: 12), bar(110, 12), const SizedBox(height: 6), bar(60, 10)],
          ),
        ),
        childCount: columns * 2,
      ),
    );
  }
  return DecoratedSliver(
    decoration: BoxDecoration(color: _bgCard, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: _border)),
    sliver: SliverFixedExtentList(
      itemExtent: _rowExtent,
      delegate: SliverChildBuilderDelegate(
        (context, index) => Container(
          padding: const EdgeInsets.fromLTRB(10, 7, 8, 7),
          decoration: BoxDecoration(border: index == 0 ? null : Border(top: BorderSide(color: _border))),
          child: Row(
            children: <Widget>[
              bar(34, 34),
              const SizedBox(width: 14),
              Expanded(child: Align(alignment: Alignment.centerLeft, child: bar(140.0 + (index * 37) % 120, 12))),
              bar(60, 10),
              const SizedBox(width: 40),
            ],
          ),
        ),
        childCount: 8,
      ),
    ),
  );
}

/// First run: an empty Home explains the three things to do first.
class _OnboardingCard extends StatelessWidget {
  const _OnboardingCard({required this.controller, required this.onUpload, required this.onNewFolder});

  final NeoCloudController controller;
  final VoidCallback onUpload;
  final VoidCallback onNewFolder;

  Widget _step(IconData icon, String title, String body, Widget action) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Row(
        children: <Widget>[
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(color: _bgTertiary, shape: BoxShape.circle),
            child: Icon(icon, size: 18, color: _textSecondary),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(title, style: _sectionTitleStyle()),
                const SizedBox(height: 2),
                Text(body, style: TextStyle(color: _textSecondary, fontSize: 13, height: 1.4)),
              ],
            ),
          ),
          const SizedBox(width: 12),
          action,
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return _EntranceMotion(
      child: _AppCard(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(appStrings.filesWelcomeTitle, style: _displayTitleStyle(18)),
            const SizedBox(height: 4),
            Text(appStrings.filesWelcomeSubtitle, style: TextStyle(color: _textSecondary, fontSize: 14, height: 1.45)),
            const SizedBox(height: 10),
            _step(
              Icons.upload_rounded,
              appStrings.filesWelcomeUploadTitle,
              appStrings.filesWelcomeUploadBody,
              Wrap(spacing: 8, children: <Widget>[
                OutlinedButton(onPressed: onNewFolder, child: Text(appStrings.newFolder)),
                FilledButton(onPressed: onUpload, child: Text(appStrings.upload)),
              ]),
            ),
            Divider(height: 1, color: _border),
            _step(
              Icons.computer_rounded,
              appStrings.filesWelcomeDesktopTitle,
              appStrings.filesWelcomeDesktopBody,
              OutlinedButton(
                onPressed: () => unawaited(launchUrl(controller.client.uri('/'), webOnlyWindowName: '_blank')),
                child: Text(appStrings.filesWelcomeDesktopAction),
              ),
            ),
            if (!controller.usesDeviceLogin) ...<Widget>[
              Divider(height: 1, color: _border),
              _step(
                Icons.move_to_inbox_outlined,
                appStrings.filesWelcomeImportTitle,
                appStrings.filesWelcomeImportBody,
                OutlinedButton(
                  onPressed: () {
                    controller.setSettingsPage(SettingsPage.imports);
                    controller.setSelectedSection(AppSection.settings);
                  },
                  child: Text(appStrings.filesWelcomeImportAction),
                ),
              ),
            ],
            if (controller.isAdmin) ...<Widget>[
              Divider(height: 1, color: _border),
              _step(
                Icons.key_rounded,
                appStrings.filesWelcomeKeyTitle,
                appStrings.filesWelcomeKeyBody,
                OutlinedButton(onPressed: () => controller.setSelectedSection(AppSection.admin), child: Text(appStrings.filesWelcomeKeyAction)),
              ),
            ],
          ],
        ),
      ),
    );
  }
}
