import 'dart:async';

import '../api/backend_client.dart';
import '../api/models.dart';
import '../l10n/app_language.dart';

/// The signed-out browser's half of QR sign-in, ported from NeoAgent's
/// controller: show a code, wait (long-poll) for a phone to scan and approve
/// it, then claim a session. The root controller owns one and redraws on
/// [onChanged].
///
/// Polls right after each held status check returns, or every
/// [_pollInterval] against servers that answer at once; failures (rate
/// limits included) double the wait up to [_pollMaxInterval].
class QrLoginFlow {
  QrLoginFlow({required this.client, required this.onChanged, required this.onSignedIn, required this.describeError});

  final BackendClient client;
  final void Function() onChanged;

  /// Gets the claim's answer, which looks like a password sign-in's.
  final Future<void> Function(Map<String, dynamic> result) onSignedIn;
  final String Function(Object error) describeError;

  static const Duration _pollInterval = Duration(seconds: 5);
  static const Duration _heldPollGap = Duration(milliseconds: 300);
  static const Duration _pollMaxInterval = Duration(seconds: 60);

  QrLoginChallenge? challenge;

  /// Set while a phone that scanned [challenge] decides on it.
  QrLoginScan? scan;
  String? errorMessage;
  bool isPreparing = false;

  Timer? _pollTimer;
  Duration _pollDelay = _pollInterval;
  bool _polling = false;
  bool _serverHoldsPolls = false;
  String? _pollingChallengeId;

  bool get canShowCode => challenge != null && challenge!.isUsable && !challenge!.isExpired;

  /// What this browser says about itself; the server reads the platform
  /// and browser the phone shows from the User-Agent.
  static const Map<String, Object?> browserMetadata = <String, Object?>{'platform': 'web', 'appMode': 'web'};

  void _stopPolling() {
    _pollTimer?.cancel();
    _pollTimer = null;
    _polling = false;
  }

  /// Stops checking while nobody can see the code (the tab is hidden, or
  /// the card shows something else). [prepare] resumes.
  void pause() => _stopPolling();

  /// Forgets the code for good, e.g. once signed in another way.
  void clear() {
    _stopPolling();
    challenge = null;
    scan = null;
    errorMessage = null;
  }

  void _ensurePolling() {
    _stopPolling();
    final current = challenge;
    if (current == null || !current.isUsable) return;
    _polling = true;
    _schedulePoll();
  }

  void _schedulePoll() {
    _pollTimer = Timer(_pollDelay, () async {
      _pollTimer = null;
      final succeeded = await _poll();
      // Skipped or outdated: another chain owns the polling.
      if (succeeded == null) return;
      final doubled = _pollDelay * 2;
      if (succeeded) {
        _pollDelay = _serverHoldsPolls ? _heldPollGap : _pollInterval;
      } else {
        _pollDelay = doubled > _pollMaxInterval ? _pollMaxInterval : doubled;
      }
      if (_polling && _pollTimer == null && challenge != null) _schedulePoll();
    });
  }

  /// Shows a code (a fresh one with [force]) and keeps watching it.
  Future<void> prepare({bool force = false}) async {
    if (isPreparing) return;
    final current = challenge;
    if (!force && current != null && current.isUsable && !current.isExpired) {
      _ensurePolling();
      return;
    }
    isPreparing = true;
    errorMessage = null;
    if (force) challenge = null;
    scan = null;
    onChanged();
    try {
      final created = await client.createQrLoginChallenge(browserMetadata);
      if (!created.isUsable) throw ApiException(500, appStrings.qrLoginCouldNotStart);
      challenge = created;
      _pollDelay = _heldPollGap;
      _ensurePolling();
    } catch (error) {
      clear();
      errorMessage = _errorText(error);
    } finally {
      isPreparing = false;
      onChanged();
    }
  }

  String _errorText(Object error) {
    if (error is ApiException && error.statusCode == 429) return appStrings.qrLoginRateLimited;
    return describeError(error);
  }

  /// Whether the status check reached the server, or null when it was
  /// skipped or its code was replaced while it waited.
  Future<bool?> _poll() async {
    final current = challenge;
    if (current == null || !current.isUsable || _pollingChallengeId == current.challengeId) return null;
    _pollingChallengeId = current.challengeId;
    try {
      final status = await client.qrLoginStatus(current, knownStatus: scan != null ? 'scanned' : 'pending');
      if (challenge?.challengeId != current.challengeId) return null;
      _serverHoldsPolls = status['longPoll'] == true;
      if (errorMessage != null) {
        errorMessage = null;
        onChanged();
      }
      switch (status['status']?.toString() ?? 'pending') {
        case 'approved':
          await _claim(current);
        case 'scanned':
          if (scan == null) {
            scan = QrLoginScan.fromStatus(status);
            onChanged();
          }
        case 'declined':
          await prepare(force: true);
          errorMessage = appStrings.qrLoginDeclinedOnPhone;
          onChanged();
        case 'expired' || 'claimed':
          await prepare(force: true);
      }
      return true;
    } catch (error) {
      if (challenge?.challengeId != current.challengeId) return null;
      errorMessage = _errorText(error);
      onChanged();
      return false;
    } finally {
      if (_pollingChallengeId == current.challengeId) _pollingChallengeId = null;
    }
  }

  Future<void> _claim(QrLoginChallenge current) async {
    try {
      final result = await client.claimQrLogin(current);
      clear();
      await onSignedIn(result);
    } on ApiException catch (error) {
      if (error.statusCode == 404 || error.code == 'QR_LOGIN_USED') {
        await prepare(force: true);
        errorMessage = appStrings.qrLoginExpired;
      } else {
        errorMessage = _errorText(error);
      }
      onChanged();
    } catch (error) {
      errorMessage = _errorText(error);
      onChanged();
    }
  }

  /// Gives up on a scanned code and shows a fresh one, so an approval the
  /// phone sends late cannot sign this browser in behind the user's back.
  Future<void> cancelScan() async {
    final current = challenge;
    if (current != null) {
      try {
        await client.cancelQrLogin(current);
      } catch (_) {
        // A fresh code replaces it either way; the old one expires.
      }
    }
    await prepare(force: true);
  }

  void dispose() => _stopPolling();
}
