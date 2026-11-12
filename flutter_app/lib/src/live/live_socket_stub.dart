import 'live_connection.dart';

/// Native apps sign in with a device token, which the socket doesn't
/// accept; they poll the change journal instead.
LiveSocketOpener? liveSocketOpener() => null;
