import 'dart:js_interop';

import 'package:web/web.dart' as web;

JSFunction? _handler;

/// While [active], closing or reloading the tab asks first, so running
/// uploads aren't lost by accident.
void setUnloadGuard(bool active) {
  if (active == (_handler != null)) return;
  if (active) {
    final handler = ((web.BeforeUnloadEvent event) {
      event.preventDefault();
      event.returnValue = '';
    }).toJS;
    _handler = handler;
    web.window.addEventListener('beforeunload', handler);
  } else {
    web.window.removeEventListener('beforeunload', _handler);
    _handler = null;
  }
}
