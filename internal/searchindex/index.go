// Package searchindex stores a NeoCloud-compatible encrypted text cache and
// FTS5 token index. The index contains searchable terms only; plaintext used
// for result snippets remains AES-GCM encrypted with the owner's data key.
package searchindex

import (
	"bytes"
	"compress/flate"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/ocr"
)

const (
	MaxTextBytes    = 1 << 20
	MaxSourceText   = 10 << 20
	MaxPDFSource    = 100 << 20
	MaxOfficeSource = 100 << 20
	MaxFilesPerPass = 25
	JournalPageSize = 500

	extractTimeout = 60 * time.Second
)

var ErrNotIndexable = errors.New("file type is not supported by the text index")

type Node struct {
	ID     string
	Name   string
	MIME   string
	Size   int64
	BlobID string
}
type BlobReader func(context.Context, int64, Node) ([]byte, error)
type Extractor func(context.Context, Node, []byte) (string, error)
type ContentMatch struct {
	NodeID  string
	Snippet string
}
type ContentResults struct {
	Snippets map[string]string
	Rows     []ContentMatch
}
type Indexer struct {
	db       *sql.DB
	keys     *cryptox.Keyring
	readBlob BlobReader
	extract  Extractor
	enabled  func() bool
	mu       sync.Mutex
	// idleAt is the work mark at which a pass last found nothing to do.
	idleAt string
}

// Status is what the administrator is shown about the index.
type Status struct {
	Enabled bool  `json:"enabled"`
	Indexed int64 `json:"indexed"`
	Pending int64 `json:"pending"`
	Failed  int64 `json:"failed"`
	Bytes   int64 `json:"bytes"`
	// Available names the extractors: pdftotext is optional, Office and
	// OpenDocument text comes from the built-in zip reader, and OCR reads
	// pictures and scanned PDF pages when tesseract is installed.
	Available struct {
		Pdftotext bool       `json:"pdftotext"`
		Office    bool       `json:"office"`
		OCR       ocr.Status `json:"ocr"`
	} `json:"available"`
}

func New(db *sql.DB, keys *cryptox.Keyring, read BlobReader, extract Extractor, enabled func() bool) (*Indexer, error) {
	if db == nil || keys == nil || read == nil || extract == nil {
		return nil, errors.New("searchindex: database, keyring, blob reader, and extractor are required")
	}
	if enabled == nil {
		enabled = func() bool { return false }
	}
	return &Indexer{db: db, keys: keys, readBlob: read, extract: extract, enabled: enabled}, nil
}

func textAAD(nodeID string) []byte { return []byte("search-text:" + nodeID) }
func (i *Indexer) encryptText(ctx context.Context, userID int64, nodeID, text string) ([]byte, error) {
	dek, err := i.keys.GetUserDEK(ctx, i.db, userID)
	if err != nil {
		return nil, err
	}
	var compressed bytes.Buffer
	writer, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(writer, text); err != nil {
		writer.Close()
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return cryptox.SealBytes(dek, compressed.Bytes(), textAAD(nodeID))
}
func (i *Indexer) decryptText(ctx context.Context, userID int64, nodeID string, sealed []byte) (string, error) {
	dek, err := i.keys.GetUserDEK(ctx, i.db, userID)
	if err != nil {
		return "", err
	}
	compressed, err := cryptox.OpenBytes(dek, sealed, textAAD(nodeID))
	if err != nil {
		return "", err
	}
	reader := flate.NewReader(bytes.NewReader(compressed))
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, MaxTextBytes+1))
	if err != nil {
		return "", err
	}
	if len(raw) > MaxTextBytes {
		return "", errors.New("searchindex: encrypted text exceeds size limit")
	}
	return string(raw), nil
}

func (i *Indexer) SaveIndexed(ctx context.Context, userID int64, node Node, text string) error {
	text = capText(text)
	var encrypted any
	if text != "" {
		value, err := i.encryptText(ctx, userID, node.ID, text)
		if err != nil {
			return err
		}
		encrypted = value
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, node.ID, userID).Scan(&current); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	} else if current != node.BlobID {
		return nil
	}
	var docID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM search_docs WHERE node_id=?`, node.ID).Scan(&docID)
	if err == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE search_docs SET blob_id=?,status='indexed',text_enc=?,text_bytes=?,indexed_at=datetime('now') WHERE id=?`, node.BlobID, encrypted, len([]byte(text)), docID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid=?`, docID); err != nil {
			return err
		}
	} else if err == sql.ErrNoRows {
		result, e := tx.ExecContext(ctx, `INSERT INTO search_docs(user_id,node_id,blob_id,status,text_enc,text_bytes) VALUES(?,?,?,'indexed',?,?)`, userID, node.ID, node.BlobID, encrypted, len([]byte(text)))
		if e != nil {
			return e
		}
		docID, e = result.LastInsertId()
		if e != nil {
			return e
		}
	} else {
		return err
	}
	if text != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO search_fts(rowid,body) VALUES(?,?)`, docID, text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// IsEnabled reports whether the operator has enabled the background text
