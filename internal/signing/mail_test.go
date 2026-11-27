package signing

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOwnerNoticesAndLargeCompletionInGerman(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.mailOn = true
	e.h.attachmentLimit = 100
	e.as(owner, "PUT", "/api/signing/profile", map[string]any{"notifyOnEachSignature": true}, 200)
	d := e.draft(owner, "angebot", []map[string]any{{"name": "Erika", "email": "erika@example.com"}, {"name": "Max", "email": "max@example.com"}},
		textField(0, "A", 0.2), textField(1, "B", 0.4))
	e.as(owner, "PUT", "/api/signing/documents/"+d.id, map[string]any{"locale": "de", "signingOrder": "random"}, 200)
	tokens := e.send(owner, d, nil)
	if len(e.deliverMail()) != 2 {
		t.Fatal("everybody is invited at once")
	}
	sign := func(i int) {
		e.consent(tokens[i], "Name")
		e.public("PUT", tokens[i], "/fields/"+d.fields[i], map[string]any{"value": "x"}, 200)
		e.public("POST", tokens[i], "/complete", nil, 200)
	}
	sign(0)
	notice := e.deliverMail()
	if len(notice) != 1 || notice[0].to != "owner@example.com" || !strings.Contains(notice[0].message.Subject, "unterschrieben") ||
		!strings.Contains(mailBody(notice[0].message), "1 von 2") || strings.Contains(mailBody(notice[0].message), tokens[0]) {
		t.Fatalf("notice: %+v", notice)
	}
	sign(1)
	e.h.finalizeDue(ctx)
	mails := e.deliverMail()
	if len(mails) != 3 {
		t.Fatalf("completion: %d", len(mails))
	}
	for _, m := range mails {
		if len(m.message.Attachments) != 0 || !strings.Contains(m.message.Paragraphs[0], "zu groß") || m.message.Lang != "de" {
			t.Fatalf("large file: %+v", m.message)
		}
	}
	var name string
	if err := e.db.W.QueryRow(`SELECT name FROM nodes WHERE id=(SELECT final_node_id FROM sign_documents WHERE id=?)`, d.id).Scan(&name); err != nil || name != "angebot (signiert).pdf" {
		t.Fatal(name, err)
	}

	declined := e.draft(owner, "no", []map[string]any{{"name": "Dora", "email": "dora@example.com"}}, textField(0, "A", 0.2))
	token := e.send(owner, declined, nil)[0]
	e.deliverMail()
	e.public("POST", token, "/decline", map[string]any{"reason": "Too expensive"}, 200)
	notice = e.deliverMail()
	if len(notice) != 1 || notice[0].message.Quote == nil || notice[0].message.Quote.Text != "Too expensive" {
		t.Fatalf("decline notice: %+v", notice)
	}

	expiring := e.draft(owner, "late", []map[string]any{{"name": "L", "email": "late@example.com"}}, textField(0, "A", 0.2))
	e.send(owner, expiring, nil)
	e.clock = e.clock.Add(31 * 24 * time.Hour)
	e.h.runPass(ctx)
	mails = e.deliverMail()
	if len(mails) != 1 || mails[0].to != "owner@example.com" || !strings.HasPrefix(mails[0].message.Subject, "Expired:") {
		t.Fatalf("expiry notice (the queued invitation is dropped): %+v", mails)
	}
}
