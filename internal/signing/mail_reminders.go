package signing

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"
)

// Reminders, the expiry warning and what the owner's screen shows about each
// party's mail, all worked out from the outbox.

const (
	maxReminders   = 5
	reminderGap    = day
	expiryWarnLead = day
)

var liveMail = []string{"queued", "sending", "sent"}

func outboxByParty(ctx context.Context, q querier, documentID string) (map[string][]outboxRow, error) {
	rows, err := queryOutbox(ctx, q, `WHERE document_id=? AND party_id IS NOT NULL ORDER BY id`, documentID)
	out := map[string][]outboxRow{}
	for _, m := range rows {
		out[m.partyID.String] = append(out[m.partyID.String], m)
	}
	return out, err
}

// countedReminders are those queued, being sent or sent.
func countedReminders(rows []outboxRow) []outboxRow {
	var out []outboxRow
	for _, m := range rows {
		if m.kind == mailReminder && slices.Contains(liveMail, m.status) {
			out = append(out, m)
		}
	}
	return out
}

// nextReminderAt is when the automatic reminder is due next, or "".
func nextReminderAt(d document, p party, rows []outboxRow) string {
	if d.ReminderDays == 0 || d.DeliverEmail == 0 || p.Email.String == "" || p.IsSelf == 1 || !isLive(d.Status) ||
		p.Status == partyCompleted || p.Status == partyDeclined || p.Status == partyWaiting || len(countedReminders(rows)) >= maxReminders {
		return ""
	}
	last := ""
	for _, m := range rows {
		if slices.Contains(requestKinds, m.kind) && slices.Contains(liveMail, m.status) {
			last = m.createdAt
		}
	}
	base, ok := parseStamp(last)
	if !ok {
		return ""
	}
	return stamp(base.Add(time.Duration(d.ReminderDays) * day))
}

func describeDelivery(d document, p party, rows []outboxRow) map[string]any {
	var latest *outboxRow
	var lastSent string
	reminders := 0
	for i := range rows {
		m := &rows[i]
		if m.kind == mailReminder && m.status == "sent" {
			reminders++
		}
		if !slices.Contains(requestKinds, m.kind) {
			continue
		}
		if m.status != "canceled" {
			latest = m
		}
		lastSent = max(lastSent, m.lastSent.String)
	}
	status := "skipped"
	if p.Email.String == "" {
		status = "no_email"
	} else if latest != nil {
		status = map[string]string{"queued": "queued", "sending": "queued", "sent": "sent", "failed": "failed"}[latest.status]
	}
	out := map[string]any{"status": status, "lastSentAt": nullIfEmpty(lastSent), "attempts": 0, "errorCode": nil, "lastError": nil,
		"remindersSent": reminders, "nextReminderAt": nullIfEmpty(nextReminderAt(d, p, rows))}
	if latest != nil {
		out["attempts"] = latest.attempts
		if latest.status != "sent" && latest.lastError.String != "" {
			code := latest.lastError.String
			if !slices.Contains(mailErrorCodes, code) {
				code = "UNKNOWN"
			}
			out["errorCode"], out["lastError"] = code, mailErrorSentences[mailLanguage(d.Locale)][code]
		}
	}
	return out
}

func deliveryByParty(ctx context.Context, q querier, d document, parties []party) (map[string]map[string]any, error) {
	rows, err := outboxByParty(ctx, q, d.ID)
	out := map[string]map[string]any{}
	for _, p := range parties {
		out[p.ID] = describeDelivery(d, p, rows[p.ID])
	}
	return out, err
}

func isActiveParty(p party) bool { return p.Status == partyPending || p.Status == partyOpened }

// remind queues a reminder to one party the owner is waiting for.
func (h *Handler) remind(ctx context.Context, q querier, d document, p party, rows []outboxRow, message string) error {
	notActive := httpError(409, "This party is not waiting to sign.", "PARTY_NOT_ACTIVE")
	if !isLive(d.Status) || !isActiveParty(p) || p.IsSelf == 1 {
		return notActive
	}
	if p.Email.String == "" {
		return httpError(400, "This party has no email address.", "NO_EMAIL")
	}
	if !p.TokenHash.Valid || !p.TokenEnc.Valid {
		return notActive
	}
	counted := countedReminders(rows)
	if len(counted) >= maxReminders {
		return httpError(429, "A party can be reminded "+strconv.Itoa(maxReminders)+" times at most.", "REMINDER_LIMIT")
	}
	if len(counted) > 0 {
		if last, ok := parseStamp(counted[len(counted)-1].createdAt); ok {
			if wait := last.Add(reminderGap).Sub(h.now()); wait > 0 {
				refused := httpError(429, "This party was reminded recently. Try again later.", "REMINDER_TOO_SOON")
				refused.retryAfter = int(math.Ceil(wait.Seconds()))
				return refused
			}
		}
	}
	if err := h.assertMayMail(ctx, q, d.OwnerID, []mailItem{{p.Email.String, mailReminder}}); err != nil {
		return err
	}
	payload := map[string]any{}
	if message != "" {
		payload["message"] = message
	}
	return h.enqueueMail(ctx, q, d.ID, p.ID, mailReminder, p.Email.String, payload)
}

