package signing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/documentconvert"
	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
	"github.com/google/uuid"
)

// Documents as the owner manages them.

var (
	officeMIME = regexp.MustCompile(`(?i)officedocument|msword|ms-excel|ms-powerpoint|opendocument|rtf$`)
	imageMIME  = regexp.MustCompile(`(?i)^image/(png|jpeg|webp|gif|bmp|tiff|avif|heic|heif)$`)
)

func (h *Handler) capabilities() map[string]any {
	return map[string]any{
		"formats":        map[string]bool{"pdf": true, "image": true, "office": documentconvert.Available()},
		"maxSourceBytes": maxSourceBytes, "maxPages": maxPages, "maxParties": maxParties, "maxFields": maxFields,
		"email": map[string]bool{"configured": h.mailEnabled()},
	}
}

// pdfRefusal turns the engine's refusals into API errors; anything else is
// the server's own failure.
func pdfRefusal(err error) error {
	var refused *signpdf.Error
	if !errors.As(err, &refused) {
		return err
	}
	slog.Warn("the PDF engine refused a file", "code", refused.Code)
	switch refused.Code {
	case signpdf.CodeInvalid:
		return httpError(400, "This file could not be read as a PDF.", "INVALID_PDF")
	case signpdf.CodeEncrypted:
		return httpError(400, "This PDF is password-protected. Remove the protection first.", "PDF_ENCRYPTED")
	case signpdf.CodeTooLarge:
		return httpError(413, "This file is too large to sign (50 MB at most).", "DOCUMENT_TOO_LARGE")
	case signpdf.CodeTooManyPages:
		return httpError(400, "This PDF has too many pages.", "TOO_MANY_PAGES")
	case signpdf.CodeImage:
		return httpError(400, "This image could not be used.", "INVALID_IMAGE")
	default:
		return err
	}
}

type fileNode struct {
	id, name, kind, blobID, etag string
	parentID, mime, vaultID      text
	size                         int64
}

func fileNotFound() *apiError { return httpError(404, "File or folder not found", "NOT_FOUND") }

