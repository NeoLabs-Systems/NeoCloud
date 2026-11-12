part of 'main.dart';

/// NeoAgent's "Connect NeoRouter": one sign-in sends text, vision,
/// embeddings, speech to text and images through a NeoRouter. Admin › AI
/// shows it for the whole server ([scope] 'server'); for a person's own
/// account ('user') it is the first row of Settings › AI › Bring your own
/// key, laid out like the other providers there. Each kind of work can be
/// switched off again under Manage; it then goes back to its usual provider.
class NeoRouterConnectCard extends StatefulWidget {
  const NeoRouterConnectCard({super.key, required this.controller, required this.scope, this.onChanged});

  final NeoCloudController controller;
  final String scope;

  /// After a connect, a role change or a disconnect, for pages that show
  /// what the models resolve to.
  final VoidCallback? onChanged;

  @override
  State<NeoRouterConnectCard> createState() => _NeoRouterConnectCardState();
}

class _NeoRouterConnectCardState extends State<NeoRouterConnectCard> {
  Map<String, dynamic>? _state;
  String? _loadError;
  bool _busy = false;
  Map<String, dynamic>? _pending;

  /// Bumped by every start and cancel, so a cancelled sign-in that is still
  /// polling can never touch a newer one.
  int _attempt = 0;

  bool get _server => widget.scope == 'server';

  AiApi get _ai => widget.controller.ai;

