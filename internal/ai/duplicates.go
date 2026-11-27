package ai

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/bits"
	"net/http"
	"slices"
)

// Duplicate files for Review › Duplicates. No AI involved. Exact groups share
// their content hash; similar photos have 64-bit dHashes at most three bits
// apart (made by the photos hasher).

const (
	duplicateDistance = 3
	// A hash with this few (or this many) set bits comes from a nearly flat
	// picture, which all look alike to a dHash.
	duplicateMinDetail  = 10
	maxAspectDifference = 0.1
	maxDuplicateGroups  = 500
	maxHashSourceBytes  = 200 << 20
	maxDismissIDs       = 1000
)

const hashableMIMEs = `('image/jpeg','image/png','image/gif','image/webp','image/avif','image/tiff','image/heic','image/heif','image/bmp')`

type duplicate struct {
	id, hash      string
	size          int64
	takenAt       sql.NullString
	width, height sql.NullInt64
}

type duplicateGroup struct {
	kind       string
	similarity int
	keep       string
	members    []duplicate
}

func (g duplicateGroup) wasted() int64 {
	var total int64
	for _, member := range g.members {
		if member.id != g.keep {
			total += member.size
		}
	}
	return total
}

// The oldest copy is the one to keep.
func (h *Handler) exactGroups(ctx context.Context, userID int64) ([]duplicateGroup, error) {
	type row struct {
		duplicate
		sha string
	}
	rows, err := queryAll(ctx, h.db, func(s scanner) (row, error) {
		var r row
		err := s.Scan(&r.id, &r.size, &r.sha, &r.takenAt, &r.width, &r.height)
		return r, err
	}, `WITH RECURSIVE hidden(id) AS (
		SELECT id FROM nodes WHERE user_id=? AND trashed_at IS NOT NULL
		UNION SELECT n.id FROM hidden CROSS JOIN nodes n ON n.user_id=? AND n.parent_id=hidden.id
	), live AS (
		SELECT n.id,b.sha256 FROM nodes n JOIN blobs b ON b.id=n.blob_id
		WHERE n.user_id=? AND n.type='file' AND n.size>0 AND n.vault_id IS NULL AND n.trashed_at IS NULL AND n.id NOT IN (SELECT id FROM hidden)
	), repeated AS (SELECT sha256 FROM live GROUP BY sha256 HAVING COUNT(*)>1)
	SELECT n.id,n.size,live.sha256,p.taken_at,p.width,p.height FROM live JOIN nodes n ON n.id=live.id
	LEFT JOIN photo_items p ON p.node_id=n.id
	WHERE live.sha256 IN (SELECT sha256 FROM repeated) ORDER BY live.sha256,n.created_at,n.id`, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	var groups []duplicateGroup
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].sha == rows[start].sha {
			end++
		}
		if end-start > 1 {
			group := duplicateGroup{kind: "exact", similarity: 100, keep: rows[start].id}
			for _, r := range rows[start:end] {
				group.members = append(group.members, r.duplicate)
			}
			groups = append(groups, group)
		}
		start = end
	}
	return groups, nil
}

func alike(a, b duplicate, hashA, hashB uint64) bool {
	if bits.OnesCount64(hashA^hashB) > duplicateDistance {
		return false
	}
	if a.width.Int64 <= 0 || a.height.Int64 <= 0 || b.width.Int64 <= 0 || b.height.Int64 <= 0 {
		return true
	}
	ratioA := float64(a.width.Int64) / float64(a.height.Int64)
	ratioB := float64(b.width.Int64) / float64(b.height.Int64)
	return math.Abs(ratioA-ratioB)/math.Max(ratioA, ratioB) <= maxAspectDifference
}

// similarGroups builds groups where every photo is alike to every other,
// greedily from the photo with the most close matches.
func similarGroups(rows []duplicate) []duplicateGroup {
	var photos []duplicate
	var hashes []uint64
	for _, row := range rows {
		raw, err := hex.DecodeString(row.hash)
		if err != nil || len(raw) != 8 {
			continue
		}
		var hash uint64
		for _, value := range raw {
			hash = hash<<8 | uint64(value)
		}
		if set := bits.OnesCount64(hash); set < duplicateMinDetail || set > 64-duplicateMinDetail {
			continue
		}
		photos, hashes = append(photos, row), append(hashes, hash)
	}
	// Seven chunks of the 64 bits: hashes at most three bits apart agree on at
	// least one, so only hashes sharing a chunk are compared.
	neighbours := make([]map[int]bool, len(photos))
	for index := range neighbours {
		neighbours[index] = map[int]bool{}
	}
	for _, chunk := range [][2]int{{0, 9}, {9, 18}, {18, 27}, {27, 36}, {36, 45}, {45, 54}, {54, 64}} {
		buckets := map[uint64][]int{}
		for index, hash := range hashes {
			key := hash >> (64 - chunk[1]) & (1<<(chunk[1]-chunk[0]) - 1)
			for _, other := range buckets[key] {
				if alike(photos[index], photos[other], hash, hashes[other]) {
					neighbours[index][other], neighbours[other][index] = true, true
				}
			}
			buckets[key] = append(buckets[key], index)
		}
	}
	order := make([]int, len(photos))
	for index := range order {
		order[index] = index
	}
	slices.SortStableFunc(order, func(a, b int) int { return len(neighbours[b]) - len(neighbours[a]) })
	assigned := map[int]bool{}
	var groups []duplicateGroup
	for _, seed := range order {
		if assigned[seed] || len(neighbours[seed]) == 0 {
			continue
		}
		members := []int{seed}
		candidates := make([]int, 0, len(neighbours[seed]))
		for candidate := range neighbours[seed] {
			candidates = append(candidates, candidate)
		}
		slices.Sort(candidates)
		for _, candidate := range candidates {
			if !assigned[candidate] && !slices.ContainsFunc(members, func(member int) bool { return !neighbours[candidate][member] }) {
				members = append(members, candidate)
			}
		}
		if len(members) < 2 {
			continue
		}
		// The largest picture is the one to keep; then the bigger file, then the older.
		area := func(index int) int64 { return photos[index].width.Int64 * photos[index].height.Int64 }
		keep := slices.MinFunc(members, func(a, b int) int {
			return cmp.Or(cmp.Compare(area(b), area(a)), cmp.Compare(photos[b].size, photos[a].size), cmp.Compare(a, b))
		})
		group := duplicateGroup{kind: "similar", keep: photos[keep].id}
		worst := 0
		for _, member := range members {
			assigned[member] = true
			worst = max(worst, bits.OnesCount64(hashes[member]^hashes[keep]))
			group.members = append(group.members, photos[member])
		}
		group.similarity = int(math.Round(float64(64-worst) / 64 * 100))
		groups = append(groups, group)
	}
	return groups
}

