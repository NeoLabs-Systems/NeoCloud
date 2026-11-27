part of 'backend_client.dart';

/// Everything that needs the server's mail setup (docs/api.md, "E-mail").
/// These routes exist for every client, but the app only offers them once
/// `GET /api/auth/status` says `email.configured`.
extension BackendEmailCalls on BackendClient {
  /// "Forgot password?": always answers the same message, whether or not
  /// the account exists.
  Future<Map<String, dynamic>> requestPasswordReset(String account) =>
      _neoAuth.forgotPassword(baseUrl, account);

  /// Sends the sign-up confirmation again (username or email); uniform too.
  Future<Map<String, dynamic>> resendConfirmation(String account) async =>
      _map(await _request('POST', '/api/auth/email/resend', json: {'account': account}, expireOn401: false));

  /// Changes the account email. With confirmation on, the answer is
  /// `{emailChangePending: true, pendingEmail}` and nothing changes until the
  /// new address confirms.
  Future<Map<String, dynamic>> updateAccountEmail({required String email, required String currentPassword}) async =>
      _map(await _request('PUT', '/api/account/email', json: {'email': email, 'currentPassword': currentPassword}));

  Future<void> resendAccountEmailConfirmation() => _request('POST', '/api/account/email/resend', json: const <String, Object?>{});

  /// `{configured, missing, settings}`; the SMTP password is never returned,
  /// only `settings.smtpPasswordConfigured`.
  Future<Map<String, dynamic>> adminEmailConfig() async => _map(await _request('GET', '/api/admin/config/email'));

  Future<Map<String, dynamic>> adminSaveEmailConfig(Map<String, Object?> body) async =>
      _map(await _request('PUT', '/api/admin/config/email', json: body));

  /// Sends a test message through the saved settings. A failure carries the
  /// mail server's own text as the [ApiException] message.
  Future<String> adminSendTestEmail(String? to) async {
    final answer = _map(await _request('POST', '/api/admin/config/email/test', json: <String, Object?>{if (to != null && to.isNotEmpty) 'to': to}));
    return answer['message']?.toString() ?? '';
  }

  Future<void> adminVerifyUserEmail(int userId) => _request('POST', '/api/admin/users/$userId/email/verify', json: const <String, Object?>{});
}