// liveNode is a node of the account with nothing above it in the trash.
// "root" names the account's top folder.
func liveNode(ctx context.Context, q querier, userID int64, id, kind string) (fileNode, error) {
	if id == "root" {
		if err := q.QueryRowContext(ctx, `SELECT id FROM nodes WHERE user_id=? AND parent_id IS NULL`, userID).Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return fileNode{}, fileNotFound()
		} else if err != nil {
			return fileNode{}, err
		}
	}
	var n fileNode
	var blobID text
	var trashed int
	err := q.QueryRowContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,depth) AS (
		SELECT id,parent_id,trashed_at,0 FROM nodes WHERE id=? AND user_id=?
		UNION ALL SELECT n.id,n.parent_id,n.trashed_at,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id WHERE n.user_id=? AND up.depth<256
	) SELECT n.id,n.name,n.type,n.blob_id,COALESCE(n.etag,''),n.parent_id,n.mime,n.vault_id,COALESCE(n.size,0),
		(SELECT COUNT(*) FROM up WHERE trashed_at IS NOT NULL) FROM nodes n WHERE n.id=? AND n.user_id=?`, id, userID, userID, id, userID).
		Scan(&n.id, &n.name, &n.kind, &blobID, &n.etag, &n.parentID, &n.mime, &n.vaultID, &n.size, &trashed)
	if errors.Is(err, sql.ErrNoRows) || err == nil && trashed > 0 {
		return fileNode{}, fileNotFound()
	}
	if err != nil {
		return fileNode{}, err
	}
	n.blobID = blobID.String
	if kind == "file" && n.kind != "file" {
		return fileNode{}, httpError(400, "That is not a file.", "NOT_A_FILE")
	}
	if kind == "folder" && n.kind != "folder" {
		return fileNode{}, httpError(400, "That is not a folder.", "NOT_A_FOLDER")
	}
	return n, nil
}

// usableFolder is a folder the feature may put files in: live, the account's
// own and never a vault. It returns "" otherwise.
func usableFolder(ctx context.Context, q querier, userID int64, id string) string {
	if id == "" {
		return ""
	}
	n, err := liveNode(ctx, q, userID, id, "folder")
	if err != nil || n.vaultID.Valid {
		return ""
	}
	var vaults int
	if q.QueryRowContext(ctx, `SELECT COUNT(*) FROM vaults WHERE folder_id=?`, n.id).Scan(&vaults) != nil || vaults > 0 {
		return ""
	}
	return n.id
}

type snapshot struct {
	pdf    []byte
	sha256 string
	info   signpdf.Info
	reuse  string // the source's own blob when it already was the PDF
}

// prepareSnapshot freezes the bytes of the document to sign as one PDF.
func (h *Handler) prepareSnapshot(ctx context.Context, userID int64, n fileNode) (snapshot, error) {
	var s snapshot
	if n.vaultID.Valid {
		return s, httpError(400, "Files in a vault can’t be prepared for signing.", "VAULT_FILE")
	}
	if n.size > maxSourceBytes {
		return s, httpError(413, "This file is too large to sign (50 MB at most).", "DOCUMENT_TOO_LARGE")
	}
	data, err := h.readBlob(ctx, userID, n.blobID, n.size)
	if err != nil {
		slog.Error("signing source could not be read", "error", err)
		return s, httpError(500, "This file could not be read: its stored data is missing or damaged.", "FILE_UNREADABLE")
	}
	switch {
	case bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")):
		s.pdf, s.reuse = data, n.blobID
	case imageMIME.MatchString(n.mime.String):
		if s.pdf, err = signpdf.ImagePDF(ctx, data); err != nil {
			return s, pdfRefusal(err)
		}
	case officeMIME.MatchString(n.mime.String):
		s.pdf, err = documentconvert.ConvertToPDF(ctx, n.name, bytes.NewReader(data))
		switch {
		case errors.Is(err, documentconvert.ErrUnavailable):
			return s, httpError(400, "This server cannot convert documents. Convert the file to PDF first.", "CONVERSION_UNAVAILABLE")
		case errors.Is(err, documentconvert.ErrUnsupported):
			return s, httpError(415, "Only PDF files, images and office documents can be signed.", "UNSUPPORTED_TYPE")
		case err != nil:
			slog.Warn("signing document conversion failed", "error", err)
			return s, httpError(400, "This document could not be converted to PDF. Convert it to PDF first.", "CONVERSION_FAILED")
		}
	default:
		return s, httpError(415, "Only PDF files, images and office documents can be signed.", "UNSUPPORTED_TYPE")
	}
	if s.info, err = signpdf.Probe(ctx, s.pdf); err != nil {
		return s, pdfRefusal(err)
	}
	sum := sha256.Sum256(s.pdf)
	s.sha256 = hex.EncodeToString(sum[:])
	return s, nil
}

func cleanTitle(value any) (string, error) {
	title, _ := value.(string)
	title = singleLine(title)
	if title == "" || length(title) > maxTitleLength {
		return "", badRequest("A title has 1 to " + strconv.Itoa(maxTitleLength) + " characters.")
	}
	return title, nil
}

func cleanLocale(value any) (string, error) {
	if value == "en" || value == "de" {
		return value.(string), nil
	}
	return "", badRequest("The language must be one of: en, de.")
}

func (h *Handler) cleanExpiry(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	raw, _ := value.(string)
	var at time.Time
	var err error
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if at, err = time.Parse(layout, raw); err == nil {
			break
		}
	}
	if err != nil {
		return nil, badRequest("expiresAt must be a date or null.")
	}
	if !at.After(h.now()) {
		return nil, httpError(400, "The expiry must lie in the future.", "EXPIRY_IN_PAST")
	}
	if at.After(h.now().Add(maxValidityDays * 24 * time.Hour)) {
		return nil, badRequest("A link can stay valid for " + strconv.Itoa(maxValidityDays) + " days at most.")
	}
	return stamp(at), nil
}

func splitExtension(name string) (string, string) {
	index := strings.LastIndexByte(name, '.')
	if index <= 0 {
		return name, ""
	}
	return name[:index], name[index:]
}

func defaultPartyName(locale string, n int) string {
	if locale == "de" {
		return "Partei " + strconv.Itoa(n)
	}
	return "Party " + strconv.Itoa(n)
}

func colorFor(position int) string { return partyColors[position%len(partyColors)] }

func insertParty(ctx context.Context, q querier, documentID string, p party, at string) (string, error) {
	id := uuid.NewString()
	_, err := q.ExecContext(ctx, `INSERT INTO sign_parties(id,document_id,position,name,color,email,status,is_self,user_id,created_at) VALUES(?,?,?,?,?,?,'pending',?,?,?)`,
		id, documentID, p.Position, p.Name, p.Color, nullable(p.Email), p.IsSelf, p.UserID, at)
	return id, err
}

// createDocument makes a draft from a file of the library.
func (h *Handler) createDocument(r *http.Request, userID int64, input body) (any, error) {
	nodeID, _ := input["nodeId"].(string)
	if nodeID == "" {
		return nil, badRequest("nodeId is required.")
	}
	var title, locale any = input["title"], input["locale"]
	_, hasTitle := input["title"]
	_, hasLocale := input["locale"]
	created, hasForm, err := h.newDraft(r.Context(), userID, nodeID, title, hasTitle, locale, hasLocale, clientOf(r))
	if err != nil {
		return nil, err
	}
	view, err := h.describe(r.Context(), userID, created)
	if err != nil {
		return nil, err
	}
	// The final file is built from the frozen pages: existing form fields are
	// flattened and an existing digital signature in the source is lost.
	warnings := []string{}
	if hasForm {
		warnings = []string{"FORM_FIELDS_FLATTENED"}
	}
	view["warnings"] = warnings
	return view, nil
}

func (h *Handler) newDraft(ctx context.Context, userID int64, nodeID string, titleInput any, hasTitle bool, localeInput any, hasLocale bool, from client) (string, bool, error) {
	n, err := liveNode(ctx, h.db, userID, nodeID, "file")
	if err != nil {
		return "", false, err
	}
	locale := "en"
	if hasLocale {
		if locale, err = cleanLocale(localeInput); err != nil {
			return "", false, err
		}
	}
	if !hasTitle {
		stem, _ := splitExtension(n.name)
		titleInput = stem
	}
	title, err := cleanTitle(titleInput)
	if err != nil {
		return "", false, err
	}
	s, err := h.prepareSnapshot(ctx, userID, n)
	if err != nil {
		return "", false, err
	}
	var stored storedBlob
	blobID := s.reuse
	if s.reuse == "" {
		if err = h.checkRoom(ctx, h.db, userID, int64(len(s.pdf)), "Not enough storage left for this file."); err != nil {
			return "", false, err
		}
		if stored, err = h.storeBlob(ctx, userID, s.pdf); err != nil {
			return "", false, err
		}
		blobID = stored.id
	}
	id := uuid.NewString()
	at := h.stampNow()
	pages, _ := json.Marshal(s.info.Pages)
	err = h.tx(ctx, func(tx *sql.Tx) error {
		if s.reuse == "" {
			if err := adoptBlob(ctx, tx, userID, stored); err != nil {
				return err
			}
		} else {
			result, err := tx.ExecContext(ctx, `UPDATE blobs SET ref_count=ref_count+1 WHERE id=? AND user_id=?`, blobID, userID)
			if err != nil {
				return err
			}
			if changed, _ := result.RowsAffected(); changed != 1 {
				return httpError(409, "The file changed while the draft was prepared.", "SOURCE_CHANGED")
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sign_documents(id,owner_id,source_node_id,source_name,source_parent_id,title,locale,expires_at,
			snapshot_blob_id,snapshot_sha256,snapshot_size,page_count,pages_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, userID, n.id, n.name, nullable(n.parentID), title, locale, stamp(h.now().Add(defaultValidity)), blobID, s.sha256, len(s.pdf), len(s.info.Pages), string(pages), at, at); err != nil {
			return err
		}
		if _, err := insertParty(ctx, tx, id, party{Name: defaultPartyName(locale, 1), Color: colorFor(0)}, at); err != nil {
			return err
		}
		return appendEvent(ctx, tx, id, event{kind: "created", at: at, client: from, data: map[string]any{"sourceNodeId": n.id, "sha256": s.sha256}})
	})
	if err != nil {
		if s.reuse == "" {
			stored.discard()
		}
		return "", false, err
	}
	h.notifyOwner(ctx, userID, id, statusDraft)
	return id, s.info.HasAcroForm, nil
}

func (h *Handler) describe(ctx context.Context, userID int64, documentID string) (map[string]any, error) {
	d, err := ownedDocument(ctx, h.db, userID, documentID)
	if err != nil {
		return nil, err
	}
	parties, err := listParties(ctx, h.db, d.ID)
	if err != nil {
		return nil, err
	}
	delivery, err := deliveryByParty(ctx, h.db, d, parties)
	if err != nil {
		return nil, err
	}
	partyViews := make([]map[string]any, 0, len(parties))
	for _, p := range parties {
		view := partyView(p)
		view["delivery"] = delivery[p.ID]
		partyViews = append(partyViews, view)
	}
	fields, err := listFields(ctx, h.db, d.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"document": documentView(d), "parties": partyViews, "fields": fieldViews(fields)}, nil
}

func (h *Handler) getDocument(r *http.Request, userID int64, _ body) (any, error) {
	return h.describe(r.Context(), userID, r.PathValue("id"))
}

var documentStatuses = []string{statusDraft, statusSent, statusInProgress, statusFinalizing, statusCompleted, statusDeclined, statusExpired, statusCanceled}

// pageSize reads a limit like Number() does: anything unusable is the default.
func pageSize(raw string, fallback int) int {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(n) || math.Trunc(n) == 0 {
		return fallback
	}
	return int(math.Min(100, math.Max(1, math.Trunc(n))))
}

func (h *Handler) listDocuments(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	query := r.URL.Query()
	var status any
	if query.Has("status") {
		if !slices.Contains(documentStatuses, query.Get("status")) {
			return nil, badRequest("Unknown status.")
		}
		status = query.Get("status")
	}
	limit := pageSize(query.Get("limit"), 50)
	var afterAt, afterID any
	if raw := query.Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		var cursor []string
		if err != nil || json.Unmarshal(decoded, &cursor) != nil || len(cursor) != 2 {
			return nil, httpError(400, "The cursor is not valid.", "INVALID_CURSOR")
		}
		afterAt, afterID = cursor[0], cursor[1]
	}
	documents, err := queryDocuments(ctx, h.db, `WHERE owner_id=? AND (? IS NULL OR status=?) AND (? IS NULL OR updated_at<? OR (updated_at=? AND id<?))
		ORDER BY updated_at DESC,id DESC LIMIT ?`, userID, status, status, afterAt, afterAt, afterAt, afterID, limit+1)
	if err != nil {
		return nil, err
	}
	more := len(documents) > limit
	documents = documents[:min(len(documents), limit)]
	items := make([]map[string]any, 0, len(documents))
	for _, d := range documents {
		parties, err := listParties(ctx, h.db, d.ID)
		if err != nil {
			return nil, err
		}
		summaries := make([]map[string]any, 0, len(parties))
		for _, p := range parties {
			summaries = append(summaries, map[string]any{"id": p.ID, "name": p.Name, "color": p.Color, "status": p.Status})
		}
		items = append(items, map[string]any{"id": d.ID, "title": d.Title, "status": d.Status, "parties": summaries, "pageCount": d.PageCount,
			"expiresAt": nullable(d.ExpiresAt), "createdAt": d.CreatedAt, "updatedAt": d.UpdatedAt, "completedAt": nullable(d.CompletedAt),
			"finalNodeId": nullable(d.FinalNodeID), "sourceNodeId": nullable(d.SourceNodeID)})
	}
	var next any
	if more {
		last := documents[len(documents)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(encodeJSON([]string{last.UpdatedAt, last.ID})))
	}
	return map[string]any{"items": items, "nextCursor": next}, nil
}

func lockedError() *apiError {
	return httpError(409, "This document can no longer be changed.", "DOCUMENT_LOCKED")
}

// editable: a draft, or sent while nobody has opened a link.
func editable(ctx context.Context, q querier, d document) error {
	if d.Status == statusDraft {
		return nil
	}
	if d.Status == statusSent {
		var opened int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sign_parties WHERE document_id=? AND opened_at IS NOT NULL`, d.ID).Scan(&opened); err != nil {
			return err
		}
		if opened == 0 {
			return nil
		}
	}
	return lockedError()
}

