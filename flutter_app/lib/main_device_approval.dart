part of 'main.dart';

// The page a desktop or phone sends its user to (/app/device?code=…): it
// names the device that asks for access and lets the signed-in account
// approve or deny it. Laid out like the sign-in card.

enum _ApprovalState { entering, loading, asking, answering, approved, denied }

class DeviceApprovalView extends StatefulWidget {
  const DeviceApprovalView({super.key, required this.controller});

  final NeoCloudController controller;

  @override
  State<DeviceApprovalView> createState() => _DeviceApprovalViewState();
}

class _DeviceApprovalViewState extends State<DeviceApprovalView> {
  late final TextEditingController _code = TextEditingController(text: widget.controller.pendingDeviceCode ?? '');
  _ApprovalState _state = _ApprovalState.entering;
  DeviceAuthorizationRequest? _request;
  String? _error;

  NeoCloudController get _controller => widget.controller;

  @override
  void initState() {
    super.initState();
    if (_code.text.trim().isNotEmpty) unawaited(_lookUp());
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  String get _userCode => _code.text.trim().toUpperCase();

  Future<void> _lookUp() async {
    if (_userCode.isEmpty) return;
    setState(() {
      _state = _ApprovalState.loading;
      _error = null;
    });
    try {
      final request = await _controller.client.deviceAuthorization(_userCode);
      if (!mounted) return;
      setState(() {
        _request = request;
        _state = _ApprovalState.asking;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _state = _ApprovalState.entering;
        _error = error is ApiException && error.statusCode == 404 ? appStrings.deviceCodeUnknown : _controller.friendlyError(error);
      });
    }
  }

  Future<void> _answer(bool approve) async {
    setState(() {
      _state = _ApprovalState.answering;
      _error = null;
    });
    try {
      await _controller.client.answerDeviceAuthorization(_userCode, approve: approve);
      if (mounted) setState(() => _state = approve ? _ApprovalState.approved : _ApprovalState.denied);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _state = _ApprovalState.asking;
        _error = error is ApiException && error.statusCode == 404 ? appStrings.deviceCodeUnknown : _controller.friendlyError(error);
      });
    }
  }

  List<Widget> _body() {
    final request = _request;
    switch (_state) {
      case _ApprovalState.entering:
      case _ApprovalState.loading:
        final loading = _state == _ApprovalState.loading;
        return <Widget>[
          TextField(
            controller: _code,
            autofocus: true,
            enabled: !loading,
            textCapitalization: TextCapitalization.characters,
            inputFormatters: <TextInputFormatter>[_UserCodeFormatter()],
            onSubmitted: (_) => unawaited(_lookUp()),
            style: GoogleFonts.geistMono(fontSize: 18, letterSpacing: 2, color: _textPrimary),
            decoration: InputDecoration(labelText: appStrings.deviceCodeLabel, hintText: 'ABCD-EFGH'),
          ),
          const SizedBox(height: AppSpacing.lg),
          FilledButton(
            onPressed: loading ? null : () => unawaited(_lookUp()),
            child: loading ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.continueLabel),
          ),
        ];
      case _ApprovalState.asking:
      case _ApprovalState.answering:
        final busy = _state == _ApprovalState.answering;
        return <Widget>[
          _AppCard(
            padding: const EdgeInsets.all(16),
            child: Row(
              children: <Widget>[
                Container(
                  width: 40,
                  height: 40,
                  decoration: BoxDecoration(color: _bgTertiary, borderRadius: BorderRadius.circular(12), border: Border.all(color: _border)),
                  child: Icon(_platformIcon(request?.platform ?? ''), size: 20, color: _textSecondary),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(request?.name ?? '', style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600, fontSize: 15)),
                      const SizedBox(height: 2),
                      Text(
                        <String>[
                          if ((request?.platform ?? '').isNotEmpty) request!.platform,
                          _userCode,
                        ].join(' · '),
                        style: TextStyle(color: _textMuted, fontSize: 12.5),
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: AppSpacing.md),
          Text(appStrings.deviceApproveHint(_controller.accountLabel), style: TextStyle(color: _textSecondary, height: 1.45, fontSize: 13.5)),
          const SizedBox(height: AppSpacing.lg),
          Row(
            children: <Widget>[
              Expanded(
                child: OutlinedButton(onPressed: busy ? null : () => unawaited(_answer(false)), child: Text(appStrings.deviceDeny)),
              ),
              const SizedBox(width: AppSpacing.sm),
              Expanded(
                child: FilledButton(
                  onPressed: busy ? null : () => unawaited(_answer(true)),
                  child: busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(appStrings.deviceApprove),
                ),
              ),
            ],
          ),
        ];
      case _ApprovalState.approved:
      case _ApprovalState.denied:
        final approved = _state == _ApprovalState.approved;
        return <Widget>[
          if (approved)
            _InlineSuccess(message: appStrings.deviceApproved(request?.name ?? ''))
          else
            _InlineNote(message: appStrings.deviceDenied(request?.name ?? '')),
          const SizedBox(height: AppSpacing.lg),
          FilledButton(onPressed: _controller.finishDeviceApproval, child: Text(appStrings.openNeoCloud)),
        ];
    }
  }

  @override
  Widget build(BuildContext context) {
    final done = _state == _ApprovalState.approved || _state == _ApprovalState.denied;
    return _ControlSurfaceBackdrop(
      child: Scaffold(
        backgroundColor: Colors.transparent,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: _EntranceMotion(
                child: _GlassSurface(
                  width: 420,
                  padding: const EdgeInsets.all(AppSpacing.xl),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      const Center(child: _BrandLockup(logoSize: 44, showName: false)),
                      const SizedBox(height: AppSpacing.md),
                      Text(appStrings.deviceApprovalTitle, style: _displayTitleStyle(24)),
                      const SizedBox(height: AppSpacing.sm),
                      Text(
                        _state == _ApprovalState.entering || _state == _ApprovalState.loading
                            ? appStrings.deviceApprovalEnterCode
                            : appStrings.deviceApprovalSubtitle,
                        style: TextStyle(color: _textSecondary, height: 1.45),
                      ),
                      const SizedBox(height: AppSpacing.lg),
                      if (_error != null) ...<Widget>[_InlineError(message: _error!), const SizedBox(height: AppSpacing.md)],
                      ..._body(),
                      if (!done) ...<Widget>[
                        const SizedBox(height: AppSpacing.sm),
                        TextButton(onPressed: _controller.finishDeviceApproval, child: Text(appStrings.cancel)),
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

IconData _platformIcon(String platform) {
  final value = platform.toLowerCase();
  if (value.contains('android') || value.contains('ios') || value.contains('iphone')) return Icons.smartphone_rounded;
  if (value.contains('mac') || value.contains('windows') || value.contains('linux')) return Icons.laptop_mac_rounded;
  return Icons.devices_other_rounded;
}

/// User codes as ABCD-EFGH while they are typed or pasted.
class _UserCodeFormatter extends TextInputFormatter {
  @override
  TextEditingValue formatEditUpdate(TextEditingValue oldValue, TextEditingValue newValue) {
    final raw = newValue.text.toUpperCase().replaceAll(RegExp(r'[^0-9A-Z]'), '');
    final clipped = raw.length > 8 ? raw.substring(0, 8) : raw;
    final text = clipped.length > 4 ? '${clipped.substring(0, 4)}-${clipped.substring(4)}' : clipped;
    return TextEditingValue(text: text, selection: TextSelection.collapsed(offset: text.length));
  }
}
