part of 'main.dart';

// The form to connect or change one cloud storage: a provider picker, the
// fields that provider needs, a read-only switch and "Test connection". A
// connection is saved only after the server connected with it. Secrets are
// never shown again; leaving one empty keeps the saved one.

/// Opens the form; true when something was saved.
Future<bool> showCloudStorageForm(BuildContext context, NeoCloudController controller, {ExternalMount? mount}) async {
  final saved = await showAdaptiveSheet<bool>(
    context: context,
    builder: (dialogContext) => _CloudStorageForm(controller: controller, mount: mount),
  );
  return saved == true;
}

/// One input of a provider's form: the settings key, its label and what kind of text it takes.
class _CloudField {
  const _CloudField(this.key, this.label, {this.hint, this.secret = false, this.multiline = false, this.number = false, this.optional = false});

  final String key;
  final String label;
  final String? hint;
  final bool secret;
  final bool multiline;
  final bool number;
  final bool optional;
}

List<_CloudField> _cloudFieldsFor(ExternalProvider provider) {
  switch (provider) {
    case ExternalProvider.s3:
      return <_CloudField>[
        _CloudField('endpoint', appStrings.cloudEndpoint, hint: appStrings.cloudEndpointHint, optional: true),
        _CloudField('region', appStrings.cloudRegion, hint: 'us-east-1', optional: true),
        _CloudField('bucket', appStrings.cloudBucket),
        _CloudField('prefix', appStrings.cloudPrefix, optional: true),
        _CloudField('accessKeyId', appStrings.cloudAccessKey),
        _CloudField('secretAccessKey', appStrings.cloudSecretKey, secret: true),
      ];
    case ExternalProvider.webdav:
      return <_CloudField>[
        _CloudField('url', appStrings.cloudWebdavUrl, hint: 'https://cloud.example.com/remote.php/dav/files/name/'),
        _CloudField('username', appStrings.accountUsername),
        _CloudField('password', appStrings.cloudPassword, secret: true),
      ];
    case ExternalProvider.sftp:
      return <_CloudField>[
        _CloudField('host', appStrings.cloudHost),
        _CloudField('port', appStrings.cloudPort, hint: '22', number: true, optional: true),
        _CloudField('username', appStrings.accountUsername),
        _CloudField('password', appStrings.cloudPassword, secret: true, optional: true),
        _CloudField('privateKey', appStrings.cloudPrivateKey, secret: true, multiline: true, optional: true),
        _CloudField('path', appStrings.cloudSftpFolder, hint: '/', optional: true),
      ];
  }
}

class _CloudStorageForm extends StatefulWidget {
  const _CloudStorageForm({required this.controller, this.mount});

  final NeoCloudController controller;
  final ExternalMount? mount;

  @override
  State<_CloudStorageForm> createState() => _CloudStorageFormState();
}

class _CloudStorageFormState extends State<_CloudStorageForm> {
  late ExternalProvider _provider = widget.mount?.provider ?? ExternalProvider.s3;
  late final TextEditingController _name = TextEditingController(text: widget.mount?.name ?? '');
  late bool _readOnly = widget.mount?.readOnly ?? false;
  final Map<ExternalProvider, Map<String, TextEditingController>> _inputs = <ExternalProvider, Map<String, TextEditingController>>{};
  bool _busy = false;
  bool _configChanged = false;
  String? _error;
  ExternalTest? _test;

  ExternalMount? get _existing => widget.mount;
  NeoCloudController get _controller => widget.controller;

  TextEditingController _input(_CloudField field) {
    final byProvider = _inputs.putIfAbsent(_provider, () => <String, TextEditingController>{});
    return byProvider.putIfAbsent(field.key, () {
      final stored = _existing?.config[field.key];
      return TextEditingController(text: field.secret || stored == null ? '' : '$stored');
    });
  }

  @override
  void dispose() {
    _name.dispose();
    for (final inputs in _inputs.values) {
      for (final input in inputs.values) {
        input.dispose();
      }
    }
    super.dispose();
  }

  Map<String, Object?> get _config {
    final config = <String, Object?>{};
    for (final field in _cloudFieldsFor(_provider)) {
      final value = _input(field).text;
      if (value.isNotEmpty) config[field.key] = field.number ? int.tryParse(value.trim()) ?? value : (field.secret ? value : value.trim());
    }
    return config;
  }

