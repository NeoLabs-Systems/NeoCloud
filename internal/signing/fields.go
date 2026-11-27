package signing

import (
	"context"
	"database/sql"
	"log/slog"
	"math"
	"strconv"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
	"github.com/google/uuid"
)

// The boxes on the pages, edited as one list. Every entry is validated against
// its type; nothing the client sends is trusted.

const (
	minSide   = 0.005
	tolerance = 1e-4
)

func round5(v float64) float64 { return math.Round(v*100000) / 100000 }

func normalizeAreas(input any, pageCount int) ([]signpdf.Area, error) {
	list, ok := input.([]any)
	if !ok || len(list) < 1 || len(list) > maxAreas {
		return nil, invalidField("A field has 1 to " + strconv.Itoa(maxAreas) + " areas.")
	}
	out := make([]signpdf.Area, 0, len(list))
	for _, raw := range list {
		area, ok := raw.(map[string]any)
		if !ok {
			return nil, invalidField("An area must be an object.")
		}
		page, ok := area["page"].(float64)
		if !ok || page != math.Trunc(page) || page < 0 || int(page) >= pageCount {
			return nil, invalidField("An area lies on a page the document does not have.")
		}
		var v [4]float64
		for i, key := range []string{"x", "y", "w", "h"} {
			n, ok := area[key].(float64)
			if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, invalidField("Area values must be numbers.")
			}
			v[i] = n
		}
		x, y, w, h := v[0], v[1], v[2], v[3]
		if x < 0 || y < 0 || w < minSide || h < minSide || x+w > 1+tolerance || y+h > 1+tolerance {
			return nil, invalidField("An area must lie inside its page.")
		}
		out = append(out, signpdf.Area{Page: int(page), X: round5(x), Y: round5(y), W: round5(math.Min(w, 1-x)), H: round5(math.Min(h, 1-y))})
	}
	return out, nil
}

// normalizeField is one field as it will be stored, with the id the client
// sent.
func normalizeField(raw any, pageCount int, partyIDs map[string]bool) (field, error) {
	var f field
	input, ok := raw.(map[string]any)
	if !ok {
		return f, invalidField("Each field must be an object.")
	}
	f.Type, _ = input["type"].(string)
	t, known := fieldTypes[f.Type]
	if !known {
		return f, invalidField(`Unknown field type "` + f.Type + `".`)
	}
	f.PartyID, _ = input["partyId"].(string)
	if !partyIDs[f.PartyID] {
		return f, invalidField("A field belongs to a party of this document.")
	}
	name := ""
	if v := input["name"]; v != nil {
		if name, ok = v.(string); !ok || length(name) > maxNameLength {
			return f, invalidField("A field name has at most " + strconv.Itoa(maxNameLength) + " characters.")
		}
	}
	required, readonly := true, false
	for key, target := range map[string]*bool{"required": &required, "readonly": &readonly} {
		if v, present := input[key]; present {
			b, ok := v.(bool)
			if !ok {
				return f, invalidField(key + " must be true or false.")
			}
			*target = b
		}
	}
	prefs, err := normalizePrefs(f.Type, input["prefs"])
	if err != nil {
		return f, err
	}
	if t.storage == storeStatic {
		readonly = true
	}
	if readonly && t.storage == storeBlob {
		return f, invalidField("A signature, image or file field can’t be read-only.")
	}
	f.Name, f.Readonly = singleLine(name), readonly
	// A read-only field is filled in by the sender: nothing to require.
	f.Required = required && !readonly
	def, err := normalizeDefault(f.Type, prefs, input["defaultValue"])
	if err != nil {
		return f, err
	}
	if def != nil {
		f.Default = text{String: encodeJSON(def), Valid: true}
	}
	areas, err := normalizeAreas(input["areas"], pageCount)
	if err != nil {
		return f, err
	}
	f.PrefsJSON, f.AreasJSON = encodeJSON(prefs), encodeJSON(areas)
	f.ID, _ = input["id"].(string)
	return f, nil
}