// listDuplicates is the account's groups, most space to win first. pending
// photos are not compared yet.
func (h *Handler) listDuplicates(r *http.Request, userID int64, _ body) (any, error) {
	ctx := r.Context()
	if h.options.PhotoCatchUp != nil {
		if err := h.options.PhotoCatchUp(ctx, userID); err != nil {
			return nil, err
		}
	}
	var total, hashed int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(EXISTS(SELECT 1 FROM image_hashes h WHERE h.node_id=n.id AND h.etag=n.etag)),0)
		FROM photo_items p JOIN nodes n ON n.id=p.node_id WHERE p.user_id=? AND p.kind='photo' AND n.blob_id=p.blob_id AND n.size<=? AND n.mime IN `+hashableMIMEs,
		userID, maxHashSourceBytes).Scan(&total, &hashed); err != nil {
		return nil, err
	}
	groups, err := h.exactGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	// One photo per content: exact copies are their own group already.
	photos, err := queryAll(ctx, h.db, func(s scanner) (duplicate, error) {
		var d duplicate
		err := s.Scan(&d.id, &d.size, &d.hash, &d.takenAt, &d.width, &d.height)
		return d, err
	}, `SELECT n.id,n.size,h.dhash,p.taken_at,p.width,p.height FROM nodes n
		JOIN photo_items p ON p.node_id=n.id AND p.blob_id=n.blob_id
		JOIN image_hashes h ON h.node_id=n.id AND h.etag=n.etag
		WHERE p.user_id=? AND p.kind='photo' AND h.dhash IS NOT NULL AND n.trashed_at IS NULL AND n.vault_id IS NULL
		AND n.id IN (SELECT id FROM (SELECT id,ROW_NUMBER() OVER (PARTITION BY etag ORDER BY created_at,id) AS copy FROM nodes WHERE user_id=? AND trashed_at IS NULL) WHERE copy=1)
		ORDER BY n.created_at,n.id`, userID, userID)
	if err != nil {
		return nil, err
	}
	groups = append(groups, similarGroups(photos)...)
	dismissals, err := queryStrings(ctx, h.db, `SELECT node_ids_json FROM duplicate_dismissals WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	for _, raw := range dismissals {
		var ids []string
		if json.Unmarshal([]byte(raw), &ids) != nil {
			continue
		}
		groups = slices.DeleteFunc(groups, func(g duplicateGroup) bool {
			return !slices.ContainsFunc(g.members, func(member duplicate) bool { return !slices.Contains(ids, member.id) })
		})
	}
	slices.SortStableFunc(groups, func(a, b duplicateGroup) int { return cmp.Compare(b.wasted(), a.wasted()) })
	groups = groups[:min(len(groups), maxDuplicateGroups)]
	var ids []string
	for _, group := range groups {
		for _, member := range group.members {
			ids = append(ids, member.id)
		}
	}
	views, err := h.nodeViews(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(groups))
	for index, group := range groups {
		items := make([]map[string]any, len(group.members))
		for position, member := range group.members {
			item := map[string]any{"node": views[member.id], "size": member.size, "keep": member.id == group.keep}
			if member.takenAt.String != "" {
				item["takenAt"] = member.takenAt.String
			}
			if member.width.Int64 > 0 && member.height.Int64 > 0 {
				item["width"], item["height"] = member.width.Int64, member.height.Int64
			}
			items[position] = item
		}
		result[index] = map[string]any{"kind": group.kind, "similarity": group.similarity, "items": items}
	}
	return map[string]any{"groups": result, "pending": total - hashed, "hashed": hashed, "total": total}, nil
}

// dismissDuplicates: "not duplicates", the group of these files stays hidden.
func (h *Handler) dismissDuplicates(r *http.Request, userID int64, input body) (any, error) {
	ctx := r.Context()
	list, _ := input["nodeIds"].([]any)
	ids, ok := idList(input["nodeIds"], maxDismissIDs)
	if !ok || len(list) < 2 {
		return nil, fail(http.StatusBadRequest, "nodeIds must be a list of 2–1000 file ids.", "INVALID_REQUEST")
	}
	var owned int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE user_id=? AND id IN (SELECT value FROM json_each(?))`, userID, jsonText(ids)).Scan(&owned); err != nil {
		return nil, err
	}
	if owned != len(ids) {
		return nil, notFound()
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO duplicate_dismissals(user_id,node_ids_json) VALUES(?,?)`, userID, jsonText(ids)); err != nil {
		return nil, err
	}
	return map[string]any{"success": true}, nil
}