// reminding opens a transaction in which the owner may send reminders.
func (h *Handler) reminding(ctx context.Context, userID int64, documentID string, input body, work func(tx *sql.Tx, d document, rows map[string][]outboxRow, message string) error) error {
	_, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if !h.mailEnabled() {
			return httpError(503, "Email is not set up on this server.", "EMAIL_NOT_CONFIGURED")
		}
		message, err := cleanMessage(input["message"])
		if err != nil {
			return err
		}
		verified, _, err := verifiedUser(ctx, tx, userID)
		if err != nil {
			return err
		}
		if !verified {
			return mailRefusal("EMAIL_NOT_VERIFIED")
		}
		rows, err := outboxByParty(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		return work(tx, d, rows, message)
	})
	if err == nil {
		h.wakeMail()
	}
	return err
}

func (h *Handler) remindParty(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	// Another account's or an unknown party is 404 before anything else.
	d, err := ownedDocument(ctx, h.db, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if _, err := documentParty(ctx, h.db, d.ID, r.PathValue("partyId")); err != nil {
		return nil, err
	}
	var partyID string
	err = h.reminding(ctx, userID, d.ID, input, func(tx *sql.Tx, d document, rows map[string][]outboxRow, message string) error {
		p, err := documentParty(ctx, tx, d.ID, r.PathValue("partyId"))
		if err != nil {
			return err
		}
		partyID = p.ID
		return h.remind(ctx, tx, d, p, rows[p.ID], message)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"delivery": map[string]any{"partyId": partyID, "status": "queued"}}, nil
}

// remindEveryone reminds everybody waiting to sign: one result per party that
// has an address.
func (h *Handler) remindEveryone(ctx context.Context, userID int64, documentID string, input body) ([]map[string]any, error) {
	results := []map[string]any{}
	err := h.reminding(ctx, userID, documentID, input, func(tx *sql.Tx, d document, rows map[string][]outboxRow, message string) error {
		parties, err := listParties(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		for _, p := range parties {
			if p.Email.String == "" || !isActiveParty(p) || p.IsSelf == 1 {
				continue
			}
			err := h.remind(ctx, tx, d, p, rows[p.ID], message)
			result := map[string]any{"partyId": p.ID, "status": "queued"}
			var refused *apiError
			if errors.As(err, &refused) {
				result["status"], result["code"] = "skipped", refused.code
				if refused.retryAfter > 0 {
					result["retryAfter"] = refused.retryAfter
				}
			} else if err != nil {
				return err
			}
			results = append(results, result)
		}
		return nil
	})
	return results, err
}

func (h *Handler) remindAll(r *http.Request, userID int64, input body) (any, error) {
	results, err := h.remindEveryone(r.Context(), userID, r.PathValue("id"), input)
	if err != nil {
		return nil, err
	}
	return map[string]any{"results": results}, nil
}

func dueExpiryWarning(d document, rows []outboxRow, now time.Time) bool {
	expires, ok := parseStamp(d.ExpiresAt.String)
	sentAt, sentOK := parseStamp(d.SentAt.String)
	if !ok || !sentOK {
		return false
	}
	warnAt := expires.Add(-expiryWarnLead)
	// A request that never lasted 24 hours has nothing to warn about.
	if now.Before(warnAt) || !now.Before(expires) || !sentAt.Before(warnAt) {
		return false
	}
	return !slices.ContainsFunc(rows, func(m outboxRow) bool { return m.kind == mailExpiryWarning })
}

func dueReminder(d document, p party, rows []outboxRow, now time.Time) bool {
	due, ok := parseStamp(nextReminderAt(d, p, rows))
	if !ok || due.After(now) {
		return false
	}
	counted := countedReminders(rows)
	if len(counted) == 0 {
		return true
	}
	last, ok := parseStamp(counted[len(counted)-1].createdAt)
	return ok && len(counted) < maxReminders && now.Sub(last) >= reminderGap
}

// runReminders queues the automatic reminders and expiry warnings that are
// due, and forgets old ledger entries.
func (h *Handler) runReminders(ctx context.Context) {
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sign_mail_ledger WHERE at<?`, stamp(h.now().Add(-2*day))); err != nil && ctx.Err() == nil {
		slog.Error("signing mail ledger cleanup failed", "error", err)
	}
	if !h.mailEnabled() {
		return
	}
	documents, err := queryDocuments(ctx, h.db, `WHERE status IN ('sent','in_progress') AND deliver_email=1`)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("signing reminder pass failed", "error", err)
		}
		return
	}
	for _, d := range documents {
		err := h.tx(ctx, func(tx *sql.Tx) error {
			rows, err := outboxByParty(ctx, tx, d.ID)
			if err != nil {
				return err
			}
			parties, err := listParties(ctx, tx, d.ID)
			if err != nil {
				return err
			}
			for _, p := range parties {
				if p.Email.String == "" || !isActiveParty(p) || p.IsSelf == 1 || !p.TokenHash.Valid || !p.TokenEnc.Valid {
					continue
				}
				// The warning also restarts the reminder clock, so the two
				// never arrive together.
				kind := ""
				if dueExpiryWarning(d, rows[p.ID], h.now()) {
					kind = mailExpiryWarning
				} else if dueReminder(d, p, rows[p.ID], h.now()) {
					kind = mailReminder
				}
				if kind != "" {
					if _, err := h.enqueueRequest(ctx, tx, d, p.ID, kind, p.Email.String); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if err != nil {
			slog.Error("signing reminders for a document failed", "documentId", d.ID, "error", err)
		}
	}
}
