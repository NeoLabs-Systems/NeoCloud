package signing

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

// Turning a fully signed document into its final file: the frozen PDF with
// every value drawn in, the audit pages appended and the seal applied, saved
// as an ordinary file next to the source. Safe to repeat; a failure is
// recorded and retried with growing pauses until it works.

const (
	workerInterval = 30 * time.Second
	finalizeLease  = 5 * time.Minute
	firstRetry     = 30 * time.Second
	maxRetry       = time.Hour

	sealReason = "Signed with NeoCloud"
	sealName   = "NeoCloud"
)

// RunWorker expires documents that ran out of time, builds final files,
// saves signed files to signers' accounts and clears finished grants.
func (h *Handler) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(workerInterval)
	defer ticker.Stop()
	h.runPass(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.finalizeWake:
			h.finalizeDue(ctx)
		case <-ticker.C:
			h.runPass(ctx)
		}
	}
}

func (h *Handler) wakeFinalizer() {
	select {
	case h.finalizeWake <- struct{}{}:
	default:
	}
}

func (h *Handler) runPass(ctx context.Context) {
	h.expireDue(ctx)
	if _, err := h.db.ExecContext(ctx, `DELETE FROM sign_party_grants WHERE expires_at<=?`, h.stampNow()); err != nil && ctx.Err() == nil {
		slog.Error("signing grant cleanup failed", "error", err)
	}
	h.finalizeDue(ctx)
	h.retryAutosaves(ctx)
}

func (h *Handler) expireDue(ctx context.Context) {
	due, err := queryDocuments(ctx, h.db, `WHERE status IN ('sent','in_progress') AND expires_at IS NOT NULL AND expires_at<=?`, h.stampNow())
	if err != nil {
		slog.Error("signing expiry pass failed", "error", err)
		return
	}
	for _, d := range due {
		if _, err := h.expireIfDue(ctx, d); err != nil {
			slog.Error("signing document could not expire", "documentId", d.ID, "error", err)
		}
	}
}

func (h *Handler) finalizeDue(ctx context.Context) {
	due, err := queryDocuments(ctx, h.db, `WHERE status='finalizing' AND (finalize_next_at IS NULL OR finalize_next_at<=?)`, h.stampNow())
	if err != nil {
		slog.Error("signing finalization pass failed", "error", err)
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		h.finalize(ctx, d.ID)
		// A first build makes the signers' copies right away.
		h.autosaveDocument(ctx, d.ID)
	}
}

// finalize builds and saves the final file of a due document. The lease on
// finalize_next_at keeps two processes from building the same one.
func (h *Handler) finalize(ctx context.Context, documentID string) {
	now := h.stampNow()
	claimed, err := h.db.ExecContext(ctx, `UPDATE sign_documents SET finalize_next_at=? WHERE id=? AND status='finalizing' AND (finalize_next_at IS NULL OR finalize_next_at<=?)`,
		stamp(h.now().Add(finalizeLease)), documentID, now)
	if err != nil {
		return
	}
	if n, _ := claimed.RowsAffected(); n != 1 {
		return
	}
	d, err := documentByID(ctx, h.db, documentID)
	if err != nil {
		return
	}
	err = func() error {
		pdf, sha, err := h.build(ctx, d, true)
		if err != nil {
			return err
		}
		nodeID, err := h.saveFinalFile(ctx, d, pdf)
		if err != nil {
			return err
		}
		return h.markCompleted(ctx, d, nodeID, sha)
	}()
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		h.markFailed(ctx, d, err)
		h.notifyOwner(ctx, d.OwnerID, d.ID, statusFinalizing)
		return
	}
	h.wakeMail()
	h.notifyOwner(ctx, d.OwnerID, d.ID, statusCompleted)
}

