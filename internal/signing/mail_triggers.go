package signing

import (
	"context"
	"database/sql"
	"errors"
	"slices"
)

// When the signing lifecycle sends mail. These only decide and queue, inside
// the transaction of the step; without a configured mail service they do
// nothing, so signing stays link-only.

// canBeMailed: it has an address and is not the owner signing themselves.
func canBeMailed(p party) bool { return p.Email.String != "" && p.IsSelf == 0 }

func (h *Handler) partiesMayBeMailed(d document) bool { return h.mailEnabled() && d.DeliverEmail == 1 }

// queueInvitations queues the invitations due right after sending and says
// what happens to each party.
func (h *Handler) queueInvitations(ctx context.Context, q querier, d document, parties []party) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(parties))
	for _, p := range parties {
		entry := map[string]any{"partyId": p.ID, "status": "skipped"}
		if p.Email.String == "" {
			entry["status"] = "no_email"
		} else if h.partiesMayBeMailed(d) && canBeMailed(p) && p.Status != partyWaiting {
			code, err := h.enqueueRequest(ctx, q, d, p.ID, mailInvitation, p.Email.String)
			if err != nil {
				return nil, err
			}
			if code == "" {
				entry["status"] = "queued"
			} else {
				entry["code"] = code
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// queueRenewedInvitation mails a party's new link again; nil when it is not
// mailed.
func (h *Handler) queueRenewedInvitation(ctx context.Context, q querier, d document, p party) (map[string]any, error) {
	if !h.partiesMayBeMailed(d) || !canBeMailed(p) || p.Status == partyWaiting {
		return nil, nil
	}
	if err := cancelQueued(ctx, q, d.ID, p.ID, requestKinds...); err != nil {
		return nil, err
	}
	code, err := h.enqueueRequest(ctx, q, d, p.ID, mailInvitation, p.Email.String)
	if code != "" {
		return map[string]any{"partyId": p.ID, "status": "skipped", "code": code}, err
	}
	return map[string]any{"partyId": p.ID, "status": "queued"}, err
}

// verifiedEmailOf is the only kind of address the owner's notices go to.
func verifiedEmailOf(ctx context.Context, q querier, userID int64) (string, error) {
	var email, verified, disabled text
	err := q.QueryRowContext(ctx, `SELECT email,email_verified_at,disabled_at FROM users WHERE id=?`, userID).Scan(&email, &verified, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil || !verified.Valid || disabled.Valid {
		return "", err
	}
	return email.String, nil
}

// queueNotice tells the owner about a party or the document.
func (h *Handler) queueNotice(ctx context.Context, q querier, kind string, d document, p *party, extra map[string]any) error {
	to, err := verifiedEmailOf(ctx, q, d.OwnerID)
	if err != nil || to == "" || !h.mailEnabled() {
		return err
	}
	parties, err := listParties(ctx, q, d.ID)
	if err != nil {
		return err
	}
	done := 0
	for _, other := range parties {
		if other.Status == partyCompleted {
			done++
		}
	}
	name := ""
	if p != nil {
		name = p.SignerName.String
		if name == "" {
			name = p.Name
		}
	}
	payload := map[string]any{"name": name, "done": done, "total": len(parties)}
	for key, v := range extra {
		payload[key] = v
	}
	return h.enqueueMail(ctx, q, d.ID, "", kind, to, payload)
}

// queueCompletionStep cancels the party's pending mail, tells the next one it
// is its turn and the owner when their profile asks.
func (h *Handler) queueCompletionStep(ctx context.Context, q querier, documentID, partyID, unlocked string, finalizing bool) error {
	d, err := documentByID(ctx, q, documentID)
	if err != nil {
		return err
	}
	if err := cancelQueued(ctx, q, d.ID, partyID, requestKinds...); err != nil {
		return err
	}
	parties, err := listParties(ctx, q, d.ID)
	if err != nil {
		return err
	}
	find := func(id string) (party, bool) {
		i := slices.IndexFunc(parties, func(p party) bool { return p.ID == id })
		if i < 0 {
			return party{}, false
		}
		return parties[i], true
	}
	if next, ok := find(unlocked); ok && h.partiesMayBeMailed(d) && canBeMailed(next) {
		if _, err := h.enqueueRequest(ctx, q, d, next.ID, mailYourTurn, next.Email.String); err != nil {
			return err
		}
	}
	p, ok := find(partyID)
	if finalizing || !ok || p.IsSelf == 1 {
		return nil
	}
	settings, err := h.profileOf(ctx, q, d.OwnerID)
	if err != nil || !settings.notify {
		return err
	}
	return h.queueNotice(ctx, q, mailCreatorSigned, d, &p, nil)
}

func (h *Handler) queueDecline(ctx context.Context, q querier, documentID, partyID, reason string) error {
	d, err := documentByID(ctx, q, documentID)
	if err != nil {
		return err
	}
	if err := cancelQueued(ctx, q, d.ID, "", requestKinds...); err != nil {
		return err
	}
	p, err := partyByID(ctx, q, partyID)
	if err != nil || p.IsSelf == 1 {
		return err
	}
	return h.queueNotice(ctx, q, mailCreatorDeclined, d, &p, map[string]any{"reason": reason})
}

// queueCompletionMails: every party with an address and the owner get the
// signed file.
func (h *Handler) queueCompletionMails(ctx context.Context, q querier, documentID string) error {
	d, err := documentByID(ctx, q, documentID)
	if err != nil || !h.mailEnabled() {
		return err
	}
	if h.partiesMayBeMailed(d) {
		parties, err := listParties(ctx, q, d.ID)
		if err != nil {
			return err
		}
		for _, p := range parties {
			if canBeMailed(p) {
				if _, err := h.enqueueRequest(ctx, q, d, p.ID, mailCompleted, p.Email.String); err != nil {
					return err
				}
			}
		}
	}
	to, err := verifiedEmailOf(ctx, q, d.OwnerID)
	if err != nil || to == "" {
		return err
	}
	return h.enqueueMail(ctx, q, d.ID, "", mailCompleted, to, nil)
}
