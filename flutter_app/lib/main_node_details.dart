part of 'main.dart';

/// Comments and activity of one file or folder, in the adaptive dialog (a
/// bottom sheet on phones). Opened from the item's menu.
Future<void> showNodeDetails(BuildContext context, NeoCloudController controller, FileNode node) {
  return showAdaptiveSheet<void>(
    context: context,
    builder: (dialogContext) => _NodeDetailsDialog(controller: controller, node: node),
  );
}

class _NodeDetailsDialog extends StatelessWidget {
  const _NodeDetailsDialog({required this.controller, required this.node});

  final NeoCloudController controller;
  final FileNode node;

  @override
  Widget build(BuildContext context) {
    final height = math.min(MediaQuery.sizeOf(context).height * 0.6, 520.0);
    return DefaultTabController(
      length: 2,
      child: _AdaptiveDialog(
        title: Text(node.name, maxLines: 2, overflow: TextOverflow.ellipsis),
        content: SizedBox(
          width: 480,
          height: height,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TabBar(
                tabs: <Widget>[Tab(text: appStrings.commentsTitle), Tab(text: appStrings.activityTitle)],
              ),
              const SizedBox(height: 12),
              Expanded(
                child: TabBarView(
                  children: <Widget>[
                    SingleChildScrollView(child: CommentsSection(controller: controller, node: node)),
                    SingleChildScrollView(child: ActivityFeed(controller: controller, nodeId: node.id)),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close))],
      ),
    );
  }
}
