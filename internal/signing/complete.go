package signing

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// A signer finishing or declining. Completing is one transaction: required
// fields are checked against what is stored, the party is marked complete and
// the next turn opens or, after the last one, the document moves to
// finalizing.

func contains(list []string, item string) bool { return slices.Contains(list, item) }

// completeParty is idempotent: a completed party gets the same answer again.
func (h *Handler) completeParty(r *http.Request, c signerContext, _ body) (any, error) {
	ctx := r.Context()
	if c.state == "completed" {
		available, err := finalAvailable(ctx, h.db, c.document)
		return map[string]any{"state": "completed", "finalAvailable": available}, err
	}
	if err := c.ready(); err != nil {
		return nil, err
	}
	finalizing := false
	err := h.tx(ctx, func(tx *sql.Tx) error {
		if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, true); err != nil {
			return err
		}
		at, from := h.stampNow(), clientOf(r)
		fields, err := queryFields(ctx, tx, `WHERE document_id=? AND party_id=? ORDER BY position`, c.document.ID, c.party.ID)
		if err != nil {
			return err
		}
		stored, err := queryValues(ctx, tx, `WHERE document_id=? AND party_id=?`, c.document.ID, c.party.ID)
		if err != nil {
			return err
		}
		values := map[string]value{}
		for _, v := range stored {
			values[v.FieldID] = v
		}
		count := 0
		missing := []string{}
		for _, f := range fields {
			if fieldTypes[f.Type].storage == storeStatic {
				continue
			}
			count++
			v, ok := values[f.ID]
			// A default the signer left alone keeps that value: it is stored
			// now, so the document's final form is made of stored values.
			if !ok && f.Default.Valid {
				v, ok = value{ValueJSON: f.Default}, true
				if _, err := tx.ExecContext(ctx, `INSERT INTO sign_values(id,document_id,field_id,party_id,value_json,updated_at) VALUES(?,?,?,?,?,?)`,
					uuid.NewString(), c.document.ID, f.ID, c.party.ID, f.Default.String, at); err != nil {
					return err
				}
			}
			var current any
			if ok {
				current = v.decoded()
			}
			if f.Required && !isFilled(f.Type, f.prefs(), current) {
				missing = append(missing, f.ID)
			}
		}
		if len(missing) > 0 {
			incomplete := httpError(400, "Some required fields are still empty.", "FIELDS_INCOMPLETE")
			incomplete.fieldIDs = missing
			return incomplete
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET status='completed',completed_at=?,ip=?,user_agent=? WHERE id=?`,
			at, nullIfEmpty(from.ip), nullIfEmpty(from.userAgent), c.party.ID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, c.document.ID, event{kind: "party_completed", partyID: c.party.ID, at: at, client: from, data: map[string]any{"fields": count}}); err != nil {
			return err
		}
		// A signed party's account links stop working; its inbox opens a new
		// one to read the result.
		if err := revokePartyGrants(ctx, tx, c.party.ID); err != nil {
			return err
		}
		parties, err := listParties(ctx, tx, c.document.ID)
		if err != nil {
			return err
		}
		unlocked := ""
		if !slices.ContainsFunc(parties, func(p party) bool { return p.Status != partyCompleted }) {
			finalizing = true
			if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='finalizing',finalize_next_at=?,updated_at=? WHERE id=?`, at, at, c.document.ID); err != nil {
				return err
			}
			if err := appendEvent(ctx, tx, c.document.ID, event{kind: "completed", at: at, data: map[string]any{"parties": len(parties)}}); err != nil {
				return err
			}
		} else {
			if c.document.SigningOrder == "preserved" {
				for _, next := range parties {
					if next.Status == partyWaiting {
						unlocked = next.ID
						if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET status='pending' WHERE id=?`, next.ID); err != nil {
							return err
						}
						break
					}
				}
			}
			if err := touch(ctx, tx, c.document.ID, at); err != nil {
				return err
			}
		}
		return h.queueCompletionStep(ctx, tx, c.document.ID, c.party.ID, unlocked, finalizing)
	})
	if err != nil {
		return nil, err
	}
	d, err := documentByID(ctx, h.db, c.document.ID)
	if err != nil {
		return nil, err
	}
	h.wakeMail()
	h.notifyOwner(ctx, d.OwnerID, d.ID, d.Status)
	if finalizing {
		h.wakeFinalizer()
	}
	return map[string]any{"state": "completed", "finalAvailable": false}, nil
}

var declineControls = regexp.MustCompile("[\x00-\x08\x0b-\x1f\x7f]")

// declineParty ends the document as declined; every link shows that.
func (h *Handler) declineParty(r *http.Request, c signerContext, input body) (any, error) {
	ctx := r.Context()
	if err := c.ready(); err != nil {
		return nil, err
	}
	raw, _ := input["reason"].(string)
	reason := strings.TrimSpace(declineControls.ReplaceAllString(withoutInvisible(raw), ""))
	if length(reason) > maxDeclineReason {
		return nil, badRequest("A reason has at most " + strconv.Itoa(maxDeclineReason) + " characters.")
	}
	err := h.tx(ctx, func(tx *sql.Tx) error {
		if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, false); err != nil {
			return err
		}
		at := h.stampNow()
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET status='declined',declined_at=?,decline_reason=? WHERE id=?`, at, nullIfEmpty(reason), c.party.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='declined',updated_at=? WHERE id=?`, at, c.document.ID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, c.document.ID, event{kind: "party_declined", partyID: c.party.ID, at: at, client: clientOf(r), data: map[string]any{"reason": nullIfEmpty(reason)}}); err != nil {
			return err
		}
		if err := revokeDocumentGrants(ctx, tx, c.document.ID); err != nil {
			return err
		}
		return h.queueDecline(ctx, tx, c.document.ID, c.party.ID, reason)
	})
	if err != nil {
		return nil, err
	}
	h.wakeMail()
	h.notifyOwner(context.WithoutCancel(ctx), c.document.OwnerID, c.document.ID, statusDeclined)
	return map[string]any{"state": "declined"}, nil
}
