package signing

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/google/uuid"
)

// What the holder of a signing link can see and do. The token finds the party;
// everything else follows from the party's rows, never from ids the client
// sends. Unknown, malformed, revoked and replaced tokens are the same 404, no
// cookie is read or set, and no answer is cacheable.

func linkNotFound() *apiError { return httpError(404, "This link does not exist.", "NOT_FOUND") }

type signerContext struct {
	document document
	party    party
	parties  []party
	state    string
}

// publicState is what a signer's link shows.
func publicState(documentStatus, partyStatus string) string {
	switch {
	case documentStatus == statusCanceled || documentStatus == statusExpired || documentStatus == statusDeclined:
		return documentStatus
	case partyStatus == partyCompleted:
		return "completed"
	case partyStatus == partyWaiting:
		return "waiting"
	default:
		return "ready"
	}
}

func refusal(state string) *apiError {
	switch state {
	case "waiting":
		return httpError(409, "It is not your turn yet.", "NOT_YOUR_TURN")
	case "completed":
		return httpError(409, "You have already signed this document.", "ALREADY_COMPLETED")
	default:
		return httpError(409, "This document is closed.", "DOCUMENT_CLOSED")
	}
}

func (c signerContext) ready() error {
	if c.state != "ready" {
		return refusal(c.state)
	}
	return nil
}

// partyByToken finds the party behind a link's token or a grant token.
func (h *Handler) partyByToken(ctx context.Context, token string, from client) (party, error) {
	if !tokenPattern.MatchString(token) {
		return party{}, linkNotFound()
	}
	hash := hashToken(token)
	p, err := scanParty(h.db.QueryRowContext(ctx, `SELECT `+partyColumns+` FROM sign_parties WHERE token_hash=?`, hash))
	if err == nil && subtle.ConstantTimeCompare([]byte(hash), []byte(p.TokenHash.String)) == 1 {
		return p, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return party{}, err
	}
	return h.resolveGrant(ctx, hash, from)
}

// openContext expires the document first when its time is up.
func (h *Handler) openContext(ctx context.Context, token string, from client) (signerContext, error) {
	var c signerContext
	p, err := h.partyByToken(ctx, token, from)
	if err != nil {
		return c, err
	}
	d, err := documentByID(ctx, h.db, p.DocumentID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, linkNotFound()
	}
	if err != nil {
		return c, err
	}
	var disabled text
	err = h.db.QueryRowContext(ctx, `SELECT disabled_at FROM users WHERE id=?`, d.OwnerID).Scan(&disabled)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (disabled.Valid || d.Status == statusDraft) {
		return c, linkNotFound()
	}
	if err != nil {
		return c, err
	}
	expired, err := h.expireIfDue(ctx, d)
	if err != nil {
		return c, err
	}
	if expired {
		if d, err = documentByID(ctx, h.db, d.ID); err != nil {
			return c, err
		}
	}
	parties, err := listParties(ctx, h.db, d.ID)
	if err != nil {
		return c, err
	}
	return signerContext{document: d, party: p, parties: parties, state: publicState(d.Status, p.Status)}, nil
}

