package signing

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Saving the signed file into the cloud of every signer who has an account and
// whose profile says so. A failure never touches the document: it is recorded
// and tried once more later.

const (
	autoFolderName = "Signed documents"
	autosaveRetry  = 5 * time.Minute
	// Parties that completed longer ago are left alone.
	autosaveWindow = 24 * time.Hour
)

var (
	controlRun  = regexp.MustCompile("[\x00-\x1f\x7f]+")
	slashRun    = regexp.MustCompile(`[/\\]+`)
	failureCode = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
)

// autoFolder is the "Signed documents" folder in the root, made on demand.
func (h *Handler) autoFolder(ctx context.Context, userID int64) (string, error) {
	if h.agent == nil {
		return "", errors.New("signing: no folder service")
	}
	var err error
	for n := 0; n < 20; n++ {
		name := autoFolderName
		if n > 0 {
			name += " (" + strconv.Itoa(n) + ")"
		}
		var id string
		if id, err = h.agent.EnsureFolder(ctx, userID, name); err == nil && usableFolder(ctx, h.db, userID, id) != "" {
			return id, nil
		}
	}
	return "", errors.Join(errors.New("signing: no folder for signed documents"), err)
}

func (h *Handler) autosaveEvents(ctx context.Context, partyID, kind string) ([]string, error) {
	rows, err := h.db.QueryContext(ctx, `SELECT at FROM sign_events WHERE party_id=? AND type=? ORDER BY id`, partyID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var at string
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out = append(out, at)
	}
	return out, rows.Err()
}

// autosaveParty saves the final file for one party, if it qualifies.
func (h *Handler) autosaveParty(ctx context.Context, documentID, partyID string) {
	h.autosaveMu.Lock()
	defer h.autosaveMu.Unlock()
	d, err := documentByID(ctx, h.db, documentID)
	if err != nil {
		return
	}
	p, err := partyByID(ctx, h.db, partyID)
	if err != nil || p.DocumentID != d.ID || d.Status != statusCompleted || p.Status != partyCompleted || !p.UserID.Valid || p.UserID.Int64 == d.OwnerID {
		return
	}
	userID := p.UserID.Int64
	if done, err := h.autosaveEvents(ctx, p.ID, "autosaved"); err != nil || len(done) > 0 {
		return
	}
	settings, err := h.profileOf(ctx, h.db, userID)
	if err != nil || !settings.autoSave {
		return
	}
	nodeID, err := func() (string, error) {
		node, live, err := liveFinalNode(ctx, h.db, d)
		if err != nil {
			return "", err
		}
		if !live {
			return "", finalUnavailable()
		}
		content, err := h.readBlob(ctx, d.OwnerID, node.blobID, node.size)
		if err != nil {
			return "", err
		}
		folder := usableFolder(ctx, h.db, userID, settings.saveFolder.String)
		if folder == "" {
			if folder, err = h.autoFolder(ctx, userID); err != nil {
				return "", err
			}
		}
		title := strings.TrimSpace(slashRun.ReplaceAllString(controlRun.ReplaceAllString(d.Title, " "), "-"))
		if title = truncate(title, 200); title == "" {
			title = "document"
		}
		return h.storeFile(ctx, userID, folder, title+" (signed).pdf", content)
	}()
	result := event{kind: "autosaved", partyID: p.ID, at: h.stampNow(), data: map[string]any{"nodeId": nodeID}}
	if err != nil {
		code := "FAILED"
		var refused *apiError
		var coded interface {
			error
			ErrorCode() string
		}
		if errors.As(err, &refused) {
			code = refused.code
		} else if errors.As(err, &coded) && failureCode.MatchString(coded.ErrorCode()) {
			code = coded.ErrorCode()
		}
		slog.Warn("saving a signed file to an account failed", "code", code, "error", err)
		result.kind, result.data = "autosave_failed", map[string]any{"code": code}
	}
	if err := appendEvent(ctx, h.db, d.ID, result); err != nil {
		slog.Error("signing autosave could not be recorded", "error", err)
		return
	}
	if result.kind == "autosaved" {
		h.notifyUser(userID, "signing:saved", map[string]any{"documentId": d.ID, "nodeId": nodeID})
		h.announceDocument(ctx, d.ID, d.Status)
	}
}

func (h *Handler) boundParties(ctx context.Context, where string, args ...any) ([]party, error) {
	return queryParties(ctx, h.db, `WHERE status='completed' AND user_id IS NOT NULL AND `+where, args...)
}

// autosaveDocument saves the final file for every qualifying party.
func (h *Handler) autosaveDocument(ctx context.Context, documentID string) {
	parties, err := h.boundParties(ctx, `document_id=?`, documentID)
	if err != nil {
		return
	}
	for _, p := range parties {
		h.autosaveParty(ctx, documentID, p.ID)
	}
}

// retryAutosaves tries parties never tried (a stop in between, or a late
// claim) and, once more after a pause, those that failed.
func (h *Handler) retryAutosaves(ctx context.Context) {
	parties, err := h.boundParties(ctx, `document_id IN (SELECT id FROM sign_documents WHERE status='completed' AND completed_at>=?)`, stamp(h.now().Add(-autosaveWindow)))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("signing autosave pass failed", "error", err)
		}
		return
	}
	for _, p := range parties {
		failures, err := h.autosaveEvents(ctx, p.ID, "autosave_failed")
		if err != nil || len(failures) >= 2 {
			continue
		}
		if len(failures) > 0 {
			if last, ok := parseStamp(failures[len(failures)-1]); ok && h.now().Sub(last) < autosaveRetry {
				continue
			}
		}
		h.autosaveParty(ctx, p.DocumentID, p.ID)
	}
}
