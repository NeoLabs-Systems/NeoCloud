part of 'main.dart';

// Pieces of the progress page: the header, one card per party, the timeline.

class _StatusHeader extends StatelessWidget {
  const _StatusHeader({
    required this.detail,
    required this.onBack,
    required this.onCancel,
    required this.onDelete,
    required this.onDuplicate,
    required this.onOpenSigned,
    required this.onDownloadSigned,
    required this.onRetryFinalization,
  });

  final SigningDetail detail;
  final VoidCallback onBack;
  final VoidCallback onCancel;
  final VoidCallback onDelete;
  final VoidCallback onDuplicate;
  final VoidCallback onOpenSigned;
  final ValueChanged<bool> onDownloadSigned;
  final VoidCallback onRetryFinalization;

  @override
  Widget build(BuildContext context) {
    final document = detail.document;
    final total = detail.parties.length;
    final done = detail.parties.where((party) => party.status == PartyStatus.completed).length;
    final completed = document.status == SigningStatus.completed;
    return _AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              IconButton(key: const ValueKey<String>('signing-back'), tooltip: appStrings.signingFinish, onPressed: onBack, icon: const Icon(Icons.arrow_back_rounded)),
              Expanded(child: Text(document.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: _displayTitleStyle(22))),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 14,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              _StatusPill(label: _statusLabel(document.status), color: _statusColor(document.status)),
              Text(appStrings.signingProgressCount('$done', '$total'), style: TextStyle(color: _textSecondary, fontSize: 13)),
              if (document.expiresAt != null && (document.status == SigningStatus.sent || document.status == SigningStatus.inProgress))
                Text(appStrings.signingExpiresOn(DateFormat.yMMMd(currentAppLanguage.code).format(document.expiresAt!.toLocal())), style: TextStyle(color: _textMuted, fontSize: 13)),
            ],
          ),
          const SizedBox(height: 12),
          ClipRRect(
            borderRadius: BorderRadius.circular(4),
            child: LinearProgressIndicator(
              key: const ValueKey<String>('signing-progress'),
              value: total == 0 ? 0 : done / total,
              minHeight: 6,
              backgroundColor: _bgTertiary,
              color: completed ? _success : _accent,
            ),
          ),
          if (document.status == SigningStatus.finalizing) ...<Widget>[
            const SizedBox(height: 12),
            _Notice(message: document.finalizeError == null ? appStrings.signingFinalizing : appStrings.signingFinalizeFailed, color: _warning, icon: Icons.hourglass_top_rounded),
            Align(alignment: Alignment.centerLeft, child: TextButton(key: const ValueKey<String>('signing-retry-final'), onPressed: onRetryFinalization, child: Text(appStrings.signingRetryNow))),
          ],
          const SizedBox(height: 14),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              if (completed && document.finalNodeId != null)
                FilledButton.icon(key: const ValueKey<String>('signing-open-signed'), onPressed: onOpenSigned, icon: const Icon(Icons.folder_open_outlined, size: 18), label: Text(appStrings.signingOpenSigned)),
              if (completed && document.finalNodeId != null)
                _DownloadSplitButton(keyPrefix: 'signing-download-signed', label: appStrings.signingDownloadSigned, onDownload: onDownloadSigned),
              OutlinedButton.icon(onPressed: onDuplicate, icon: const Icon(Icons.copy_rounded, size: 18), label: Text(appStrings.signingDuplicate)),
              if (_canCancelSigning(document.status))
                OutlinedButton.icon(key: const ValueKey<String>('signing-cancel'), onPressed: onCancel, icon: const Icon(Icons.block_rounded, size: 18), label: Text(appStrings.signingCancelDoc)),
              if (_canDeleteSigning(document.status))
                TextButton.icon(
                  key: const ValueKey<String>('signing-delete'),
                  onPressed: onDelete,
                  style: TextButton.styleFrom(foregroundColor: _danger),
                  icon: const Icon(Icons.delete_outline_rounded, size: 18),
                  label: Text(appStrings.signingDeleteDoc),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _PartyCard extends StatelessWidget {
  const _PartyCard({
    required this.party,
    required this.index,
    required this.live,
    required this.onRegenerate,
    required this.onRevoke,
    this.mail = false,
    this.autoDays = 0,
    this.remind,
    this.onReminded,
    this.onSignNow,
  });

  final SigningParty party;
  final int index;
  final bool live;
  final VoidCallback onRegenerate;
  final VoidCallback onRevoke;

  /// The server sends mail: delivery state and reminders are shown.
  final bool mail;
  final int autoDays;

  /// Sends a reminder; null when this party can't be reminded.
  final Future<void> Function()? remind;
  final VoidCallback? onReminded;

  /// The creator's own part, to sign here and now.
  final VoidCallback? onSignNow;

  String _when(DateTime? time) => time == null ? '' : formatDate(time.toLocal(), currentAppLanguage.code);

  @override
  Widget build(BuildContext context) {
    final finished = party.status == PartyStatus.completed || party.status == PartyStatus.declined;
    final time = party.status == PartyStatus.completed ? party.completedAt : (party.status == PartyStatus.declined ? party.declinedAt : party.openedAt);
    final color = resolvePartyColor(party.color, index);
    return Padding(
      key: ValueKey<String>('party-card-${party.id}'),
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              _PartyDot(name: party.name, color: color, status: party.status, size: 34),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(party.signerName != null && party.signerName != party.name ? '${party.name} · ${party.signerName}' : party.name, style: TextStyle(color: _textPrimary, fontWeight: FontWeight.w600)),
                    if (party.email != null) Text(party.email!, style: TextStyle(color: _textMuted, fontSize: 12.5)),
                  ],
                ),
              ),
              Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: <Widget>[
                  _StatusPill(label: _partyStatusLabel(party.status), color: _partyStatusColor(party.status)),
                  if (time != null) Text(_when(time), style: TextStyle(color: _textMuted, fontSize: 12)),
                ],
              ),
            ],
          ),
          if ((mail || party.delivery.status != DeliveryStatus.none) && !party.isSelf) _PartyDeliveryInfo(party: party, autoDays: autoDays),
          if (party.status == PartyStatus.declined && party.declineReason != null)
            Padding(padding: const EdgeInsets.only(top: 8), child: Text(appStrings.signingDeclineReason(party.declineReason!), style: TextStyle(color: _textSecondary, fontSize: 13))),
          if (!finished)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: <Widget>[
                  Icon(Icons.link_rounded, size: 16, color: _textMuted),
                  Text(party.hasLink ? appStrings.signingLinkHint(party.linkHint ?? '…') : appStrings.signingNoLink, style: TextStyle(color: _textMuted, fontSize: 12.5)),
                  if (live) ...<Widget>[
                    TextButton(key: ValueKey<String>('regen-${party.id}'), onPressed: onRegenerate, child: Text(appStrings.signingRegenerate)),
                    if (party.hasLink) TextButton(key: ValueKey<String>('revoke-${party.id}'), onPressed: onRevoke, child: Text(appStrings.signingRevoke)),
                  ],
                ],
              ),
            ),
          if (!finished && (remind != null || onSignNow != null || (mail && live && party.status != PartyStatus.waiting && (party.email ?? '').isEmpty && party.delivery.status != DeliveryStatus.noEmail)))
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 8,
                runSpacing: 8,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: <Widget>[
                  if (remind != null) _ReminderButton(key: ValueKey<String>('reminder-${party.id}'), party: party, remind: remind!, onSent: onReminded ?? () {}),
                  if (remind == null && onSignNow == null) Text(appStrings.signingDeliveryNoEmail, style: TextStyle(color: _textMuted, fontSize: 12.5)),
                  if (onSignNow != null) OutlinedButton.icon(key: ValueKey<String>('sign-now-${party.id}'), onPressed: onSignNow, icon: const Icon(Icons.draw_outlined, size: 18), label: Text(appStrings.signingSignNow)),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

String _eventText(SigningEvent event, String party) {
  switch (event.type) {
    case SigningEventType.created:
      return appStrings.signingEventCreated;
    case SigningEventType.updated:
      return appStrings.signingEventUpdated;
    case SigningEventType.sent:
      return appStrings.signingEventSent;
    case SigningEventType.linkRegenerated:
      return appStrings.signingEventLinkRegenerated(party);
    case SigningEventType.revoked:
      return appStrings.signingEventRevoked(party);
    case SigningEventType.opened:
      return appStrings.signingEventOpened(party);
    case SigningEventType.consented:
      return appStrings.signingEventConsented(party);
    case SigningEventType.partyCompleted:
      return appStrings.signingEventPartyCompleted(party);
    case SigningEventType.partyDeclined:
      return appStrings.signingEventPartyDeclined(party);
    case SigningEventType.completed:
      return appStrings.signingEventCompleted;
    case SigningEventType.finalized:
      return appStrings.signingEventFinalized;
    case SigningEventType.canceled:
      return appStrings.signingEventCanceled;
    case SigningEventType.expired:
      return appStrings.signingEventExpired;
    case SigningEventType.finalDownloaded:
      return appStrings.signingEventFinalDownloaded;
    case SigningEventType.pagesRotated:
      return appStrings.signingEventPagesRotated;
    case SigningEventType.other:
      return appStrings.signingEventOther;
  }
}

class _Timeline extends StatelessWidget {
  const _Timeline({required this.log, required this.parties, required this.verifyMessage, required this.verifyOk, required this.onVerify, required this.onDownloadAudit});

  final SigningEventLog? log;
  final List<SigningParty> parties;
  final String? verifyMessage;
  final bool verifyOk;
  final VoidCallback onVerify;
  final VoidCallback onDownloadAudit;

  String _partyName(String? id) => parties.where((party) => party.id == id).firstOrNull?.name ?? '';

  @override
  Widget build(BuildContext context) {
    final events = (log?.events ?? const <SigningEvent>[]).reversed.toList(growable: false);
    return _AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(appStrings.signingTimeline, style: _sectionTitleStyle()),
          const SizedBox(height: 10),
          for (final event in events)
            Padding(
              key: ValueKey<String>('event-${event.id}'),
              padding: const EdgeInsets.symmetric(vertical: 5),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Padding(padding: const EdgeInsets.only(top: 6, right: 10), child: Container(width: 7, height: 7, decoration: BoxDecoration(color: _textMuted, shape: BoxShape.circle))),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(_eventText(event, _partyName(event.partyId)), style: TextStyle(color: _textPrimary, fontSize: 13.5)),
                        if (event.at != null) Text(formatDate(event.at!.toLocal(), currentAppLanguage.code), style: TextStyle(color: _textMuted, fontSize: 12)),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: <Widget>[
              OutlinedButton.icon(key: const ValueKey<String>('signing-verify'), onPressed: onVerify, icon: const Icon(Icons.verified_outlined, size: 18), label: Text(appStrings.signingVerify)),
              OutlinedButton.icon(key: const ValueKey<String>('signing-audit'), onPressed: onDownloadAudit, icon: const Icon(Icons.download_rounded, size: 18), label: Text(appStrings.signingAuditDownload)),
            ],
          ),
          if (verifyMessage != null) ...<Widget>[
            const SizedBox(height: 10),
            _Notice(message: verifyMessage!, color: verifyOk ? _success : _danger, icon: verifyOk ? Icons.check_circle_outline_rounded : Icons.error_outline_rounded),
          ],
        ],
      ),
    );
  }
}
