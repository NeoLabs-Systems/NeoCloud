package signing

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/textproto"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	mail "github.com/wneessen/go-mail"
)

// The queue of signing mail. A mail is first a row, so a slow or broken SMTP
// server never loses an invitation and a restart picks up where it stopped.
// Rows hold template parameters, never a link or token.

const (
	mailInterval     = 15 * time.Second
	reminderInterval = 5 * time.Minute
	mailLanes        = 2
	mailStuckAfter   = 10 * time.Minute
)

var mailBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 2 * time.Hour}

// maskEmail is how the trail shows an address.
func maskEmail(address string) string {
	at := strings.LastIndexByte(address, '@')
	if at < 1 {
		return "***"
	}
	first := []rune(address)[:1]
	return string(first) + "***" + address[at:]
}

func (h *Handler) wakeMail() {
	select {
	case h.mailWake <- struct{}{}:
	default:
	}
}

// enqueueMail queues one mail and records it in the trail. Wake the worker
// after the transaction commits.
func (h *Handler) enqueueMail(ctx context.Context, q querier, documentID, partyID, kind, to string, payload map[string]any) error {
	at := h.stampNow()
	if payload == nil {
		payload = map[string]any{}
	}
	result, err := q.ExecContext(ctx, `INSERT INTO sign_mail_outbox(document_id,party_id,kind,to_email,payload_json,status,next_attempt_at,created_at) VALUES(?,?,?,?,?,'queued',?,?)`,
		documentID, nullIfEmpty(partyID), kind, to, encodeJSON(payload), at, at)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if err := appendEvent(ctx, q, documentID, event{kind: "mail_queued", partyID: partyID, at: at, data: map[string]any{"kind": kind, "to": maskEmail(to), "mailId": id}}); err != nil {
		return err
	}
	var owner int64
	if err := q.QueryRowContext(ctx, `SELECT owner_id FROM sign_documents WHERE id=?`, documentID).Scan(&owner); err != nil {
		return err
	}
	return recordInLedger(ctx, q, owner, to, kind, id, at)
}

// enqueueRequest queues a mail that asks somebody to sign. Over the limits it
// is recorded as failed, so the owner sees it and can copy the link instead.
func (h *Handler) enqueueRequest(ctx context.Context, q querier, d document, partyID, kind, to string) (string, error) {
	code, err := h.mailProblem(ctx, q, d.OwnerID, []mailItem{{to, kind}})
	if err != nil {
		return "", err
	}
	if code == "" {
		return "", h.enqueueMail(ctx, q, d.ID, partyID, kind, to, nil)
	}
	at := h.stampNow()
	stored := "UNKNOWN"
	if code == "MAIL_QUOTA_EXCEEDED" {
		stored = "RATE_LIMITED"
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO sign_mail_outbox(document_id,party_id,kind,to_email,payload_json,status,attempts,last_error,next_attempt_at,created_at) VALUES(?,?,?,?,'{}','failed',0,?,?,?)`,
		d.ID, nullIfEmpty(partyID), kind, to, stored, at, at); err != nil {
		return "", err
	}
	return code, appendEvent(ctx, q, d.ID, event{kind: "mail_failed", partyID: partyID, at: at, data: map[string]any{"kind": kind, "to": maskEmail(to), "reason": code}})
}

type outboxRow struct {
	id                         int64
	documentID, kind, to       string
	partyID, payload, lastSent text
	status                     string
	attempts                   int
	lastError                  text
	createdAt                  string
}

const outboxColumns = `id,document_id,party_id,kind,to_email,payload_json,status,attempts,last_error,created_at,sent_at`

func queryOutbox(ctx context.Context, q querier, where string, args ...any) ([]outboxRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+outboxColumns+` FROM sign_mail_outbox `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var m outboxRow
		if err := rows.Scan(&m.id, &m.documentID, &m.partyID, &m.kind, &m.to, &m.payload, &m.status, &m.attempts, &m.lastError, &m.createdAt, &m.lastSent); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func forgetInLedger(ctx context.Context, q querier, mailID int64) error {
	_, err := q.ExecContext(ctx, `DELETE FROM sign_mail_ledger WHERE mail_id=?`, mailID)
	return err
}

// cancelQueued cancels waiting mails of these kinds (all when none are named);
// a mail canceled before it left no longer counts.
func cancelQueued(ctx context.Context, q querier, documentID, partyID string, kinds ...string) error {
	queued, err := queryOutbox(ctx, q, `WHERE document_id=? AND status='queued' AND (?='' OR party_id=?)`, documentID, partyID, partyID)
	if err != nil {
		return err
	}
	for _, m := range queued {
		if len(kinds) > 0 && !slices.Contains(kinds, m.kind) {
			continue
		}
		if _, err := q.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='canceled' WHERE id=? AND status='queued'`, m.id); err != nil {
			return err
		}
		if err := forgetInLedger(ctx, q, m.id); err != nil {
			return err
		}
	}
	return nil
}