// index. Per-account opt-outs are still checked by indexing and search calls.
func (i *Indexer) IsEnabled() bool { return i.enabled() }

func (i *Indexer) SaveFailed(ctx context.Context, userID int64, node Node) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT blob_id FROM nodes WHERE id=? AND user_id=?`, node.ID, userID).Scan(&current); err == sql.ErrNoRows {
		return nil
	} else if err != nil {
		return err
	} else if current != node.BlobID {
		return nil
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM search_docs WHERE node_id=?`, node.ID).Scan(&id)
	if err == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE search_docs SET blob_id=?,status='failed',text_enc=NULL,text_bytes=0,indexed_at=datetime('now') WHERE id=?`, node.BlobID, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM search_fts WHERE rowid=?`, id); err != nil {
			return err
		}
	} else if err == sql.ErrNoRows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO search_docs(user_id,node_id,blob_id,status,text_enc,text_bytes) VALUES(?,?,?,'failed',NULL,0)`, userID, node.ID, node.BlobID); err != nil {
			return err
		}
	} else {
		return err
	}
	return tx.Commit()
}

func (i *Indexer) ReadIndexedText(ctx context.Context, userID int64, node Node) (string, string, error) {
	var status string
	var sealed []byte
	err := i.db.QueryRowContext(ctx, `SELECT status,text_enc FROM search_docs WHERE node_id=? AND user_id=? AND blob_id=?`, node.ID, userID, node.BlobID).Scan(&status, &sealed)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if status != "indexed" || len(sealed) == 0 {
		return status, "", nil
	}
	text, err := i.decryptText(ctx, userID, node.ID, sealed)
	return status, text, err
}
func (i *Indexer) RemoveNode(ctx context.Context, nodeID string) error {
	_, err := i.db.ExecContext(ctx, `DELETE FROM search_docs WHERE node_id=?`, nodeID)
	return err
}
func (i *Indexer) RemoveSubtree(ctx context.Context, userID int64, nodeID string) error {
	_, err := i.db.ExecContext(ctx, `WITH RECURSIVE down(id) AS (SELECT id FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id FROM nodes n JOIN down d ON n.parent_id=d.id WHERE n.user_id=?) DELETE FROM search_docs WHERE node_id IN (SELECT id FROM down)`, nodeID, userID, userID)
	return err
}
func (i *Indexer) RemoveOptedOut(ctx context.Context) (int64, error) {
	res, err := i.db.ExecContext(ctx, `DELETE FROM search_docs WHERE user_id IN (SELECT user_id FROM user_settings WHERE key='fullTextSearch' AND value='false')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// clear empties the index; the indexer then reads every file again.
func (i *Indexer) clear(ctx context.Context) error {
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO search_fts(search_fts) VALUES('delete-all')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM search_docs`); err != nil {
		return err
	}
	return tx.Commit()
}

// Rebuild throws the index away, after a running pass so that its results
// cannot land after the wipe.
func (i *Indexer) Rebuild(ctx context.Context) (Status, error) {
	i.mu.Lock()
	err := i.clear(ctx)
	if err == nil {
		var seq int64
		if seq, err = i.latestSeq(ctx); err == nil {
			err = i.writeState(ctx, "cursor", strconv.FormatInt(seq, 10))
		}
	}
	i.idleAt = ""
	i.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	return i.Status(ctx)
}

// Status counts the index and the files still to be read.
func (i *Indexer) Status(ctx context.Context) (Status, error) {
	status := Status{Enabled: i.enabled()}
	status.Available.Pdftotext, status.Available.Office, status.Available.OCR = pdftotextPath() != "", true, ocr.CurrentStatus()
	err := i.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM search_docs WHERE status='indexed'),(SELECT COUNT(*) FROM search_docs WHERE status='failed'),(SELECT COALESCE(SUM(length(text_enc)),0) FROM search_docs)+(SELECT COALESCE(SUM(length(block)),0) FROM search_fts_data)+(SELECT COALESCE(SUM(length(sz)),0) FROM search_fts_docsize)`).Scan(&status.Indexed, &status.Failed, &status.Bytes)
	if err != nil {
		return Status{}, err
	}
	users, err := i.indexedUsers(ctx)
	if err != nil {
		return Status{}, err
	}
	for _, userID := range users {
		cte, args := pendingQuery(userID)
		var count int64
		if err = i.db.QueryRowContext(ctx, cte+` SELECT COUNT(*)`+pendingFrom, args...).Scan(&count); err != nil {
			return Status{}, err
		}
		status.Pending += count
	}
	return status, nil
}

