// Package photos keeps the timeline of an account's photos and videos, their
// perceptual hashes, and the similar-upload check of camera upload.
package photos

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"sync"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const defaultLimit = 200

var dayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var takenAtPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}$`)

// Principal is the signed-in account. Of device tokens only the phone app
// (Platform "android" or "ios") may use these routes.
type Principal struct {
	UserID   int64
	Device   bool
	Platform string
}

type ResolvePrincipal func(*http.Request) (Principal, bool)

type Options struct {
	DB               *sql.DB
	ResolvePrincipal ResolvePrincipal
	Keys             PhotoKeyring
	BlobDir          string
}

// PhotoKeyring captures the one operation required to decrypt stored media
// without coupling the photo handlers to the runtime key implementation.
type PhotoKeyring interface {
	GetUserDEK(context.Context, *sql.DB, int64) ([]byte, error)
}

type Handler struct {
	db        *sql.DB
	resolve   ResolvePrincipal
	keys      PhotoKeyring
	blobDir   string
	indexMu   sync.Mutex
	checkedAt map[int64]int64
	// backfilled is guarded by indexMu.
	backfilled       bool
	wake             chan struct{}
	prepareThumbnail func(context.Context, int64, string) error
}

// SetThumbnailer makes the indexer prepare the grid thumbnail of every photo
// it reads. It must be set before the worker starts.
func (h *Handler) SetThumbnailer(prepare func(ctx context.Context, userID int64, nodeID string) error) {
	h.prepareThumbnail = prepare
}

func (h *Handler) wakeWorker() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func New(options Options) (*Handler, error) {
	if options.DB == nil || options.ResolvePrincipal == nil {
		return nil, errors.New("photos: database and principal resolver are required")
	}
	return &Handler{db: options.DB, resolve: options.ResolvePrincipal, keys: options.Keys, blobDir: options.BlobDir, checkedAt: make(map[int64]int64), wake: make(chan struct{}, 1)}, nil
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/photos", h.list)
	mux.HandleFunc("GET /api/photos/summary", h.summary)
	mux.HandleFunc("POST /api/uploads/similar", h.similarUploads)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := h.resolve(r)
	if !ok || p.UserID < 1 {
		refuse(w, http.StatusUnauthorized, "Unauthorized", "UNAUTHORIZED")
		return 0, false
	}
	if p.Device && p.Platform != "android" && p.Platform != "ios" {
		refuse(w, http.StatusForbidden, "Use a browser or the NeoCloud phone app for this.", "PHONE_REQUIRED")
		return 0, false
	}
	return p.UserID, true
}

func refuse(w http.ResponseWriter, status int, message, code string) {
	httpx.WriteJSON(w, status, map[string]string{"error": message, "code": code})
}

type cursor struct {
	State   int
	TakenAt string
	NodeID  string
}

func decodeCursor(value string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor{}, err
	}
	var parts []json.RawMessage
	if json.Unmarshal(data, &parts) != nil || len(parts) != 3 {
		return cursor{}, errors.New("invalid cursor")
	}
	var c cursor
	if json.Unmarshal(parts[0], &c.State) != nil || json.Unmarshal(parts[1], &c.TakenAt) != nil || json.Unmarshal(parts[2], &c.NodeID) != nil ||
		(c.State != 0 && c.State != 1 && c.State != 2) || !takenAtPattern.MatchString(c.TakenAt) || c.NodeID == "" {
		return cursor{}, errors.New("invalid cursor")
	}
	return c, nil
}

func encodeCursor(c cursor) string {
	b, _ := json.Marshal([]any{c.State, c.TakenAt, c.NodeID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func pageLimit(raw string) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || value < 1 || math.Trunc(value) != value {
		return 0, errors.New("invalid limit")
	}
	if value > 500 {
		return 500, nil
	}
	return int(value), nil
}

type photo struct {
	ID              string `json:"id"`
	TakenAt         string `json:"takenAt"`
	Kind            string `json:"kind"`
	Width           any    `json:"width"`
	Height          any    `json:"height"`
	DurationSeconds any    `json:"durationSeconds"`
	Name            string `json:"name"`
	ParentID        any    `json:"parentId"`
	Mime            any    `json:"mime"`
	Size            int64  `json:"size"`
	ETag            string `json:"etag"`
	Favorite        bool   `json:"favorite"`
	DateSource      string `json:"dateSource"`
	Provisional     bool   `json:"provisional"`
	DateUnknown     bool   `json:"dateUnknown"`
	state           int
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	limit, err := pageLimit(r.URL.Query().Get("limit"))
	if err != nil {
		refuse(w, http.StatusBadRequest, "Invalid limit", "INVALID_LIMIT")
		return
	}
	start := cursor{State: 2, TakenAt: "9999-12-31T23:59:59", NodeID: "\uffff"}
	if value := r.URL.Query().Get("cursor"); value != "" {
		start, err = decodeCursor(value)
		if err != nil {
			refuse(w, http.StatusBadRequest, "Invalid cursor", "INVALID_CURSOR")
			return
		}
	} else if day := r.URL.Query().Get("day"); day != "" {
		if day == "unknown" {
			start = cursor{State: 0, TakenAt: "9999-12-31T23:59:59", NodeID: "\uffff"}
		} else if !dayPattern.MatchString(day) {
			refuse(w, http.StatusBadRequest, "day must be YYYY-MM-DD or unknown", "INVALID_DAY")
			return
		} else {
			start = cursor{State: 1, TakenAt: day + "T23:59:59", NodeID: "\uffff"}
		}
	}
	if err := h.CatchUp(r.Context(), uid); err != nil {
		internal(w)
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT p.node_id,p.taken_at,p.taken_at_source,p.date_state,p.kind,p.width,p.height,p.duration_seconds,
		n.name,n.parent_id,n.mime,n.size,n.etag,
		EXISTS(SELECT 1 FROM favorites f WHERE f.user_id=p.user_id AND f.node_id=p.node_id)
		FROM photo_items p JOIN nodes n ON n.id=p.node_id
		WHERE p.user_id=? AND n.user_id=? AND (p.date_state,p.taken_at,p.node_id)<(?,?,?)
		AND n.trashed_at IS NULL AND n.vault_id IS NULL
		ORDER BY p.date_state DESC,p.taken_at DESC,p.node_id DESC LIMIT ?`, uid, uid, start.State, start.TakenAt, start.NodeID, limit)
	if err != nil {
		internal(w)
		return
	}
	defer rows.Close()
	items := make([]photo, 0, limit)
	for rows.Next() {
		var p photo
		var source sql.NullString
		var width, height, duration sql.NullFloat64
		var parent, mime sql.NullString
		if err = rows.Scan(&p.ID, &p.TakenAt, &source, &p.state, &p.Kind, &width, &height, &duration, &p.Name, &parent, &mime, &p.Size, &p.ETag, &p.Favorite); err != nil {
			internal(w)
			return
		}
		if width.Valid {
			p.Width = int64(width.Float64)
		}
		if height.Valid {
			p.Height = int64(height.Float64)
		}
		if duration.Valid {
			p.DurationSeconds = duration.Float64
		}
		if parent.Valid {
			p.ParentID = parent.String
		}
		if mime.Valid {
			p.Mime = mime.String
		}
		p.DateSource = "upload"
		if source.Valid && source.String != "" {
			p.DateSource = source.String
		}
		p.Provisional = p.state == 2
		p.DateUnknown = p.state == 0
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		internal(w)
		return
	}
	var next any
	if len(items) == limit {
		last := items[len(items)-1]
		next = encodeCursor(cursor{State: last.state, TakenAt: last.TakenAt, NodeID: last.ID})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": next})
}

