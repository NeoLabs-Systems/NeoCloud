package signing

import (
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
)

func TestAgentCreatesDocumentsAndSigningRequests(t *testing.T) {
	e := newEnv(t)
	pdf := e.agentCall(owner, "POST", "/api/v1/documents/pdf", map[string]any{"path": "/Contracts/Offer.pdf", "title": "Offer", "markdown": "# Offer\n\nTerms."}, 201)
	if pdf.body["path"] != "/Contracts/Offer.pdf" || pdf.body["mimeType"] != "application/pdf" {
		t.Fatalf("pdf: %s", pdf.raw)
	}
	e.agentCall(owner, "POST", "/api/v1/documents/pdf", map[string]any{"path": "/Contracts/Offer.pdf", "markdown": "again"}, 409)
	e.agentCall(owner, "POST", "/api/v1/documents/pdf", map[string]any{"path": "/x.pdf"}, 400)
	e.agentCall(owner, "POST", "/api/v1/documents/pdf", map[string]any{"path": "/x.pdf", "markdown": strings.Repeat("a", signpdf.MaxMarkdownBytes+1)}, 413)

	signers := []any{map[string]any{"name": "Ann", "email": "ann@example.com"}, map[string]any{"name": "Ben"}}
	created := e.agentCall(owner, "POST", "/api/v1/signing/requests", map[string]any{
		"document": map[string]any{"path": "/Contracts/NDA.pdf", "title": "NDA", "markdown": "Keep it secret."},
		"signers":  signers, "signingOrder": "sequential", "expiresInDays": 7,
	}, 201)
	views := list(created.body["signers"])
	if created.body["status"] != "pending" || created.body["title"] != "NDA" || len(views) != 2 || str(obj(views[0])["url"]) == "" {
		t.Fatalf("created: %s", created.raw)
	}
	url := str(obj(views[0])["url"])
	view := e.public("GET", url[strings.LastIndexByte(url, '/')+1:], "", nil, 200)
	// Its own three fields and the other signer's read-only name.
	if len(list(view.body["fields"])) != 4 {
		t.Fatalf("fields: %s", view.raw)
	}

	fromFile := e.agentCall(owner, "POST", "/api/v1/signing/requests", map[string]any{"fileId": pdf.body["id"], "signers": signers}, 201)
	if fromFile.body["title"] != "Offer" || e.count(`SELECT COUNT(*) FROM nodes WHERE user_id=? AND name='Offer (for signature).pdf'`, owner) != 1 {
		t.Fatalf("from file: %s", fromFile.raw)
	}
	id := str(created.body["id"])
	listed := e.agentCall(owner, "GET", "/api/v1/signing/requests?status=pending", nil, 200)
	if len(list(listed.body["items"])) != 2 || strings.Contains(string(listed.raw), "/app/sign/") {
		t.Fatalf("list: %s", listed.raw)
	}
	remind := e.agentCall(owner, "POST", "/api/v1/signing/requests/"+id+"/remind", nil, 409)
	if remind.body["code"] != "EMAIL_NOT_CONFIGURED" {
		t.Fatalf("remind: %s", remind.raw)
	}
	link := e.agentCall(owner, "POST", "/api/v1/signing/requests/"+id+"/signers/"+str(obj(views[0])["id"])+"/link", nil, 200)
	if !strings.HasPrefix(str(link.body["url"]), "https://cloud.test/app/sign/") || len(link.body) != 1 {
		t.Fatalf("link: %s", link.raw)
	}
	for _, call := range [][2]string{{"GET", ""}, {"POST", "/cancel"}, {"POST", "/remind"}, {"POST", "/signers/" + str(obj(views[0])["id"]) + "/link"}} {
		e.agentCall(other, call[0], "/api/v1/signing/requests/"+id+call[1], nil, 404)
	}
	e.agentCall(other, "POST", "/api/v1/signing/requests", map[string]any{"fileId": pdf.body["id"], "signers": signers}, 404)
	canceled := e.agentCall(owner, "POST", "/api/v1/signing/requests/"+id+"/cancel", nil, 200)
	if canceled.body["status"] != "cancelled" {
		t.Fatalf("cancel: %s", canceled.raw)
	}

	// A request that cannot be sent leaves no draft behind.
	before := e.count(`SELECT COUNT(*) FROM sign_documents`)
	e.agentCall(owner, "POST", "/api/v1/signing/requests", map[string]any{"fileId": pdf.body["id"], "signers": signers, "message": 5}, 400)
	if e.count(`SELECT COUNT(*) FROM sign_documents`) != before {
		t.Fatal("a half-made draft stayed")
	}
	e.agentCall(owner, "POST", "/api/v1/signing/requests", map[string]any{"signers": signers}, 400)

	e.scopes = []string{"signing:write"}
	denied := e.agentCall(owner, "POST", "/api/v1/signing/requests", map[string]any{"document": map[string]any{"path": "/n.pdf", "markdown": "x"}, "signers": signers}, 403)
	if denied.body["code"] != "INSUFFICIENT_SCOPE" {
		t.Fatalf("scope: %s", denied.raw)
	}
	refused := e.agentCall(owner, "GET", "/api/v1/signing/requests", nil, 403)
	if !strings.Contains(refused.header.Get("WWW-Authenticate"), `scope="signing:read"`) {
		t.Fatal(refused.header)
	}
}
