package signing

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"
)

const (
	statusDraft      = "draft"
	statusSent       = "sent"
	statusInProgress = "in_progress"
	statusFinalizing = "finalizing"
	statusCompleted  = "completed"
	statusDeclined   = "declined"
	statusExpired    = "expired"
	statusCanceled   = "canceled"

	partyPending   = "pending"
	partyWaiting   = "waiting"
	partyOpened    = "opened"
	partyCompleted = "completed"
	partyDeclined  = "declined"
)

const (
	maxSourceBytes   = 50 << 20
	maxPages         = 300
	maxParties       = 20
	maxFields        = 500
	maxAreas         = 20
	maxTitleLength   = 200
	maxNameLength    = 100
	maxEmailLength   = 254
	maxDeclineReason = 500
	maxMailMessage   = 1000
	maxSignerName    = 120
	maxImageUpload   = 8 << 20
	maxFileUpload    = 10 << 20
	maxDocumentBytes = 50 << 20

	defaultValidity = 30 * 24 * time.Hour
	maxValidityDays = 365

	// signPagePath is the web page of a signer; ownerPagePath that of a
	// document's owner. The last path segment is the token or document id.
	signPagePath  = "/app/sign/"
	ownerPagePath = "/app/signing/"
)

var partyColors = []string{"#D4A017", "#2E86AB", "#C0392B", "#27AE60", "#8E44AD", "#E67E22", "#16A085", "#D81B60", "#5C6BC0", "#7F8C8D"}

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
	colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func isLive(status string) bool { return status == statusSent || status == statusInProgress }

// querier is a database or a transaction. The database has one connection, so
// everything inside a transaction must go through it.
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (h *Handler) tx(ctx context.Context, work func(tx *sql.Tx) error) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = work(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// stamp formats a time like JavaScript's toISOString, so stored times compare
// as text with those of existing databases.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func (h *Handler) stampNow() string { return stamp(h.now()) }

func parseStamp(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil
}

// text is a nullable column.
type text = sql.NullString

func nullable(v text) any {
	if v.Valid {
		return v.String
	}
	return nil
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func flag(v bool) int {
	if v {
		return 1
	}
	return 0
}

const invisibleRunes = "\u200b\u200c\u200d\u200e\u200f\u2028\u2029\u202a\u202b\u202c\u202d\u202e\u2060\u2061\u2062\u2063\u2064\u2065\u2066\u2067\u2068\u2069\ufeff"

// withoutInvisible drops direction overrides, separators and zero-width
// characters, which can make a name read as something else.
func withoutInvisible(value string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(invisibleRunes, r) {
			return -1
		}
		return r
	}, value)
}

var repeatedSpaces = regexp.MustCompile(` {2,}`)

// singleLine also turns every control character, line breaks included, into a
// space.
func singleLine(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f && r <= 0x9f {
			return ' '
		}
		return r
	}, withoutInvisible(value))
	return strings.TrimSpace(repeatedSpaces.ReplaceAllString(value, " "))
}

// plainText keeps line breaks and tabs and drops other control characters.
func plainText(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(withoutInvisible(value), "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, value)
}

// length counts UTF-16 units, as the limits were defined.
func length(value string) int { return len(utf16.Encode([]rune(value))) }

func truncate(value string, max int) string {
	units := 0
	for i, r := range value {
		units += utf16.RuneLen(r)
		if units > max {
			return value[:i]
		}
	}
	return value
}

func decodeJSON(raw text, fallback any) any {
	if !raw.Valid {
		return fallback
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw.String))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return fallback
	}
	return plainNumbers(value)
}

func encodeJSON(value any) string {
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(out.String(), "\n")
}

type document struct {
	ID, Title, Status, SigningOrder, Locale      string
	OwnerID                                      int64
	SourceNodeID, SourceName, SourceParentID     text
	ExpiresAt, FinalNodeID, FinalSHA256, Message text
	SnapshotBlobID, SnapshotSHA256               string
	SnapshotSize                                 int64
	PageCount, ReminderDays, DeliverEmail        int
	PagesJSON                                    string
	ConsentVersion                               sql.NullInt64
	FinalizeAttempts                             int
	FinalizeError, FinalizeNextAt                text
	CreatedAt, UpdatedAt                         string
	SentAt, CompletedAt, CanceledAt              text
	CleanBlobID, CleanSHA256                     text
	CleanSize                                    sql.NullInt64
}

const documentColumns = `id,owner_id,title,status,signing_order,locale,source_node_id,source_name,source_parent_id,expires_at,final_node_id,final_sha256,message,
	snapshot_blob_id,snapshot_sha256,snapshot_size,page_count,COALESCE(reminder_days,0),COALESCE(deliver_email,0),pages_json,consent_text_version,
	COALESCE(finalize_attempts,0),finalize_error,finalize_next_at,created_at,updated_at,sent_at,completed_at,canceled_at,clean_blob_id,clean_sha256,clean_size`