// expireIfDue marks a live document expired once its time is up.
func (h *Handler) expireIfDue(ctx context.Context, d document) (bool, error) {
	due, ok := parseStamp(d.ExpiresAt.String)
	if !ok || due.After(h.now()) || !isLive(d.Status) {
		return false, nil
	}
	changed := false
	err := h.tx(ctx, func(tx *sql.Tx) error {
		at := h.stampNow()
		result, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='expired',updated_at=? WHERE id=? AND status IN ('sent','in_progress')`, at, d.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return nil
		}
		changed = true
		if err := appendEvent(ctx, tx, d.ID, event{kind: "expired", at: at}); err != nil {
			return err
		}
		if err := revokeDocumentGrants(ctx, tx, d.ID); err != nil {
			return err
		}
		if err := cancelQueued(ctx, tx, d.ID, "", requestKinds...); err != nil {
			return err
		}
		return h.queueNotice(ctx, tx, mailCreatorExpired, d, nil, nil)
	})
	if err == nil && changed {
		h.wakeMail()
		h.notifyOwner(ctx, d.OwnerID, d.ID, statusExpired)
	}
	return changed, err
}

// guesses slows an address down that keeps trying links that do not exist;
// only answers that say "no such link" count.
type guesses struct {
	mu   sync.Mutex
	seen map[string]*struct {
		count int
		reset time.Time
	}
}

const maxGuesses = 60

func newGuesses() *guesses {
	return &guesses{seen: map[string]*struct {
		count int
		reset time.Time
	}{}}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (g *guesses) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			key = r.RemoteAddr
		}
		now := time.Now()
		g.mu.Lock()
		for other, entry := range g.seen {
			if now.After(entry.reset) {
				delete(g.seen, other)
			}
		}
		entry := g.seen[key]
		blocked := entry != nil && entry.count >= maxGuesses
		g.mu.Unlock()
		if blocked {
			httpx.WriteJSON(w, http.StatusTooManyRequests, map[string]string{"error": "Too many requests, try again later", "code": "RATE_LIMITED"})
			return
		}
		recorder := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(recorder, r)
		if recorder.status != http.StatusNotFound {
			return
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.seen[key] == nil {
			g.seen[key] = &struct {
				count int
				reset time.Time
			}{reset: now.Add(15 * time.Minute)}
		}
		g.seen[key].count++
	})
}

func (h *Handler) publicRoutes(mux *http.ServeMux) {
	guess := newGuesses()
	read := limiter(600, nil)
	write := limiter(300, nil)
	perToken := limiter(500, func(r *http.Request) string { return hashToken(r.PathValue("token")) })

	route := func(method, path string, handler func(w http.ResponseWriter, r *http.Request, c signerContext)) {
		limit := write
		if method == http.MethodGet {
			limit = read
		}
		var next http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := h.openContext(r.Context(), r.PathValue("token"), clientOf(r))
			if err != nil {
				writeErr(w, err)
				return
			}
			handler(w, r, c)
		})
		next = guess.middleware(limit.Middleware(perToken.Middleware(next)))
		mux.Handle(method+" /api/public/sign/{token}"+path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", publicCacheControl)
			w.Header().Set("X-Robots-Tag", "noindex, nofollow")
			w.Header().Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		}))
	}
	call := func(method, path string, status int, action func(r *http.Request, c signerContext, input body) (any, error)) {
		route(method, path, func(w http.ResponseWriter, r *http.Request, c signerContext) {
			input, err := readBody(w, r)
			if err != nil {
				writeErr(w, err)
				return
			}
			value, err := action(r, c, input)
			respond(w, status, value, err)
		})
	}
	call("GET", "", 200, h.publicDescribe)
	route("GET", "/pdf", h.publicPDF)
	call("POST", "/consent", 200, h.recordConsent)
	call("PUT", "/fields/{fieldId}", 200, h.saveFieldValue)
	route("POST", "/fields/{fieldId}/upload", h.uploadFieldValue)
	route("GET", "/values/{fieldId}/image", h.publicValueImage)
	call("POST", "/complete", 200, h.completeParty)
	call("POST", "/decline", 200, h.declineParty)
	call("GET", "/result", 200, func(r *http.Request, c signerContext, _ body) (any, error) {
		available, err := finalAvailable(r.Context(), h.db, c.document)
		return map[string]any{"state": c.state, "finalAvailable": available}, err
	})
	route("GET", "/final", h.publicFinal)
}

// recordOpened counts the first time a party's turn is seen.
func (h *Handler) recordOpened(r *http.Request, c signerContext) error {
	if c.party.OpenedAt.Valid {
		return nil
	}
	ctx := r.Context()
	opened := false
	err := h.tx(ctx, func(tx *sql.Tx) error {
		at := h.stampNow()
		result, err := tx.ExecContext(ctx, `UPDATE sign_parties SET opened_at=?,status=CASE WHEN status='pending' THEN 'opened' ELSE status END WHERE id=? AND opened_at IS NULL`, at, c.party.ID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return nil
		}
		opened = true
		if c.document.Status == statusSent {
			if _, err := tx.ExecContext(ctx, `UPDATE sign_documents SET status='in_progress',updated_at=? WHERE id=? AND status='sent'`, at, c.document.ID); err != nil {
				return err
			}
		}
		return appendEvent(ctx, tx, c.document.ID, event{kind: "opened", partyID: c.party.ID, at: at, client: clientOf(r)})
	})
	if err == nil && opened {
		h.notifyOwner(ctx, c.document.OwnerID, c.document.ID, statusInProgress)
	}
	return err
}

// publicDescribe is the JSON the signing page starts from.
func (h *Handler) publicDescribe(r *http.Request, c signerContext, _ body) (any, error) {
	ctx := r.Context()
	d, p := c.document, c.party
	status := p.Status
	if c.state == "ready" && status == partyPending {
		status = partyOpened
	}
	brief := map[string]any{"id": p.ID, "name": p.Name, "color": p.Color, "status": status}
	if c.state == statusCanceled || c.state == statusExpired || c.state == statusDeclined {
		return map[string]any{"state": c.state, "document": map[string]any{"locale": d.Locale}, "party": brief}, nil
	}
	if c.state == "ready" {
		if err := h.recordOpened(r, c); err != nil {
			return nil, err
		}
	}
	brief["consentedAt"], brief["signerName"] = nullable(p.ConsentedAt), nullable(p.SignerName)
	consent := consentTexts[d.Locale]
	if consent == "" {
		consent = consentTexts["en"]
	}
	version := int64(consentVersion)
	if d.ConsentVersion.Valid && d.ConsentVersion.Int64 > 0 {
		version = d.ConsentVersion.Int64
	}
	done, ahead := 0, 0
	for _, other := range c.parties {
		if other.Status == partyCompleted {
			done++
		} else if other.Position < p.Position {
			ahead++
		}
	}
	out := map[string]any{
		"state": c.state,
		"document": map[string]any{"title": d.Title, "pageCount": d.PageCount, "pages": decodeJSON(text{String: d.PagesJSON, Valid: true}, []any{}),
			"locale": d.Locale, "expiresAt": nullable(d.ExpiresAt), "consentText": consent, "consentVersion": version},
		"party": brief,
		// Counts only: a signer never learns who the other parties are.
		"progress": map[string]any{"done": done, "total": len(c.parties)},
	}
	if c.state == "waiting" {
		out["fields"], out["myValues"] = []any{}, map[string]any{}
		out["turn"] = map[string]any{"canSign": false, "waitingCount": ahead}
		return out, nil
	}
	fields, mine, err := h.signerFields(ctx, c)
	if err != nil {
		return nil, err
	}
	out["fields"], out["myValues"] = fields, mine
	out["turn"] = map[string]any{"canSign": c.state == "ready"}
	if c.state == "completed" {
		available, err := finalAvailable(ctx, h.db, d)
		if err != nil {
			return nil, err
		}
		out["result"] = map[string]any{"finalAvailable": available}
	}
	return out, nil
}

// signerFields are the party's own fields, and of the others what already
// shows a final value.
func (h *Handler) signerFields(ctx context.Context, c signerContext) ([]map[string]any, map[string]any, error) {
	fields, err := listFields(ctx, h.db, c.document.ID)
	if err != nil {
		return nil, nil, err
	}
	stored, err := listValues(ctx, h.db, c.document.ID)
	if err != nil {
		return nil, nil, err
	}
	values := map[string]value{}
	for _, v := range stored {
		values[v.FieldID] = v
	}
	done := map[string]bool{}
	for _, other := range c.parties {
		done[other.ID] = other.Status == partyCompleted
	}
	out, mine := []map[string]any{}, map[string]any{}
	for _, f := range fields {
		own := f.PartyID == c.party.ID
		if !own && fieldTypes[f.Type].storage != storeStatic && !f.Readonly && !done[f.PartyID] {
			continue
		}
		var effective any
		if v, ok := values[f.ID]; ok {
			effective = v.decoded()
		} else {
			effective = decodeJSON(f.Default, nil)
		}
		if !own && effective == nil {
			continue
		}
		view := fieldView(f)
		delete(view, "position")
		view["own"] = own
		if own {
			if effective != nil {
				mine[f.ID] = effective
			}
		} else {
			view["readonly"], view["defaultValue"], view["value"] = true, nil, effective
		}
		out = append(out, view)
	}
	return out, mine, nil
}

var timezonePattern = regexp.MustCompile(`^[A-Za-z0-9_+\-/]{1,64}$`)

func (h *Handler) recordConsent(r *http.Request, c signerContext, input body) (any, error) {
	ctx := r.Context()
	if err := c.ready(); err != nil {
		return nil, err
	}
	if input["accepted"] != true {
		return nil, httpError(400, "Consent is needed to sign.", "CONSENT_REQUIRED")
	}
	raw, _ := input["signerName"].(string)
	name := singleLine(raw)
	if name == "" || length(name) > maxSignerName {
		return nil, badRequest("Enter your name (up to " + strconv.Itoa(maxSignerName) + " characters).")
	}
	zone, _ := input["timezone"].(string)
	zone = strings.TrimSpace(zone)
	if !timezonePattern.MatchString(zone) {
		zone = ""
	}
	var offset any
	if n, ok := input["tzOffsetMinutes"].(float64); ok && n == float64(int(n)) && n >= -14*60 && n <= 14*60 {
		offset = int(n)
	}
	at := h.stampNow()
	from := clientOf(r)
	version := int64(consentVersion)
	if c.document.ConsentVersion.Valid && c.document.ConsentVersion.Int64 > 0 {
		version = c.document.ConsentVersion.Int64
	}
	err := h.tx(ctx, func(tx *sql.Tx) error {
		if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, false); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_parties SET consented_at=COALESCE(consented_at,?),signer_name=?,timezone=?,tz_offset_minutes=?,ip=?,user_agent=? WHERE id=?`,
			at, name, nullIfEmpty(zone), offset, nullIfEmpty(from.ip), nullIfEmpty(from.userAgent), c.party.ID); err != nil {
			return err
		}
		return appendEvent(ctx, tx, c.document.ID, event{kind: "consented", partyID: c.party.ID, at: at, client: from,
			data: map[string]any{"signerName": name, "consentVersion": version, "timezone": nullIfEmpty(zone), "tzOffsetMinutes": offset}})
	})
	if err != nil {
		return nil, err
	}
	if c.party.ConsentedAt.Valid {
		at = c.party.ConsentedAt.String
	}
	return map[string]any{"consentedAt": at, "signerName": name}, nil
}

