package signing

import (
	"context"
	"errors"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

func (e *env) raw(user int64, method, path string, content []byte, want int) response {
	e.t.Helper()
	out := e.request(method, path, map[string]string{"X-User": map[int64]string{owner: "1", other: "2", signer: "3"}[user], "Content-Type": "image/png"}, content)
	if out.status != want {
		e.t.Fatalf("%s %s = %d %s, want %d", method, path, out.status, out.raw, want)
	}
	return out
}

func TestProfilePrefillInboxGrantsAndAutosave(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	fresh := e.as(signer, "GET", "/api/signing/profile", nil, 200)
	if fresh.body["displayName"] != "signer" || fresh.body["autoFill"] != true || fresh.body["saveFolderId"] != nil {
		t.Fatalf("defaults: %s", fresh.raw)
	}
	e.as(signer, "PUT", "/api/signing/profile", map[string]any{"email": "nope"}, 400)
	e.as(signer, "PUT", "/api/signing/profile", map[string]any{"saveFolderId": "00000000-0000-0000-0000-000000000001"}, 400)
	e.as(signer, "PUT", "/api/signing/profile", map[string]any{"presets": []any{map[string]any{"label": "Ä", "value": "1"}, map[string]any{"label": "a", "value": "2"}}}, 400)
	e.as(signer, "PUT", "/api/signing/profile", map[string]any{"displayName": "Sig Ner", "phone": "+43 1 234567", "presets": []any{map[string]any{"label": "Firma", "value": "Acme"}}}, 200)

	image := e.raw(signer, "POST", "/api/signing/profile/images?kind=signature&method=draw&label=Mine", samplePNG(), 201)
	imageID := str(image.body["id"])
	if image.body["isDefault"] != true {
		t.Fatalf("image: %s", image.raw)
	}
	for range 4 {
		e.raw(signer, "POST", "/api/signing/profile/images?kind=signature", samplePNG(), 201)
	}
	e.raw(signer, "POST", "/api/signing/profile/images?kind=signature", samplePNG(), 409)
	e.raw(signer, "POST", "/api/signing/profile/images?kind=photo", samplePNG(), 400)
	e.raw(other, "GET", "/api/signing/profile/images/"+imageID, nil, 404)
	e.raw(other, "DELETE", "/api/signing/profile/images/"+imageID, nil, 404)
	if got := e.raw(signer, "GET", "/api/signing/profile/images/"+imageID, nil, 200); got.header.Get("Content-Type") != "image/png" {
		t.Fatal("image type")
	}

	d := e.draft(owner, "contract", []map[string]any{{"name": "S", "email": "signer@example.com"}, {"name": "Me", "isSelf": true}},
		signatureField(0, 0.5), textField(0, "Vollständiger Name", 0.2), textField(0, "Company", 0.3), textField(0, "Name of the bank", 0.4),
		fieldSpec{0, map[string]any{"type": "date", "name": "Date", "prefs": map[string]any{"autoToday": true}, "areas": area(0.1, 0.8)}},
		signatureField(1, 0.7))
	tokens := e.send(owner, d, nil)

	// The inbox lists requests for a verified address only.
	inbox := e.as(signer, "GET", "/api/signing/inbox", nil, 200)
	if len(list(inbox.body["items"])) != 1 || obj(inbox.body["counts"])["todo"] != float64(1) {
		t.Fatalf("inbox: %s", inbox.raw)
	}
	if _, err := e.db.W.Exec(`UPDATE users SET email_verified_at=NULL WHERE id=?`, signer); err != nil {
		t.Fatal(err)
	}
	if items := list(e.as(signer, "GET", "/api/signing/inbox", nil, 200).body["items"]); len(items) != 0 {
		t.Fatal("an unverified address entitled the account")
	}
	if _, err := e.db.W.Exec(`UPDATE users SET email_verified_at=datetime('now') WHERE id=?`, signer); err != nil {
		t.Fatal(err)
	}

	preview := e.as(signer, "POST", "/api/signing/claim", map[string]any{"token": tokens[0], "preview": true}, 200)
	if len(list(preview.body["prefill"])) != 4 || len(list(preview.body["applied"])) != 0 || e.count(`SELECT COUNT(*) FROM sign_parties WHERE user_id=?`, signer) != 0 {
		t.Fatalf("preview: %s", preview.raw)
	}
	claimed := e.as(signer, "POST", "/api/signing/claim", map[string]any{"token": tokens[0], "apply": true, "today": "2026-11-05"}, 200)
	if len(list(claimed.body["applied"])) != 4 {
		t.Fatalf("claim: %s", claimed.raw)
	}
	e.as(other, "POST", "/api/signing/claim", map[string]any{"token": tokens[0]}, 409)
	view := e.public("GET", tokens[0], "", nil, 200)
	values := obj(view.body["myValues"])
	if values[d.fields[1]] != "Sig Ner" || values[d.fields[2]] != "Acme" || values[d.fields[3]] != nil || values[d.fields[4]] != "2026-11-05" {
		t.Fatalf("values: %v", values)
	}
	e.public("POST", tokens[0], "/complete", nil, 409) // consent is still needed

	open := e.as(signer, "POST", "/api/signing/inbox/"+d.parties[0]+"/open", nil, 200)
	grant := strings.TrimPrefix(str(open.body["url"]), "/app/sign/")
	if grant == tokens[0] {
		t.Fatal("the party's own token was handed out")
	}
	e.consent(grant, "Sig Ner")
	e.public("PUT", grant, "/fields/"+d.fields[3], map[string]any{"value": "Some Bank"}, 200)
	e.public("POST", grant, "/complete", nil, 200)
	e.public("GET", grant, "", nil, 404)
	e.as(signer, "GET", "/api/signing/inbox/"+d.parties[0]+"/final", nil, 404)

	// The owner signs their own party through a grant; no second "self".
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/parties/"+d.parties[0]+"/open-as-owner", nil, 409)
	self := e.as(owner, "POST", "/api/signing/documents/"+d.id+"/parties/"+d.parties[1]+"/open-as-owner", nil, 200)
	selfGrant := strings.TrimPrefix(str(self.body["url"]), "/app/sign/")
	e.consent(selfGrant, "Me")
	e.upload(selfGrant, d.fields[5], "method=draw", "image/png", samplePNG(), 201)
	e.public("POST", selfGrant, "/complete", nil, 200)

	e.h.finalizeDue(ctx)
	if e.status(d) != "completed" {
		t.Fatal(e.status(d))
	}
	var name, folder string
	if err := e.db.W.QueryRow(`SELECT n.name,p.name FROM nodes n JOIN nodes p ON p.id=n.parent_id WHERE n.user_id=? AND n.type='file'`, signer).Scan(&name, &folder); err != nil {
		t.Fatal("not saved to the signer's cloud:", err)
	}
	if name != "contract (signed).pdf" || folder != "Signed documents" {
		t.Fatal(name, folder)
	}
	e.h.retryAutosaves(ctx)
	if e.count(`SELECT COUNT(*) FROM nodes WHERE user_id=? AND type='file'`, signer) != 1 {
		t.Fatal("saved twice")
	}
	done := e.as(signer, "GET", "/api/signing/inbox?status=done", nil, 200)
	item := obj(list(done.body["items"])[0])
	if item["finalAvailable"] != true || str(item["savedNodeId"]) == "" {
		t.Fatalf("done: %s", done.raw)
	}
	e.as(signer, "GET", "/api/signing/inbox/"+d.parties[0]+"/final", nil, 200)
	e.as(other, "GET", "/api/signing/inbox/"+d.parties[0]+"/final", nil, 404)

	// A replaced final file is no longer offered.
	if _, err := e.db.W.Exec(`UPDATE nodes SET etag='changed' WHERE id=(SELECT final_node_id FROM sign_documents WHERE id=?)`, d.id); err != nil {
		t.Fatal(err)
	}
	e.as(owner, "GET", "/api/signing/documents/"+d.id+"/final", nil, 404)
}

func TestFailedFinalizationIsRetried(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.draft(owner, "full", []map[string]any{{"name": "A"}}, textField(0, "Name", 0.5))
	tokens := e.send(owner, d, nil)
	e.consent(tokens[0], "A")
	e.public("PUT", tokens[0], "/fields/"+d.fields[0], map[string]any{"value": "A"}, 200)
	e.public("POST", tokens[0], "/complete", nil, 200)
	if _, err := e.db.W.Exec(`UPDATE users SET quota_bytes=used_bytes WHERE id=?`, owner); err != nil {
		t.Fatal(err)
	}
	// The file service has its own quota check.
	e.h.finalizeDue(ctx)
	described := e.as(owner, "GET", "/api/signing/documents/"+d.id, nil, 200)
	document := obj(described.body["document"])
	if document["status"] != "finalizing" || str(document["finalizeError"]) == "" {
		t.Fatalf("failure: %s", described.raw)
	}
	if _, err := e.db.W.Exec(`UPDATE users SET quota_bytes=NULL WHERE id=?`, owner); err != nil {
		t.Fatal(err)
	}
	e.h.finalizeDue(ctx)
	if e.status(d) != "finalizing" {
		t.Fatal("retried before the pause was over")
	}
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/finalize", nil, 200)
	e.h.finalizeDue(ctx)
	if e.status(d) != "completed" {
		t.Fatal(e.status(d))
	}
	e.as(owner, "POST", "/api/signing/documents/"+d.id+"/finalize", nil, 409)
}

func TestMailLimitsRemindersAndFailures(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.mailOn = true
	send := func(user int64, name, address string, want int) draft {
		d := e.draft(user, name, []map[string]any{{"name": "P", "email": address}}, signatureField(0, 0.5))
		e.as(user, "POST", "/api/signing/documents/"+d.id+"/send", nil, want)
		return d
	}
	if _, err := e.db.W.Exec(`UPDATE users SET email_verified_at=NULL WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	send(other, "unverified", "x@example.com", 403)
	first := send(owner, "one", "same@example.com", 200)
	send(owner, "two", "SAME@example.com", 200)
	send(owner, "three", "same@example.com", 200)
	send(owner, "four", "same@example.com", 429)
	e.as(owner, "POST", "/api/signing/documents/"+first.id+"/cancel", nil, 200)
	e.as(owner, "DELETE", "/api/signing/documents/"+first.id, nil, 200)

	e.deliverMail()
	d := send(owner, "remind", "slow@example.com", 200)
	e.deliverMail()
	remind := "/api/signing/documents/" + d.id + "/parties/" + d.parties[0] + "/remind"
	e.as(owner, "POST", remind, map[string]any{"message": strings.Repeat("x", 1001)}, 400)
	e.as(owner, "POST", remind, map[string]any{"message": "<b>now</b>"}, 202)
	soon := e.as(owner, "POST", remind, nil, 429)
	if soon.body["code"] != "REMINDER_TOO_SOON" || soon.header.Get("Retry-After") == "" || soon.body["retryAfter"] == nil {
		t.Fatalf("too soon: %s", soon.raw)
	}
	reminder := e.deliverMail()
	if len(reminder) != 1 || !strings.HasPrefix(reminder[0].message.Subject, "Reminder:") || strings.Contains(reminder[0].message.Subject, "\n") ||
		strings.Contains(mailBody(reminder[0].message), "<b>now</b></") {
		t.Fatalf("reminder: %+v", reminder)
	}
	all := e.as(owner, "POST", "/api/signing/documents/"+d.id+"/remind-all", nil, 202)
	if obj(list(all.body["results"])[0])["code"] != "REMINDER_TOO_SOON" {
		t.Fatalf("remind-all: %s", all.raw)
	}

	// Failures keep only a code and a sentence, and give up after six tries.
	e.mailErr = &textproto.Error{Code: 450, Msg: "mailbox of slow@example.com busy on mx.internal"}
	e.clock = e.clock.Add(25 * time.Hour)
	e.as(owner, "POST", remind, nil, 202)
	for range 6 {
		e.deliverMail()
		e.clock = e.clock.Add(3 * time.Hour)
	}
	described := e.as(owner, "GET", "/api/signing/documents/"+d.id, nil, 200)
	delivery := obj(obj(list(described.body["parties"])[0])["delivery"])
	if delivery["status"] != "failed" || delivery["errorCode"] != "RATE_LIMITED" || delivery["attempts"] != float64(6) ||
		strings.Contains(string(described.raw), "mx.internal") {
		t.Fatalf("delivery: %s", described.raw)
	}
	if classifyMailError(errors.New("dial tcp: connection refused 550 no such user")) != "INVALID_ADDRESS" {
		t.Fatal("classification")
	}
	e.mailErr = nil

	// Automatic reminders and the expiry warning.
	auto := e.draft(owner, "auto", []map[string]any{{"name": "P", "email": "auto@example.com"}}, signatureField(0, 0.5))
	e.send(owner, auto, map[string]any{"reminderDays": 2})
	e.deliverMail()
	e.clock = e.clock.Add(49 * time.Hour)
	e.h.runReminders(ctx)
	if mails := e.deliverMail(); len(mails) != 1 || !strings.HasPrefix(mails[0].message.Subject, "Reminder:") {
		t.Fatalf("automatic reminder: %+v", mails)
	}
	e.clock = e.clock.Add(30*24*time.Hour - 49*time.Hour - 12*time.Hour)
	e.h.runReminders(ctx)
	e.h.runReminders(ctx)
	warnings := 0
	for _, m := range e.deliverMail() {
		if m.to == "auto@example.com" && strings.Contains(m.message.Subject, "expires soon") {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("expiry warnings: %d", warnings)
	}

	// The admin's switch drops what waits.
	german := e.draft(owner, "de", []map[string]any{{"name": "P", "email": "de@example.com"}}, signatureField(0, 0.5))
	e.as(owner, "PUT", "/api/signing/documents/"+german.id, map[string]any{"locale": "de"}, 200)
	e.send(owner, german, nil)
	e.mailOn = false
	e.h.processMail(ctx)
	if e.count(`SELECT COUNT(*) FROM sign_mail_outbox WHERE document_id=? AND status='canceled'`, german.id) != 1 {
		t.Fatal("queued mail survived the switch")
	}
	e.as(owner, "POST", "/api/signing/documents/"+german.id+"/remind-all", nil, 503)
}