// recoverInterrupted puts mails left "sending" back in the queue.
func (h *Handler) recoverInterrupted(ctx context.Context, all bool) {
	limit := h.now().Add(-mailStuckAfter)
	if all {
		limit = h.now().Add(time.Millisecond)
	}
	if _, err := h.db.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='queued',next_attempt_at=? WHERE status='sending' AND next_attempt_at<=?`, h.stampNow(), stamp(limit)); err != nil && ctx.Err() == nil {
		slog.Error("signing mail recovery failed", "error", err)
	}
}

// RunMailWorker sends what the outbox holds and queues automatic reminders and
// expiry warnings.
func (h *Handler) RunMailWorker(ctx context.Context) {
	h.recoverInterrupted(ctx, true)
	ticker := time.NewTicker(mailInterval)
	defer ticker.Stop()
	var lastReminders time.Time
	for {
		if time.Since(lastReminders) >= reminderInterval {
			lastReminders = time.Now()
			h.runReminders(ctx)
		}
		h.processMail(ctx)
		select {
		case <-ctx.Done():
			return
		case <-h.mailWake:
		case <-ticker.C:
			h.recoverInterrupted(ctx, false)
		}
	}
}

// processMail sends every mail that is due, two at a time.
func (h *Handler) processMail(ctx context.Context) {
	if !h.mailEnabled() {
		// The admin's kill switch: what waits is dropped, not sent later.
		if h.mailConfig().Enabled {
			if err := h.dropQueued(ctx); err != nil && ctx.Err() == nil {
				slog.Error("signing mail could not be dropped", "error", err)
			}
		}
		return
	}
	due := h.stampNow()
	var lanes sync.WaitGroup
	for range mailLanes {
		lanes.Add(1)
		go func() {
			defer lanes.Done()
			for ctx.Err() == nil {
				row, err := h.claimMail(ctx, due)
				if err != nil {
					if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
						slog.Error("signing mail could not be claimed", "error", err)
					}
					return
				}
				h.deliver(ctx, row)
			}
		}()
	}
	lanes.Wait()
}

func (h *Handler) dropQueued(ctx context.Context) error {
	return h.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM sign_mail_ledger WHERE mail_id IN (SELECT id FROM sign_mail_outbox WHERE status='queued')`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='canceled',last_error='Signing mail is switched off.' WHERE status='queued'`)
		return err
	})
}

func (h *Handler) claimMail(ctx context.Context, due string) (outboxRow, error) {
	var m outboxRow
	err := h.db.QueryRowContext(ctx, `UPDATE sign_mail_outbox SET status='sending',attempts=attempts+1,next_attempt_at=?
		WHERE id=(SELECT id FROM sign_mail_outbox WHERE status='queued' AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT 1) RETURNING `+outboxColumns, due, due).
		Scan(&m.id, &m.documentID, &m.partyID, &m.kind, &m.to, &m.payload, &m.status, &m.attempts, &m.lastError, &m.createdAt, &m.lastSent)
	return m, err
}

// permanentError is a failure no retry can fix.
type permanentError struct{ message string }

func (e permanentError) Error() string { return e.message }

var addressPattern = regexp.MustCompile(`^[^\s@<>,;:"()\[\]\\]+@[^\s@<>,;:"()\[\]\\]+\.[^\s@<>,;:"()\[\]\\]+$`)

func (h *Handler) deliver(ctx context.Context, m outboxRow) {
	message, err := h.compose(ctx, m)
	if err == nil && message != nil {
		if !addressPattern.MatchString(m.to) || len(m.to) > maxEmailLength {
			err = permanentError{"INVALID_ADDRESS"}
		} else {
			err = h.sendMail(ctx, m.to, *message)
		}
	}
	if ctx.Err() != nil {
		return
	}
	switch {
	case err != nil:
		err = errors.Join(h.markMailFailed(ctx, m, err))
	case message == nil:
		err = h.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='canceled',last_error='Not needed any more.' WHERE id=?`, m.id); err != nil {
				return err
			}
			return forgetInLedger(ctx, tx, m.id)
		})
	default:
		err = h.tx(ctx, func(tx *sql.Tx) error {
			at := h.stampNow()
			if _, err := tx.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='sent',sent_at=?,last_error=NULL WHERE id=?`, at, m.id); err != nil {
				return err
			}
			data := map[string]any{"kind": m.kind, "to": maskEmail(m.to), "mailId": m.id}
			if err := appendEvent(ctx, tx, m.documentID, event{kind: "mail_sent", partyID: m.partyID.String, at: at, data: data}); err != nil {
				return err
			}
			if m.kind != mailReminder {
				return nil
			}
			return appendEvent(ctx, tx, m.documentID, event{kind: "reminder_sent", partyID: m.partyID.String, at: at, data: data})
		})
	}
	if err != nil {
		slog.Error("signing mail state could not be saved", "error", err)
	}
	if d, err := documentByID(ctx, h.db, m.documentID); err == nil {
		h.notifyOwner(ctx, d.OwnerID, d.ID, d.Status)
	}
}

