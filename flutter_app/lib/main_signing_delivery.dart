part of 'main.dart';

// Mail around a signing document: the options in the send dialog, the chips
// that say how an invitation went, and the reminder controls of the progress
// page. All of it exists only when the server can send mail
// (`capabilities.email.configured`); without mail nothing here is drawn.

const int _signingMessageMax = 1000;

extension NeoCloudSigningMail on NeoCloudController {
  /// Whether the server sends signing mail. Asked once, then kept; false when
  /// the server can't say.
  Future<bool> signingMailAvailable() async {
    final known = _signingMail;
    if (known != null) return known;
    try {
      return _signingMail = (await signing.capabilities()).emailConfigured;
    } catch (_) {
      return false;
    }
  }
}

String _deliveryLabel(DeliveryStatus status, {bool waitsTurn = false}) {
  if (waitsTurn && status == DeliveryStatus.skipped) return appStrings.signingDeliveryWaitsTurn;
  switch (status) {
    case DeliveryStatus.sent:
      return appStrings.signingDeliverySent;
    case DeliveryStatus.queued:
      return appStrings.signingDeliveryQueued;
    case DeliveryStatus.failed:
      return appStrings.signingDeliveryFailed;
    case DeliveryStatus.noEmail:
      return appStrings.signingDeliveryNoEmail;
    case DeliveryStatus.skipped:
      return appStrings.signingDeliverySkipped;
    case DeliveryStatus.none:
      return '';
  }
}

Color _deliveryColor(DeliveryStatus status) {
  switch (status) {
    case DeliveryStatus.sent:
      return _success;
    case DeliveryStatus.queued:
      return _info;
    case DeliveryStatus.failed:
      return _danger;
    case DeliveryStatus.noEmail:
    case DeliveryStatus.skipped:
    case DeliveryStatus.none:
      return _textMuted;
  }
}

/// "Email sent", "Sending…", "Email failed" …; nothing for a party that was
/// not mailed at all.
class _DeliveryChip extends StatelessWidget {
  const _DeliveryChip({super.key, required this.status, this.waitsTurn = false});

  final DeliveryStatus status;

  /// A sequential document: this party's mail goes out when their turn comes.
  final bool waitsTurn;

  @override
  Widget build(BuildContext context) {
    if (status == DeliveryStatus.none) return const SizedBox.shrink();
    return _StatusPill(label: _deliveryLabel(status, waitsTurn: waitsTurn), color: _deliveryColor(status));
  }
}

/// The wording of a countdown: "3 h 12 min", "7 min", "40 s".
String _waitText(Duration wait) {
  if (wait.inHours >= 1) return '${wait.inHours} h ${wait.inMinutes % 60} min';
  if (wait.inMinutes >= 1) return '${wait.inMinutes} min';
  return '${math.max(1, wait.inSeconds)} s';
}

/// Why a mail failed, by the server's stable code; never the mail server's own text.
String _deliveryErrorText(String? code) => switch (code) {
  'SMTP_UNREACHABLE' => appStrings.signingDeliveryErrorUnreachable,
  'SMTP_REJECTED' => appStrings.signingDeliveryErrorRejected,
  'INVALID_ADDRESS' => appStrings.signingDeliveryErrorInvalidAddress,
  'RATE_LIMITED' => appStrings.signingDeliveryErrorRateLimited,
  _ => appStrings.signingDeliveryErrorUnknown,
};

/// A sentence for a refused reminder, by the server's code.
String _reminderErrorText(SigningException error, String name) {
  switch (error.code) {
    case 'PARTY_NOT_ACTIVE':
      return appStrings.signingRemindNotActive(name);
    case 'NO_EMAIL':
      return appStrings.signingRemindNoEmail(name);
    case 'EMAIL_NOT_CONFIGURED':
      return appStrings.signingRemindNotConfigured;
    case 'REMINDER_LIMIT':
      return appStrings.signingRemindLimit(name);
    case 'EMAIL_NOT_VERIFIED':
      return appStrings.signingMailNotVerified;
    case 'MAIL_QUOTA_EXCEEDED':
      return appStrings.signingMailQuota;
    case 'REMINDER_TOO_SOON':
      return appStrings.signingRemindWait(_waitText(error.retryAfter ?? const Duration(hours: 24)));
    default:
      return error.isNetwork ? appStrings.signingRemindOffline : appStrings.signingRemindFailed;
  }
}

