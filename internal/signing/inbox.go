package signing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// A signed-in account's signing requests: the parties bound to it and the
// parties whose address is its verified email. Opening one mints a grant: a
// short-lived stand-in for the party's link, which never leaves the server.

const (
	grantTTL      = time.Hour
	maxInboxItems = 200
)

func (h *Handler) createGrant(ctx context.Context, userID int64, partyID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	now := h.now()
	_, err = h.db.ExecContext(ctx, `INSERT INTO sign_party_grants(id,party_id,user_id,token_hash,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		uuid.NewString(), partyID, userID, hashToken(token), stamp(now.Add(grantTTL)), stamp(now))
	return token, err
}

// resolveGrant opens the party of a grant token. The first use binds the party
// to the grant's account and is recorded.
func (h *Handler) resolveGrant(ctx context.Context, tokenHash string, from client) (party, error) {
	var p party
	err := h.tx(ctx, func(tx *sql.Tx) error {
		var id, partyID, expires string
		var userID int64
		var used text
		err := tx.QueryRowContext(ctx, `SELECT id,party_id,user_id,expires_at,first_used_at FROM sign_party_grants WHERE token_hash=?`, tokenHash).
			Scan(&id, &partyID, &userID, &expires, &used)
		if errors.Is(err, sql.ErrNoRows) || err == nil && expires <= h.stampNow() {
			return linkNotFound()
		}
		if err != nil {
			return err
		}
		p, err = partyByID(ctx, tx, partyID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && p.UserID.Valid && p.UserID.Int64 != userID {
			return linkNotFound()
		}
		if err != nil || used.Valid {
			return err
		}
		at := h.stampNow()
		if _, err := tx.ExecContext(ctx, `UPDATE sign_party_grants SET first_used_at=? WHERE id=?`, at, id); err != nil {
			return err
		}
		if !p.UserID.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET user_id=? WHERE id=?`, userID, p.ID); err != nil {
				return err
			}
			if err := appendEvent(ctx, tx, p.DocumentID, event{kind: "claimed", partyID: p.ID, at: at, client: from, data: map[string]any{"via": "account"}}); err != nil {
				return err
			}
		}
		if err := appendEvent(ctx, tx, p.DocumentID, event{kind: "grant_used", partyID: p.ID, at: at, client: from, data: map[string]any{"grantId": id}}); err != nil {
			return err
		}
		p, err = partyByID(ctx, tx, p.ID)
		return err
	})
	return p, err
}

func revokePartyGrants(ctx context.Context, q querier, partyID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM sign_party_grants WHERE party_id=?`, partyID)
	return err
}

func revokeDocumentGrants(ctx context.Context, q querier, documentID string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM sign_party_grants WHERE party_id IN (SELECT id FROM sign_parties WHERE document_id=?)`, documentID)
	return err
}

type inboxRow struct {
	partyID, partyStatus, documentID, title, documentStatus, creator string
	completedAt, tokenHash, expires, sentAt                          text
	finalNodeID, finalSHA                                            text
}

// A request whose link was turned off cannot be acted on; it only stays listed
// once it is finished.
func (r inboxRow) listed() bool {
	return r.tokenHash.Valid || r.partyStatus == partyCompleted || r.partyStatus == partyDeclined
}

