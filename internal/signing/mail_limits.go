package signing

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
)

// Abuse limits for signing mail. "Mail whoever I name" would make an instance
// a mail relay, so a person may only mail from a verified address and only so
// much per day. The ledger outlives the documents: deleting one does not give
// its mails back.

const (
	creatorMailsPerDay    = 100
	adminMailsPerDay      = 1000
	recipientsPerDocument = 20
	// Invitations to one address from the whole instance, whoever asks.
	invitationsPerAddress = 3
	day                   = 24 * time.Hour
)

const (
	mailInvitation      = "invitation"
	mailReminder        = "reminder"
	mailYourTurn        = "your_turn"
	mailExpiryWarning   = "expiry_warning"
	mailCompleted       = "completed"
	mailCreatorSigned   = "creator_signed"
	mailCreatorDeclined = "creator_declined"
	mailCreatorExpired  = "creator_expired"
)

// requestKinds are the mails that ask a party to sign.
var requestKinds = []string{mailInvitation, mailYourTurn, mailReminder, mailExpiryWarning}

type mailItem struct{ email, kind string }

func recipientHash(address string) string {
	return hashToken(strings.ToLower(strings.TrimSpace(address)))
}

func recordInLedger(ctx context.Context, q querier, userID int64, to, kind string, mailID int64, at string) error {
	if !slices.Contains(requestKinds, kind) {
		return nil
	}
	_, err := q.ExecContext(ctx, `INSERT INTO sign_mail_ledger(user_id,recipient_hash,kind,mail_id,at) VALUES(?,?,?,?,?)`, userID, recipientHash(to), kind, mailID, at)
	return err
}

func verifiedUser(ctx context.Context, q querier, userID int64) (verified, admin bool, err error) {
	var email, at text
	var isAdmin int
	err = q.QueryRowContext(ctx, `SELECT email,email_verified_at,is_admin FROM users WHERE id=?`, userID).Scan(&email, &at, &isAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	return email.String != "" && at.Valid, isAdmin == 1, err
}

// mailProblem is why the account may not have items mailed now, as a code, or
// "".
func (h *Handler) mailProblem(ctx context.Context, q querier, userID int64, items []mailItem) (string, error) {
	if len(items) == 0 {
		return "", nil
	}
	verified, admin, err := verifiedUser(ctx, q, userID)
	if err != nil || !verified {
		return "EMAIL_NOT_VERIFIED", err
	}
	since := stamp(h.now().Add(-day))
	var used int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sign_mail_ledger WHERE user_id=? AND at>?`, userID, since).Scan(&used); err != nil {
		return "", err
	}
	limit := creatorMailsPerDay
	if admin {
		limit = adminMailsPerDay
	}
	if used+len(items) > limit {
		return "MAIL_QUOTA_EXCEEDED", nil
	}
	perAddress := map[string]int{}
	for _, item := range items {
		if item.kind == mailInvitation || item.kind == mailYourTurn {
			perAddress[recipientHash(item.email)]++
		}
	}
	for hash, extra := range perAddress {
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sign_mail_ledger WHERE recipient_hash=? AND at>? AND kind IN ('invitation','your_turn')`, hash, since).Scan(&count); err != nil {
			return "", err
		}
		if count+extra > invitationsPerAddress {
			return "MAIL_QUOTA_EXCEEDED", nil
		}
	}
	return "", nil
}

func mailRefusal(code string) *apiError {
	if code == "EMAIL_NOT_VERIFIED" {
		return httpError(403, "Confirm your email address before sending requests by email. You can still copy the links and send them yourself.", code)
	}
	return httpError(429, "Too many signing emails today (or to one of these addresses). Try again tomorrow, or copy the links and send them yourself.", "MAIL_QUOTA_EXCEEDED")
}

// assertMayMail also bounds the addresses of one document.
func (h *Handler) assertMayMail(ctx context.Context, q querier, userID int64, items []mailItem) error {
	addresses := map[string]bool{}
	for _, item := range items {
		addresses[recipientHash(item.email)] = true
	}
	if len(addresses) > recipientsPerDocument || len(items) > maxParties {
		return mailRefusal("MAIL_QUOTA_EXCEEDED")
	}
	code, err := h.mailProblem(ctx, q, userID, items)
	if err != nil || code == "" {
		return err
	}
	return mailRefusal(code)
}