type scanner interface{ Scan(...any) error }

func scanDocument(row scanner) (document, error) {
	var d document
	err := row.Scan(&d.ID, &d.OwnerID, &d.Title, &d.Status, &d.SigningOrder, &d.Locale, &d.SourceNodeID, &d.SourceName, &d.SourceParentID, &d.ExpiresAt, &d.FinalNodeID, &d.FinalSHA256, &d.Message,
		&d.SnapshotBlobID, &d.SnapshotSHA256, &d.SnapshotSize, &d.PageCount, &d.ReminderDays, &d.DeliverEmail, &d.PagesJSON, &d.ConsentVersion,
		&d.FinalizeAttempts, &d.FinalizeError, &d.FinalizeNextAt, &d.CreatedAt, &d.UpdatedAt, &d.SentAt, &d.CompletedAt, &d.CanceledAt, &d.CleanBlobID, &d.CleanSHA256, &d.CleanSize)
	return d, err
}

func documentByID(ctx context.Context, q querier, id string) (document, error) {
	return scanDocument(q.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM sign_documents WHERE id=?`, id))
}

func documentNotFound() *apiError { return httpError(404, "Document not found", "NOT_FOUND") }

// ownedDocument answers another account's id, a malformed one and a missing
// one the same way.
func ownedDocument(ctx context.Context, q querier, userID int64, id string) (document, error) {
	if !uuidPattern.MatchString(id) {
		return document{}, documentNotFound()
	}
	d, err := scanDocument(q.QueryRowContext(ctx, `SELECT `+documentColumns+` FROM sign_documents WHERE id=? AND owner_id=?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return document{}, documentNotFound()
	}
	return d, err
}