  /// Every field the provider needs that is empty (a saved secret counts as filled).
  bool get _complete {
    if (_name.text.trim().isEmpty) return false;
    for (final field in _cloudFieldsFor(_provider)) {
      final saved = _existing != null && field.secret && _existing!.config['${field.key}Set'] == true;
      if (!field.optional && !saved && _input(field).text.trim().isEmpty) return false;
    }
    if (_provider == ExternalProvider.sftp) {
      final hasSecret = (_existing?.config['passwordSet'] == true) || (_existing?.config['privateKeySet'] == true);
      final fields = _cloudFieldsFor(_provider);
      final typed = fields.where((f) => f.key == 'password' || f.key == 'privateKey').any((f) => _input(f).text.isNotEmpty);
      if (!hasSecret && !typed) return false;
    }
    return true;
  }

  void _edited([String _ = '']) => setState(() {
    _configChanged = true;
    _test = null;
    _error = null;
  });

  Map<String, Object?> get _body => <String, Object?>{
    'name': _name.text.trim(),
    'provider': _provider.name,
    'readOnly': _readOnly,
    if (_existing == null || _configChanged) 'config': _config,
    'id': ?_existing?.id,
  };

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (error) {
      if (mounted) setState(() => _error = _cloudError(_controller, error));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _testConnection() => _run(() async {
    final result = await _controller.client.testExternalMount(<String, Object?>{..._body, 'config': _config});
    if (mounted) setState(() => _test = result);
  });

  Future<void> _save() => _run(() async {
    final body = _body..remove('id');
    if (_existing == null) {
      await _controller.client.connectExternalMount(body);
    } else {
      await _controller.client.updateExternalMount(_existing!.id, body..remove('provider'));
    }
    if (mounted) Navigator.of(context).pop(true);
  });

  Widget _fieldWidget(_CloudField field) {
    final keepSaved = field.secret && _existing?.config['${field.key}Set'] == true;
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: TextField(
        controller: _input(field),
        obscureText: field.secret && !field.multiline,
        minLines: field.multiline ? 3 : 1,
        maxLines: field.multiline ? 6 : 1,
        autocorrect: false,
        enableSuggestions: false,
        keyboardType: field.number ? TextInputType.number : (field.key == 'url' || field.key == 'endpoint' ? TextInputType.url : null),
        onChanged: _edited,
        decoration: InputDecoration(labelText: field.label, hintText: keepSaved ? appStrings.cloudKeepSecret : field.hint),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final test = _test;
    final editing = _existing != null;
    return _AdaptiveDialog(
      title: Text(editing ? appStrings.cloudEdit : appStrings.cloudConnect),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (!editing) ...<Widget>[
                _SettingsChoice<ExternalProvider>(
                  value: _provider,
                  options: <(ExternalProvider, String)>[for (final provider in ExternalProvider.values) (provider, provider.shortLabel)],
                  onChanged: (provider) => setState(() {
                    _provider = provider;
                    _test = null;
                  }),
                ),
                const SizedBox(height: 16),
              ],
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: TextField(
                  controller: _name,
                  onChanged: (_) => setState(() => _error = null),
                  decoration: InputDecoration(labelText: appStrings.cloudName, hintText: appStrings.cloudNameHint),
                ),
              ),
              for (final field in _cloudFieldsFor(_provider)) _fieldWidget(field),
              if (_provider == ExternalProvider.webdav) Padding(padding: const EdgeInsets.only(bottom: 12), child: Text(appStrings.cloudWebdavHelp, style: TextStyle(color: _textMuted, fontSize: 12.5, height: 1.4))),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: Text(appStrings.cloudReadOnly),
                subtitle: Text(appStrings.cloudReadOnlyHint),
                value: _readOnly,
                onChanged: (value) => setState(() => _readOnly = value),
              ),
              if (_error != null) Padding(padding: const EdgeInsets.only(top: 8), child: _InlineError(message: _error!)),
              if (test != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: test.ok
                      ? _InlineSuccess(message: appStrings.cloudTestOk(test.items))
                      : _InlineError(message: test.error?.isNotEmpty == true ? test.error! : appStrings.cloudTestFailed),
                ),
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _busy ? null : () => Navigator.of(context).pop(false), child: Text(appStrings.cancel)),
        OutlinedButton(onPressed: _busy || !_complete || (editing && !_configChanged) ? null : () => unawaited(_testConnection()), child: Text(appStrings.cloudTest)),
        FilledButton(
          onPressed: _busy || !_complete ? null : () => unawaited(_save()),
          child: _busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(editing ? appStrings.save : appStrings.cloudConnectAction),
        ),
      ],
    );
  }
}

extension on ExternalProvider {
  String get shortLabel {
    switch (this) {
      case ExternalProvider.s3:
        return 'S3';
      case ExternalProvider.webdav:
        return 'WebDAV';
      case ExternalProvider.sftp:
        return 'SFTP';
    }
  }
}
