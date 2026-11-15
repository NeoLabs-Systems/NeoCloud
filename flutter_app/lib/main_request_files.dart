part of 'main.dart';

// "Request files": a link anyone can use to send files into a folder without
// seeing what is in it (a file drop). One tap makes it (or finds the one the
// folder already has), copies it and offers the options.

/// A request link works for a month unless the options say otherwise.
const Duration _requestFilesExpiry = Duration(days: 30);

Future<void> requestFilesInto(BuildContext context, NeoCloudController controller, FileNode folder) async {
  final messenger = ScaffoldMessenger.maybeOf(context);
  try {
    final existing = (await controller.client.shares(nodeId: folder.id)).where((link) => link.nodeId == folder.id && link.active && link.mode == ShareMode.uploadOnly);
    final link = existing.isNotEmpty
        ? existing.first
        : await controller.client.createShare(folder.id, mode: ShareMode.uploadOnly, expiresAt: DateTime.now().add(_requestFilesExpiry));
    await Clipboard.setData(ClipboardData(text: link.url));
    controller.notifyFilesChanged();
    messenger?.showSnackBar(
      SnackBar(
        content: Text(appStrings.requestFilesCopied(folder.name)),
        action: SnackBarAction(
          label: appStrings.requestFilesOptions,
          onPressed: () {
            if (context.mounted) unawaited(showEditShareLinkDialog(context, controller, link));
          },
        ),
      ),
    );
  } catch (error) {
    messenger?.showSnackBar(SnackBar(content: Text(controller.friendlyError(error))));
  }
}