// entitledRows: an unverified address entitles to nothing, or anyone could
// read other people's requests by typing their address.
func (h *Handler) entitledRows(ctx context.Context, userID int64) ([]inboxRow, error) {
	var email, verified text
	if err := h.db.QueryRowContext(ctx, `SELECT email,email_verified_at FROM users WHERE id=?`, userID).Scan(&email, &verified); err != nil {
		return nil, err
	}
	var address any
	if email.String != "" && verified.Valid {
		address = strings.ToLower(email.String)
	}
	rows, err := h.db.QueryContext(ctx, `SELECT p.id,p.status,p.completed_at,p.token_hash,d.id,d.title,d.status,d.expires_at,d.sent_at,d.final_node_id,d.final_sha256,
		COALESCE(NULLIF(u.display_name,''),u.username)
		FROM sign_parties p JOIN sign_documents d ON d.id=p.document_id JOIN users u ON u.id=d.owner_id
		WHERE d.status!='draft' AND u.disabled_at IS NULL AND (p.user_id=? OR (p.user_id IS NULL AND ? IS NOT NULL AND lower(p.email)=?))
		ORDER BY d.updated_at DESC,d.id DESC LIMIT ?`, userID, address, address, maxInboxItems)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []inboxRow
	for rows.Next() {
		var r inboxRow
		if err := rows.Scan(&r.partyID, &r.partyStatus, &r.completedAt, &r.tokenHash, &r.documentID, &r.title, &r.documentStatus, &r.expires, &r.sentAt, &r.finalNodeID, &r.finalSHA, &r.creator); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (h *Handler) savedNodeID(ctx context.Context, partyID string) (any, error) {
	var raw string
	err := h.db.QueryRowContext(ctx, `SELECT data_json FROM sign_events WHERE type='autosaved' AND party_id=? ORDER BY id DESC LIMIT 1`, partyID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	var data struct {
		NodeID string `json:"nodeId"`
	}
	_ = json.Unmarshal([]byte(raw), &data)
	return nullIfEmpty(data.NodeID), err
}

// inbox is the account's items and how many wait for its signature.
func (h *Handler) inbox(ctx context.Context, userID int64) ([]map[string]any, int, error) {
	rows, err := h.entitledRows(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	items := []map[string]any{}
	todo := 0
	for _, row := range rows {
		if !row.listed() {
			continue
		}
		documentStatus := row.documentStatus
		// A document whose time is up counts as expired before the pass does.
		if expires, ok := parseStamp(row.expires.String); ok && isLive(documentStatus) && !expires.After(h.now()) {
			documentStatus = statusExpired
		}
		status := publicState(documentStatus, row.partyStatus)
		if status == "ready" {
			todo++
		}
		available := false
		if row.partyStatus == partyCompleted {
			available, err = finalAvailable(ctx, h.db, document{Status: row.documentStatus, FinalNodeID: row.finalNodeID, FinalSHA256: row.finalSHA})
			if err != nil {
				return nil, 0, err
			}
		}
		item := map[string]any{"documentId": row.documentID, "partyId": row.partyID, "title": row.title, "creatorName": row.creator, "status": status,
			"expiresAt": nullable(row.expires), "sentAt": nullable(row.sentAt), "completedAt": nullable(row.completedAt), "finalAvailable": available}
		saved, err := h.savedNodeID(ctx, row.partyID)
		if err != nil {
			return nil, 0, err
		}
		if saved != nil {
			item["savedNodeId"] = saved
		}
		items = append(items, item)
	}
	return items, todo, nil
}

func (h *Handler) listInbox(r *http.Request, userID int64, _ body) (any, error) {
	query := r.URL.Query()
	status := query.Get("status")
	if query.Has("status") && status != "todo" && status != "done" {
		return nil, badRequest("status is todo or done.")
	}
	all, todo, err := h.inbox(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, item := range all {
		waiting := item["status"] == "ready" || item["status"] == "waiting"
		if !query.Has("status") || (status == "todo") == waiting {
			items = append(items, item)
		}
	}
	return map[string]any{"items": items, "counts": map[string]int{"todo": todo}}, nil
}

// announceDocument tells every account entitled to a party of the document
// that its inbox changed.
func (h *Handler) announceDocument(ctx context.Context, documentID, status string) {
	users, err := func() ([]int64, error) {
		rows, err := h.db.QueryContext(ctx, `SELECT DISTINCT u.id FROM sign_parties p JOIN users u ON u.id=p.user_id
			OR (p.user_id IS NULL AND p.email IS NOT NULL AND u.email_verified_at IS NOT NULL AND lower(u.email)=lower(p.email))
			WHERE p.document_id=?`, documentID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}()
	for _, userID := range users {
		var todo int
		if _, todo, err = h.inbox(ctx, userID); err != nil {
			break
		}
		h.notifyUser(userID, "signing:inbox", map[string]any{"documentId": documentID, "status": status, "counts": map[string]int{"todo": todo}})
	}
	if err != nil {
		slog.Warn("signing inbox could not be announced", "error", err)
	}
}

// entitledParty is one of the account's requests, else the uniform 404.
func (h *Handler) entitledParty(ctx context.Context, userID int64, partyID string) (party, error) {
	notFound := httpError(404, "Request not found", "NOT_FOUND")
	rows, err := h.entitledRows(ctx, userID)
	if err != nil {
		return party{}, err
	}
	for _, row := range rows {
		if row.partyID != partyID || !row.listed() {
			continue
		}
		p, err := partyByID(ctx, h.db, partyID)
		if err == nil && p.UserID.Valid && p.UserID.Int64 != userID {
			err = alreadyClaimed()
		}
		return p, err
	}
	return party{}, notFound
}

func (h *Handler) openInbox(r *http.Request, userID int64, _ body) (any, error) {
	p, err := h.entitledParty(r.Context(), userID, r.PathValue("partyId"))
	if err != nil {
		return nil, err
	}
	token, err := h.createGrant(r.Context(), userID, p.ID)
	return map[string]any{"url": signPagePath + token}, err
}

// inboxFinal is the signed file of one of the account's finished requests: a
// grant dies with the signature, so this is how it gets the file afterwards.
func (h *Handler) inboxFinal(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	var d document
	p, err := h.entitledParty(ctx, userID, r.PathValue("partyId"))
	if err == nil {
		d, err = documentByID(ctx, h.db, p.DocumentID)
	}
	if err == nil && (d.Status != statusCompleted || p.Status != partyCompleted) {
		err = finalUnavailable()
	}
	certificate := true
	if err == nil {
		certificate, err = wantsCertificate(r)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	h.sendFinal(w, r, d, p.ID, "signer", certificate)
}

// openAsOwner marks a party as the owner's own and opens it with a grant.
func (h *Handler) openAsOwner(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	partyID := ""
	_, err := h.change(ctx, userID, r.PathValue("id"), func(tx *sql.Tx, d document) error {
		p, err := documentParty(ctx, tx, d.ID, r.PathValue("partyId"))
		if err != nil {
			return err
		}
		if d.Status == statusDraft {
			return httpError(409, "Send the document before you sign it.", "INVALID_STATE")
		}
		if !p.TokenHash.Valid && p.Status != partyCompleted && p.Status != partyDeclined {
			return httpError(409, "This link was turned off.", "LINK_REVOKED")
		}
		if p.UserID.Valid && p.UserID.Int64 != userID {
			return httpError(409, "This party was taken by another account.", "ALREADY_CLAIMED")
		}
		var others int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sign_parties WHERE document_id=? AND is_self=1 AND id!=?`, d.ID, p.ID).Scan(&others); err != nil {
			return err
		}
		if others > 0 {
			return httpError(409, "Another party is already you.", "SELF_PARTY_EXISTS")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET is_self=1,user_id=? WHERE id=?`, userID, p.ID); err != nil {
			return err
		}
		partyID = p.ID
		return cancelQueued(ctx, tx, d.ID, p.ID)
	})
	if err != nil {
		return nil, err
	}
	token, err := h.createGrant(ctx, userID, partyID)
	return map[string]any{"url": signPagePath + token}, err
}
