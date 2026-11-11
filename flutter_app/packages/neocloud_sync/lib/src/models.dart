/// The server answered with an error (`{ error, code }`).
class NeoCloudApiException implements Exception {
  NeoCloudApiException(this.status, this.code, this.message);

  final int status;
  final String code;
  final String message;

  bool get isUnauthorized => status == 401;
  bool get isNotFound => status == 404;

  @override
  String toString() => 'NeoCloudApiException($status $code: $message)';
}

/// The server could not be reached (network down, server stopped, 502–504).
class NeoCloudOfflineException implements Exception {
  NeoCloudOfflineException(this.cause);

  final Object cause;

  @override
  String toString() => 'NeoCloudOfflineException($cause)';
}
