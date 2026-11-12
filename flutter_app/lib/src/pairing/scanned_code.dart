/// Everything the phone app's QR scanner understands: the web app's pairing
/// code, a signed-out browser's sign-in code, and a device-code approval
/// link (the lite desktop client shows one for its sign-in).
library;

import 'pairing_link.dart';

/// A code the scanner recognised.
sealed class ScannedCode {
  const ScannedCode();

  /// The server the code belongs to, `https://cloud.example.com`.
  Uri get server;
}

/// `neocloud://pair?server=…&code=…`: signs this phone in.
class PairingScan extends ScannedCode {
  const PairingScan(this.link);

  final PairingLinkData link;

  @override
  Uri get server => link.server;
}

/// `neocloud://qr-login?v=1&backend=…&challenge=…&secret=…`: a browser asks
/// to be signed in by this phone.
class QrLoginScanPayload extends ScannedCode {
  const QrLoginScanPayload({required this.server, required this.challengeId, required this.secret});

  @override
  final Uri server;
  final String challengeId;
  final String secret;
}

/// `https://<server>/app/device?code=ABCD-EFGH`: a device waiting for the
/// account to approve its sign-in.
class DeviceCodeScan extends ScannedCode {
  const DeviceCodeScan({required this.server, required this.userCode});

  @override
  final Uri server;

  /// `ABCD-EFGH`.
  final String userCode;
}

final RegExp _secretPattern = RegExp(r'^[A-Za-z0-9_-]{16,}$');
final RegExp _challengePattern = RegExp(r'^[A-Za-z0-9-]{8,64}$');
const String _userCodeAlphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';

/// Reads a scanned QR code; null for anything NeoCloud didn't make.
ScannedCode? parseScannedCode(String raw) {
  final pairing = parsePairingLink(raw);
  if (pairing != null) return PairingScan(pairing);
  final uri = Uri.tryParse(raw.trim());
  if (uri == null) return null;
  if (uri.scheme == 'neocloud') return _qrLogin(uri);
  if (uri.scheme == 'http' || uri.scheme == 'https') return _deviceCode(uri);
  return null;
}

QrLoginScanPayload? _qrLogin(Uri uri) {
  if (uri.host != 'qr-login' || (uri.path.isNotEmpty && uri.path != '/')) return null;
  if ((uri.queryParameters['v'] ?? '1') != '1') return null;
  final server = _server(uri.queryParameters['backend'] ?? '');
  final challengeId = (uri.queryParameters['challenge'] ?? '').trim();
  final secret = (uri.queryParameters['secret'] ?? '').trim();
  if (server == null || !_challengePattern.hasMatch(challengeId) || !_secretPattern.hasMatch(secret)) return null;
  return QrLoginScanPayload(server: server, challengeId: challengeId, secret: secret);
}

DeviceCodeScan? _deviceCode(Uri uri) {
  final path = uri.path.endsWith('/') ? uri.path.substring(0, uri.path.length - 1) : uri.path;
  if (!path.endsWith('/app/device')) return null;
  final server = _server(uri.replace(path: path.substring(0, path.length - '/app/device'.length), query: '', fragment: '').toString());
  final letters = (uri.queryParameters['code'] ?? '').toUpperCase().replaceAll(RegExp(r'[^A-Z0-9]'), '');
  if (server == null || letters.length != 8 || letters.split('').any((c) => !_userCodeAlphabet.contains(c))) return null;
  return DeviceCodeScan(server: server, userCode: '${letters.substring(0, 4)}-${letters.substring(4)}');
}

Uri? _server(String value) {
  final uri = Uri.tryParse(value.trim());
  if (uri == null || (uri.scheme != 'https' && uri.scheme != 'http') || uri.host.isEmpty || uri.userInfo.isNotEmpty) return null;
  final path = uri.path.endsWith('/') ? uri.path.substring(0, uri.path.length - 1) : uri.path;
  return Uri(scheme: uri.scheme, host: uri.host, port: uri.hasPort ? uri.port : null, path: path);
}

/// Whether [a] and [b] name the same server, ignoring a trailing slash and
/// the default port.
bool sameServer(Uri a, String b) {
  final other = _server(b);
  if (other == null) return false;
  return a.scheme == other.scheme && a.host.toLowerCase() == other.host.toLowerCase() && a.port == other.port && a.path == other.path;
}
