package signing

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

func twoParties() []map[string]any {
	return []map[string]any{
		{"name": "Alice Ahorn", "email": "alice@example.com"},
		{"name": "Bob Birke", "email": "bob@example.com", "color": "#123456"},
	}
}

func TestSequentialSigningToSealedFileAndMail(t *testing.T) {
	e := newEnv(t)
	e.mailOn = true
	ctx := context.Background()
	d := e.draft(owner, "lease", twoParties(), signatureField(0, 0.5), textField(0, "Name", 0.5), signatureField(1, 0.7),
		fieldSpec{1, map[string]any{"type": "file", "name": "Proof", "areas": area(0.5, 0.7)}})
	tokens := e.send(owner, d, map[string]any{"message": "Please sign\nsoon"})

	invitations := e.deliverMail()
	if len(invitations) != 1 || invitations[0].to != "alice@example.com" {
		t.Fatalf("invitations: %+v", invitations)
	}
	invitation := invitations[0].message
	if invitation.Subject != `owner asked you to sign "lease"` || invitation.ActionURL != "https://cloud.test/app/sign/"+tokens[0] ||
		invitation.Quote == nil || invitation.Quote.Text != "Please sign\nsoon" || invitation.Headers["Auto-Submitted"] != "auto-generated" ||
		!strings.Contains(invitation.Footer, "mailto:cloud@test.example") || strings.Contains(mailBody(invitation), tokens[1]) {
		t.Fatalf("invitation: %+v", invitation)
	}

	waiting := e.public("GET", tokens[1], "", nil, 200)
	if waiting.body["state"] != "waiting" || obj(waiting.body["turn"])["waitingCount"] != float64(1) {
		t.Fatalf("waiting: %s", waiting.raw)
	}
	e.public("POST", tokens[1], "/consent", map[string]any{"accepted": true, "signerName": "Bob"}, 409)

	view := e.public("GET", tokens[0], "", nil, 200)
	if view.body["state"] != "ready" || view.header.Get("Cache-Control") != "no-store, max-age=0" || view.header.Get("Referrer-Policy") != "no-referrer" || view.header.Get("Set-Cookie") != "" {
		t.Fatalf("view: %v %s", view.header, view.raw)
	}
	for _, secret := range []string{"Bob", "Birke", "bob@", "#123456"} {
		if bytes.Contains(view.raw, []byte(secret)) {
			t.Fatalf("the first signer sees %q: %s", secret, view.raw)
		}
	}
	if pdf := e.public("GET", tokens[0], "/pdf", nil, 200); !bytes.HasPrefix(pdf.raw, []byte("%PDF-")) {
		t.Fatal("snapshot")
	}
	e.public("PUT", tokens[0], "/fields/"+d.fields[1], map[string]any{"value": "Alice"}, 409)
	e.consent(tokens[0], "Alice Ahorn")
	e.public("PUT", tokens[0], "/fields/"+d.fields[2], map[string]any{"value": "x"}, 404)
	incomplete := e.public("POST", tokens[0], "/complete", nil, 400)
	if incomplete.body["code"] != "FIELDS_INCOMPLETE" || len(list(incomplete.body["fieldIds"])) != 2 {
		t.Fatalf("incomplete: %s", incomplete.raw)
	}
	e.public("PUT", tokens[0], "/fields/"+d.fields[1], map[string]any{"value": "Alice \u202eAhorn"}, 200)
	e.upload(tokens[0], d.fields[0], "method=draw", "image/png", []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"), 400)
	e.upload(tokens[0], d.fields[0], "method=draw", "image/gif", sampleGIF(), 201)
	uploaded := e.upload(tokens[0], d.fields[0], "method=draw", "image/png", samplePNG(), 201)
	if obj(uploaded.body["value"])["kind"] != "image" || obj(uploaded.body["value"])["width"] != float64(300) {
		t.Fatalf("upload: %s", uploaded.raw)
	}
	e.public("GET", tokens[0], "/values/"+d.fields[0]+"/image", nil, 200)
	e.public("POST", tokens[0], "/complete", nil, 200)
	e.public("POST", tokens[0], "/complete", nil, 200)
	e.public("PUT", tokens[0], "/fields/"+d.fields[1], map[string]any{"value": "later"}, 409)

	turn := e.deliverMail()
	if len(turn) != 1 || turn[0].to != "bob@example.com" || !strings.Contains(turn[0].message.Subject, "Your turn") {
		t.Fatalf("turn mail: %+v", turn)
	}
	e.consent(tokens[1], "Bob Birke")
	e.upload(tokens[1], d.fields[2], "method=type&text=Bob", "image/png", samplePNG(), 201)
	e.upload(tokens[1], d.fields[3], "name=..%2F..%2Fproof.txt", "text/plain", []byte("proof"), 201)
	// Bob sees Alice's finished picture but not another unfinished one.
	e.public("GET", tokens[1], "/values/"+d.fields[0]+"/image", nil, 200)
	e.public("POST", tokens[1], "/complete", nil, 200)
	if e.status(d) != "finalizing" {
		t.Fatal(e.status(d))
	}
	e.public("GET", tokens[1], "/final", nil, 404)

	e.h.finalizeDue(ctx)
	described := e.as(owner, "GET", "/api/signing/documents/"+d.id, nil, 200)
	document := obj(described.body["document"])
	if document["status"] != "completed" || str(document["finalNodeId"]) == "" {
		t.Fatalf("not completed: %s", described.raw)
	}
	var name string
	if err := e.db.W.QueryRow(`SELECT name FROM nodes WHERE id=?`, document["finalNodeId"]).Scan(&name); err != nil || name != "lease (signed).pdf" {
		t.Fatal(name, err)
	}
	final := e.as(owner, "GET", "/api/signing/documents/"+d.id+"/final", nil, 200)
	if signed, intact := signpdf.Verify(final.raw); !signed || !intact {
		t.Fatalf("final seal: %v %v", signed, intact)
	}
	if final.header.Get("ETag") != `"`+str(document["finalSha256"])+`"` {
		t.Fatal("etag")
	}
	again, sha, err := e.h.build(ctx, mustDocument(e, d.id), true)
	if err != nil || len(again) == 0 || sha == "" {
		t.Fatal(err)
	}
	clean := e.public("GET", tokens[0], "/final?certificate=0", nil, 200)
	if _, intact := signpdf.Verify(clean.raw); !intact || bytes.Equal(clean.raw, final.raw) || !strings.Contains(clean.header.Get("Content-Disposition"), "no certificate") {
		t.Fatal("clean variant")
	}
	second := e.as(owner, "GET", "/api/signing/nodes/"+str(document["finalNodeId"])+"/clean", nil, 200)
	if !bytes.Equal(second.raw, clean.raw) {
		t.Fatal("the clean variant is built once")
	}
	e.as(owner, "GET", "/api/signing/documents/"+d.id+"/final?certificate=2", nil, 400)
	e.as(owner, "GET", "/api/signing/documents/"+d.id+"/final", nil, 200)
	if n := e.count(`SELECT COUNT(*) FROM sign_events WHERE document_id=? AND type='final_downloaded' AND party_id IS NULL`, d.id); n != 2 {
		t.Fatalf("downloads logged: %d", n)
	}

	completion := e.deliverMail()
	if len(completion) != 3 {
		t.Fatalf("completion mails: %d", len(completion))
	}
	for _, m := range completion {
		if len(m.message.Attachments) != 1 || !bytes.Equal(m.message.Attachments[0].Content, final.raw) || m.message.Attachments[0].Name != "lease (signed).pdf" {
			t.Fatalf("attachment for %s: %+v", m.to, m.message.Paragraphs)
		}
		if m.to == "alice@example.com" && strings.Contains(mailBody(m.message), tokens[1]) {
			t.Fatal("another party's link in a mail")
		}
	}

	events := e.as(owner, "GET", "/api/signing/documents/"+d.id+"/events", nil, 200)
	if obj(events.body["verify"])["chainValid"] != true {
		t.Fatalf("chain: %s", events.raw)
	}
	if _, err := e.db.W.Exec(`UPDATE sign_events SET ip='203.0.113.9' WHERE document_id=? AND type='consented'`, d.id); err != nil {
		t.Fatal(err)
	}
	events = e.as(owner, "GET", "/api/signing/documents/"+d.id+"/events", nil, 200)
	if obj(events.body["verify"])["chainValid"] != false {
		t.Fatal("a changed entry went unnoticed")
	}
	export := e.as(owner, "GET", "/api/signing/documents/"+d.id+"/audit.json", nil, 200)
	if export.body["kind"] != "neocloud-signing-audit" || bytes.Contains(export.raw, []byte(tokens[0])) || bytes.Contains(export.raw, []byte("token_")) {
		t.Fatal("audit export")
	}

	e.as(owner, "DELETE", "/api/signing/documents/"+d.id, nil, 200)
	if n := e.count(`SELECT COUNT(*) FROM blobs WHERE user_id=? AND id NOT IN (SELECT blob_id FROM nodes WHERE blob_id IS NOT NULL)`, owner); n != 0 {
		t.Fatalf("%d blobs left behind", n)
	}
	if e.count(`SELECT used_bytes FROM users WHERE id=?`, owner) != e.count(`SELECT COALESCE(SUM(size),0) FROM blobs WHERE user_id=?`, owner) {
		t.Fatal("quota accounting")
	}
}

func mustDocument(e *env, id string) document {
	d, err := documentByID(context.Background(), e.db.W, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

// The seal certificate of an existing database is used as it is, never replaced.
func TestExistingSealKeyIsReused(t *testing.T) {
	e := newEnv(t)
	p12, err := os.ReadFile("signpdf/testdata/node-seal.p12")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := os.ReadFile("signpdf/testdata/node-seal.passphrase")
	if err != nil {
		t.Fatal(err)
	}
	sealedP12, _ := e.h.keys.EncryptValue(base64Std(p12))
	sealedSecret, _ := e.h.keys.EncryptValue(string(secret))
	if _, err := e.db.W.Exec(`INSERT INTO sign_seal_keys(id,p12_enc,passphrase_enc,created_at) VALUES(1,?,?,'2026-01-01T00:00:00.000Z')`, sealedP12, sealedSecret); err != nil {
		t.Fatal(err)
	}
	if signerKey, err := e.h.sealer(context.Background()); err != nil || signerKey == nil {
		t.Fatal(err)
	}
	var stored string
	if err := e.db.W.QueryRow(`SELECT p12_enc FROM sign_seal_keys`).Scan(&stored); err != nil || stored != sealedP12 {
		t.Fatal("the seal key was replaced")
	}
}

func TestOtherAccountsReachNothing(t *testing.T) {
	e := newEnv(t)
	e.mailOn = true
	d := e.draft(owner, "secret", twoParties(), signatureField(0, 0.5), signatureField(1, 0.7))
	mine := e.draft(other, "mine", []map[string]any{{"name": "Me"}}, signatureField(0, 0.5))
	tokens := e.send(owner, d, map[string]any{"deliverEmail": false})
	e.consent(tokens[0], "Alice")
	e.upload(tokens[0], d.fields[0], "method=draw", "image/png", samplePNG(), 201)
	base := "/api/signing/documents/" + d.id
	node := e.file(owner, "/own.pdf", samplePDF(t))
	for _, call := range []struct{ method, path string }{
		{"GET", base}, {"PUT", base}, {"DELETE", base}, {"PUT", base + "/fields"}, {"PUT", base + "/parties"},
		{"POST", base + "/import-fields"}, {"POST", base + "/send"}, {"POST", base + "/cancel"}, {"POST", base + "/duplicate"}, {"POST", base + "/finalize"},
		{"POST", base + "/parties/" + d.parties[0] + "/link"}, {"POST", base + "/parties/" + d.parties[0] + "/revoke"},
		{"POST", base + "/parties/" + d.parties[0] + "/remind"}, {"POST", base + "/remind-all"}, {"POST", base + "/parties/" + d.parties[0] + "/open-as-owner"},
		{"GET", base + "/pdf"}, {"GET", base + "/preview-values"}, {"GET", base + "/values/" + d.fields[0] + "/image"}, {"GET", base + "/events"},
		{"GET", base + "/audit.json"}, {"GET", base + "/final"}, {"GET", base + "/final?certificate=0"},
		{"GET", "/api/signing/nodes/" + node + "/clean"}, {"GET", "/api/signing/inbox/" + d.parties[0] + "/final"}, {"POST", "/api/signing/inbox/" + d.parties[0] + "/open"},
		// A party of another document under the caller's own document.
		{"POST", "/api/signing/documents/" + mine.id + "/parties/" + d.parties[0] + "/link"},
		{"GET", "/api/signing/documents/" + mine.id + "/values/" + d.fields[0] + "/image"},
	} {
		out := e.request(call.method, call.path, map[string]string{"X-User": "2", "Content-Type": "application/json"}, []byte(`{}`))
		if out.status != 404 && !(out.status == 409 && strings.Contains(call.path, mine.id)) {
			t.Errorf("%s %s = %d %s", call.method, call.path, out.status, out.raw)
		}
	}
	e.as(other, "POST", "/api/signing/documents", map[string]any{"nodeId": node}, 404)
	if items := list(e.as(other, "GET", "/api/signing/documents", nil, 200).body["items"]); len(items) != 1 {
		t.Fatal("list leaks")
	}
	// Ids of another document reach nothing through a link either.
	e.public("PUT", tokens[0], "/fields/"+mine.fields[0], map[string]any{"value": nil}, 404)
	e.public("GET", tokens[0], "/values/"+mine.fields[0]+"/image", nil, 404)
	// A desktop token is not accepted, the phone app is.
	e.request("GET", "/api/signing/documents", nil, nil)
	if out := e.request("GET", "/api/signing/documents", map[string]string{"X-User": "1", "X-Platform": "macos"}, nil); out.status != 403 || out.body["code"] != "PHONE_REQUIRED" {
		t.Fatal(out.status)
	}
	if out := e.request("GET", "/api/signing/documents", map[string]string{"X-User": "1", "X-Platform": "ios"}, nil); out.status != 200 {
		t.Fatal(out.status)
	}
	if out := e.request("GET", "/api/signing/documents", nil, nil); out.status != 401 {
		t.Fatal(out.status)
	}
}

func TestLinksLifecycleAndGuessing(t *testing.T) {
	e := newEnv(t)
	d := e.draft(owner, "doc", twoParties(), signatureField(0, 0.5), signatureField(1, 0.7))
	e.as(owner, "PUT", "/api/signing/documents/"+d.id, map[string]any{"signingOrder": "random"}, 200)
	tokens := e.send(owner, d, nil)
	if n := e.count(`SELECT COUNT(*) FROM sign_parties WHERE token_hash=? OR token_enc LIKE '%'||?||'%'`, tokens[0], tokens[0]); n != 0 {
		t.Fatal("a token is stored in the clear")
	}
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/send", nil, 409)
	// Unopened: fields can still change. Opened: they lock.
	e.as(owner, "PUT", "/api/signing/documents/"+d.id, map[string]any{"title": "New"}, 200)
	e.consent(tokens[0], "Alice")
	e.public("GET", tokens[0], "", nil, 200)
	e.as(owner, "PUT", "/api/signing/documents/"+d.id, map[string]any{"title": "Late"}, 409)

	renewed := e.as(owner, "POST", "/api/signing/documents/"+d.id+"/parties/"+d.parties[0]+"/link", nil, 200)
	url := str(renewed.body["url"])
	fresh := url[strings.LastIndexByte(url, '/')+1:]
	e.public("GET", tokens[0], "", nil, 404)
	view := e.public("GET", fresh, "", nil, 200)
	if obj(view.body["party"])["consentedAt"] != nil || obj(view.body["party"])["signerName"] != nil {
		t.Fatal("consent carried over to a new link")
	}
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/parties/"+d.parties[1]+"/revoke", nil, 200)
	e.public("GET", tokens[1], "", nil, 404)
	e.public("GET", strings.Repeat("a", 43), "", nil, 404)
	e.public("GET", "short", "", nil, 404)

	e.public("POST", fresh, "/decline", map[string]any{"reason": "No\x07 thanks"}, 200)
	if e.status(d) != "declined" {
		t.Fatal(e.status(d))
	}
	closed := e.public("GET", fresh, "", nil, 200)
	if closed.body["state"] != "declined" || len(obj(closed.body["document"])) != 1 {
		t.Fatalf("closed: %s", closed.raw)
	}

	expiring := e.draft(owner, "old", []map[string]any{{"name": "A"}}, signatureField(0, 0.5))
	old := e.send(owner, expiring, nil)
	e.clock = e.clock.Add(31 * 24 * time.Hour)
	if out := e.public("GET", old[0], "", nil, 200); out.body["state"] != "expired" {
		t.Fatal(string(out.raw))
	}
	if e.status(expiring) != "expired" {
		t.Fatal("expiry not recorded")
	}

	canceled := e.draft(owner, "stop", []map[string]any{{"name": "A"}}, signatureField(0, 0.5))
	stopped := e.send(owner, canceled, nil)
	e.as(owner, "DELETE", "/api/signing/documents/"+canceled.id, nil, 409)
	e.as(owner, "POST", "/api/signing/documents/"+canceled.id+"/cancel", nil, 200)
	e.public("POST", stopped[0], "/consent", map[string]any{"accepted": true, "signerName": "A"}, 409)
	copied := e.as(owner, "POST", "/api/signing/documents/"+canceled.id+"/duplicate", nil, 201)
	if obj(copied.body["document"])["status"] != "draft" || len(list(copied.body["fields"])) != 1 {
		t.Fatalf("duplicate: %s", copied.raw)
	}

	// An address that keeps guessing is slowed down.
	last := 0
	for range maxGuesses + 5 {
		last = e.request("GET", "/api/public/sign/"+strings.Repeat("b", 43), nil, nil).status
	}
	if last != 429 {
		t.Fatalf("guessing was not slowed: %d", last)
	}
}

func TestFieldValuesAreCheckedByType(t *testing.T) {
	for _, c := range []struct {
		kind      string
		prefs     map[string]any
		good, bad any
	}{
		{"text", map[string]any{"maxLength": 5.0}, "abc", "abcdef"},
		{"text", map[string]any{"format": "email"}, "a@b.co", "nope"},
		{"number", map[string]any{"min": 1.0, "max": 9.0, "decimals": 1.0}, "2.5", "2.55"},
		{"date", nil, "2026-02-28", "2026-02-30"},
		{"phone", nil, "+43 660 1234567", "12"},
		{"checkbox", nil, true, "yes"},
		{"radio", map[string]any{"options": []any{"a"}}, "a", "b"},
		{"multiple", map[string]any{"options": []any{"a", "b"}, "maxSelect": 1.0}, []any{"b"}, []any{"a", "b"}},
		{"cells", map[string]any{"cells": 3.0, "charset": "digits"}, "12", "1a"},
		{"signature", nil, nil, "x"},
	} {
		if _, err := normalizeValue(c.kind, c.prefs, c.good); err != nil {
			t.Errorf("%s refused %v: %v", c.kind, c.good, err)
		}
		if _, err := normalizeValue(c.kind, c.prefs, c.bad); err == nil {
			t.Errorf("%s accepted %v", c.kind, c.bad)
		}
	}
	for _, name := range []string{"constructor", "__proto__", "toString"} {
		if _, err := normalizePrefs("text", map[string]any{name: 1.0}); err == nil {
			t.Error("accepted preference", name)
		}
	}
}

func TestFormImportKeepsChoicesAndSignature(t *testing.T) {
	e := newEnv(t)
	form, err := os.ReadFile("signpdf/testdata/form.pdf")
	if err != nil {
		t.Fatal(err)
	}
	node := e.file(owner, "/form.pdf", form)
	created := e.as(owner, "POST", "/api/signing/documents", map[string]any{"nodeId": node}, 201)
	if len(list(created.body["warnings"])) != 1 {
		t.Fatalf("warnings: %s", created.raw)
	}
	id := str(obj(created.body["document"])["id"])
	imported := e.as(owner, "POST", "/api/signing/documents/"+id+"/import-fields", nil, 200)
	kinds := map[string]map[string]any{}
	for _, f := range list(imported.body["fields"]) {
		kinds[str(obj(f)["type"])] = obj(f)
	}
	for _, kind := range []string{"text", "cells", "checkbox", "radio", "select", "multiple", "signature"} {
		if kinds[kind] == nil {
			t.Errorf("no %s field imported: %s", kind, imported.raw)
		}
	}
	if radio := kinds["radio"]; radio != nil && obj(list(radio["areas"])[0])["option"] != "red" {
		t.Errorf("radio areas: %v", radio["areas"])
	}
}
