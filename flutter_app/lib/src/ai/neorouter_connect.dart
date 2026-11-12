import '../api/backend_client.dart';

/// Why a NeoRouter sign-in ended without a key.
enum NeoRouterConnectProblem { cancelled, failed, expired, timedOut }

class NeoRouterConnectException implements Exception {
  const NeoRouterConnectException(this.problem, [this.message]);

  final NeoRouterConnectProblem problem;

  /// The server's own words for a failed sign-in, when it gave some.
  final String? message;

  @override
  String toString() => message ?? problem.name;
}

/// Waits until NeoRouter approved the sign-in [id] and the server holds its
/// key (NeoAgent's waitForNeoRouterConnect). Throws once [isActive] turns
/// false, the sign-in fails or expires, or after [timeout].
Future<void> waitForNeoRouterConnect(
  AiApi api,
  String id, {
  required bool Function() isActive,
  Duration interval = const Duration(seconds: 2),
  Duration timeout = const Duration(minutes: 10),
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (!isActive()) throw const NeoRouterConnectException(NeoRouterConnectProblem.cancelled);
    final Map<String, dynamic> response;
    try {
      response = await api.neoRouterConnectStatus(id);
    } on ApiException catch (error) {
      if (error.statusCode == 404) throw const NeoRouterConnectException(NeoRouterConnectProblem.expired);
      rethrow;
    }
    switch (response['status']?.toString()) {
      case 'completed':
        return;
      case 'failed':
        final message = response['error']?.toString();
        throw NeoRouterConnectException(NeoRouterConnectProblem.failed, message == null || message.isEmpty ? null : message);
    }
    await Future<void>.delayed(interval);
  }
  throw const NeoRouterConnectException(NeoRouterConnectProblem.timedOut);
}