  Map<String, dynamic> get _mine => _neoMap(_state?[_server ? 'server' : 'user']);

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    try {
      final state = await _ai.neoRouter();
      if (!mounted) return;
      setState(() {
        _state = state;
        _loadError = null;
      });
    } catch (error) {
      if (mounted) setState(() => _loadError = _aiFriendlyError(widget.controller, error));
    }
  }

  void _showError(Object error) {
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(_aiFriendlyError(widget.controller, error))));
  }

  Future<void> _connect() async {
    final pending = await showAdaptiveSheet<Map<String, dynamic>>(
      context: context,
      builder: (_) => _NeoRouterConnectDialog(controller: widget.controller, scope: widget.scope),
    );
    if (pending == null || !mounted) return;
    final attempt = ++_attempt;
    bool isCurrent() => mounted && attempt == _attempt;
    setState(() => _pending = pending);
    try {
      await waitForNeoRouterConnect(_ai, pending['id']?.toString() ?? '', isActive: isCurrent);
      if (!mounted || !isCurrent()) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(appStrings.aiNeoRouterConnected)));
      await _load();
      widget.onChanged?.call();
    } catch (error) {
      if (isCurrent()) _showError(error);
    } finally {
      if (isCurrent()) setState(() => _pending = null);
    }
  }

  void _cancel() {
    setState(() {
      _attempt++;
      _pending = null;
    });
  }

  Future<void> _manage() async {
    await showAdaptiveSheet<void>(
      context: context,
      builder: (_) => _NeoRouterRolesDialog(
        controller: widget.controller,
        scope: widget.scope,
        allRoles: _neoList(_state?['roles']).map((item) => item.toString()).toList(),
        connection: _mine,
      ),
    );
    if (!mounted) return;
    await _load();
    widget.onChanged?.call();
  }

  Future<void> _disconnect() async {
    await _confirmDelete(
      context,
      title: appStrings.aiNeoRouterDisconnectTitle,
      message: appStrings.aiNeoRouterDisconnectBody,
      confirmLabel: appStrings.disconnect,
      onConfirm: () async {
        setState(() => _busy = true);
        try {
          await _ai.disconnectNeoRouter(widget.scope);
          await _load();
          widget.onChanged?.call();
        } catch (error) {
          if (mounted) _showError(error);
        } finally {
          if (mounted) setState(() => _busy = false);
        }
      },
    );
  }

  Widget _row() {
    final pending = _pending;
    if (pending != null) {
      return _SettingsRow(
        label: 'NeoRouter',
        description: appStrings.aiNeoRouterWaiting(pending['userCode']?.toString() ?? ''),
        control: Wrap(
          spacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)),
            TextButton(
              onPressed: () => unawaited(_openNeoRouterApproval(pending['url']?.toString() ?? '').catchError(_showError)),
              child: Text(appStrings.aiNeoRouterOpenAgain),
            ),
            TextButton(onPressed: _cancel, child: Text(appStrings.cancel)),
          ],
        ),
      );
    }
    if (_state == null) {
      return _SettingsRow(
        label: 'NeoRouter',
        description: _loadError,
        control: _loadError == null
            ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
            : TextButton(onPressed: () => unawaited(_load()), child: Text(appStrings.refresh)),
      );
    }
    final mine = _mine;
    if (mine['connected'] != true) {
      // The server's own NeoRouter stays unnamed here: people only learn
      // that one is in use, never where it lives.
      final serverInUse = !_server && _neoMap(_state?['server'])['connected'] == true;
      String? description;
      if (serverInUse) {
        description = appStrings.aiNeoRouterServerInUse;
      } else if (!_server) {
        description = appStrings.aiNeoRouterUserDescription;
      }
      return _SettingsRow(
        label: 'NeoRouter',
        description: description,
        control: Wrap(
          spacing: 12,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            _StatusPill(label: appStrings.aiNotSetUp, color: _textMuted),
            OutlinedButton(onPressed: _busy ? null : () => unawaited(_connect()), child: Text(appStrings.aiConnect)),
          ],
        ),
      );
    }
    final reachable = mine['offer'] is Map;
    return _SettingsRow(
      label: 'NeoRouter',
      description: reachable ? mine['address']?.toString() : appStrings.aiNeoRouterUnreachable,
      control: Wrap(
        spacing: 12,
        runSpacing: 8,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: <Widget>[
          _StatusPill(label: appStrings.aiConnected, color: reachable ? _success : _warning),
          OutlinedButton(onPressed: _busy ? null : () => unawaited(_manage()), child: Text(appStrings.aiManage)),
          IconButton(
            tooltip: appStrings.disconnect,
            onPressed: _busy ? null : () => unawaited(_disconnect()),
            icon: const Icon(Icons.delete_outline, size: 18),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    if (!_server) return _row();
    return _AiEyebrowCard(title: 'NeoRouter', description: appStrings.aiNeoRouterServerDescription, child: _row());
  }
}

/// Opens NeoRouter's approval page in the browser.
Future<void> _openNeoRouterApproval(String url) async {
  final uri = Uri.tryParse(url);
  final opened = uri != null && uri.hasScheme && await launchUrl(uri, mode: LaunchMode.externalApplication, webOnlyWindowName: '_blank');
  if (!opened) throw NeoRouterConnectException(NeoRouterConnectProblem.failed, appStrings.aiNeoRouterCouldNotOpen);
}

/// Asks for the NeoRouter address and starts its device sign-in. Pops with
/// the started sign-in (`id`, `url`, `userCode`) once NeoRouter answered
/// and its approval page is open.
class _NeoRouterConnectDialog extends StatefulWidget {
  const _NeoRouterConnectDialog({required this.controller, required this.scope});

  final NeoCloudController controller;
  final String scope;

  @override
  State<_NeoRouterConnectDialog> createState() => _NeoRouterConnectDialogState();
}

class _NeoRouterConnectDialogState extends State<_NeoRouterConnectDialog> {
  final TextEditingController _address = TextEditingController();
  bool _starting = false;
  String? _errorMessage;

  @override
  void dispose() {
    _address.dispose();
    super.dispose();
  }

  Future<void> _start() async {
    final address = _address.text.trim();
    if (address.isEmpty || _starting) return;
    setState(() {
      _starting = true;
      _errorMessage = null;
    });
    try {
      final pending = await widget.controller.ai.beginNeoRouterConnect(widget.scope, address);
      // Still waited on when the browser refuses to open: the row offers
      // "Open again".
      await _openNeoRouterApproval(pending['url']?.toString() ?? '').catchError((Object _) {});
      if (mounted) Navigator.of(context).pop(pending);
    } catch (error) {
      if (!mounted) return;
      setState(() => _errorMessage = _aiFriendlyError(widget.controller, error));
    } finally {
      if (mounted) setState(() => _starting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return _AdaptiveDialog(
      title: Text(appStrings.aiNeoRouterConnect),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              widget.scope == 'server' ? appStrings.aiNeoRouterServerDescription : appStrings.aiNeoRouterUserDescription,
              style: TextStyle(color: _textSecondary, height: 1.4),
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _address,
              autofocus: true,
              autocorrect: false,
              enabled: !_starting,
              keyboardType: TextInputType.url,
              decoration: InputDecoration(labelText: appStrings.aiNeoRouterAddress, hintText: 'https://router.example.com'),
              onSubmitted: (_) => unawaited(_start()),
            ),
            if (_errorMessage != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: _errorMessage!)],
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _starting ? null : () => Navigator.of(context).pop(), child: Text(appStrings.cancel)),
        FilledButton(
          onPressed: _starting ? null : () => unawaited(_start()),
          child: _starting
              ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
              : Text(appStrings.aiConnect),
        ),
      ],
    );
  }
}

