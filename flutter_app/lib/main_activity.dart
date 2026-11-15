part of 'main.dart';

/// The Activity page: what happened to the account's files and to the files
/// shared with it. Reached from the sidebar's Files group.
class ActivityPanel extends StatefulWidget {
  const ActivityPanel({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<ActivityPanel> createState() => _ActivityPanelState();
}

class _ActivityPanelState extends State<ActivityPanel> {
  ActivityScope _scope = ActivityScope.all;

  String _label(ActivityScope scope) {
    switch (scope) {
      case ActivityScope.all:
        return appStrings.activityScopeAll;
      case ActivityScope.mine:
        return appStrings.activityScopeMine;
      case ActivityScope.shared:
        return appStrings.activityScopeShared;
    }
  }

  @override
  Widget build(BuildContext context) {
    return _PageScaffold(
      children: <Widget>[
        _PageTitle(title: appStrings.activityTitle, subtitle: appStrings.activitySubtitle),
        Padding(
          padding: const EdgeInsets.only(bottom: 12),
          child: Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              for (final scope in ActivityScope.values)
                ChoiceChip(
                  label: Text(_label(scope)),
                  selected: _scope == scope,
                  showCheckmark: false,
                  onSelected: (_) => setState(() => _scope = scope),
                ),
            ],
          ),
        ),
        _AppCard(child: ActivityFeed(controller: widget.controller, scope: _scope)),
      ],
    );
  }
}
