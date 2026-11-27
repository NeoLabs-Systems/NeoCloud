package signing

import (
	"context"
	"encoding/base64"
	"testing"
)

func quickFields(imageID string) []any {
	return []any{
		map[string]any{"type": "signature", "imageId": imageID, "areas": []any{
			map[string]any{"page": 0, "x": 0.1, "y": 0.7, "w": 0.3, "h": 0.08},
			map[string]any{"page": 0, "x": 0.5, "y": 0.7, "w": 0.3, "h": 0.08},
		}},
		map[string]any{"type": "initials", "image": base64.StdEncoding.EncodeToString(samplePNG()), "method": "draw", "areas": area(0.1, 0.5)},
		map[string]any{"type": "date", "value": "2026-11-27", "prefs": map[string]any{"format": "DD.MM.YYYY"}, "areas": area(0.1, 0.8)},
		map[string]any{"type": "text", "value": "Ada Owner", "areas": area(0.5, 0.8)},
	}
}

func TestQuickSignAuthorization(t *testing.T) {
	e := newEnv(t)
	d := e.draft(owner, "lease", []map[string]any{{"name": "Signer 1"}}, signatureField(0, 0.5))
	path := "/api/signing/documents/" + d.id + "/quick-sign"
	mine := str(e.raw(owner, "POST", "/api/signing/profile/images?kind=signature&method=draw", samplePNG(), 201).body["id"])
	theirs := str(e.raw(other, "POST", "/api/signing/profile/images?kind=signature&method=draw", samplePNG(), 201).body["id"])
	input := map[string]any{"signerName": "Ada Owner", "fields": quickFields(mine)}

	if out := e.request("POST", path, map[string]string{"Content-Type": "application/json"}, jsonBytes(input)); out.status != 401 {
		t.Fatalf("no session = %d", out.status)
	}
	// Another account can neither sign the owner's draft nor be signed with.
	e.as(other, "POST", path, input, 404)
	e.as(owner, "POST", path, map[string]any{"signerName": "Ada Owner", "fields": quickFields(theirs)}, 404)
	if e.status(d) != "draft" || e.count(`SELECT COUNT(*) FROM sign_parties WHERE document_id=? AND is_self=0`, d.id) != 1 {
		t.Fatal("a refused quick sign changed the draft")
	}
}

func TestQuickSignRefusesBadInputAndKeepsTheDraft(t *testing.T) {
	e := newEnv(t)
	d := e.draft(owner, "lease", []map[string]any{{"name": "Signer 1"}}, signatureField(0, 0.5))
	path := "/api/signing/documents/" + d.id + "/quick-sign"
	image := str(e.raw(owner, "POST", "/api/signing/profile/images?kind=initials&method=draw", samplePNG(), 201).body["id"])
	field := func(f map[string]any) map[string]any {
		return map[string]any{"signerName": "Ada", "fields": []any{f}}
	}
	e.as(owner, "POST", path, map[string]any{"fields": quickFields(image)}, 400)
	e.as(owner, "POST", path, map[string]any{"signerName": "Ada", "fields": []any{}}, 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "checkbox", "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "date", "value": "27.11.2026", "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "text", "value": " ", "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "signature", "imageId": image, "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "signature", "image": "not base64", "method": "draw", "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "signature", "image": base64.StdEncoding.EncodeToString(samplePNG()), "method": "scan", "areas": area(0.1, 0.1)}), 400)
	e.as(owner, "POST", path, field(map[string]any{"type": "text", "value": "Ada", "areas": []any{map[string]any{"page": 9, "x": 0.1, "y": 0.1, "w": 0.2, "h": 0.1}}}), 400)
	if e.status(d) != "draft" || e.count(`SELECT COUNT(*) FROM sign_parties WHERE document_id=? AND is_self=0`, d.id) != 1 {
		t.Fatal("a refused quick sign changed the draft")
	}
}

func TestQuickSignSealsAndSavesBesideTheOriginal(t *testing.T) {
	e := newEnv(t)
	e.mailOn = true
	ctx := context.Background()
	d := e.draft(owner, "lease", []map[string]any{{"name": "Signer 1", "email": "someone@example.com"}}, signatureField(0, 0.5))
	image := str(e.raw(owner, "POST", "/api/signing/profile/images?kind=signature&method=draw", samplePNG(), 201).body["id"])
	path := "/api/signing/documents/" + d.id + "/quick-sign"

	out := e.as(owner, "POST", path, map[string]any{"signerName": "Ada Owner", "timezone": "Europe/Vienna", "fields": quickFields(image)}, 200)
	parties := list(out.body["parties"])
	if str(obj(out.body["document"])["status"]) != "finalizing" || len(parties) != 1 || obj(parties[0])["isSelf"] != true || str(obj(parties[0])["signerName"]) != "Ada Owner" {
		t.Fatalf("quick sign: %s", out.raw)
	}
	if n := e.count(`SELECT COUNT(*) FROM sign_values WHERE document_id=?`, d.id); n != 4 {
		t.Fatalf("%d values", n)
	}
	e.as(owner, "POST", path, map[string]any{"signerName": "Ada Owner", "fields": quickFields(image)}, 409)

	e.h.finalizeDue(ctx)
	if e.status(d) != "completed" {
		t.Fatal(e.status(d))
	}
	var name string
	var sameFolder bool
	if err := e.db.W.QueryRow(`SELECT n.name, n.parent_id IS s.parent_id FROM sign_documents d JOIN nodes n ON n.id=d.final_node_id JOIN nodes s ON s.id=d.source_node_id WHERE d.id=?`, d.id).Scan(&name, &sameFolder); err != nil {
		t.Fatal(err)
	}
	if name != "lease (signed).pdf" || !sameFolder {
		t.Fatal(name, sameFolder)
	}
	events := e.as(owner, "GET", "/api/signing/documents/"+d.id+"/events", nil, 200)
	kinds := map[string]bool{}
	for _, item := range list(events.body["events"]) {
		kinds[str(obj(item)["type"])] = true
	}
	for _, kind := range []string{"created", "sent", "opened", "consented", "party_completed", "completed", "finalized"} {
		if !kinds[kind] {
			t.Fatalf("no %s event: %v", kind, kinds)
		}
	}
	if obj(events.body["verify"])["chainValid"] != true {
		t.Fatal("broken audit trail")
	}
	// Nobody is invited; the owner gets the usual completion mail.
	if sent := e.deliverMail(); len(sent) != 1 || sent[0].to != "owner@example.com" {
		t.Fatalf("quick sign mailed %v", sent)
	}
}

func TestQuickSignFailingAfterSendLeavesNothing(t *testing.T) {
	e := newEnv(t)
	d := e.draft(owner, "lease", []map[string]any{{"name": "Signer 1"}}, signatureField(0, 0.5))
	fields := []any{map[string]any{"type": "signature", "image": base64.StdEncoding.EncodeToString(samplePNG()), "method": "draw", "areas": area(0.1, 0.1)}}
	// No room for the picture once the draft holds its pages.
	if _, err := e.db.W.Exec(`UPDATE users SET quota_bytes=used_bytes WHERE id=?`, owner); err != nil {
		t.Fatal(err)
	}
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/quick-sign", map[string]any{"signerName": "Ada", "fields": fields}, 413)
	e.as(owner, "GET", "/api/signing/documents/"+d.id, nil, 404)
	if n := e.count(`SELECT COUNT(*) FROM sign_documents`); n != 0 {
		t.Fatalf("%d documents left", n)
	}
}