// writableField is one of the party's own fields that takes input.
func (h *Handler) writableField(ctx context.Context, c signerContext, id string) (field, error) {
	if uuidPattern.MatchString(id) {
		fields, err := queryFields(ctx, h.db, `WHERE id=? AND document_id=? AND party_id=?`, id, c.document.ID, c.party.ID)
		if err != nil {
			return field{}, err
		}
		if len(fields) == 1 && !fields[0].Readonly && fieldTypes[fields[0].Type].storage != storeStatic {
			return fields[0], nil
		}
	}
	return field{}, httpError(404, "Field not found", "NOT_FOUND")
}

// assertWritable re-checks inside a transaction that a party may write now, so
// a value can never change after it finished or the document closed.
func assertWritable(ctx context.Context, q querier, documentID, partyID string, needConsent bool) error {
	var documentStatus, partyStatus string
	var consented text
	err := q.QueryRowContext(ctx, `SELECT d.status,p.status,p.consented_at FROM sign_parties p JOIN sign_documents d ON d.id=p.document_id WHERE p.id=? AND d.id=?`, partyID, documentID).
		Scan(&documentStatus, &partyStatus, &consented)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !isLive(documentStatus) {
		return httpError(409, "This document is closed.", "DOCUMENT_CLOSED")
	}
	if err != nil {
		return err
	}
	if partyStatus == partyCompleted {
		return refusal("completed")
	}
	if partyStatus == partyWaiting {
		return refusal("waiting")
	}
	if needConsent && !consented.Valid {
		return httpError(409, "Agree to sign electronically first.", "CONSENT_REQUIRED")
	}
	return nil
}