/// The options under the recipients: the invitation mail, a personal note and
/// automatic reminders.
class _SendMailOptions extends StatelessWidget {
  const _SendMailOptions({required this.deliver, required this.onDeliver, required this.message, required this.onMessage, required this.reminderDays, required this.onReminderDays, required this.mailCount});

  final bool deliver;
  final ValueChanged<bool> onDeliver;
  final TextEditingController message;
  final VoidCallback onMessage;
  final int reminderDays;
  final ValueChanged<int> onReminderDays;

  /// How many parties have an address.
  final int mailCount;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        const SizedBox(height: 4),
        _ToggleLine(
          key: const ValueKey<String>('send-mail-toggle'),
          title: appStrings.signingMailSend,
          subtitle: mailCount == 0 ? appStrings.signingMailSendNoAddress : appStrings.signingMailSendHint,
          value: deliver,
          onChanged: onDeliver,
        ),
        if (deliver) ...<Widget>[
          const SizedBox(height: 10),
          TextField(
            key: const ValueKey<String>('send-message'),
            controller: message,
            maxLength: _signingMessageMax,
            maxLines: 3,
            minLines: 2,
            textCapitalization: TextCapitalization.sentences,
            onChanged: (_) => onMessage(),
            decoration: InputDecoration(labelText: appStrings.signingMailMessage, hintText: appStrings.signingMailMessageHint, alignLabelWithHint: true),
          ),
          const SizedBox(height: 6),
          Text(appStrings.signingMailReminders, style: TextStyle(color: _textSecondary, fontSize: 12.5)),
          const SizedBox(height: 6),
          DropdownButtonFormField<int>(
            key: const ValueKey<String>('send-reminders'),
            initialValue: reminderDays,
            isDense: true,
            items: <DropdownMenuItem<int>>[
              for (final days in SendOptions.reminderChoices)
                DropdownMenuItem<int>(value: days, child: Text(days == 0 ? appStrings.signingMailRemindOff : appStrings.signingMailRemindEvery(days))),
            ],
            onChanged: (value) => onReminderDays(value ?? 0),
          ),
        ],
      ],
    );
  }
}

/// A title and a sentence beside a switch.
class _ToggleLine extends StatelessWidget {
  const _ToggleLine({super.key, required this.title, required this.value, required this.onChanged, this.subtitle});

  final String title;
  final String? subtitle;
  final bool value;
  final ValueChanged<bool>? onChanged;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: <Widget>[
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              Text(title, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
              if (subtitle != null) Padding(padding: const EdgeInsets.only(top: 2), child: Text(subtitle!, style: TextStyle(color: _textSecondary, fontSize: 12.5, height: 1.4))),
            ],
          ),
        ),
        const SizedBox(width: 12),
        Switch(value: value, onChanged: onChanged),
      ],
    );
  }
}

/// "Send reminder" for one party: disabled with a countdown while the
/// server says it is too soon, with a sentence for each refusal.
class _ReminderButton extends StatefulWidget {
  const _ReminderButton({super.key, required this.party, required this.remind, required this.onSent});

  final SigningParty party;

  /// Sends the reminder; throws [SigningException] when refused.
  final Future<void> Function() remind;
  final VoidCallback onSent;

  @override
  State<_ReminderButton> createState() => _ReminderButtonState();
}

class _ReminderButtonState extends State<_ReminderButton> {
  bool _busy = false;
  DateTime? _until;
  String? _error;
  Timer? _tick;

  @override
  void initState() {
    super.initState();
    _syncFromParty();
  }