func (i *Indexer) indexedUsers(ctx context.Context) ([]int64, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT id FROM users WHERE id NOT IN(SELECT user_id FROM user_settings WHERE key='fullTextSearch' AND value='false') ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		users = append(users, id)
	}
	return users, rows.Err()
}

// workMark is what the files still to be read depend on: the journal position
// and who opted out.
func (i *Indexer) workMark(ctx context.Context) (string, error) {
	var mark string
	err := i.db.QueryRowContext(ctx, `SELECT (SELECT COALESCE(MAX(seq),0) FROM changes) || '|' ||
		COALESCE((SELECT group_concat(user_id) FROM user_settings WHERE key='fullTextSearch' AND value='false'),'')`).Scan(&mark)
	return mark, err
}

func (i *Indexer) readState(ctx context.Context, key string) (string, error) {
	var value string
	err := i.db.QueryRowContext(ctx, `SELECT value FROM search_index_state WHERE key=?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
func (i *Indexer) writeState(ctx context.Context, key, value string) error {
	_, err := i.db.ExecContext(ctx, `INSERT INTO search_index_state(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// RunPass processes one bounded journal/indexing pass. Extraction and blob
// decryption are injected by the runtime so hostile document parsing can be
// isolated or time-boxed by the application-specific extractor.
func (i *Indexer) RunPass(ctx context.Context, maxFiles int) (int, error) {
	if !i.enabled() {
		return 0, nil
	}
	if maxFiles < 1 {
		maxFiles = MaxFilesPerPass
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	mark, err := i.workMark(ctx)
	if err != nil || mark == i.idleAt {
		return 0, err
	}
	if _, err = i.RemoveOptedOut(ctx); err != nil {
		return 0, err
	}
	if err = i.followJournal(ctx); err != nil {
		return 0, err
	}
	users, err := i.indexedUsers(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, userID := range users {
		pending, err := i.pendingFiles(ctx, userID, maxFiles-done)
		if err != nil {
			return done, err
		}
		for _, node := range pending {
			if err = i.indexFile(ctx, userID, node); err != nil {
				return done, err
			}
			done++
		}
		if done >= maxFiles {
			break
		}
	}
	if done == 0 {
		i.idleAt = mark
	}
	return done, nil
}

// indexFile records the text of one file, or that it could not be read, so
// that it is not tried again until it changes.
func (i *Indexer) indexFile(ctx context.Context, userID int64, node Node) error {
	text, reused, err := i.sharedText(ctx, userID, node)
	if err != nil {
		return err
	}
	if reused {
		return i.SaveIndexed(ctx, userID, node, text)
	}
	err = checkSourceSize(node)
	if err == nil {
		var plain []byte
		if plain, err = i.readBlob(ctx, userID, node); err == nil {
			text, err = i.extractWithin(ctx, node, plain)
		}
	}
	// A shutdown says nothing about the file.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		slog.Warn("could not read a file for the search index", "node_id", node.ID, "error", err)
		return i.SaveFailed(ctx, userID, node)
	}
	return i.SaveIndexed(ctx, userID, node, text)
}

// sharedText is the text already read from the same content under another
// of the account's files (a copy shares its blob), so that a picture or a
// scan is not recognised twice. It counts only when both files are read the
// same way.
func (i *Indexer) sharedText(ctx context.Context, userID int64, node Node) (string, bool, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT d.node_id,d.text_enc,n.name,COALESCE(n.mime,'') FROM search_docs d JOIN nodes n ON n.id=d.node_id AND n.blob_id=d.blob_id
		WHERE d.user_id=? AND d.blob_id=? AND d.node_id<>? AND d.status='indexed' LIMIT 8`, userID, node.BlobID, node.ID)
	if err != nil {
		return "", false, err
	}
	kind := extractionKind(node.Name, node.MIME)
	var same []textRow
	for rows.Next() {
		var other textRow
		var name, mime string
		if err = rows.Scan(&other.nodeID, &other.sealed, &name, &mime); err != nil {
			rows.Close()
			return "", false, err
		}
		if extractionKind(name, mime) == kind {
			same = append(same, other)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", false, err
	}
	for _, other := range same {
		if len(other.sealed) == 0 {
			return "", true, nil
		}
		// An entry that does not open is read again from the file.
		if text, err := i.decryptText(ctx, userID, other.nodeID, other.sealed); err == nil {
			return text, true, nil
		}
	}
	return "", false, nil
}

// extractWithin time-boxes the extractor and keeps a failing parser from
// taking the server down: the files are untrusted. A parser that ignores its
// context is left behind.
func (i *Indexer) extractWithin(ctx context.Context, node Node, plain []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, extractTimeout)
	defer cancel()
	type outcome struct {
		text string
		err  error
	}
	result := make(chan outcome, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result <- outcome{err: fmt.Errorf("searchindex: extractor failed: %v", recovered)}
			}
		}()
		text, err := i.extract(ctx, node, plain)
		result <- outcome{text, err}
	}()
	select {
	case found := <-result:
		return found.text, found.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

const pendingFrom = ` FROM nodes n WHERE n.user_id=? AND n.type='file' AND n.vault_id IS NULL AND n.trashed_at IS NULL AND n.blob_id IS NOT NULL AND n.id NOT IN(SELECT id FROM hidden) AND (n.mime LIKE 'text/%' OR lower(n.mime)='application/pdf' OR lower(n.mime) IN(SELECT value FROM json_each(?)) OR EXISTS(SELECT 1 FROM json_each(?) e WHERE lower(n.name) LIKE '%' || e.value)) AND NOT EXISTS(SELECT 1 FROM search_docs d WHERE d.node_id=n.id AND d.blob_id=n.blob_id)`

// pendingQuery is the CTE and arguments of the files without a current entry.
func pendingQuery(userID int64) (string, []any) {
	mimes, extensions := []string{}, indexableExtensions
	if ocr.Available() {
		mimes, extensions = pictureMIMEs, append(slices.Clone(indexableExtensions), pictureExtensions...)
	}
	mimeList, _ := json.Marshal(mimes)
	extensionList, _ := json.Marshal(extensions)
	return `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?)`,
		[]any{userID, userID, userID, string(mimeList), string(extensionList)}
}

func (i *Indexer) pendingFiles(ctx context.Context, userID int64, limit int) ([]Node, error) {
	if limit <= 0 {
		return nil, nil
	}
	cte, args := pendingQuery(userID)
	rows, err := i.db.QueryContext(ctx, cte+` SELECT n.id,n.name,COALESCE(n.mime,''),n.size,n.blob_id`+pendingFrom+` ORDER BY n.modified_at DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Node{}
	for rows.Next() {
		var n Node
		if err = rows.Scan(&n.ID, &n.Name, &n.MIME, &n.Size, &n.BlobID); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (i *Indexer) followJournal(ctx context.Context) error {
	saved, err := i.readState(ctx, "cursor")
	if err != nil {
		return err
	}
	if saved == "" {
		var seq int64
		seq, err = i.latestSeq(ctx)
		if err != nil {
			return err
		}
		if err = i.writeState(ctx, "cursor", fmt.Sprint(seq)); err != nil {
			return err
		}
		return nil
	}
	cursor, err := strconv.ParseInt(saved, 10, 64)
	if err != nil {
		return err
	}
	for {
		rows, err := i.db.QueryContext(ctx, `SELECT seq,user_id,node_id,op FROM changes WHERE seq>? ORDER BY seq LIMIT ?`, cursor, JournalPageSize)
		if err != nil {
			return err
		}
		type change struct {
			seq, userID int64
			nodeID, op  string
		}
		changes := []change{}
		for rows.Next() {
			var c change
			if err = rows.Scan(&c.seq, &c.userID, &c.nodeID, &c.op); err != nil {
				rows.Close()
				return err
			}
			changes = append(changes, c)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(changes) == 0 {
			return nil
		}
		for _, c := range changes {
			if c.op != "trash" && c.op != "rename" && c.op != "move" {
				continue
			}
			if err = i.applyChange(ctx, c.userID, c.nodeID); err != nil {
				return err
			}
		}
		cursor = changes[len(changes)-1].seq
		if err = i.writeState(ctx, "cursor", fmt.Sprint(cursor)); err != nil {
			return err
		}
		if len(changes) < JournalPageSize {
			return nil
		}
	}
}
func (i *Indexer) latestSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := i.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM changes`).Scan(&seq)
	return seq, err
}
func (i *Indexer) applyChange(ctx context.Context, userID int64, nodeID string) error {
	rows, err := i.db.QueryContext(ctx, `WITH RECURSIVE up(id,parent_id,trashed_at,depth) AS(SELECT id,parent_id,trashed_at,0 FROM nodes WHERE id=? AND user_id=? UNION ALL SELECT n.id,n.parent_id,n.trashed_at,up.depth+1 FROM nodes n JOIN up ON n.id=up.parent_id) SELECT trashed_at FROM up`, nodeID, userID)
	if err != nil {
		return err
	}
	found, hidden := false, false
	for rows.Next() {
		found = true
		var t sql.NullString
		if err = rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		if t.Valid {
			hidden = true
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if !found {
		return i.RemoveNode(ctx, nodeID)
	}
	var kind string
	var vault sql.NullString
	err = i.db.QueryRowContext(ctx, `SELECT type,vault_id FROM nodes WHERE id=? AND user_id=?`, nodeID, userID).Scan(&kind, &vault)
	if err != nil {
		return err
	}
	if hidden || vault.Valid {
		return i.RemoveSubtree(ctx, userID, nodeID)
	}
	if kind == "file" {
		var name, mime string
		if err = i.db.QueryRowContext(ctx, `SELECT name,COALESCE(mime,'') FROM nodes WHERE id=?`, nodeID).Scan(&name, &mime); err != nil {
			return err
		}
		if !isIndexable(name, mime) {
			return i.RemoveNode(ctx, nodeID)
		}
	}
	return nil
}

func isIndexable(name, mime string) bool {
	m := strings.ToLower(mime)
	if m == "application/pdf" || strings.HasPrefix(m, "text/") {
		return true
	}
	if isPicture(name, m) {
		return ocr.Available()
	}
	return slices.Contains(indexableExtensions, strings.ToLower(filepath.Ext(name)))
}

// indexableExtensions are the file name extensions the index reads, besides
// pictures while text recognition is available.
var indexableExtensions = []string{".pdf", ".html", ".htm", ".xhtml", ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp", ".txt", ".text", ".md", ".markdown", ".rst", ".adoc", ".org", ".tex", ".log", ".csv", ".tsv", ".json", ".jsonl", ".ndjson", ".xml", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".properties", ".srt", ".vtt", ".ics", ".vcf", ".eml", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".vue", ".svelte", ".css", ".scss", ".sass", ".less", ".py", ".rb", ".go", ".rs", ".java", ".kt", ".kts", ".swift", ".dart", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".php", ".pl", ".lua", ".r", ".scala", ".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".sql", ".gradle", ".cmake", ".mk"}

func capText(text string) string {
	text = strings.ReplaceAll(text, "\x00", " ")
	raw := []byte(text)
	if len(raw) <= MaxTextBytes {
		return text
	}
	raw = raw[:MaxTextBytes]
	for len(raw) > 0 && !utf8.Valid(raw) {
		raw = raw[:len(raw)-1]
	}
	return string(raw)
}

func (i *Indexer) ContentSearch(ctx context.Context, userID int64, terms []Term, nameIDs []string, limit int) (ContentResults, error) {
	return i.ContentSearchScoped(ctx, userID, terms, nameIDs, ContentScope{}, limit)
}

// ContentScope constrains indexed hits to the same server-side filters used
// by the file-name search route. CTE must be assembled from trusted server
// constants; dynamic values belong in CTEArgs and Args.
type ContentScope struct {
	CTE     string
	CTEArgs []any
	Where   string
	Args    []any
}

func (i *Indexer) ContentSearchScoped(ctx context.Context, userID int64, terms []Term, nameIDs []string, scope ContentScope, limit int) (ContentResults, error) {
	result := ContentResults{Snippets: map[string]string{}, Rows: []ContentMatch{}}
	if !i.enabled() {
		return result, nil
	}
	var optedOut bool
	if err := i.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_settings WHERE user_id=? AND key='fullTextSearch' AND value='false')`, userID).Scan(&optedOut); err != nil {
		return result, err
	}
	if optedOut {
		return result, nil
	}
	match := MatchExpression(terms)
	if match == "" || limit <= 0 {
		return result, nil
	}
	if nameIDs == nil {
		nameIDs = []string{}
	}
	excluded, err := json.Marshal(nameIDs)
	if err != nil {
		return result, err
	}
	nameDocs := []textRow{}
	cte := `WITH RECURSIVE hidden(id) AS (SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL UNION ALL SELECT n.id FROM nodes n JOIN hidden h ON n.parent_id=h.id WHERE n.user_id=?)`
	if strings.TrimSpace(scope.CTE) != "" {
		cte += `, ` + scope.CTE
	}
	filter := ""
	if strings.TrimSpace(scope.Where) != "" {
		filter = ` AND (` + scope.Where + `)`
	}
	cteArgs := append([]any{userID, userID}, scope.CTEArgs...)
	if len(nameIDs) > 0 {
		args := append(append([]any{}, cteArgs...), match, userID, string(excluded))
		args = append(args, scope.Args...)
		rows, err := i.db.QueryContext(ctx, cte+` SELECT d.node_id,d.text_enc FROM search_fts JOIN search_docs d ON d.id=search_fts.rowid JOIN nodes n ON n.id=d.node_id AND n.blob_id=d.blob_id WHERE search_fts MATCH ? AND d.user_id=? AND d.status='indexed' AND n.type='file' AND n.vault_id IS NULL AND n.trashed_at IS NULL AND n.id NOT IN(SELECT id FROM hidden) AND n.id IN(SELECT value FROM json_each(?))`+filter, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var doc textRow
			if err = rows.Scan(&doc.nodeID, &doc.sealed); err != nil {
				rows.Close()
				return result, err
			}
			nameDocs = append(nameDocs, doc)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return result, err
		}
		rows.Close()
	}
	wanted := limit - len(nameIDs)
	if wanted < 0 {
		wanted = 0
	}
	overfetch := 1
	if needsVerification(terms) {
		overfetch = 4
	}
	rowsOut := []candidate{}
	if wanted > 0 {
		limitCandidates := wanted * overfetch
		query := cte + ` SELECT n.id,d.text_enc,bm25(search_fts) FROM search_fts JOIN search_docs d ON d.id=search_fts.rowid JOIN nodes n ON n.id=d.node_id AND n.blob_id=d.blob_id WHERE search_fts MATCH ? AND d.user_id=? AND d.status='indexed' AND n.type='file' AND n.vault_id IS NULL AND n.trashed_at IS NULL AND n.id NOT IN(SELECT id FROM hidden) AND n.id NOT IN(SELECT value FROM json_each(?))` + filter + ` ORDER BY bm25(search_fts),n.modified_at DESC LIMIT ?`
		args := append(append([]any{}, cteArgs...), match, userID, string(excluded))
		args = append(args, scope.Args...)
		args = append(args, limitCandidates)
		rows, err := i.db.QueryContext(ctx, query, args...)
		if err != nil {
			return result, err
		}
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.nodeID, &c.sealed, &c.score); err != nil {
				rows.Close()
				return result, err
			}
			rowsOut = append(rowsOut, c)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return result, err
		}
		rows.Close()
	}
	texts := map[string]string{}
	for _, doc := range nameDocs {
		value, err := i.decryptText(ctx, userID, doc.nodeID, doc.sealed)
		if err != nil {
			return result, err
		}
		texts[doc.nodeID] = value
	}
	for _, c := range rowsOut {
		value, err := i.decryptText(ctx, userID, c.nodeID, c.sealed)
		if err != nil {
			return result, err
		}
		texts[c.nodeID] = value
	}
	for _, doc := range nameDocs {
		if matchesAll(texts[doc.nodeID], terms) {
			snippet := MakeSnippet(texts[doc.nodeID], terms)
			result.Snippets[doc.nodeID] = snippet
		}
	}
	for _, c := range rowsOut {
		if len(result.Rows) >= wanted {
			break
		}
		if !matchesAll(texts[c.nodeID], terms) {
			continue
		}
		result.Rows = append(result.Rows, ContentMatch{NodeID: c.nodeID, Snippet: MakeSnippet(texts[c.nodeID], terms)})
	}
	return result, nil
}

type textRow struct {
	nodeID string
	sealed []byte
}
type candidate struct {
	nodeID string
	sealed []byte
	score  float64
}