func insertField(ctx context.Context, q querier, f field) error {
	_, err := q.ExecContext(ctx, `INSERT INTO sign_fields(id,document_id,party_id,type,name,required,readonly,default_value,prefs_json,areas_json,position) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, f.DocumentID, f.PartyID, f.Type, f.Name, flag(f.Required), flag(f.Readonly), nullable(f.Default), f.PrefsJSON, f.AreasJSON, f.Position)
	return err
}

func partyIDsOf(ctx context.Context, q querier, documentID string) (map[string]bool, error) {
	parties, err := listParties(ctx, q, documentID)
	ids := map[string]bool{}
	for _, p := range parties {
		ids[p.ID] = true
	}
	return ids, err
}

// replaceFields makes the document's fields equal to input: an entry with a
// known id updates it, any other becomes new, fields left out are removed.
func replaceFields(ctx context.Context, tx *sql.Tx, d document, input any) error {
	list, ok := input.([]any)
	if !ok || len(list) > maxFields {
		return invalidField("A document has at most " + strconv.Itoa(maxFields) + " fields.")
	}
	partyIDs, err := partyIDsOf(ctx, tx, d.ID)
	if err != nil {
		return err
	}
	fields := make([]field, 0, len(list))
	for _, raw := range list {
		f, err := normalizeField(raw, d.PageCount, partyIDs)
		if err != nil {
			return err
		}
		fields = append(fields, f)
	}
	stored, err := listFields(ctx, tx, d.ID)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for _, f := range stored {
		existing[f.ID] = true
	}
	kept := map[string]bool{}
	for i := range fields {
		f := &fields[i]
		f.DocumentID, f.Position = d.ID, i
		if !existing[f.ID] {
			f.ID = uuid.NewString()
			continue
		}
		if kept[f.ID] {
			return invalidField("A field is listed twice.")
		}
		kept[f.ID] = true
	}
	for id := range existing {
		if !kept[id] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM sign_fields WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	for _, f := range fields {
		if !kept[f.ID] {
			if err := insertField(ctx, tx, f); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sign_fields SET party_id=?,type=?,name=?,required=?,readonly=?,default_value=?,prefs_json=?,areas_json=?,position=? WHERE id=? AND document_id=?`,
			f.PartyID, f.Type, f.Name, flag(f.Required), flag(f.Readonly), nullable(f.Default), f.PrefsJSON, f.AreasJSON, f.Position, f.ID, d.ID); err != nil {
			return err
		}
	}
	return nil
}

// addImported adds the PDF's form fields as fields of the first party.
// Entries that don't pass validation are skipped.
func addImported(ctx context.Context, tx *sql.Tx, d document, found []signpdf.ImportedField) (int, error) {
	parties, err := listParties(ctx, tx, d.ID)
	if err != nil || len(parties) == 0 {
		return 0, err
	}
	existing, err := listFields(ctx, tx, d.ID)
	if err != nil {
		return 0, err
	}
	first := map[string]bool{parties[0].ID: true}
	count := 0
	for _, draft := range found {
		if len(existing)+count >= maxFields {
			break
		}
		areas := make([]any, len(draft.Areas))
		for i, a := range draft.Areas {
			areas[i] = map[string]any{"page": float64(a.Page), "x": a.X, "y": a.Y, "w": a.W, "h": a.H}
		}
		f, err := normalizeField(map[string]any{"type": draft.Type, "partyId": parties[0].ID, "name": truncate(draft.Name, maxNameLength),
			"required": draft.Required, "prefs": draft.Prefs, "areas": areas}, d.PageCount, first)
		if err != nil {
			slog.Debug("skipped an imported form field", "reason", err)
			continue
		}
		// A radio group's boxes keep the option each of them stands for.
		for i, a := range draft.Areas {
			if a.Option != nil {
				stored := decodeJSON(text{String: f.AreasJSON, Valid: true}, nil).([]any)
				stored[i].(map[string]any)["option"] = *a.Option
				f.AreasJSON = encodeJSON(stored)
			}
		}
		f.ID, f.DocumentID, f.Position = uuid.NewString(), d.ID, len(existing)+count
		if err := insertField(ctx, tx, f); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
