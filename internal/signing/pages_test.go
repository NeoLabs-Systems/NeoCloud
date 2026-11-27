package signing

import (
	"context"
	"math"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

func TestRotatePagesMovesFieldsAndKeepsTheAuditTrail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.draft(owner, "scan", []map[string]any{{"name": "Signer 1"}}, signatureField(0, 0.5))
	base := "/api/signing/documents/" + d.id
	before := obj(e.as(owner, "GET", base, nil, 200).body["document"])
	page := obj(list(before["pages"])[0])
	width, height := page["width"].(float64), page["height"].(float64)

	e.as(owner, "POST", base+"/rotate", map[string]any{"angle": 45.0}, 400)
	e.as(owner, "POST", base+"/rotate", map[string]any{"angle": 90.0, "pages": []any{3.0}}, 400)
	e.as(other, "POST", base+"/rotate", map[string]any{"angle": 90.0}, 404)

	out := e.as(owner, "POST", base+"/rotate", map[string]any{"angle": 90.0, "pages": []any{0.0}}, 200)
	after := obj(out.body["document"])
	turned := obj(list(after["pages"])[0])
	if turned["rotate"] != 90.0 || turned["width"] != height || turned["height"] != width || after["snapshotSha256"] == before["snapshotSha256"] {
		t.Fatalf("rotated: %s", out.raw)
	}
	got := obj(list(obj(list(out.body["fields"])[0])["areas"])[0])
	want := signpdf.RotateArea(signpdf.Area{X: 0.1, Y: 0.5, W: 0.3, H: 0.05}, 1, width, height)
	for key, v := range map[string]float64{"x": want.X, "y": want.Y, "w": want.W, "h": want.H} {
		if math.Abs(got[key].(float64)-v) > 1e-6 {
			t.Fatalf("area %s = %v, want %v", key, got[key], v)
		}
	}
	pdf := e.as(owner, "GET", base+"/pdf", nil, 200)
	if info, err := signpdf.Probe(ctx, pdf.raw); err != nil || info.Pages[0].Rotate != 90 {
		t.Fatalf("served pdf: %+v %v", info, err)
	}
	// A quarter turn back and forth ends where it began.
	e.as(owner, "POST", base+"/rotate", map[string]any{"angle": -90.0}, 200)

	events := e.as(owner, "GET", base+"/events", nil, 200)
	var rotations []map[string]any
	for _, item := range list(events.body["events"]) {
		if str(obj(item)["type"]) == "pages_rotated" {
			rotations = append(rotations, obj(obj(item)["data"]))
		}
	}
	if len(rotations) != 2 || rotations[0]["previousSha256"] != before["snapshotSha256"] || rotations[0]["sha256"] != after["snapshotSha256"] || rotations[1]["angle"] != 270.0 {
		t.Fatalf("rotation events: %v", rotations)
	}
	if obj(events.body["verify"])["chainValid"] != true {
		t.Fatal("broken audit trail")
	}

	// The turned file is what gets signed and sealed.
	image := str(e.raw(owner, "POST", "/api/signing/profile/images?kind=signature&method=draw", samplePNG(), 201).body["id"])
	e.as(owner, "POST", base+"/quick-sign", map[string]any{"signerName": "Ada Owner", "fields": quickFields(image)}, 200)
	e.h.finalizeDue(ctx)
	if e.status(d) != "completed" {
		t.Fatal(e.status(d))
	}
	e.as(owner, "POST", base+"/rotate", map[string]any{"angle": 90.0}, 409)
	if n := e.count(`SELECT COUNT(*) FROM blobs b JOIN sign_documents d ON d.snapshot_blob_id=b.id WHERE d.id=? AND b.ref_count=1`, d.id); n != 1 {
		t.Fatalf("snapshot blob references: %d", n)
	}
}

func TestDetectFieldsByRoleWithoutDuplicates(t *testing.T) {
	e := newEnv(t)
	pdf, _, err := signpdf.AppendSignaturePage(context.Background(), samplePDF(t), []string{"Ann"}, "de")
	if err != nil {
		t.Fatal(err)
	}
	node := e.file(owner, "/vertrag.pdf", pdf)
	id := str(obj(e.as(owner, "POST", "/api/signing/documents", map[string]any{"nodeId": node}, 201).body["document"])["id"])
	path := "/api/signing/documents/" + id + "/import-fields"

	e.as(owner, "POST", path, map[string]any{"roles": []any{"autograph"}}, 400)
	e.as(owner, "POST", path, map[string]any{"minConfidence": 2.0}, 400)
	if out := e.as(owner, "POST", path, map[string]any{"minConfidence": 0.99}, 200); out.body["imported"] != 0.0 {
		t.Fatalf("certain only: %s", out.raw)
	}
	dates := e.as(owner, "POST", path, map[string]any{"roles": []any{"date"}}, 200)
	if dates.body["imported"] != 1.0 || str(obj(list(dates.body["fields"])[0])["type"]) != "date" {
		t.Fatalf("dates: %s", dates.raw)
	}
	all := e.as(owner, "POST", path, map[string]any{"roles": []any{"signature", "initials", "date", "name"}}, 200)
	roles := map[string]bool{}
	for _, item := range list(all.body["detected"]) {
		found := obj(item)
		roles[str(found["role"])] = true
		if c := found["confidence"].(float64); c < 0.5 || c > 1 || found["source"] != "layout" {
			t.Fatalf("detected: %v", found)
		}
	}
	if all.body["imported"] != 2.0 || !roles["signature"] || !roles["name"] || len(list(all.body["fields"])) != 3 {
		t.Fatalf("second pass: %s", all.raw)
	}
	if again := e.as(owner, "POST", path, nil, 200); again.body["imported"] != 0.0 {
		t.Fatalf("detected twice: %s", again.raw)
	}
}