// sealer opens the server's certificate, creating it on first use. It is the
// one existing databases already hold, so earlier files and new ones carry the
// same seal.
func (h *Handler) sealer(ctx context.Context) (*signpdf.Signer, error) {
	if h.noSeal {
		return nil, nil
	}
	var p12, passphrase string
	load := func() error {
		return h.db.QueryRowContext(ctx, `SELECT p12_enc,passphrase_enc FROM sign_seal_keys WHERE id=1`).Scan(&p12, &passphrase)
	}
	err := load()
	if errors.Is(err, sql.ErrNoRows) {
		bundle, secret, err := signpdf.GenerateP12("NeoCloud document seal", 10)
		if err != nil {
			return nil, err
		}
		sealedBundle, err := h.keys.EncryptValue(base64Std(bundle))
		if err != nil {
			return nil, err
		}
		sealedSecret, err := h.keys.EncryptValue(secret)
		if err != nil {
			return nil, err
		}
		// Two builds may race to create it; the first row wins.
		if _, err := h.db.ExecContext(ctx, `INSERT OR IGNORE INTO sign_seal_keys(id,p12_enc,passphrase_enc,created_at) VALUES(1,?,?,?)`, sealedBundle, sealedSecret, h.stampNow()); err != nil {
			return nil, err
		}
		if err := load(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	encoded, err := h.keys.DecryptValue(p12)
	if err != nil {
		return nil, err
	}
	secret, err := h.keys.DecryptValue(passphrase)
	if err != nil {
		return nil, err
	}
	bundle, err := fromBase64Std(encoded)
	if err != nil {
		return nil, err
	}
	return signpdf.LoadSigner(bundle, secret, sealReason, sealName)
}

func prefsOf(raw string) signpdf.Prefs {
	m := field{PrefsJSON: raw}.prefs()
	str := func(key string) string { s, _ := m[key].(string); return s }
	fontSize, _ := m["fontSize"].(float64)
	cells, _ := prefInt(m, "cells")
	return signpdf.Prefs{FontSize: fontSize, FontType: str("fontType"), Color: str("color"), Background: str("background"), Align: str("align"),
		Valign: str("valign"), Format: str("format"), Multiline: m["multiline"] == true, Cells: cells, Options: prefStrings(m, "options")}
}

// build makes the final PDF; certificate=false leaves out the audit pages. A
// rebuild uses the same inputs and times, so it gives the same bytes.
func (h *Handler) build(ctx context.Context, d document, certificate bool) ([]byte, string, error) {
	h.buildMu.Lock()
	defer h.buildMu.Unlock()
	fields, err := listFields(ctx, h.db, d.ID)
	if err != nil {
		return nil, "", err
	}
	values, err := listValues(ctx, h.db, d.ID)
	if err != nil {
		return nil, "", err
	}
	parties, err := listParties(ctx, h.db, d.ID)
	if err != nil {
		return nil, "", err
	}
	events, err := listEvents(ctx, h.db, d.ID)
	if err != nil {
		return nil, "", err
	}
	source, err := h.readBlob(ctx, d.OwnerID, d.SnapshotBlobID, d.SnapshotSize)
	if err != nil {
		return nil, "", err
	}
	byField := map[string]value{}
	for _, v := range values {
		byField[v.FieldID] = v
	}
	in := signpdf.FinalInput{PDF: source, Locale: d.Locale, NoCertificate: !certificate, Document: signpdf.Document{
		ID: d.ID, Title: d.Title, SigningOrder: d.SigningOrder, CreatedAt: d.CreatedAt, SentAt: d.SentAt.String,
		FinalizedAt: h.now().UTC(), OriginalSHA256: d.SnapshotSHA256, ConsentVersion: int(d.ConsentVersion.Int64),
	}}
	for _, e := range events {
		if e.kind == "completed" {
			if at, ok := parseStamp(e.at); ok {
				in.Document.FinalizedAt = at
			}
		}
		in.Events = append(in.Events, signpdf.Event{Type: e.kind, At: e.at, PartyID: e.partyID.String, IP: e.ip.String, Hash: e.hash})
	}
	for _, p := range parties {
		var offset *int
		if p.TZOffset.Valid {
			n := int(p.TZOffset.Int64)
			offset = &n
		}
		in.Parties = append(in.Parties, signpdf.Party{ID: p.ID, Name: p.Name, Email: p.Email.String, Color: p.Color, SignerName: p.SignerName.String,
			IP: p.IP.String, UserAgent: p.UserAgent.String, Timezone: p.Timezone.String, TZOffsetMinutes: offset, OpenedAt: p.OpenedAt.String,
			ConsentedAt: p.ConsentedAt.String, CompletedAt: p.CompletedAt.String, DeclinedAt: p.DeclinedAt.String, DeclineReason: p.Reason.String})
	}
	for _, f := range fields {
		entry := signpdf.Field{ID: f.ID, PartyID: f.PartyID, Type: f.Type, Name: f.Name, Default: decodeJSON(f.Default, nil), Prefs: prefsOf(f.PrefsJSON)}
		for _, raw := range decodeList(f.AreasJSON) {
			a, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			num := func(key string) float64 { n, _ := a[key].(float64); return n }
			area := signpdf.Area{Page: int(num("page")), X: num("x"), Y: num("y"), W: num("w"), H: num("h")}
			if option, ok := a["option"].(string); ok {
				area.Option = &option
			}
			entry.Areas = append(entry.Areas, area)
		}
		if v, ok := byField[f.ID]; ok {
			described, _ := v.decoded().(map[string]any)
			str := func(key string) string { s, _ := described[key].(string); return s }
			if !v.BlobRef.Valid {
				entry.Value = &signpdf.Value{Data: v.decoded()}
			} else {
				var size int64
				if err := h.db.QueryRowContext(ctx, `SELECT size FROM blobs WHERE id=?`, v.BlobRef.String).Scan(&size); err != nil {
					return nil, "", err
				}
				content, err := h.readBlob(ctx, d.OwnerID, v.BlobRef.String, size)
				if err != nil {
					return nil, "", err
				}
				if f.Type == "file" {
					entry.Value = &signpdf.Value{File: content, FileName: str("name"), Mime: str("mime")}
				} else {
					entry.Value = &signpdf.Value{Image: content, Method: str("method"), Typed: str("text")}
				}
			}
		}
		in.Fields = append(in.Fields, entry)
	}
	if in.Signer, err = h.sealer(ctx); err != nil {
		return nil, "", err
	}
	return signpdf.Build(ctx, in)
}

// finalFileName is "<base> (signed).pdf" in the document's language, or the
// variant without audit pages.
func finalFileName(d document, certificate bool) string {
	base, _ := splitExtension(d.SourceName.String)
	if base == "" {
		base = d.Title
	}
	word, extra := "signed", ", no certificate"
	if d.Locale == "de" {
		word, extra = "signiert", ", ohne Protokoll"
	}
	if certificate {
		extra = ""
	}
	return truncate(base, 200) + " (" + word + extra + ").pdf"
}

// saveFinalFile puts the file into the folder of the source, else the folder
// it was in when the document was made, else the root.
func (h *Handler) saveFinalFile(ctx context.Context, d document, pdf []byte) (string, error) {
	var sourceParent text
	if d.SourceNodeID.Valid {
		err := h.db.QueryRowContext(ctx, `SELECT parent_id FROM nodes WHERE id=?`, d.SourceNodeID.String).Scan(&sourceParent)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	folder := usableFolder(ctx, h.db, d.OwnerID, sourceParent.String)
	if folder == "" {
		folder = usableFolder(ctx, h.db, d.OwnerID, d.SourceParentID.String)
	}
	if folder == "" {
		root, err := liveNode(ctx, h.db, d.OwnerID, "root", "folder")
		if err != nil {
			return "", err
		}
		folder = root.id
	}
	return h.storeFile(ctx, d.OwnerID, folder, finalFileName(d, true), pdf)
}

func (h *Handler) markCompleted(ctx context.Context, d document, nodeID, sha string) error {
	return h.tx(ctx, func(tx *sql.Tx) error {
		at := h.stampNow()
		result, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='completed',final_node_id=?,final_sha256=?,completed_at=?,finalize_error=NULL,finalize_next_at=NULL,updated_at=? WHERE id=? AND status='finalizing'`,
			nodeID, sha, at, at, d.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n != 1 {
			return nil
		}
		if err := appendEvent(ctx, tx, d.ID, event{kind: "finalized", at: at, data: map[string]any{"nodeId": nodeID, "sha256": sha}}); err != nil {
			return err
		}
		// In the same transaction: a stop here cannot lose the completion mails.
		return h.queueCompletionMails(ctx, tx, d.ID)
	})
}

// failureText is what the owner may read: our own sentences, else a generic
// one.
func failureText(err error) string {
	var coded interface {
		error
		HTTPStatus() int
	}
	var refused *apiError
	switch {
	case errors.As(err, &refused) && refused.status < 500:
		return refused.message
	case errors.As(err, &coded) && coded.HTTPStatus() < 500:
		return coded.Error()
	default:
		return "The signed file could not be created yet. NeoCloud will try again."
	}
}

func (h *Handler) markFailed(ctx context.Context, d document, cause error) {
	attempts := d.FinalizeAttempts + 1
	wait := firstRetry
	for n := 1; n < attempts && wait < maxRetry; n++ {
		wait *= 2
	}
	wait = min(wait, maxRetry)
	slog.Warn("finalizing a signing document failed", "documentId", d.ID, "attempt", attempts, "error", cause)
	if _, err := h.db.ExecContext(ctx, `UPDATE sign_documents SET finalize_attempts=?,finalize_error=?,finalize_next_at=?,updated_at=? WHERE id=? AND status='finalizing'`,
		attempts, failureText(cause), stamp(h.now().Add(wait)), h.stampNow(), d.ID); err != nil {
		slog.Error("signing failure could not be recorded", "documentId", d.ID, "error", err)
	}
}

// retryFinalization tries the final file again right away.
func (h *Handler) retryFinalization(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	_, err := h.change(ctx, userID, r.PathValue("id"), func(tx *sql.Tx, d document) error {
		if d.Status != statusFinalizing {
			return httpError(409, "This document is not waiting for its signed file.", "INVALID_STATE")
		}
		_, err := tx.ExecContext(ctx, `UPDATE sign_documents SET finalize_next_at=?,updated_at=? WHERE id=?`, h.stampNow(), h.stampNow(), d.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.wakeFinalizer()
	return h.describe(ctx, userID, r.PathValue("id"))
}