/// The kinds of work that go through the connected NeoRouter, one switch
/// each, with how much the router has for them. Saves on every switch.
class _NeoRouterRolesDialog extends StatefulWidget {
  const _NeoRouterRolesDialog({required this.controller, required this.scope, required this.allRoles, required this.connection});

  final NeoCloudController controller;
  final String scope;
  final List<String> allRoles;
  final Map<String, dynamic> connection;

  @override
  State<_NeoRouterRolesDialog> createState() => _NeoRouterRolesDialogState();
}

class _NeoRouterRolesDialogState extends State<_NeoRouterRolesDialog> {
  late Set<String> _roles = _neoList(widget.connection['roles']).map((item) => item.toString()).toSet();
  bool _saving = false;
  String? _errorMessage;

  Future<void> _setRole(String role, bool on) async {
    final previous = _roles;
    final next = <String>{...previous};
    if (on) {
      next.add(role);
    } else {
      next.remove(role);
    }
    setState(() {
      _roles = next;
      _saving = true;
      _errorMessage = null;
    });
    try {
      await widget.controller.ai.saveNeoRouterRoles(widget.scope, next.toList());
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _roles = previous;
        _errorMessage = _aiFriendlyError(widget.controller, error);
      });
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  String _roleLabel(String role) => switch (role) {
    'chat' => appStrings.aiNeoRouterRoleChat,
    'transcription' => appStrings.aiNeoRouterRoleTranscription,
    'embeddings' => appStrings.aiNeoRouterRoleEmbeddings,
    'images' => appStrings.aiNeoRouterRoleImages,
    _ => role,
  };

  // How many models NeoRouter has for this work; when there are none, why:
  // the key may not use them yet.
  String _roleAvailability(String role, Map<String, dynamic> counts, Map<String, dynamic> notAllowed) {
    final available = _neoInt(counts[role]);
    if (available > 0) return appStrings.aiNeoRouterModelCount(available);
    final hidden = _neoInt(notAllowed[role]);
    if (hidden > 0) return appStrings.aiNeoRouterNotAllowed(hidden);
    return appStrings.aiNeoRouterNoModels;
  }

