package ai

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"math"
	"slices"
	"strings"
	"unicode"
)

// Similarity between a file and the person's folders, from the embedding role:
// each folder's profile (its path and up to 15 names inside it) is embedded
// once and again only when it changes. Without an embedding model, folders are
// ranked by the words their path shares with the file.

const profileSampleNames = 15

type rankedFolder struct {
	folder folder
	score  float64
}

func encodeVector(vector []float32) []byte {
	blob := make([]byte, 4*len(vector))
	for index, value := range vector {
		binary.LittleEndian.PutUint32(blob[4*index:], math.Float32bits(value))
	}
	return blob
}

func decodeVector(blob []byte) []float32 {
	vector := make([]float32, len(blob)/4)
	for index := range vector {
		vector[index] = math.Float32frombits(binary.LittleEndian.Uint32(blob[4*index:]))
	}
	return vector
}

func cosine(left, right []float32) float64 {
	if len(left) != len(right) || len(left) == 0 {
		return 0
	}
	var dot, leftNorm, rightNorm float64
	for index, value := range left {
		dot += float64(value) * float64(right[index])
		leftNorm += float64(value) * float64(value)
		rightNorm += float64(right[index]) * float64(right[index])
	}
	if leftNorm == 0 || rightNorm == 0 {
		return 0
	}
	return dot / math.Sqrt(leftNorm*rightNorm)
}

// folderVectors embeds only the profiles that changed (or another model's).
func (h *Handler) folderVectors(ctx context.Context, userID int64, folders []folder, model string) (map[string][]float32, error) {
	type stored struct {
		id, signature, model string
		vector               []byte
	}
	rows, err := queryAll(ctx, h.db, func(s scanner) (stored, error) {
		var item stored
		err := s.Scan(&item.id, &item.signature, &item.model, &item.vector)
		return item, err
	}, `SELECT folder_id,signature,model,vector FROM ai_folder_profiles WHERE user_id=?`, userID)
	if err != nil {
		return nil, err
	}
	kept := map[string]stored{}
	for _, item := range rows {
		kept[item.id] = item
	}
	vectors := map[string][]float32{}
	var missing []folder
	var texts, signatures []string
	for _, candidate := range folders {
		names, err := queryStrings(ctx, h.db, `SELECT name FROM nodes WHERE parent_id=? AND user_id=? AND trashed_at IS NULL ORDER BY modified_at DESC LIMIT ?`, candidate.ID, userID, profileSampleNames)
		if err != nil {
			return nil, err
		}
		text := "Folder " + candidate.Path + "\n" + strings.Join(names, "\n")
		digest := sha256.Sum256([]byte(text))
		signature := hex.EncodeToString(digest[:])
		if item, ok := kept[candidate.ID]; ok && item.signature == signature && item.model == model {
			vectors[candidate.ID] = decodeVector(item.vector)
			continue
		}
		missing, texts, signatures = append(missing, candidate), append(texts, text), append(signatures, signature)
	}
	if len(missing) == 0 {
		return vectors, nil
	}
	answer, served, err := h.embed(ctx, userID, texts)
	if err != nil || answer == nil {
		return vectors, err
	}
	return vectors, h.inTx(ctx, func(tx *sql.Tx) error {
		for index, candidate := range missing {
			vectors[candidate.ID] = answer[index]
			if _, err := tx.ExecContext(ctx, `INSERT INTO ai_folder_profiles(folder_id,user_id,signature,model,vector,updated_at)
				VALUES(?,?,?,?,?,datetime('now')) ON CONFLICT(folder_id) DO UPDATE SET signature=excluded.signature,model=excluded.model,
				vector=excluded.vector,updated_at=excluded.updated_at`, candidate.ID, userID, signatures[index], served, encodeVector(answer[index])); err != nil {
				return err
			}
		}
		return nil
	})
}

func profileWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(char rune) bool { return !unicode.IsLetter(char) && !unicode.IsNumber(char) }) {
		if len([]rune(word)) > 2 {
			words[word] = true
		}
	}
	return words
}

// rankFolders orders candidates by similarity. The embedding model is "" when
// there is none, and the scores then mean nothing.
func (h *Handler) rankFolders(ctx context.Context, userID int64, n node, f fields, folders []folder) ([]rankedFolder, string, error) {
	parts := []string{}
	for _, value := range append([]string{n.Name, f.Type, f.Issuer, f.Subject, f.Summary, f.Caption}, f.Tags...) {
		if value != "" {
			parts = append(parts, value)
		}
	}
	text := strings.Join(parts, "\n")
	file, model, err := h.embed(ctx, userID, []string{text})
	if err != nil {
		return nil, "", err
	}
	ranked := make([]rankedFolder, len(folders))
	if file == nil {
		model = ""
		wanted := profileWords(text)
		for index, candidate := range folders {
			ranked[index].folder = candidate
			for word := range profileWords(candidate.Path) {
				if wanted[word] {
					ranked[index].score++
				}
			}
		}
	} else {
		vectors, err := h.folderVectors(ctx, userID, folders, model)
		if err != nil {
			return nil, "", err
		}
		for index, candidate := range folders {
			ranked[index] = rankedFolder{candidate, cosine(file[0], vectors[candidate.ID])}
		}
	}
	slices.SortStableFunc(ranked, func(a, b rankedFolder) int { return cmp.Compare(b.score, a.score) })
	return ranked, model, nil
}