func removeValue(ctx context.Context, q querier, v value) ([]string, error) {
	if _, err := q.ExecContext(ctx, `DELETE FROM sign_values WHERE id=?`, v.ID); err != nil {
		return nil, err
	}
	if !v.BlobRef.Valid {
		return nil, nil
	}
	return releaseBlobs(ctx, q, v.BlobRef.String)
}

// saveTypedValue stores a typed value; an empty one clears the field.
// needConsent is off only for a prefill: finishing still needs consent.
func (h *Handler) saveTypedValue(ctx context.Context, c signerContext, f field, raw any, needConsent bool) (any, error) {
	if fieldTypes[f.Type].storage != storeInline {
		return nil, invalidValue("This field takes an upload, not a typed value.")
	}
	normalized, err := normalizeValue(f.Type, f.prefs(), raw)
	if err != nil {
		return nil, err
	}
	var orphaned []string
	err = h.tx(ctx, func(tx *sql.Tx) error {
		if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, needConsent); err != nil {
			return err
		}
		if emptyValue(normalized) {
			existing, err := queryValues(ctx, tx, `WHERE field_id=?`, f.ID)
			if err != nil || len(existing) == 0 {
				return err
			}
			orphaned, err = removeValue(ctx, tx, existing[0])
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sign_values(id,document_id,field_id,party_id,value_json,updated_at) VALUES(?,?,?,?,?,?)
			ON CONFLICT(field_id) DO UPDATE SET value_json=excluded.value_json,updated_at=excluded.updated_at`,
			uuid.NewString(), c.document.ID, f.ID, c.party.ID, encodeJSON(normalized), h.stampNow())
		return err
	})
	if err != nil {
		return nil, err
	}
	h.removeBlobs(c.document.OwnerID, orphaned)
	if emptyValue(normalized) {
		return nil, nil
	}
	return normalized, nil
}

func (h *Handler) saveFieldValue(r *http.Request, c signerContext, input body) (any, error) {
	ctx := r.Context()
	raw, present := input["value"]
	if !present {
		return nil, badRequest(`The body needs a "value".`)
	}
	if err := c.ready(); err != nil {
		return nil, err
	}
	f, err := h.writableField(ctx, c, r.PathValue("fieldId"))
	if err != nil {
		return nil, err
	}
	if fieldTypes[f.Type].storage == storeBlob {
		if raw != nil {
			return nil, invalidValue("This field takes an upload.")
		}
		var orphaned []string
		err = h.tx(ctx, func(tx *sql.Tx) error {
			if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, true); err != nil {
				return err
			}
			existing, err := queryValues(ctx, tx, `WHERE field_id=?`, f.ID)
			if err != nil || len(existing) == 0 {
				return err
			}
			orphaned, err = removeValue(ctx, tx, existing[0])
			return err
		})
		if err != nil {
			return nil, err
		}
		h.removeBlobs(c.document.OwnerID, orphaned)
		return map[string]any{"value": nil}, nil
	}
	stored, err := h.saveTypedValue(ctx, c, f, raw, true)
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": stored}, nil
}

// readUpload reads a raw request body of at most limit bytes.
func readUpload(r *http.Request, limit int64) ([]byte, error) {
	tooLarge := httpError(413, "This upload is too large.", "UPLOAD_TOO_LARGE")
	if r.ContentLength > limit {
		return nil, tooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, tooLarge
	}
	if len(data) == 0 {
		return nil, httpError(400, "The upload is empty.", "INVALID_UPLOAD")
	}
	return data, nil
}

// uploadPNG re-encodes an uploaded picture; metadata and anything that is not
// pixels is dropped.
func uploadPNG(r *http.Request, kind string) ([]byte, int, int, error) {
	data, err := readUpload(r, maxImageUpload)
	if err != nil {
		return nil, 0, 0, err
	}
	t := fieldTypes[kind]
	png, width, height, err := signpdf.PNG(r.Context(), data, t.maxW, t.maxH)
	if err != nil {
		return nil, 0, 0, httpError(400, "This image could not be read. Use a PNG or JPEG picture.", "INVALID_IMAGE")
	}
	return png, width, height, nil
}

func cleanFileName(value string) string {
	parts := strings.Split(strings.ReplaceAll(value, `\`, "/"), "/")
	name := truncate(singleLine(parts[len(parts)-1]), 120)
	if name == "" {
		return "attachment"
	}
	return name
}

var mimePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9!#$&^_.+-]{0,60}/[a-z0-9][a-z0-9!#$&^_.+-]{0,60}$`)

func cleanMIME(value string) string {
	media, _, err := mime.ParseMediaType(value)
	if err != nil {
		media = strings.ToLower(strings.TrimSpace(strings.Split(value, ";")[0]))
	}
	if !mimePattern.MatchString(media) {
		return "application/octet-stream"
	}
	return media
}

func (h *Handler) prepareUpload(r *http.Request, f field) ([]byte, map[string]any, error) {
	t := fieldTypes[f.Type]
	query := r.URL.Query()
	if t.upload == "file" {
		data, err := readUpload(r, maxFileUpload)
		if err != nil {
			return nil, nil, err
		}
		return data, map[string]any{"kind": "file", "name": cleanFileName(query.Get("name")), "mime": cleanMIME(r.Header.Get("Content-Type")), "size": len(data)}, nil
	}
	png, width, height, err := uploadPNG(r, f.Type)
	if err != nil {
		return nil, nil, err
	}
	meta := map[string]any{"kind": "image", "mime": "image/png", "width": width, "height": height}
	if f.Type != "signature" && f.Type != "initials" {
		return png, meta, nil
	}
	method := "upload"
	if query.Has("method") {
		method = query.Get("method")
	}
	if err := signatureMeta(f, method, query.Get("text"), meta); err != nil {
		return nil, nil, err
	}
	return png, meta, nil
}

// signatureMeta checks how a signature or initials came about against the
// ways the field allows and records it (and a typed signature's text) in meta.
func signatureMeta(f field, method, typed string, meta map[string]any) error {
	allowed := prefStrings(f.prefs(), "methods")
	if len(allowed) == 0 {
		allowed = signingMethods
	}
	if !contains(signingMethods, method) || !contains(allowed, method) {
		return httpError(400, "This way of signing is not allowed for this field.", "INVALID_UPLOAD")
	}
	meta["method"] = method
	if method == "type" {
		typed = singleLine(typed)
		if typed == "" || length(typed) > maxNameLength {
			return httpError(400, "Typed signatures need the typed text.", "INVALID_UPLOAD")
		}
		meta["text"] = typed
	}
	return nil
}

// saveBlobValue records a stored upload as the field's value, replacing an
// earlier one.
func (h *Handler) saveBlobValue(ctx context.Context, c signerContext, f field, blob storedBlob, meta map[string]any, needConsent bool) error {
	var orphaned []string
	err := h.tx(ctx, func(tx *sql.Tx) error {
		if err := assertWritable(ctx, tx, c.document.ID, c.party.ID, needConsent); err != nil {
			return err
		}
		var used int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(b.size),0) FROM sign_values v JOIN blobs b ON b.id=v.blob_ref WHERE v.document_id=? AND v.field_id!=?`, c.document.ID, f.ID).Scan(&used); err != nil {
			return err
		}
		if used+blob.size > maxDocumentBytes {
			return httpError(413, "This document has reached its limit for uploads.", "UPLOAD_LIMIT")
		}
		if err := h.checkRoom(ctx, tx, c.document.OwnerID, blob.size, "The sender has no storage left for this."); err != nil {
			return err
		}
		existing, err := queryValues(ctx, tx, `WHERE field_id=?`, f.ID)
		if err != nil {
			return err
		}
		if err := adoptBlob(ctx, tx, c.document.OwnerID, blob); err != nil {
			return err
		}
		if len(existing) > 0 {
			if orphaned, err = removeValue(ctx, tx, existing[0]); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sign_values(id,document_id,field_id,party_id,value_json,blob_ref,updated_at) VALUES(?,?,?,?,?,?,?)`,
			uuid.NewString(), c.document.ID, f.ID, c.party.ID, encodeJSON(meta), blob.id, h.stampNow())
		return err
	})
	if err != nil {
		return err
	}
	h.removeBlobs(c.document.OwnerID, orphaned)
	return nil
}