// change runs work on one of the owner's documents inside a transaction.
func (h *Handler) change(ctx context.Context, userID int64, documentID string, work func(tx *sql.Tx, d document) error) (document, error) {
	var d document
	err := h.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if d, err = ownedDocument(ctx, tx, userID, documentID); err != nil {
			return err
		}
		return work(tx, d)
	})
	return d, err
}

func (h *Handler) updateSettings(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	d, err := h.change(ctx, userID, r.PathValue("id"), func(tx *sql.Tx, d document) error {
		if err := editable(ctx, tx, d); err != nil {
			return err
		}
		at := h.stampNow()
		columns, args, changed := []string{"updated_at=?"}, []any{at}, []any{}
		set := func(column string, v any) {
			columns, args, changed = append(columns, column+"=?"), append(args, v), append(changed, column)
		}
		if v, ok := input["title"]; ok {
			title, err := cleanTitle(v)
			if err != nil {
				return err
			}
			set("title", title)
		}
		if v, ok := input["locale"]; ok {
			locale, err := cleanLocale(v)
			if err != nil {
				return err
			}
			set("locale", locale)
		}
		if v, ok := input["expiresAt"]; ok {
			expires, err := h.cleanExpiry(v)
			if err != nil {
				return err
			}
			set("expires_at", expires)
		}
		order := ""
		if v, ok := input["signingOrder"]; ok {
			if v != "preserved" && v != "random" {
				return badRequest("signingOrder is preserved or random.")
			}
			order = v.(string)
			set("signing_order", order)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET `+strings.Join(columns, ",")+` WHERE id=?`, append(args, d.ID)...); err != nil {
			return err
		}
		if d.Status != statusSent {
			return nil
		}
		if order != "" {
			if err := applyInitialStatuses(ctx, tx, d.ID, order); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, d.ID, event{kind: "updated", at: at, client: clientOf(r), data: map[string]any{"changed": changed}})
	})
	if err != nil {
		return nil, err
	}
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return h.describe(ctx, userID, d.ID)
}

func initialStatus(order string, index int) string {
	if order == "random" || index == 0 {
		return partyPending
	}
	return partyWaiting
}

func applyInitialStatuses(ctx context.Context, q querier, documentID, order string) error {
	parties, err := listParties(ctx, q, documentID)
	if err != nil {
		return err
	}
	for i, p := range parties {
		if _, err = q.ExecContext(ctx, `UPDATE sign_parties SET status=? WHERE id=?`, initialStatus(order, i), p.ID); err != nil {
			return err
		}
	}
	return nil
}

func invalidParty(message string) *apiError { return httpError(400, message, "INVALID_PARTY") }

func parseParty(raw any, position int) (party, string, error) {
	var p party
	entry, ok := raw.(map[string]any)
	if !ok {
		return p, "", invalidParty("Each party must be an object.")
	}
	id, _ := entry["id"].(string)
	if v := entry["isSelf"]; v != nil {
		self, ok := v.(bool)
		if !ok {
			return p, "", invalidParty("isSelf must be true or false.")
		}
		p.IsSelf = flag(self)
	}
	if v := entry["email"]; v != nil && v != "" {
		email, _ := v.(string)
		email = strings.TrimSpace(email)
		if email == "" || length(email) > maxEmailLength || !emailPattern.MatchString(email) {
			return p, "", invalidParty("That email address is not valid.")
		}
		p.Email = text{String: email, Valid: true}
	}
	name, _ := entry["name"].(string)
	p.Name = singleLine(name)
	if p.Name == "" || length(p.Name) > maxNameLength {
		return p, "", invalidParty("A party needs a name of 1 to " + strconv.Itoa(maxNameLength) + " characters.")
	}
	p.Position, p.Color = position, colorFor(position)
	if v := entry["color"]; v != nil && v != "" {
		color, _ := v.(string)
		if !colorPattern.MatchString(color) {
			return p, "", invalidParty("A party colour looks like #1a2b3c.")
		}
		p.Color = strings.ToLower(color)
	}
	return p, id, nil
}

// replaceParties replaces the party list of a draft: known ids keep their
// party and its fields, parties left out are removed with their fields.
func replaceParties(ctx context.Context, tx *sql.Tx, d document, input any, at string) error {
	list, ok := input.([]any)
	if !ok || len(list) < 1 || len(list) > maxParties {
		return invalidParty("A document has 1 to " + strconv.Itoa(maxParties) + " parties.")
	}
	existing, err := listParties(ctx, tx, d.ID)
	if err != nil {
		return err
	}
	known := map[string]party{}
	for _, p := range existing {
		known[p.ID] = p
	}
	seen := map[string]bool{}
	var entries []party
	self := 0
	for position, raw := range list {
		p, id, err := parseParty(raw, position)
		if err != nil {
			return err
		}
		if _, ok := known[id]; ok {
			if seen[id] {
				return invalidParty("A party is listed twice.")
			}
			seen[id], p.ID = true, id
		}
		if p.IsSelf == 1 {
			self++
			p.UserID = sql.NullInt64{Int64: d.OwnerID, Valid: true}
			if !p.Email.Valid {
				var email text
				if err := tx.QueryRowContext(ctx, `SELECT email FROM users WHERE id=?`, d.OwnerID).Scan(&email); err != nil {
					return err
				}
				p.Email = text{String: email.String, Valid: email.String != ""}
			}
		}
		entries = append(entries, p)
	}
	if self > 1 {
		return invalidParty("Only one party can be you.")
	}
	for _, p := range existing {
		if !seen[p.ID] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sign_parties WHERE id=?`, p.ID); err != nil {
				return err
			}
		}
	}
	for _, p := range entries {
		if p.ID == "" {
			if _, err := insertParty(ctx, tx, d.ID, p, at); err != nil {
				return err
			}
			continue
		}
		// A party that stops being "you" is unbound again; one bound by
		// someone's claim is never touched.
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET name=?,email=?,color=?,position=?,is_self=?,
			user_id=CASE WHEN ?=1 THEN ? WHEN is_self=1 THEN NULL ELSE user_id END WHERE id=?`,
			p.Name, nullable(p.Email), p.Color, p.Position, p.IsSelf, p.IsSelf, p.UserID, p.ID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) saveParties(r *http.Request, userID int64, input body) (any, error) {
	if err := h.setParties(r.Context(), userID, r.PathValue("id"), input["parties"]); err != nil {
		return nil, err
	}
	return h.describe(r.Context(), userID, r.PathValue("id"))
}

func (h *Handler) setParties(ctx context.Context, userID int64, documentID string, parties any) error {
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if d.Status != statusDraft {
			return lockedError()
		}
		at := h.stampNow()
		if err := replaceParties(ctx, tx, d, parties, at); err != nil {
			return err
		}
		return touch(ctx, tx, d.ID, at)
	})
	if err == nil {
		h.notifyOwner(ctx, userID, d.ID, d.Status)
	}
	return err
}

func (h *Handler) saveFields(r *http.Request, userID int64, input body) (any, error) {
	return h.setFields(r.Context(), userID, r.PathValue("id"), input["fields"], clientOf(r))
}

func (h *Handler) setFields(ctx context.Context, userID int64, documentID string, input any, from client) (any, error) {
	var fields []field
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if err := editable(ctx, tx, d); err != nil {
			return err
		}
		if err := replaceFields(ctx, tx, d, input); err != nil {
			return err
		}
		at := h.stampNow()
		if err := touch(ctx, tx, d.ID, at); err != nil {
			return err
		}
		if d.Status == statusSent {
			if err := appendEvent(ctx, tx, d.ID, event{kind: "updated", at: at, client: from, data: map[string]any{"changed": []any{"fields"}}}); err != nil {
				return err
			}
		}
		var err error
		fields, err = listFields(ctx, tx, d.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return map[string]any{"fields": fieldViews(fields)}, nil
}

// importFields adds the PDF's own form fields to the first party. Draft only.
func (h *Handler) importFields(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	d, err := ownedDocument(ctx, h.db, userID, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	if d.Status != statusDraft {
		return nil, lockedError()
	}
	pdf, err := h.readBlob(ctx, userID, d.SnapshotBlobID, d.SnapshotSize)
	if err != nil {
		return nil, err
	}
	found, err := signpdf.ImportFields(ctx, pdf)
	if err != nil {
		return nil, pdfRefusal(err)
	}
	imported := 0
	var fields []field
	_, err = h.change(ctx, userID, d.ID, func(tx *sql.Tx, d document) error {
		if d.Status != statusDraft {
			return lockedError()
		}
		imported, err = addImported(ctx, tx, d, found)
		if err != nil {
			return err
		}
		if err = touch(ctx, tx, d.ID, h.stampNow()); err != nil {
			return err
		}
		fields, err = listFields(ctx, tx, d.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	h.notifyOwner(ctx, userID, d.ID, d.Status)
	return map[string]any{"imported": imported, "fields": fieldViews(fields)}, nil
}

// duplicateDocument makes a new draft with the same pages, parties and
// fields; no values, links or history.
func (h *Handler) duplicateDocument(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	id := uuid.NewString()
	_, err := h.change(ctx, userID, r.PathValue("id"), func(tx *sql.Tx, source document) error {
		at := h.stampNow()
		if _, err := tx.ExecContext(ctx, `UPDATE blobs SET ref_count=ref_count+1 WHERE id=?`, source.SnapshotBlobID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sign_documents(id,owner_id,source_node_id,source_name,source_parent_id,title,signing_order,locale,
			expires_at,snapshot_blob_id,snapshot_sha256,snapshot_size,page_count,pages_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, userID, nullable(source.SourceNodeID), nullable(source.SourceName), nullable(source.SourceParentID), truncate(source.Title+" (copy)", maxTitleLength),
			source.SigningOrder, source.Locale, stamp(h.now().Add(defaultValidity)), source.SnapshotBlobID, source.SnapshotSHA256, source.SnapshotSize, source.PageCount, source.PagesJSON, at, at); err != nil {
			return err
		}
		parties, err := listParties(ctx, tx, source.ID)
		if err != nil {
			return err
		}
		partyIDs := map[string]string{}
		for _, p := range parties {
			copied := party{Name: p.Name, Email: p.Email, Color: p.Color, Position: p.Position}
			if partyIDs[p.ID], err = insertParty(ctx, tx, id, copied, at); err != nil {
				return err
			}
		}
		fields, err := listFields(ctx, tx, source.ID)
		if err != nil {
			return err
		}
		for _, f := range fields {
			f.ID, f.DocumentID, f.PartyID = uuid.NewString(), id, partyIDs[f.PartyID]
			if err := insertField(ctx, tx, f); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, id, event{kind: "created", at: at, client: clientOf(r), data: map[string]any{"duplicatedFrom": source.ID}})
	})
	if err != nil {
		return nil, err
	}
	h.notifyOwner(ctx, userID, id, statusDraft)
	return h.describe(ctx, userID, id)
}

// cancel stops a document that is out for signing.
func (h *Handler) cancel(ctx context.Context, userID int64, documentID string, from client) error {
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if !isLive(d.Status) {
			return httpError(409, "Only a document that is out for signing can be canceled.", "INVALID_STATE")
		}
		at := h.stampNow()
		if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='canceled',canceled_at=?,updated_at=? WHERE id=?`, at, at, d.ID); err != nil {
			return err
		}
		if err := appendEvent(ctx, tx, d.ID, event{kind: "canceled", at: at, client: from}); err != nil {
			return err
		}
		if err := revokeDocumentGrants(ctx, tx, d.ID); err != nil {
			return err
		}
		return cancelQueued(ctx, tx, d.ID, "", requestKinds...)
	})
	if err != nil {
		return err
	}
	h.notifyOwner(ctx, userID, d.ID, statusCanceled)
	return nil
}