func queryDocuments(ctx context.Context, q querier, where string, args ...any) ([]document, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+documentColumns+` FROM sign_documents `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

type party struct {
	ID, DocumentID, Name, Color, Status                    string
	Position, IsSelf                                       int
	Email, TokenHash, TokenHint, TokenEnc, SignerName      text
	OpenedAt, ConsentedAt, CompletedAt, DeclinedAt, Reason text
	IP, UserAgent, Timezone                                text
	TZOffset, UserID                                       sql.NullInt64
	CreatedAt                                              string
}

const partyColumns = `id,document_id,name,color,status,position,COALESCE(is_self,0),email,token_hash,token_hint,token_enc,signer_name,
	opened_at,consented_at,completed_at,declined_at,decline_reason,ip,user_agent,timezone,tz_offset_minutes,user_id,created_at`

func scanParty(row scanner) (party, error) {
	var p party
	err := row.Scan(&p.ID, &p.DocumentID, &p.Name, &p.Color, &p.Status, &p.Position, &p.IsSelf, &p.Email, &p.TokenHash, &p.TokenHint, &p.TokenEnc, &p.SignerName,
		&p.OpenedAt, &p.ConsentedAt, &p.CompletedAt, &p.DeclinedAt, &p.Reason, &p.IP, &p.UserAgent, &p.Timezone, &p.TZOffset, &p.UserID, &p.CreatedAt)
	return p, err
}

func queryParties(ctx context.Context, q querier, where string, args ...any) ([]party, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+partyColumns+` FROM sign_parties `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []party
	for rows.Next() {
		p, err := scanParty(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func listParties(ctx context.Context, q querier, documentID string) ([]party, error) {
	return queryParties(ctx, q, `WHERE document_id=? ORDER BY position`, documentID)
}

func partyByID(ctx context.Context, q querier, id string) (party, error) {
	return scanParty(q.QueryRowContext(ctx, `SELECT `+partyColumns+` FROM sign_parties WHERE id=?`, id))
}

// documentParty is a party of the document, or 404 as for a missing one.
func documentParty(ctx context.Context, q querier, documentID, partyID string) (party, error) {
	if uuidPattern.MatchString(partyID) {
		p, err := partyByID(ctx, q, partyID)
		if err == nil && p.DocumentID == documentID {
			return p, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return party{}, err
		}
	}
	return party{}, httpError(404, "Party not found", "NOT_FOUND")
}

type field struct {
	ID, DocumentID, PartyID, Type, Name string
	Required, Readonly                  bool
	Default                             text
	PrefsJSON, AreasJSON                string
	Position                            int
}

func (f field) prefs() map[string]any {
	m, _ := decodeJSON(text{String: f.PrefsJSON, Valid: true}, nil).(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func queryFields(ctx context.Context, q querier, where string, args ...any) ([]field, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,document_id,party_id,type,name,required,readonly,default_value,prefs_json,areas_json,position FROM sign_fields `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []field
	for rows.Next() {
		var f field
		if err = rows.Scan(&f.ID, &f.DocumentID, &f.PartyID, &f.Type, &f.Name, &f.Required, &f.Readonly, &f.Default, &f.PrefsJSON, &f.AreasJSON, &f.Position); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func listFields(ctx context.Context, q querier, documentID string) ([]field, error) {
	return queryFields(ctx, q, `WHERE document_id=? ORDER BY position`, documentID)
}

type value struct {
	ID, DocumentID, FieldID, PartyID string
	ValueJSON, BlobRef               text
	UpdatedAt                        string
}

// decoded is the value as clients see it: the JSON value or an upload's
// description.
func (v value) decoded() any { return decodeJSON(v.ValueJSON, nil) }

func (v value) isImage() bool {
	m, _ := v.decoded().(map[string]any)
	return m != nil && m["kind"] == "image" && v.BlobRef.Valid
}

func queryValues(ctx context.Context, q querier, where string, args ...any) ([]value, error) {
	rows, err := q.QueryContext(ctx, `SELECT id,document_id,field_id,party_id,value_json,blob_ref,updated_at FROM sign_values `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []value
	for rows.Next() {
		var v value
		var partyID text
		if err = rows.Scan(&v.ID, &v.DocumentID, &v.FieldID, &partyID, &v.ValueJSON, &v.BlobRef, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.PartyID = partyID.String
		out = append(out, v)
	}
	return out, rows.Err()
}

func listValues(ctx context.Context, q querier, documentID string) ([]value, error) {
	return queryValues(ctx, q, `WHERE document_id=?`, documentID)
}

// touch bumps a document's updated_at.
func touch(ctx context.Context, q querier, documentID, at string) error {
	_, err := q.ExecContext(ctx, `UPDATE sign_documents SET updated_at=? WHERE id=?`, at, documentID)
	return err
}

// Rows as the API shows them. Tokens and token hashes never appear here.

func documentView(d document) map[string]any {
	var consent any
	if d.ConsentVersion.Valid {
		consent = d.ConsentVersion.Int64
	}
	var finalizeError any
	if d.Status == statusFinalizing {
		finalizeError = nullable(d.FinalizeError)
	}
	return map[string]any{
		"id": d.ID, "title": d.Title, "status": d.Status, "signingOrder": d.SigningOrder, "locale": d.Locale,
		"expiresAt": nullable(d.ExpiresAt), "pageCount": d.PageCount, "pages": decodeJSON(text{String: d.PagesJSON, Valid: true}, []any{}),
		"sourceNodeId": nullable(d.SourceNodeID), "sourceName": nullable(d.SourceName), "finalNodeId": nullable(d.FinalNodeID),
		"snapshotSha256": d.SnapshotSHA256, "finalSha256": nullable(d.FinalSHA256), "consentVersion": consent,
		"message": nullable(d.Message), "reminderDays": d.ReminderDays, "finalizeError": finalizeError,
		"createdAt": d.CreatedAt, "updatedAt": d.UpdatedAt, "sentAt": nullable(d.SentAt), "completedAt": nullable(d.CompletedAt),
		"canceledAt": nullable(d.CanceledAt),
	}
}

func partyView(p party) map[string]any {
	var hint any
	if p.TokenHash.Valid {
		hint = nullable(p.TokenHint)
	}
	return map[string]any{
		"id": p.ID, "position": p.Position, "name": p.Name, "color": p.Color, "email": nullable(p.Email), "isSelf": p.IsSelf == 1,
		"status": p.Status, "hasLink": p.TokenHash.Valid, "linkHint": hint, "openedAt": nullable(p.OpenedAt),
		"consentedAt": nullable(p.ConsentedAt), "completedAt": nullable(p.CompletedAt), "declinedAt": nullable(p.DeclinedAt),
		"declineReason": nullable(p.Reason), "signerName": nullable(p.SignerName), "createdAt": p.CreatedAt,
	}
}

func fieldView(f field) map[string]any {
	return map[string]any{
		"id": f.ID, "partyId": f.PartyID, "type": f.Type, "name": f.Name, "required": f.Required, "readonly": f.Readonly,
		"defaultValue": decodeJSON(f.Default, nil), "prefs": f.prefs(),
		"areas": decodeJSON(text{String: f.AreasJSON, Valid: true}, []any{}), "position": f.Position,
	}
}

func fieldViews(fields []field) []map[string]any {
	out := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		out = append(out, fieldView(f))
	}
	return out
}

func decodeList(raw string) []any {
	list, _ := decodeJSON(text{String: raw, Valid: true}, nil).([]any)
	return list
}

func base64Std(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func fromBase64Std(value string) ([]byte, error) { return base64.StdEncoding.DecodeString(value) }
