part of 'main.dart';

// Admin › Access › Document editor: where the self-hosted Collabora Online
// is (docs/office-editing.md), whether people may edit office files in the
// browser, and whether the server could reach the editor.

class _AdminOfficeCard extends StatefulWidget {
  const _AdminOfficeCard({required this.controller});

  final NeoCloudController controller;

  @override
  State<_AdminOfficeCard> createState() => _AdminOfficeCardState();
}

class _AdminOfficeCardState extends State<_AdminOfficeCard> {
  final TextEditingController _url = TextEditingController();
  final TextEditingController _hostUrl = TextEditingController();
  bool _enabled = false;
  bool _loading = true;
  String? _loadError;
  bool _busy = false;
  String? _error;
  String? _notice;
  String _state = 'unconfigured';
  String? _statusMessage;
  List<String> _extensions = const <String>[];

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    unawaited(_fetch());
  }

  @override
  void dispose() {
    _url.dispose();
    _hostUrl.dispose();
    super.dispose();
  }

  void _apply(Map<String, dynamic> data) {
    _url.text = data['url']?.toString() ?? '';
    _hostUrl.text = data['hostUrl']?.toString() ?? '';
    _enabled = data['enabled'] == true;
    final status = (data['status'] as Map?)?.cast<String, dynamic>() ?? const <String, dynamic>{};
    _state = status['state']?.toString() ?? 'unconfigured';
    _statusMessage = status['message']?.toString();
    _extensions = ((status['extensions'] as List?) ?? const <dynamic>[]).map((item) => '$item').toList();
  }

  Future<void> _fetch() async {
    setState(() {
      _loading = true;
      _loadError = null;
    });
    try {
      final data = await _controller.client.adminOfficeConfig();
      if (!mounted) return;
      setState(() {
        _apply(data);
        _loading = false;
      });
    } catch (error) {
      if (mounted) {
        setState(() {
          _loading = false;
          _loadError = _controller.friendlyError(error);
        });
      }
    }
  }

  Future<void> _run(Future<Map<String, dynamic>> Function() call, String notice) async {
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      final data = await call();
      if (!mounted) return;
      setState(() {
        _apply(data);
        _notice = notice;
      });
      await _controller.office.load();
    } catch (error) {
      if (mounted) setState(() => _error = _controller.friendlyError(error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _save() => _run(
    () => _controller.client.adminSaveOfficeConfig(<String, Object?>{'url': _url.text.trim(), 'hostUrl': _hostUrl.text.trim(), 'enabled': _enabled}),
    appStrings.officeAdminSaved,
  );

  Future<void> _check() => _run(_controller.client.adminCheckOffice, appStrings.officeAdminChecked);

  (String, Color) _pill() => switch (_state) {
    'ok' => (appStrings.officeAdminStateOk, _success),
    'error' => (appStrings.officeAdminStateError, _danger),
    'off' => (appStrings.officeAdminStateOff, _warning),
    _ => (appStrings.officeAdminStateUnconfigured, _warning),
  };

  Widget _form() {
    final error = _error;
    final notice = _notice;
    final message = _statusMessage;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        TextField(
          controller: _url,
          keyboardType: TextInputType.url,
          autocorrect: false,
          enableSuggestions: false,
          onChanged: (_) => setState(() => _notice = null),
          decoration: InputDecoration(labelText: appStrings.officeAdminUrl, hintText: 'https://office.example.com', helperText: appStrings.officeAdminUrlHelper, helperMaxLines: 3),
        ),
        const SizedBox(height: 14),
        TextField(
          controller: _hostUrl,
          keyboardType: TextInputType.url,
          autocorrect: false,
          enableSuggestions: false,
          onChanged: (_) => setState(() => _notice = null),
          decoration: InputDecoration(labelText: appStrings.officeAdminHostUrl, hintText: 'http://neocloud:3000', helperText: appStrings.officeAdminHostUrlHelper, helperMaxLines: 3),
        ),
        SwitchListTile(
          value: _enabled,
          contentPadding: EdgeInsets.zero,
          title: Text(appStrings.officeAdminEnable),
          subtitle: Text(appStrings.officeAdminEnableHelper),
          onChanged: (value) => setState(() {
            _enabled = value;
            _notice = null;
          }),
        ),
        if (_state == 'ok' && _extensions.isNotEmpty) ...<Widget>[
          const SizedBox(height: 4),
          Text(appStrings.officeAdminFormats(_extensions.join(', ')), style: TextStyle(fontSize: 13, color: _textSecondary)),
        ],
        if (_state == 'error' && message != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: message)],
        const SizedBox(height: 16),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            FilledButton.icon(
              onPressed: _busy ? null : () => unawaited(_save()),
              icon: _busy
                  ? SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2, color: _onAccent))
                  : const Icon(Icons.check_rounded, size: 18),
              label: Text(_busy ? appStrings.saving : appStrings.officeAdminSave),
            ),
            if (_url.text.trim().isNotEmpty)
              OutlinedButton.icon(
                onPressed: _busy ? null : () => unawaited(_check()),
                icon: const Icon(Icons.refresh_rounded, size: 18),
                label: Text(appStrings.officeAdminCheck),
              ),
          ],
        ),
        if (error != null) ...<Widget>[const SizedBox(height: 12), _InlineError(message: error)],
        if (notice != null) ...<Widget>[const SizedBox(height: 12), _InlineSuccess(message: notice)],
      ],
    );
  }

  @override
  Widget build(BuildContext context) {
    final loaded = !_loading && _loadError == null;
    final Widget body;
    if (_loading) {
      body = const _LoadingPlaceholder();
    } else if (_loadError != null) {
      body = _LoadError(message: _loadError!, onRetry: () => unawaited(_fetch()));
    } else {
      body = _form();
    }
    final (label, color) = _pill();
    return Padding(
      padding: const EdgeInsets.only(bottom: 28),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          _SettingsHeading(
            title: appStrings.officeAdminTitle,
            description: appStrings.officeAdminDescription,
            trailing: loaded ? _StatusPill(label: label, color: color) : null,
          ),
          _AppCard(child: body),
        ],
      ),
    );
  }
}