  @override
  Widget build(BuildContext context) {
    final offer = widget.connection['offer'] is Map ? _neoMap(widget.connection['offer']) : null;
    final counts = _neoMap(offer?['roles']);
    final notAllowed = _neoMap(offer?['notAllowed']);
    final String intro;
    if (offer == null) {
      intro = appStrings.aiNeoRouterUnreachable;
    } else if (widget.scope == 'server') {
      intro = appStrings.aiNeoRouterServerDescription;
    } else {
      intro = appStrings.aiNeoRouterUserDescription;
    }
    return _AdaptiveDialog(
      title: const Text('NeoRouter'),
      content: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(intro, style: TextStyle(color: _textSecondary, height: 1.4)),
            const SizedBox(height: 8),
            for (final role in widget.allRoles)
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(_roleLabel(role)),
                subtitle: offer == null ? null : Text(_roleAvailability(role, counts, notAllowed), style: TextStyle(color: _textSecondary)),
                value: _roles.contains(role),
                onChanged: _saving ? null : (value) => unawaited(_setRole(role, value)),
              ),
            if (_errorMessage != null) ...<Widget>[const SizedBox(height: 8), _InlineError(message: _errorMessage!)],
          ],
        ),
      ),
      actions: <Widget>[TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(appStrings.close))],
    );
  }
}

Map<String, dynamic> _neoMap(Object? value) => value is Map ? Map<String, dynamic>.from(value) : <String, dynamic>{};

List<Object?> _neoList(Object? value) => value is List ? List<Object?>.from(value) : const <Object?>[];

int _neoInt(Object? value) => value is num ? value.toInt() : int.tryParse('$value') ?? 0;

/// Readable text for an error from the AI routes: a sign-in that ended, a
/// server without them, or the usual mapping.
String _aiFriendlyError(NeoCloudController controller, Object error) {
  if (error is NeoRouterConnectException) {
    switch (error.problem) {
      case NeoRouterConnectProblem.cancelled:
        return appStrings.aiNeoRouterCancelled;
      case NeoRouterConnectProblem.expired:
      case NeoRouterConnectProblem.timedOut:
        return appStrings.aiNeoRouterExpired;
      case NeoRouterConnectProblem.failed:
        return error.message ?? appStrings.aiNeoRouterFailed;
    }
  }
  if (_aiRouteMissing(error)) return appStrings.aiNotAvailableBody;
  if (error is ApiException) {
    final message = _aiErrorMessage(error.code);
    if (message != null) return message;
  }
  return controller.friendlyError(error);
}

/// The AI error codes in the app's language (the server's text is English).
String? _aiErrorMessage(String? code) {
  switch (code) {
    case 'AI_DISABLED':
      return appStrings.aiErrorDisabled;
    case 'AI_NOT_OPTED_IN':
      return appStrings.aiErrorNotOptedIn;
    case 'AI_BUDGET_EXCEEDED':
      return appStrings.aiErrorBudget;
    case 'AI_MODELS_UNAVAILABLE':
      return appStrings.aiErrorNoModel;
    case 'AI_PROVIDER_ERROR':
    case 'AI_BAD_JSON':
      return appStrings.aiErrorProvider;
    case 'AI_VAULT':
      return appStrings.aiErrorVault;
    case 'AI_EXCLUDED':
      return appStrings.aiErrorExcluded;
    case 'URL_NOT_ALLOWED':
      return appStrings.aiErrorUrlNotAllowed;
    case 'AI_FEATURE_OFF':
      return appStrings.aiErrorFeatureOff;
    case 'AI_NO_CONTENT':
      return appStrings.aiErrorNoContent;
    case 'TOO_MANY_FILES':
      return appStrings.aiErrorTooManyFiles;
    case 'AI_SUGGESTION_STALE':
      return appStrings.aiSuggestionStale;
    case 'AI_SUGGESTION_DECIDED':
      return appStrings.aiSuggestionDecided;
    case 'AI_TARGET_NOT_ALLOWED':
      return appStrings.aiErrorTargetNotAllowed;
    case 'SHARE_BOUNDARY':
      return appStrings.aiErrorShareBoundary;
    case 'AI_UNDO_CONFLICT':
      return appStrings.aiUndoConflict(1);
    case 'AI_NOT_UNDOABLE':
      return appStrings.aiErrorNotUndoable;
    default:
      return null;
  }
}

/// A server from before AI answers its routes with 404 NOT_FOUND.
bool _aiRouteMissing(Object error) => error is ApiException && error.statusCode == 404 && (error.code == null || error.code == 'NOT_FOUND');