  @override
  void didUpdateWidget(_ReminderButton old) {
    super.didUpdateWidget(old);
    if (old.party.delivery.lastSentAt != widget.party.delivery.lastSentAt) _syncFromParty();
  }

  @override
  void dispose() {
    _tick?.cancel();
    super.dispose();
  }

  /// A reminder is allowed once a day: a mail sent less than a day ago holds
  /// the button back without asking the server.
  void _syncFromParty() {
    final last = widget.party.delivery.lastSentAt;
    if (last == null || widget.party.delivery.remindersSent == 0) return;
    final until = last.add(const Duration(hours: 24));
    if (until.isAfter(DateTime.now())) _holdUntil(until);
  }

  void _holdUntil(DateTime until) {
    _until = until;
    _tick?.cancel();
    _tick = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      if (!_until!.isAfter(DateTime.now())) {
        _tick?.cancel();
        setState(() => _until = null);
      } else {
        setState(() {});
      }
    });
  }

  Future<void> _press() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.remind();
      if (!mounted) return;
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(SnackBar(content: Text(appStrings.signingRemindSent(widget.party.name))));
      _holdUntil(DateTime.now().add(const Duration(hours: 24)));
      widget.onSent();
    } on SigningException catch (error) {
      if (!mounted) return;
      if (error.code == 'REMINDER_TOO_SOON') {
        _holdUntil(DateTime.now().add(error.retryAfter ?? const Duration(hours: 24)));
      } else {
        _error = _reminderErrorText(error, widget.party.name);
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final until = _until;
    final waiting = until != null && until.isAfter(DateTime.now());
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        OutlinedButton.icon(
          key: ValueKey<String>('remind-${widget.party.id}'),
          onPressed: _busy || waiting ? null : () => unawaited(_press()),
          icon: _busy ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.notifications_active_outlined, size: 18),
          label: Text(waiting ? appStrings.signingRemindWait(_waitText(until.difference(DateTime.now()))) : appStrings.signingRemindSend),
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: Text(_error!, key: ValueKey<String>('remind-error-${widget.party.id}'), style: TextStyle(color: _danger, fontSize: 12.5, height: 1.35)),
          ),
      ],
    );
  }
}

/// The delivery lines of a party card: the chip, when it was sent, how many
/// reminders went out and when the next automatic one is due.
class _PartyDeliveryInfo extends StatelessWidget {
  const _PartyDeliveryInfo({required this.party, required this.autoDays});

  final SigningParty party;
  final int autoDays;

  @override
  Widget build(BuildContext context) {
    final delivery = party.delivery;
    if (delivery.status == DeliveryStatus.none) return const SizedBox.shrink();
    final language = currentAppLanguage.code;
    final failed = delivery.status == DeliveryStatus.failed;
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Wrap(
            spacing: 12,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              _DeliveryChip(key: ValueKey<String>('delivery-${party.id}'), status: delivery.status, waitsTurn: party.status == PartyStatus.waiting),
              if (delivery.lastSentAt != null) Text(appStrings.signingDeliveryLastSent(formatDate(delivery.lastSentAt!.toLocal(), language)), style: TextStyle(color: _textMuted, fontSize: 12.5)),
              if (delivery.remindersSent > 0) Text(appStrings.signingRemindersSent(delivery.remindersSent), key: ValueKey<String>('reminders-${party.id}'), style: TextStyle(color: _textMuted, fontSize: 12.5)),
              if (delivery.nextReminderAt != null && autoDays > 0)
                Text(appStrings.signingNextReminder(formatDate(delivery.nextReminderAt!.toLocal(), language)), key: ValueKey<String>('next-reminder-${party.id}'), style: TextStyle(color: _textMuted, fontSize: 12.5)),
            ],
          ),
          if (failed)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                '${appStrings.signingDeliveryFailedHelp} (${_deliveryErrorText(delivery.errorCode)})',
                style: TextStyle(color: _danger, fontSize: 12.5, height: 1.35),
              ),
            ),
        ],
      ),
    );
  }
}