func (h *Handler) markMailFailed(ctx context.Context, m outboxRow, cause error) error {
	code := classifyMailError(cause)
	var permanent permanentError
	final := errors.As(cause, &permanent) || m.attempts > len(mailBackoff)
	// The mail server's own text can hold host names or addresses: only the
	// code is kept and logged.
	slog.Warn("a signing mail failed", "kind", m.kind, "attempt", m.attempts, "code", code)
	if !final {
		_, err := h.db.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='queued',next_attempt_at=?,last_error=? WHERE id=?`, stamp(h.now().Add(mailBackoff[max(0, m.attempts-1)])), code, m.id)
		return err
	}
	return h.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE sign_mail_outbox SET status='failed',last_error=? WHERE id=?`, code, m.id); err != nil {
			return err
		}
		return appendEvent(ctx, tx, m.documentID, event{kind: "mail_failed", partyID: m.partyID.String, at: h.stampNow(), data: map[string]any{"kind": m.kind, "to": maskEmail(m.to), "mailId": m.id}})
	})
}

var (
	smtpStatus     = regexp.MustCompile(`\b([245]\d{2})[ -]`)
	rateLimitText  = regexp.MustCompile(`(?i)rate limit|too many|throttl|quota|exceeded`)
	badAddressText = regexp.MustCompile(`(?i)recipient|mailbox|no such user|user unknown|unknown user|address|does not exist|invalid`)
)

var mailErrorCodes = []string{"SMTP_UNREACHABLE", "SMTP_REJECTED", "INVALID_ADDRESS", "RATE_LIMITED", "UNKNOWN"}

// classifyMailError reduces a send failure to one of the stable codes.
func classifyMailError(err error) string {
	var permanent permanentError
	if errors.As(err, &permanent) {
		if permanent.message == "INVALID_ADDRESS" {
			return "INVALID_ADDRESS"
		}
		return "UNKNOWN"
	}
	message := err.Error()
	if strings.Contains(message, "recipient address:") {
		return "INVALID_ADDRESS"
	}
	status := 0
	var protocol *textproto.Error
	var sending *mail.SendError
	switch {
	case errors.As(err, &protocol):
		status = protocol.Code
	case errors.As(err, &sending) && sending.ErrorCode() > 0:
		status = sending.ErrorCode()
	default:
		if found := smtpStatus.FindStringSubmatch(message); found != nil {
			status, _ = strconv.Atoi(found[1])
		}
	}
	var network net.Error
	if status == 0 && (errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded)) {
		return "SMTP_UNREACHABLE"
	}
	if slices.Contains([]int{421, 450, 451, 452, 454}, status) || rateLimitText.MatchString(message) {
		return "RATE_LIMITED"
	}
	if status >= 500 && badAddressText.MatchString(message) {
		return "INVALID_ADDRESS"
	}
	if status >= 400 {
		return "SMTP_REJECTED"
	}
	return "UNKNOWN"
}

var mailErrorSentences = map[string]map[string]string{
	"en": {
		"SMTP_UNREACHABLE": "The mail server could not be reached.",
		"SMTP_REJECTED":    "The mail server refused the message.",
		"INVALID_ADDRESS":  "The address was not accepted.",
		"RATE_LIMITED":     "The mail server is limiting how much can be sent right now.",
		"UNKNOWN":          "The email could not be sent.",
	},
	"de": {
		"SMTP_UNREACHABLE": "Der Mailserver ist nicht erreichbar.",
		"SMTP_REJECTED":    "Der Mailserver hat die Nachricht abgelehnt.",
		"INVALID_ADDRESS":  "Die Adresse wurde nicht akzeptiert.",
		"RATE_LIMITED":     "Der Mailserver bremst das Senden gerade.",
		"UNKNOWN":          "Die E-Mail konnte nicht gesendet werden.",
	},
}