// uploadFieldValue takes the raw request body as a signature image or an
// attachment.
func (h *Handler) uploadFieldValue(w http.ResponseWriter, r *http.Request, c signerContext) {
	meta, err := func() (map[string]any, error) {
		ctx := r.Context()
		if err := c.ready(); err != nil {
			return nil, err
		}
		f, err := h.writableField(ctx, c, r.PathValue("fieldId"))
		if err != nil {
			return nil, err
		}
		if fieldTypes[f.Type].upload == "" {
			return nil, httpError(400, "This field takes a typed value, not an upload.", "INVALID_UPLOAD")
		}
		// Nothing is read or stored for a party that has not agreed yet.
		if err := assertWritable(ctx, h.db, c.document.ID, c.party.ID, true); err != nil {
			return nil, err
		}
		data, meta, err := h.prepareUpload(r, f)
		if err != nil {
			return nil, err
		}
		if err := h.checkRoom(ctx, h.db, c.document.OwnerID, int64(len(data)), "The sender has no storage left for this."); err != nil {
			return nil, err
		}
		blob, err := h.storeBlob(ctx, c.document.OwnerID, data)
		if err != nil {
			return nil, err
		}
		meta["sha256"] = blob.sha256
		if err := h.saveBlobValue(ctx, c, f, blob, meta, true); err != nil {
			blob.discard()
			return nil, err
		}
		return meta, nil
	}()
	respond(w, 201, map[string]any{"value": meta}, err)
}
