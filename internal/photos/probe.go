package photos

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/imaging"
)

const (
	jpegHeadBytes = int64(256 << 10)
	maxImageBytes = int64(64 << 20)
	maxMovieBytes = int64(32 << 20)
	maxMovieBoxes = 64

	// Seconds between 1904-01-01 (the QuickTime epoch) and 1970-01-01.
	quickTimeEpoch = 2082844800
	dateTimeLayout = "2006-01-02T15:04:05"
)

var movieMIMEs = map[string]bool{"video/mp4": true, "video/quicktime": true, "video/3gpp": true, "video/3gpp2": true, "video/x-m4v": true}

var photoBlobID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type probeResult struct {
	takenAt       string
	width, height int
	duration      float64
}

type pendingPhoto struct {
	userID                               int64
	nodeID, blobID, name, mime, modified string
	size                                 int64
	created, clientModified              sql.NullString
	kind                                 string
}

func (h *Handler) probePending(ctx context.Context, limit int) (int, error) {
	rows, err := h.db.QueryContext(ctx, `WITH RECURSIVE hidden(user_id,id) AS (
		SELECT user_id,id FROM nodes WHERE trashed_at IS NOT NULL OR vault_id IS NOT NULL
		UNION SELECT child.user_id,child.id FROM nodes child JOIN hidden ON child.parent_id=hidden.id WHERE child.user_id=hidden.user_id
	) SELECT p.user_id,p.node_id,p.blob_id,n.name,n.mime,n.size,n.modified_at,
		d.created_at,d.modified_at,p.kind
		FROM photo_items p JOIN nodes n ON n.id=p.node_id
		LEFT JOIN file_client_dates d ON d.node_id=n.id AND d.blob_id=n.blob_id
		WHERE p.probed=0 AND n.user_id=p.user_id AND n.blob_id=p.blob_id
		AND NOT EXISTS (SELECT 1 FROM hidden WHERE hidden.user_id=n.user_id AND hidden.id=n.id)
		ORDER BY p.taken_at DESC,p.node_id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	items := make([]pendingPhoto, 0, limit)
	for rows.Next() {
		var item pendingPhoto
		if err = rows.Scan(&item.userID, &item.nodeID, &item.blobID, &item.name, &item.mime, &item.size, &item.modified, &item.created, &item.clientModified, &item.kind); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, item := range items {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		if err = h.probeOne(ctx, item); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func (h *Handler) probeOne(ctx context.Context, item pendingPhoto) error {
	found, probeErr := h.probeMedia(ctx, item)
	if err := ctx.Err(); err != nil {
		return err
	}
	if probeErr != nil {
		slog.Warn("could not read a file for the photo timeline", "mime", item.mime, "node_id", item.nodeID, "error", probeErr)
	}
	takenAt, source := resolveTakenAt(item.name, item.created, item.clientModified, item.modified, found.takenAt)
	state := 1
	if source == "upload" {
		state = 0
	}
	probed := 1
	if probeErr != nil {
		probed = 2
	}
	_, err := h.db.ExecContext(ctx, `UPDATE photo_items SET taken_at=?,taken_at_source=?,date_state=?,width=?,height=?,duration_seconds=?,probed=?
		WHERE node_id=? AND user_id=? AND blob_id=? AND probed=0`, takenAt, source, state, nullableInt(found.width), nullableInt(found.height), nullableFloat(found.duration), probed, item.nodeID, item.userID, item.blobID)
	if err != nil {
		return err
	}
	if probed == 1 && item.kind == "photo" && h.prepareThumbnail != nil {
		if err = h.prepareThumbnail(ctx, item.userID, item.nodeID); err != nil {
			slog.Warn("could not prepare a grid thumbnail", "node_id", item.nodeID, "error", err)
		}
	}
	return nil
}

func nullableInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}
func nullableFloat(value float64) any {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return value
}

// probeMedia reads as little of the (encrypted) file as it can: the head of a
// JPEG, the box headers and `moov` box of a movie.
func (h *Handler) probeMedia(ctx context.Context, item pendingPhoto) (probeResult, error) {
	if item.size <= 0 {
		return probeResult{}, nil
	}
	if item.kind == "video" {
		if !movieMIMEs[strings.ToLower(item.mime)] {
			return probeResult{}, nil
		}
		found, err := h.probeMovie(ctx, item)
		if !plausible(found.takenAt) {
			found.takenAt = ""
		}
		return found, err
	}
	var found imaging.Metadata
	if strings.EqualFold(item.mime, "image/jpeg") && item.size > jpegHeadBytes {
		head, err := h.readBlobRange(ctx, item.userID, item.blobID, item.size, 0, jpegHeadBytes-1)
		if err != nil {
			return probeResult{}, err
		}
		found = imaging.Inspect(head)
	}
	if found.Width == 0 {
		if item.size > maxImageBytes {
			return probeResult{}, nil
		}
		data, err := h.readBlobRange(ctx, item.userID, item.blobID, item.size, 0, item.size-1)
		if err != nil {
			return probeResult{}, err
		}
		if found = imaging.Inspect(data); found.Width == 0 {
			return probeResult{}, errors.New("photos: the picture could not be read")
		}
	}
	taken := imaging.DateTimeFromEXIF(found.DateTimeOriginal)
	if taken == "" {
		taken = imaging.DateTimeFromEXIF(found.DateTime)
	}
	if !plausible(taken) {
		taken = ""
	}
	return probeResult{takenAt: taken, width: found.Width, height: found.Height}, nil
}

// plausible reports whether a "YYYY-MM-DDTHH:MM:SS" date can be when a photo
// was taken.
func plausible(dateTime string) bool {
	parsed, err := time.Parse(dateTimeLayout, dateTime)
	return err == nil && parsed.Year() >= 1900 && !parsed.After(time.Now().UTC().Add(24*time.Hour))
}

func (h *Handler) readBlobRange(ctx context.Context, userID int64, blobID string, size, start, end int64) ([]byte, error) {
	if userID < 1 || !photoBlobID.MatchString(blobID) || start < 0 || end < start || end >= size || end-start >= maxDuplicateImageBytes {
		return nil, errors.New("photos: invalid encrypted blob range")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.keys == nil || h.blobDir == "" {
		return nil, errors.New("photos: storage is not configured")
	}
	path := filepath.Join(h.blobDir, strconv.FormatInt(userID, 10), strings.ToLower(blobID[:2]), blobID)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("photos: blob is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dek, err := h.keys.GetUserDEK(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if _, err = cryptox.DecryptBlobRange(f, size, start, end, dek, blobID, &out); err != nil {
		return nil, err
	}
	if int64(out.Len()) != end-start+1 {
		return nil, errors.New("photos: decrypted range has an unexpected size")
	}
	return out.Bytes(), nil
}

type movieBox struct {
	kind string
	body []byte
}

func (h *Handler) probeMovie(ctx context.Context, item pendingPhoto) (probeResult, error) {
	result := probeResult{}
	offset := int64(0)
	for count := 0; count < maxMovieBoxes && offset+8 <= item.size; count++ {
		readSize := int64(16)
		if offset+readSize > item.size {
			readSize = item.size - offset
		}
		head, err := h.readBlobRange(ctx, item.userID, item.blobID, item.size, offset, offset+readSize-1)
		if err != nil {
			return result, err
		}
		size := int64(binary.BigEndian.Uint32(head[:4]))
		kind := string(head[4:8])
		header := int64(8)
		if size == 1 {
			if len(head) < 16 {
				return result, nil
			}
			size = int64(binary.BigEndian.Uint64(head[8:16]))
			header = 16
		} else if size == 0 {
			size = item.size - offset
		}
		if size < header || offset+size > item.size {
			return result, nil
		}
		if kind == "moov" {
			if size > maxMovieBytes {
				return result, nil
			}
			data, err := h.readBlobRange(ctx, item.userID, item.blobID, item.size, offset, offset+size-1)
			if err != nil {
				return result, err
			}
			return parseMovie(data), nil
		}
		offset += size
	}
	return result, nil
}

func parseMovie(moov []byte) probeResult {
	result := probeResult{}
	if len(moov) < 8 || string(moov[4:8]) != "moov" {
		return result
	}
	head := int64(8)
	if binary.BigEndian.Uint32(moov[:4]) == 1 {
		head = 16
	}
	boxes := walkMovieBoxes(moov, int(head), len(moov), 0)
	for _, box := range boxes {
		switch box.kind {
		case "mvhd":
			if len(box.body) >= 20 {
				version := box.body[0]
				var created, timescale, duration uint64
				if version == 1 && len(box.body) >= 32 {
					created = binary.BigEndian.Uint64(box.body[4:12])
					timescale = uint64(binary.BigEndian.Uint32(box.body[20:24]))
					duration = binary.BigEndian.Uint64(box.body[24:32])
				} else if version == 0 {
					created = uint64(binary.BigEndian.Uint32(box.body[4:8]))
					timescale = uint64(binary.BigEndian.Uint32(box.body[12:16]))
					duration = uint64(binary.BigEndian.Uint32(box.body[16:20]))
				}
				if created > quickTimeEpoch {
					if recorded := time.Unix(int64(created-quickTimeEpoch), 0).UTC(); recorded.Year() >= 1971 && recorded.Year() <= 2200 {
						result.takenAt = recorded.Format(dateTimeLayout)
					}
				}
				if timescale > 0 && duration > 0 {
					result.duration = math.Round(float64(duration)/float64(timescale)*10) / 10
				}
			}
		case "tkhd":
			// It ends with the 3×3 matrix and the width and height as 16.16
			// numbers; the matrix's second value is set for a quarter turn.
			if end := len(box.body); end >= 84 && result.width == 0 {
				width := int(math.Round(float64(binary.BigEndian.Uint32(box.body[end-8:])) / 65536))
				height := int(math.Round(float64(binary.BigEndian.Uint32(box.body[end-4:])) / 65536))
				if width > 0 && height > 0 {
					if binary.BigEndian.Uint32(box.body[end-8-36+4:]) != 0 {
						width, height = height, width
					}
					result.width, result.height = width, height
				}
			}
		}
	}
	return result
}
func walkMovieBoxes(data []byte, start, end, depth int) []movieBox {
	boxes := make([]movieBox, 0)
	if depth > 8 {
		return boxes
	}
	for pos, count := start, 0; pos+8 <= end && count < 4096; count++ {
		size := int64(binary.BigEndian.Uint32(data[pos : pos+4]))
		kind := string(data[pos+4 : pos+8])
		header := int64(8)
		if size == 1 {
			if pos+16 > end {
				break
			}
			size = int64(binary.BigEndian.Uint64(data[pos+8 : pos+16]))
			header = 16
		} else if size == 0 {
			size = int64(end - pos)
		}
		if size < header || int64(pos)+size > int64(end) {
			break
		}
		bodyStart := pos + int(header)
		bodyEnd := pos + int(size)
		body := data[bodyStart:bodyEnd]
		boxes = append(boxes, movieBox{kind: kind, body: body})
		if kind == "moov" || kind == "trak" || kind == "mdia" || kind == "minf" || kind == "stbl" || kind == "edts" || kind == "udta" {
			boxes = append(boxes, walkMovieBoxes(data, bodyStart, bodyEnd, depth+1)...)
		}
		pos = bodyEnd
	}
	return boxes
}
