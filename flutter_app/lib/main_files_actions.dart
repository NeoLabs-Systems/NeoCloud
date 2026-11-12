part of 'main.dart';

/// "a.txt, b.txt and 3 more" for messages about several items.
String _itemNames(List<FileNode> nodes) {
  const shown = 3;
  final names = nodes.take(shown).map((node) => '“${node.name}”').join(', ');
  return nodes.length > shown ? appStrings.filesAndMore(names, nodes.length - shown) : names;
}

/// What can be done to items. Everything reversible applies at once, shows
/// in the listing before the server answers, and offers Undo; what fails is
/// put back and named, with Retry.
mixin _FilesActions on _FilesBase {
  UploadCipher? get _uploadCipher {
    final vault = vaultId;
    return vault == null ? null : controller.vaults.cipherFor(vault);
  }

  /// A name as the server should store it: encrypted inside a vault.
  Future<String> storedName(String name, {String? vault}) async {
    final target = vault ?? vaultId;
    return target == null ? name : controller.vaults.encryptName(target, name);
  }

  void _announceUndo(String message, {Future<void> Function()? onUndo}) {
    controller.undo.show(UndoEntry(message: message, onUndo: onUndo));
  }

  void _failed(String Function(String names) message, List<BatchFailure<FileNode>> failures, VoidCallback retry) {
    if (!mounted) return;
    final nodes = failures.map((failure) => failure.item).toList();
    final reason = controller.friendlyError(failures.first.error);
    setError(_FilesError('${message(_itemNames(nodes))} $reason', retry: retry));
  }

  // Uploads

  Future<void> pickAndUpload() async {
    if (!canWrite) return;
    final result = await FilePicker.platform.pickFiles(allowMultiple: true, withReadStream: true);
    if (result == null) return;
    final sources = <UploadSource>[
      for (final file in result.files)
        if (file.readStream != null) UploadSource(name: file.name, size: file.size, openRead: () => file.readStream!),
    ];
    controller.uploads.enqueue(currentFolderId, sources, cipher: _uploadCipher);
  }

  /// Dropped files and whole folder trees; folders are recreated on the
  /// server as their files arrive.
  Future<void> uploadDropped(List<DropItem> dropped) async {
    final sources = <UploadSource>[];
    Future<void> collect(DropItem item, String folder) async {
      if (item is DropItemDirectory) {
        final path = folder.isEmpty ? item.name : '$folder/${item.name}';
        for (final child in item.children) {
          await collect(child, path);
        }
        return;
      }
      sources.add(UploadSource(
        name: item.name,
        size: await item.length(),
        openRead: item.openRead,
        folder: folder,
        modifiedAt: await item.lastModified(),
      ));
    }

    if (vaultLocked || !canWrite) return;
    for (final item in dropped) {
      await collect(item, '');
    }
    controller.uploads.enqueue(currentFolderId, sources, cipher: _uploadCipher);
  }

  // Creating

  Future<void> newFolder() async {
    if (!canWrite) return;
    final name = await _promptText(context, title: appStrings.newFolder, label: appStrings.folderName, confirmLabel: appStrings.create);
    if (name == null) return;
    await guard(() async {
      await controller.client.createFolder(currentFolderId, await storedName(name));
      await load(quiet: true);
    });
  }

  Future<void> newVault() async {
    if (inShare) return;
    final folder = await showNewVaultDialog(context, controller, parentId: currentFolderId);
    if (folder != null) controller.openFolder(folder.id);
  }

  // Opening

  void open(FileNode node) {
    if (node.isFolder) {
      if (searching) clearSearch();
      goToFolder(node.id);
      return;
    }
    final files = visibleItems.where((n) => !n.isFolder).toList();
    unawaited(showFileViewer(context, controller, files, files.indexWhere((n) => n.id == node.id), readOnly: !canWrite));
  }

  void openContainingFolder(FileNode node) {
    clearSearch();
    controller.openFolder(node.parentId);
  }

  void download(List<FileNode> nodes) {
    unawaited(guard(() async {
      // Vault files are decrypted one by one; a ZIP would hold ciphertext.
      if (nodes.any((node) => node.inVault)) {
        for (final node in nodes.where((node) => !node.isFolder)) {
          await controller.downloadFile(node);
        }
        return;
      }
      if (nodes.length == 1 && !nodes.first.isFolder) {
        await controller.downloadFile(nodes.first);
        return;
      }
      await controller.downloadZip(nodes);
    }));
  }

  // Trash

  /// Moves [nodes] to the trash right away; Undo restores them.
  Future<void> trash(List<FileNode> nodes) async {
    if (nodes.isEmpty || !canWrite) return;
    final before = visibleItems;
    final ids = nodes.map((node) => node.id).toSet();
    selection.clear();
    updateItems((list) => list.where((node) => !ids.contains(node.id)).toList());
    final failures = await runBatch(nodes, (node) => controller.client.trashNode(node.id));
    _putBack(failures.map((failure) => failure.item).toList(), before);
    final done = nodes.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
    if (done.isNotEmpty) {
      _announceUndo(appStrings.undoTrashed(done.length, done.first.name), onUndo: () => _restore(done));
      controller.notifyFilesChanged();
    }
    if (failures.isNotEmpty) {
      _failed(appStrings.filesTrashFailed, failures, () => unawaited(trash(failures.map((failure) => failure.item).toList())));
    }
  }

  Future<void> _restore(List<FileNode> nodes) async {
    final failures = await runBatch(nodes, (node) => controller.client.restoreFromTrash(node.id));
    controller.notifyFilesChanged();
    if (failures.isNotEmpty) _failed(appStrings.filesRestoreFailed, failures, () => unawaited(_restore(failures.map((f) => f.item).toList())));
  }

  /// Puts [failed] items back where they were in [before].
  void _putBack(List<FileNode> failed, List<FileNode> before) {
    if (failed.isEmpty || !mounted) return;
    final ids = failed.map((node) => node.id).toSet();
    updateItems((list) {
      final present = list.map((node) => node.id).toSet();
      final restored = <FileNode>[
        for (final node in before)
          if (present.contains(node.id) || ids.contains(node.id)) node,
      ];
      return restored.length >= list.length ? restored : list;
    });
  }

  // Moving

  /// Moves [nodes] into [targetId]; used by drag & drop, cut & paste and the
  /// folder picker. Undo moves each back where it came from.
  Future<void> moveTo(List<FileNode> nodes, String targetId, String targetName) async {
    final moving = nodes.where((node) => node.id != targetId && node.parentId != targetId).toList();
    if (moving.isEmpty || !canWrite) return;
    final before = visibleItems;
    final ids = moving.map((node) => node.id).toSet();
    selection.clear();
    // Leaving this folder: gone from the listing. In search results the item
    // stays, now in its new folder.
    updateItems((list) => searching
        ? <FileNode>[for (final node in list) ids.contains(node.id) ? node.copyWith(parentId: targetId) : node]
        : list.where((node) => !ids.contains(node.id)).toList());
    final failures = await runBatch(moving, (node) => controller.client.updateNode(node.id, parentId: targetId));
    _putBack(failures.map((failure) => failure.item).toList(), before);
    final done = moving.where((node) => !failures.any((failure) => failure.item.id == node.id)).toList();
    if (done.isNotEmpty) {
      _announceUndo(appStrings.undoMoved(done.length, targetName), onUndo: () => _moveBack(done));
      controller.notifyFilesChanged();
    }
    if (failures.isNotEmpty) {
      _failed(
        (names) => appStrings.filesMoveFailed(names, targetName),
        failures,
        () => unawaited(moveTo(failures.map((failure) => failure.item).toList(), targetId, targetName)),
      );
    }
  }

  Future<void> _moveBack(List<FileNode> nodes) async {
    final failures = await runBatch(nodes, (node) => controller.client.updateNode(node.id, parentId: node.parentId ?? 'root'));
    controller.notifyFilesChanged();
    if (failures.isNotEmpty) _failed(appStrings.filesUndoFailed, failures, () => unawaited(_moveBack(failures.map((f) => f.item).toList())));
  }

  Future<void> move(List<FileNode> nodes) async {
    if (inShare) return;
    final target = await showFolderPicker(context, controller, exclude: nodes.map((n) => n.id).toSet());
    if (target == null) return;
    await moveTo(nodes, target.$1, target.$2);
  }

  /// Ctrl/⌘+X: remembers what to move; Ctrl/⌘+V in another folder moves it.
  void cut(List<FileNode> nodes) {
    if (nodes.isEmpty || !canWrite) return;
    setState(() => _cutNodes = List<FileNode>.unmodifiable(nodes));
    _announceUndo(appStrings.filesCut(nodes.length));
  }

  Future<void> paste() async {
    final nodes = _cutNodes;
    if (nodes.isEmpty || searching || vaultLocked || !canWrite) return;
    setState(() => _cutNodes = const <FileNode>[]);
    await moveTo(nodes, currentFolderId, folderName);
  }

  // Renaming and copying

  Future<void> rename(FileNode node) async {
    if (!canWrite) return;
    holdLiveUpdates(renameHold, true);
    try {
      final name = await _promptText(
        context,
        title: appStrings.rename,
        label: appStrings.newName,
        confirmLabel: appStrings.rename,
        initialValue: node.name,
        selectStem: !node.isFolder,
      );
      if (name == null || name == node.name || !mounted) return;
      await renameTo(node, name);
    } finally {
      holdLiveUpdates(renameHold, false);
    }
  }

  Future<void> renameTo(FileNode node, String name) async {
    updateItems((list) => <FileNode>[for (final item in list) item.id == node.id ? item.copyWith(name: name) : item]);
    try {
      // The vault folder itself keeps a plain name; what's inside is encrypted.
      final encrypt = node.inVault && !await controller.vaults.isVaultRoot(node);
      await controller.client.updateNode(node.id, name: encrypt ? await storedName(name, vault: node.vaultId) : name);
      _announceUndo(appStrings.undoRenamed(name), onUndo: () => _renameBack(node, encrypt));
      controller.notifyFilesChanged();
    } catch (failure) {
      if (!mounted) return;
      updateItems((list) => <FileNode>[for (final item in list) item.id == node.id ? node : item]);
      setError(_FilesError(
        '${appStrings.filesRenameFailed(node.name)} ${controller.friendlyError(failure)}',
        retry: () => unawaited(renameTo(node, name)),
      ));
    }
  }

  Future<void> _renameBack(FileNode node, bool encrypted) async {
    await guard(() async {
      await controller.client.updateNode(node.id, name: encrypted ? (node.cipherName ?? await storedName(node.name, vault: node.vaultId)) : node.name);
      controller.notifyFilesChanged();
    });
  }

  Future<void> copy(FileNode node) => guard(() async {
    // The server's "name (1)" would garble an encrypted name.
    final inside = node.inVault && !await controller.vaults.isVaultRoot(node);
    final taken = visibleItems.map((item) => item.name).toSet();
    final copied = await controller.client.copyNode(
      node.id,
      name: inside ? await storedName(nextFreeName(node.name, taken, isFolder: node.isFolder), vault: node.vaultId) : null,
    );
    controller.notifyFilesChanged();
    _announceUndo(appStrings.copied2, onUndo: () => trashQuietly(copied));
  });

  /// A copy of a shared item in the account's own files (on its own quota).
  Future<void> saveToMyFiles(FileNode node) => guard(() async {
    final copied = await controller.client.copyNode(node.id, parentId: 'root');
    controller.notifyFilesChanged();
    _announceUndo(appStrings.sharedSavedToMyFiles(node.name), onUndo: () => trashQuietly(copied));
  });

  Future<void> trashQuietly(FileNode node) => guard(() async {
    await controller.client.trashNode(node.id);
    controller.notifyFilesChanged();
  });

  // Favorites

  /// Stars or unstars [nodes] (all get the state the first one doesn't have).
  Future<void> toggleFavorite(List<FileNode> nodes) async {
    if (inShare) return;
    if (nodes.isEmpty) return;
    final favorite = !nodes.first.favorite;
    final ids = nodes.map((node) => node.id).toSet();
    updateItems((list) => <FileNode>[for (final item in list) ids.contains(item.id) ? item.copyWith(favorite: favorite) : item]);
    final failures = await runBatch(nodes, (node) => controller.client.setFavorite(node.id, favorite));
    if (failures.isEmpty) {
      _announceUndo(favorite ? appStrings.filesStarred(nodes.length) : appStrings.filesUnstarred(nodes.length));
      return;
    }
    final failed = failures.map((failure) => failure.item.id).toSet();
    updateItems((list) => <FileNode>[for (final item in list) failed.contains(item.id) ? item.copyWith(favorite: !favorite) : item]);
    _failed(appStrings.filesFavoriteFailed, failures, () => unawaited(toggleFavorite(failures.map((f) => f.item).toList())));
  }

  // Menus

  /// What an action applies to: the selection if [node] is in it, else just [node].
  List<FileNode> subjectFor(FileNode node) {
    if (selection.isSelected(node.id)) return selectedNodes;
    return <FileNode>[node];
  }

  /// What can be done to [node]. In a shared folder: no links, stars or
  /// versions (they stay the owner's), changes only with write access, and
  /// a copy into the account's own files instead of "Move to".
  List<_FileAction> actionsFor(FileNode node) {
    final vault = node.inVault;
    final own = !inShare;
    return <_FileAction>[
      _FileAction(node.isFolder ? Icons.folder_open_outlined : Icons.open_in_new_rounded, appStrings.open, () => open(node), shortcut: '↵'),
      if (searching) _FileAction(Icons.drive_folder_upload_outlined, appStrings.searchOpenFolder, () => openContainingFolder(node), quick: true),
      if (!vault || !node.isFolder)
        _FileAction(Icons.download_rounded, node.isFolder ? appStrings.downloadZip : appStrings.download, () => download(subjectFor(node)), quick: true),
      if (own && !vault) _FileAction(Icons.share_outlined, appStrings.shareLink, () => unawaited(showShareLinkDialog(context, controller, node)), quick: true),
      if (own)
        _FileAction(
          node.favorite ? Icons.star_rounded : Icons.star_outline_rounded,
          node.favorite ? appStrings.filesUnstar : appStrings.filesStar,
          () => unawaited(toggleFavorite(subjectFor(node))),
          quick: true,
        ),
      if (canWrite) _FileAction(Icons.drive_file_rename_outline_rounded, appStrings.rename, () => unawaited(rename(node)), shortcut: 'F2'),
      if (own) _FileAction(Icons.drive_file_move_outline, appStrings.moveTo, () => unawaited(move(subjectFor(node)))),
      if (canWrite) _FileAction(Icons.content_cut_rounded, appStrings.filesCutAction, () => cut(subjectFor(node)), shortcut: _shortcutLabel('X')),
      if (canWrite) _FileAction(Icons.copy_rounded, appStrings.makeCopy, () => unawaited(copy(node))),
      if (!own) _FileAction(Icons.drive_file_move_rtl_outlined, appStrings.sharedSaveToMyFiles, () => unawaited(saveToMyFiles(node))),
      if (own && !node.isFolder && !vault) _FileAction(Icons.history_rounded, appStrings.versions, () => unawaited(showVersionsDialog(context, controller, node))),
      if (canWrite) _FileAction(Icons.delete_outline_rounded, appStrings.moveToTrash, () => unawaited(trash(subjectFor(node))), danger: true, quick: true, shortcut: 'Del'),
    ];
  }
}

/// "⌘X" on Apple platforms, "Ctrl+X" elsewhere.
String _shortcutLabel(String key) {
  final apple = defaultTargetPlatform == TargetPlatform.macOS || defaultTargetPlatform == TargetPlatform.iOS;
  return apple ? '⌘$key' : 'Ctrl+$key';
}
