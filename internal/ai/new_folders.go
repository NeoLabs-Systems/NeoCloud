package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Moves into folders that don't exist yet (only with newFolders on).
//
// A proposed name is first checked against the folders the person has (the same
// name in any case, plural or language, or one the embedding model finds close
// enough means the existing folder). A new folder is never proposed for one
// file: each file's vote is held until minNewFolderFiles files of the same job
// would go into the same folder. Nothing is created here.

const (
	minNewFolderFiles   = 3
	maxFoldersPerBatch  = 3
	uploadBatch         = "upload"
	keepVotes           = "-7 days"
	sameNameSimilarity  = 0.86
	maxFolderNamesEmbed = 300
)

func (h *Handler) similarFolder(ctx context.Context, userID int64, name string, folders []folder) (*folder, error) {
	var names []string
	for _, candidate := range folders {
		if candidate.Path != "/" && !slices.Contains(names, candidate.Name) && len(names) < maxFolderNamesEmbed {
			names = append(names, candidate.Name)
		}
	}
	if len(names) == 0 {
		return nil, nil
	}
	vectors, _, err := h.embed(ctx, userID, append([]string{name}, names...))
	if err != nil || vectors == nil {
		return nil, err
	}
	best, bestScore := "", 0.0
	for index, candidate := range names {
		if score := cosine(vectors[0], vectors[index+1]); score >= sameNameSimilarity && score > bestScore {
			best, bestScore = candidate, score
		}
	}
	index := slices.IndexFunc(folders, func(item folder) bool { return item.Path != "/" && item.Name == best })
	if best == "" || index < 0 {
		return nil, nil
	}
	return &folders[index], nil
}

// resolveNewFolder is where a proposed folder really goes: an existing folder
// that means the same, the name of a new one, or neither when the name is not
// usable.
func (h *Handler) resolveNewFolder(ctx context.Context, userID int64, raw string, parent folder, scoped, everywhere []folder, language string) (*folder, string, error) {
	name := cleanNewFolderName(raw)
	if name == "" {
		return nil, "", nil
	}
	key := folderKey(name)
	sameKey := func(item folder) bool { return item.Path != "/" && folderKey(item.Name) == key }
	var same *folder
	for index := range scoped {
		if sameKey(scoped[index]) && (same == nil || scoped[index].ParentID == parent.ID && same.ParentID != parent.ID) {
			same = &scoped[index]
		}
	}
	if same != nil {
		return same, "", nil
	}
	if slices.ContainsFunc(everywhere, sameKey) {
		return nil, "", nil
	}
	// Siblings of any kind, also ones AI may not file into.
	siblings, err := queryStrings(ctx, h.db, `SELECT name FROM nodes WHERE user_id=? AND parent_id=? AND type='folder' AND trashed_at IS NULL`, userID, parent.ID)
	if err != nil {
		return nil, "", err
	}
	if slices.ContainsFunc(siblings, func(sibling string) bool { return folderKey(sibling) == key }) {
		return nil, "", nil
	}
	similar, err := h.similarFolder(ctx, userID, name, scoped)
	if err != nil || similar != nil {
		return similar, "", err
	}
	if localized := localizedConceptName(name, language); localized != "" {
		return nil, localized, nil
	}
	return nil, name, nil
}

// readyDraft is a suggestion that can be created now.
type readyDraft struct {
	node  node
	draft draft
}

type heldVote struct {
	id                                  int64
	nodeID, folderName, basisETag, held string
	released                            bool
}

// holdNewFolderMove holds one file's move into a new folder and returns the
// moves that are ready: none while fewer than minNewFolderFiles files would go
// there (or the job reached maxFoldersPerBatch new folders), then every held
// one at once, under the first file's folder name.
func (h *Handler) holdNewFolderMove(ctx context.Context, userID int64, n node, d draft, batch string) ([]readyDraft, error) {
	if batch == "" {
		batch = uploadBatch
	}
	to := *d.move
	var ready []readyDraft
	err := h.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO ai_new_folder_votes(user_id,node_id,batch_key,parent_id,folder_key,folder_name,draft_json,basis_etag)
			VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(user_id,node_id,batch_key) DO UPDATE SET parent_id=excluded.parent_id,folder_key=excluded.folder_key,
			folder_name=excluded.folder_name,draft_json=excluded.draft_json,basis_etag=excluded.basis_etag,released=0,created_at=datetime('now')`,
			userID, n.ID, batch, to.ParentID, to.Key, to.Name, jsonText(d.held()), n.ETag); err != nil {
			return err
		}
		votes, err := queryAll(ctx, tx, func(s scanner) (heldVote, error) {
			var vote heldVote
			err := s.Scan(&vote.id, &vote.nodeID, &vote.folderName, &vote.basisETag, &vote.held, &vote.released)
			return vote, err
		}, `SELECT id,node_id,folder_name,basis_etag,draft_json,released FROM ai_new_folder_votes WHERE user_id=? AND batch_key=? AND parent_id=? AND folder_key=? ORDER BY id`, userID, batch, to.ParentID, to.Key)
		if err != nil {
			return err
		}
		releasedBefore := slices.ContainsFunc(votes, func(vote heldVote) bool { return vote.released })
		type live struct {
			vote  int64
			entry readyDraft
		}
		var candidates []live
		for _, vote := range votes {
			if vote.released {
				continue
			}
			var held heldDraft
			if json.Unmarshal([]byte(vote.held), &held) != nil {
				continue
			}
			current, err := liveNode(ctx, tx, userID, vote.nodeID, "")
			var refusal *Error
			if errors.As(err, &refusal) {
				continue
			}
			if err != nil {
				return err
			}
			if current.ETag != vote.basisETag || current.ParentID != held.From.ID {
				continue
			}
			candidates = append(candidates, live{vote.id, readyDraft{current, held.draft()}})
		}
		if !releasedBefore {
			if len(candidates) < minNewFolderFiles {
				return nil
			}
			since := ""
			if batch == uploadBatch {
				since = " AND created_at>=datetime('now','" + keepVotes + "')"
			}
			var released int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT DISTINCT parent_id,folder_key FROM ai_new_folder_votes WHERE user_id=? AND batch_key=? AND released=1`+since+`)`, userID, batch).Scan(&released); err != nil {
				return err
			}
			if released >= maxFoldersPerBatch {
				return nil
			}
		}
		name := votes[0].folderName
		for _, candidate := range candidates {
			if _, err := tx.ExecContext(ctx, `UPDATE ai_new_folder_votes SET released=1 WHERE id=?`, candidate.vote); err != nil {
				return err
			}
			target := candidate.entry.draft.move
			target.Path = strings.TrimSuffix(target.Path, target.Name) + name
			target.Name = name
			candidate.entry.draft.to = target.encode()
			ready = append(ready, candidate.entry)
		}
		return nil
	})
	return ready, err
}

func dropOldVotes(ctx context.Context, q querier) error {
	_, err := q.ExecContext(ctx, `DELETE FROM ai_new_folder_votes WHERE created_at<datetime('now',?)`, keepVotes)
	return err
}