func (h *Handler) cancelDocument(r *http.Request, userID int64, _ body) (any, error) {
	if err := h.cancel(r.Context(), userID, r.PathValue("id"), clientOf(r)); err != nil {
		return nil, err
	}
	return h.describe(r.Context(), userID, r.PathValue("id"))
}

// remove deletes a document with its values and snapshot. The signed file in
// the library stays.
func (h *Handler) remove(ctx context.Context, userID int64, documentID string) error {
	var orphaned []string
	d, err := h.change(ctx, userID, documentID, func(tx *sql.Tx, d document) error {
		if isLive(d.Status) || d.Status == statusFinalizing {
			return httpError(409, "Cancel the document before deleting it.", "INVALID_STATE")
		}
		values, err := listValues(ctx, tx, d.ID)
		if err != nil {
			return err
		}
		blobs := []string{d.SnapshotBlobID}
		for _, v := range values {
			if v.BlobRef.Valid {
				blobs = append(blobs, v.BlobRef.String)
			}
		}
		if d.CleanBlobID.Valid {
			blobs = append(blobs, d.CleanBlobID.String)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sign_documents WHERE id=?`, d.ID); err != nil {
			return err
		}
		orphaned, err = releaseBlobs(ctx, tx, blobs...)
		return err
	})
	if err != nil {
		return err
	}
	h.removeBlobs(userID, orphaned)
	h.notifyOwner(ctx, userID, d.ID, "deleted")
	return nil
}

func (h *Handler) deleteDocument(r *http.Request, userID int64, _ body) (any, error) {
	if err := h.remove(r.Context(), userID, r.PathValue("id")); err != nil {
		return nil, err
	}
	return map[string]bool{"success": true}, nil
}

// notifyOwner tells the owner's open sessions that a document changed, and the
// sessions of the accounts that sign it that their inbox changed.
func (h *Handler) notifyOwner(ctx context.Context, ownerID int64, documentID, status string) {
	ctx = context.WithoutCancel(ctx)
	h.notifyUser(ownerID, "signing:updated", map[string]any{"documentId": documentID, "status": status})
	h.announceDocument(ctx, documentID, status)
}
