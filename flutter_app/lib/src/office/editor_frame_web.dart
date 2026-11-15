import 'dart:js_interop';
import 'dart:ui_web' as ui_web;

import 'package:flutter/widgets.dart';
import 'package:web/web.dart' as web;

/// The browser shows the editor in an iframe inside the page.
const bool officeEditorEmbeds = true;

int _frames = 0;

/// The editor page in an iframe. The token is posted to the editor (a form
/// in the frame) rather than put in an address that browsers keep.
Widget officeEditorFrame({required String editorUrl, required String accessToken, required int accessTokenTtl}) =>
    _OfficeEditorFrame(editorUrl: editorUrl, accessToken: accessToken, accessTokenTtl: accessTokenTtl);

String _escape(String text) => text
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;');

class _OfficeEditorFrame extends StatefulWidget {
  const _OfficeEditorFrame({required this.editorUrl, required this.accessToken, required this.accessTokenTtl});

  final String editorUrl;
  final String accessToken;
  final int accessTokenTtl;

  @override
  State<_OfficeEditorFrame> createState() => _OfficeEditorFrameState();
}

class _OfficeEditorFrameState extends State<_OfficeEditorFrame> {
  late final String _viewType = 'neocloud-office-editor-${_frames++}';

  @override
  void initState() {
    super.initState();
    final page =
        '<!doctype html><html><body>'
        '<form id="open" method="post" action="${_escape(widget.editorUrl)}">'
        '<input type="hidden" name="access_token" value="${_escape(widget.accessToken)}">'
        '<input type="hidden" name="access_token_ttl" value="${widget.accessTokenTtl}">'
        '</form><script>document.getElementById("open").submit();</script></body></html>';
    ui_web.platformViewRegistry.registerViewFactory(_viewType, (int viewId) {
      final frame = web.HTMLIFrameElement()
        ..srcdoc = page.toJS
        ..allow = 'clipboard-read; clipboard-write; fullscreen';
      frame.style
        ..border = '0'
        ..width = '100%'
        ..height = '100%';
      return frame;
    });
  }

  @override
  Widget build(BuildContext context) => HtmlElementView(viewType: _viewType);
}
