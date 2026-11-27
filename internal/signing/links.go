package signing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Sending a document and managing the links of its parties. A link's URL
// exists only in the response that creates it: the database keeps the hash,
// and for a party with an address an encrypted copy of the token so later
// mails can link to the same page.

const consentVersion = 1

var consentTexts = map[string]string{
	"en": "I agree to review and sign this document electronically. I understand that my electronic signature is meant to have the same effect as a handwritten one, that I may decline to sign, and that I can ask the sender for a paper copy instead. My name, the time of each step, my IP address and my browser are recorded as evidence of the signing.",
	"de": "Ich bin damit einverstanden, dieses Dokument elektronisch zu prüfen und zu unterschreiben. Ich weiß, dass meine elektronische Unterschrift die gleiche Absicht ausdrückt wie eine handschriftliche, dass ich die Unterschrift ablehnen kann und dass ich beim Absender stattdessen eine Papierfassung verlangen kann. Mein Name, der Zeitpunkt jedes Schritts, meine IP-Adresse und mein Browser werden als Nachweis der Unterzeichnung gespeichert.",
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *Handler) linkURL(r *http.Request, token string) string {
	origin := ""
	if h.publicOrigin != nil {
		origin = h.publicOrigin(r)
	}
	return strings.TrimRight(origin, "/") + signPagePath + token
}

// tokenCopy is kept only for a party that has an address.
func (h *Handler) tokenCopy(p party, token string) (any, error) {
	if !p.Email.Valid || p.Email.String == "" {
		return nil, nil
	}
	return h.keys.EncryptValue(token)
}

var reminderChoices = []float64{0, 2, 3, 5, 7, 14}

type sendOptions struct {
	message      any
	deliverEmail bool
	reminderDays int
}

// cleanMessage is a plain-text note: line breaks stay, other control
// characters go; "" when empty.
func cleanMessage(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	raw, ok := value.(string)
	if !ok {
		return "", badRequest("The message must be text.")
	}
	message := strings.TrimSpace(plainText(raw))
	if length(message) > maxMailMessage {
		return "", badRequest("A message has at most " + strconv.Itoa(maxMailMessage) + " characters.")
	}
	return message, nil
}

func parseSendOptions(input body) (sendOptions, error) {
	options := sendOptions{deliverEmail: true}
	if v, ok := input["deliverEmail"]; ok {
		deliver, isBool := v.(bool)
		if !isBool {
			return options, badRequest("deliverEmail must be true or false.")
		}
		options.deliverEmail = deliver
	}
	if v, ok := input["reminderDays"]; ok {
		days, isNumber := v.(float64)
		if !isNumber || !slices.Contains(reminderChoices, days) {
			return options, badRequest("reminderDays must be one of: 0, 2, 3, 5, 7, 14.")
		}
		options.reminderDays = int(days)
	}
	message, err := cleanMessage(input["message"])
	options.message = nullIfEmpty(message)
	return options, err
}

type sent struct {
	links    []map[string]any
	delivery []map[string]any
}

// send sends a draft: one link per party.
func (h *Handler) send(r *http.Request, userID int64, documentID string, input body) (sent, error) {
	ctx := r.Context()
	var out sent
	options, optionsErr := parseSendOptions(input)
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if d.Status != statusDraft {
			return httpError(409, "This document was already sent. Create a new link for a party instead.", "ALREADY_SENT")
		}
		if expires, ok := parseStamp(d.ExpiresAt.String); ok && !expires.After(h.now()) {
			return httpError(400, "The expiry has passed. Set a new one before sending.", "EXPIRY_IN_PAST")
		}
		parties, err := listParties(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		fields, err := listFields(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		// A party with nothing to fill in could never "sign".
		for _, p := range parties {
			if !slices.ContainsFunc(fields, func(f field) bool {
				return f.PartyID == p.ID && !f.Readonly && fieldTypes[f.Type].storage != storeStatic
			}) {
				return httpError(400, p.Name+" has nothing to fill in or sign.", "PARTY_WITHOUT_FIELDS")
			}
		}
		if optionsErr != nil {
			return optionsErr
		}
		deliver := options.deliverEmail && h.mailEnabled()
		if deliver {
			var items []mailItem
			for i, p := range parties {
				if p.Email.Valid && p.IsSelf == 0 && initialStatus(d.SigningOrder, i) != partyWaiting {
					items = append(items, mailItem{p.Email.String, mailInvitation})
				}
			}
			if err := h.assertMayMail(ctx, tx, userID, items); err != nil {
				return err
			}
		}
		for i := range parties {
			p := &parties[i]
			token, err := newToken()
			if err != nil {
				return err
			}
			sealed, err := h.tokenCopy(*p, token)
			if err != nil {
				return err
			}
			p.Status = initialStatus(d.SigningOrder, i)
			if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET token_hash=?,token_hint=?,token_enc=?,status=? WHERE id=?`,
				hashToken(token), token[len(token)-4:], sealed, p.Status, p.ID); err != nil {
				return err
			}
			out.links = append(out.links, map[string]any{"partyId": p.ID, "url": h.linkURL(r, token)})
		}
		at := h.stampNow()
		if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='sent',sent_at=?,consent_text_version=?,message=?,reminder_days=?,deliver_email=?,updated_at=? WHERE id=?`,
			at, consentVersion, options.message, options.reminderDays, flag(deliver), at, d.ID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, d.ID, event{kind: "sent", at: at, client: clientOf(r), data: map[string]any{"parties": len(parties), "order": d.SigningOrder}}); err != nil {
			return err
		}
		d.DeliverEmail = flag(deliver)
		out.delivery, err = h.queueInvitations(ctx, tx, d, parties)
		return err
	})
	if err != nil {
		return out, err
	}
	h.wakeMail()
	h.notifyOwner(ctx, userID, d.ID, statusSent)
	return out, nil
}