func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	uid, ok := h.user(w, r)
	if !ok {
		return
	}
	if err := h.CatchUp(r.Context(), uid); err != nil {
		internal(w)
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT substr(p.taken_at,1,10),COUNT(*) FROM photo_items p JOIN nodes n ON n.id=p.node_id
		WHERE p.user_id=? AND n.user_id=? AND p.date_state=1 AND n.trashed_at IS NULL AND n.vault_id IS NULL
		GROUP BY substr(p.taken_at,1,10) ORDER BY substr(p.taken_at,1,10) DESC`, uid, uid)
	if err != nil {
		internal(w)
		return
	}
	defer rows.Close()
	type dayCount struct {
		Date  string `json:"date"`
		Count int64  `json:"count"`
	}
	days := make([]dayCount, 0)
	months := make([]map[string]any, 0)
	var dated int64
	for rows.Next() {
		var d dayCount
		if err = rows.Scan(&d.Date, &d.Count); err != nil {
			internal(w)
			return
		}
		days = append(days, d)
		dated += d.Count
		month := d.Date[:7]
		if len(months) > 0 && months[len(months)-1]["month"] == month {
			months[len(months)-1]["count"] = months[len(months)-1]["count"].(int64) + d.Count
		} else {
			months = append(months, map[string]any{"month": month, "count": d.Count})
		}
	}
	if err = rows.Err(); err != nil {
		internal(w)
		return
	}
	var pending, unknown int64
	if err = h.db.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(p.date_state=2),0),COALESCE(SUM(p.date_state=0),0) FROM photo_items p JOIN nodes n ON n.id=p.node_id
		WHERE p.user_id=? AND n.user_id=? AND n.trashed_at IS NULL AND n.vault_id IS NULL`, uid, uid).Scan(&pending, &unknown); err != nil {
		internal(w)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"total": pending + unknown + dated, "pending": pending, "unknown": unknown, "days": days, "months": months})
}

func internal(w http.ResponseWriter) {
	refuse(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
}
