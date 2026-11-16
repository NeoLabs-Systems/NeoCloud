import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../api/backend_client.dart';
import '../files/upload_queue.dart';
import 'handoff_models.dart';
import 'background/handoff_background.dart';
import 'handoff_stream.dart';

/// Send to device (docs/handoff.md): the account's other devices, what this
/// device sent and what waits for it. The web app feeds it the socket's
/// events ([handleLive]); a phone app holds the event stream itself.
class HandoffController extends ChangeNotifier {
  HandoffController(
    this.client, {
    SharedPreferences? preferences,
    HandoffStream? Function()? streamFactory,
    HandoffBackground? background,
    this.reconnectBase = const Duration(seconds: 1),
  }) : _preferences = preferences,
       _streamFactory = streamFactory ?? createHandoffStream {
    this.background =
        background ??
        HandoffBackground(client: client, preferences: preferences);
    this.background.receiving = () => receiving;
  }

  static const String _notifyKey = 'handoff_notify_in_browser';
  static const String _receivingKey = 'handoff_receiving';
  final SharedPreferences? _preferences;

  final BackendClient client;

  /// Receiving while the app is closed (Android).
  late final HandoffBackground background;
  final HandoffStream? Function() _streamFactory;
  final Duration reconnectBase;

  List<HandoffDevice> _devices = const <HandoffDevice>[];
  List<HandoffOffer> _sent = const <HandoffOffer>[];
  final List<HandoffOffer> _incoming = <HandoffOffer>[];
  final Set<String> _announced = <String>{};
  HandoffStream? _stream;
  Timer? _retry;
  int _attempt = 0;
  bool _running = false;

  /// Whether this device may be sent to. Off keeps it out of other devices'
  /// lists (and stops receiving in the background). Kept across restarts.
  late bool receiving = _preferences?.getBool(_receivingKey) ?? true;

  /// The person turned on browser notifications for incoming files (web).
  bool get notifyInBrowser => _preferences?.getBool(_notifyKey) ?? false;

  Future<void> setNotifyInBrowser(bool value) async {
    await _preferences?.setBool(_notifyKey, value);
    notifyListeners();
  }

  /// Devices a send can go to now.
  List<HandoffDevice> get targets => <HandoffDevice>[
    for (final device in _devices)
      if (device.isTarget) device,
  ];

  List<HandoffOffer> get sent => _sent;

  /// Offers waiting for this device's answer, oldest first. Each is shown once.
  List<HandoffOffer> get incoming => List<HandoffOffer>.unmodifiable(_incoming);

  /// Set by the UI: an offer arrived that this device has not shown yet.
  void Function(HandoffOffer offer)? onIncoming;

  void start() {
    if (_running) return;
    _running = true;
    unawaited(refresh());
    _openStream();
    unawaited(background.sync());
  }

  void stop() {
    _running = false;
    _retry?.cancel();
    _retry = null;
    _stream?.close();
    _stream = null;
    _devices = const <HandoffDevice>[];
    _sent = const <HandoffOffer>[];
    _incoming.clear();
    _announced.clear();
    unawaited(background.suspend());
    notifyListeners();
  }

  @override
  void dispose() {
    stop();
    super.dispose();
  }

  void _openStream() {
    final token = client.bearerToken;
    final stream = _streamFactory();
    if (!_running || token == null || stream == null) return;
    _stream = stream;
    stream.open(
      client.uri('/api/handoff/events', <String, String>{
        'receive': receiving ? '1' : '0',
      }),
      <String, String>{'Authorization': 'Bearer $token'},
      onEvent: (event, data) => handleLive(event, data),
      onClosed: _reconnectLater,
    );
  }

  void _reconnectLater() {
    if (!_running) return;
    _retry?.cancel();
    final seconds = math.min(30, 1 << math.min(_attempt, 5));
    _attempt += 1;
    _retry = Timer(reconnectBase * seconds, () {
      _openStream();
      unawaited(refresh());
    });
  }

  /// Turns "this device can receive" on or off; the stream reconnects with it.
  void setReceiving(bool value) {
    if (receiving == value) return;
    receiving = value;
    unawaited(_preferences?.setBool(_receivingKey, value));
    unawaited(background.sync());
    if (_stream != null) {
      _stream!.close();
      _openStream();
    }
    notifyListeners();
  }