func (h *Handler) sendRoute(r *http.Request, userID int64, input body) (any, error) {
	out, err := h.send(r, userID, r.PathValue("id"), input)
	if err != nil {
		return nil, err
	}
	view, err := h.describe(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	view["links"], view["delivery"] = out.links, out.delivery
	return view, nil
}

// livePartyOf is a party of a document that is out for signing and still has
// to act.
func livePartyOf(ctx context.Context, q querier, d document, partyID string) (party, error) {
	if !isLive(d.Status) {
		return party{}, httpError(409, "This document is not out for signing.", "INVALID_STATE")
	}
	p, err := documentParty(ctx, q, d.ID, partyID)
	if err != nil {
		return p, err
	}
	if p.Status == partyCompleted || p.Status == partyDeclined {
		return p, httpError(409, "This party has already finished.", "PARTY_FINISHED")
	}
	return p, nil
}

// regenerateLink gives a party a new link; the old one stops working at once.
func (h *Handler) regenerateLink(r *http.Request, userID int64, documentID, partyID string) (map[string]any, error) {
	ctx := r.Context()
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	response := map[string]any{"url": h.linkURL(r, token)}
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		p, err := livePartyOf(ctx, tx, d, partyID)
		if err != nil {
			return err
		}
		sealed, err := h.tokenCopy(p, token)
		if err != nil {
			return err
		}
		// Whoever agreed and typed a name through the old link must not stand
		// in for the new one.
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET token_hash=?,token_hint=?,token_enc=?,consented_at=NULL,signer_name=NULL,timezone=NULL,
			tz_offset_minutes=NULL,ip=NULL,user_agent=NULL,user_id=CASE WHEN is_self=1 THEN user_id ELSE NULL END WHERE id=?`,
			hashToken(token), token[len(token)-4:], sealed, p.ID); err != nil {
			return err
		}
		if err := revokePartyGrants(ctx, tx, p.ID); err != nil {
			return err
		}
		at := h.stampNow()
		if err := touch(ctx, tx, d.ID, at); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, d.ID, event{kind: "link_regenerated", partyID: p.ID, at: at, client: clientOf(r)}); err != nil {
			return err
		}
		delivery, err := h.queueRenewedInvitation(ctx, tx, d, p)
		if delivery != nil {
			response["delivery"] = delivery
		}
		response["partyId"] = p.ID
		return err
	})
	if err != nil {
		return nil, err
	}
	h.wakeMail()
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return response, nil
}

func (h *Handler) regenerateLinkRoute(r *http.Request, userID int64, _ body) (any, error) {
	return h.regenerateLink(r, userID, r.PathValue("id"), r.PathValue("partyId"))
}

// revokeLink turns a party's link off without replacing it.
func (h *Handler) revokeLink(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	var updated party
	d, err := h.change(ctx, userID, r.PathValue("id"), func(tx *sql.Tx, d document) error {
		p, err := livePartyOf(ctx, tx, d, r.PathValue("partyId"))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET token_hash=NULL,token_hint=NULL,token_enc=NULL WHERE id=?`, p.ID); err != nil {
			return err
		}
		if err := cancelQueued(ctx, tx, d.ID, p.ID); err != nil {
			return err
		}
		if err := revokePartyGrants(ctx, tx, p.ID); err != nil {
			return err
		}
		at := h.stampNow()
		if err := touch(ctx, tx, d.ID, at); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, d.ID, event{kind: "revoked", partyID: p.ID, at: at, client: clientOf(r)}); err != nil {
			return err
		}
		updated, err = partyByID(ctx, tx, p.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return map[string]any{"party": partyView(updated)}, nil
}