  /// An event of the live connection (socket or stream).
  void handleLive(String event, Object? data) {
    _attempt = 0;
    final payload = _decode(data);
    switch (event) {
      case 'handoff:devices':
        unawaited(refreshDevices());
      case 'handoff:offer':
        if (payload != null) _offered(HandoffOffer.fromJson(payload));
      case 'handoff:updated':
        final offer = payload?['offer'];
        if (offer is Map) {
          _updated(HandoffOffer.fromJson(offer.cast<String, dynamic>()));
        }
    }
  }

  Map<String, dynamic>? _decode(Object? data) {
    if (data is Map) return data.cast<String, dynamic>();
    if (data is String && data.isNotEmpty) {
      final decoded = _jsonOrNull(data);
      return decoded is Map ? decoded.cast<String, dynamic>() : null;
    }
    return null;
  }

  /// Shown once per offer id, however often the server pushes it again.
  void _offered(HandoffOffer offer) {
    if (!receiving || !_announced.add(offer.id)) return;
    _incoming.add(offer);
    notifyListeners();
    onIncoming?.call(offer);
  }

  void _updated(HandoffOffer offer) {
    if (offer.receiving) {
      // Answered (here, or from the notification of the background receiver),
      // cancelled or expired: nothing is left to ask.
      if (offer.status != HandoffStatus.pending) {
        _incoming.removeWhere((other) => other.id == offer.id);
      }
    } else {
      _sent = <HandoffOffer>[
        offer,
        for (final other in _sent)
          if (other.id != offer.id) other,
      ];
    }
    notifyListeners();
  }

  Future<void> refresh() async {
    await Future.wait(<Future<void>>[refreshDevices(), _refreshOffers()]);
  }

  Future<void> refreshDevices() async {
    try {
      _devices = await client.handoffDevices();
      notifyListeners();
    } catch (_) {
      // Offline; the next event or refresh tries again.
    }
  }

  Future<void> _refreshOffers() async {
    try {
      final lists = await client.handoffOffers();
      _sent = lists.sent;
      final waiting = <HandoffOffer>[
        for (final offer in lists.inbox)
          if (offer.status == HandoffStatus.pending) offer,
      ];
      // What was answered meanwhile (a notification's button) isn't asked again.
      _incoming.removeWhere(
        (known) => !waiting.any((offer) => offer.id == known.id),
      );
      waiting.forEach(_offered);
      notifyListeners();
    } catch (_) {
      // Offline.
    }
  }

  /// Takes [offer] off the waiting list once the person answered it.
  void answered(HandoffOffer offer) {
    _incoming.removeWhere((other) => other.id == offer.id);
    notifyListeners();
  }

  /// Sends [files] from this device and [nodeIds] (files in NeoCloud) to
  /// [target]. [progress] gets the bytes uploaded so far. Returns the send.
  Future<HandoffOffer> send(
    HandoffDevice target, {
    List<UploadSource> files = const <UploadSource>[],
    List<String> nodeIds = const <String>[],
    void Function(int sent, int total)? progress,
  }) async {
    var offer = await client.createHandoff(
      targetId: target.id,
      files: <({String name, int size})>[
        for (final file in files) (name: file.name, size: file.size),
      ],
      nodeIds: nodeIds,
    );
    _sent = <HandoffOffer>[
      offer,
      for (final other in _sent)
        if (other.id != offer.id) other,
    ];
    notifyListeners();
    if (files.isNotEmpty) {
      final total = files.fold<int>(0, (sum, file) => sum + file.size);
      var done = 0;
      try {
        for (var i = 0; i < files.length; i += 1) {
          final base = done;
          await client.uploadHandoffFile(
            offer.id,
            offer.files[i].id,
            files[i],
            onProgress: (sent) => progress?.call(base + sent, total),
          );
          done += files[i].size;
          progress?.call(done, total);
        }
      } catch (_) {
        unawaited(
          client.cancelHandoff(offer.id).then((_) {}, onError: (Object _) {}),
        );
        rethrow;
      }
      offer = await client.handoffOffer(offer.id);
    }
    _sent = <HandoffOffer>[
      offer,
      for (final other in _sent)
        if (other.id != offer.id) other,
    ];
    notifyListeners();
    return offer;
  }

  Future<void> cancel(HandoffOffer offer) async {
    await client.cancelHandoff(offer.id);
    await _refreshOffers();
  }
}

Object? _jsonOrNull(String text) {
  try {
    return jsonDecode(text);
  } on FormatException {
    return null;
  }
}
