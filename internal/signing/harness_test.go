package signing

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/agentapi"
	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/oauth"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neocloud/internal/signing/signpdf"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
)

const (
	owner  int64 = 1
	other  int64 = 2
	signer int64 = 3
)

type sentMail struct {
	to      string
	message servicemail.Message
}

type env struct {
	t       *testing.T
	h       *Handler
	db      *dbx.DB
	agent   *agentapi.Handler
	mux     *http.ServeMux
	mu      sync.Mutex
	mails   []sentMail
	mailOn  bool
	mailErr error
	clock   time.Time
	scopes  []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	base := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(base, "neo.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(base, "backups")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("42", 32))
	if err != nil {
		t.Fatal(err)
	}
	blobs := filepath.Join(base, "blobs")
	if err = os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, mux: http.NewServeMux(), clock: time.Now().UTC().Truncate(time.Millisecond),
		scopes: []string{"files:read", "files:write", "signing:read", "signing:write"}}
	for id, name := range map[int64]string{owner: "owner", other: "other", signer: "signer"} {
		if _, err = db.W.Exec(`INSERT INTO users(id,username,password,email,email_verified_at) VALUES(?,?,'x',?,datetime('now'))`, id, name, name+"@example.com"); err != nil {
			t.Fatal(err)
		}
		if _, err = db.W.Exec(`INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES(?,?,NULL,'folder','','root')`, "00000000-0000-0000-0000-00000000000"+strconv.FormatInt(id, 10), id); err != nil {
			t.Fatal(err)
		}
	}
	store := files.New(files.Options{DB: db.W, Keys: keys, BlobDir: blobs, UploadTmpDir: filepath.Join(base, "tmp"),
		PreviewDir: filepath.Join(base, "previews"), MaxNodes: 1000, MaxUploadBytes: 100 << 20, DefaultQuotaBytes: 1 << 30, VersionRetention: 5})
	e.agent, err = agentapi.New(agentapi.Options{DB: db.W, Keys: keys, BlobDir: blobs, Files: store, DefaultQuotaBytes: 1 << 30, MaxUploadBytes: 100 << 20, MaxNodes: 1000,
		Authenticate: func(_ context.Context, token string) (oauth.AccessPrincipal, error) {
			id, err := strconv.ParseInt(token, 10, 64)
			if err != nil {
				return oauth.AccessPrincipal{}, oauth.ErrInvalidAccessToken
			}
			return oauth.AccessPrincipal{UserID: id, ClientID: "agent", Scopes: e.scopes}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	e.h, err = New(Options{
		DB: db.W, Keys: keys, BlobDir: blobs, DefaultQuotaBytes: 1 << 30, Agent: e.agent,
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			id, err := strconv.ParseInt(r.Header.Get("X-User"), 10, 64)
			return Principal{UserID: id, Device: r.Header.Get("X-Platform") != "", Platform: r.Header.Get("X-Platform")}, err == nil
		},
		RemoveBlobs: store.RemoveBlobFiles,
		StoreFile: func(ctx context.Context, userID int64, parentID, name string, content []byte) (string, error) {
			stored, err := store.StoreGeneratedFile(ctx, userID, parentID, name, content)
			return stored.ID, err
		},
		MailConfig: func() servicemail.Config {
			return servicemail.Config{Enabled: true, BrandName: "NeoCloud", PublicURL: "https://cloud.test", From: "NeoCloud <cloud@test.example>"}
		},
		MailEnabled: func() bool { return e.mailOn },
		SendMail: func(_ context.Context, to string, m servicemail.Message) error {
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.mailErr != nil {
				return e.mailErr
			}
			e.mails = append(e.mails, sentMail{to, m})
			return nil
		},
		PublicOrigin: func(*http.Request) string { return "https://cloud.test" },
	})
	if err != nil {
		t.Fatal(err)
	}
	e.h.now = func() time.Time { return e.clock }
	e.h.Routes(e.mux)
	e.agent.Routes(e.mux)
	return e
}

type response struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (e *env) request(method, path string, headers map[string]string, content []byte) response {
	e.t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(content))
	r.RemoteAddr = "192.0.2.10:5000"
	for key, v := range headers {
		r.Header.Set(key, v)
	}
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	out := response{status: w.Code, header: w.Header(), raw: w.Body.Bytes(), body: map[string]any{}}
	_ = json.Unmarshal(out.raw, &out.body)
	return out
}

func jsonBytes(v any) []byte {
	if v == nil {
		return nil
	}
	raw, _ := json.Marshal(v)
	return raw
}

// as calls an owner route as an account and expects a status.
func (e *env) as(user int64, method, path string, input any, want int) response {
	e.t.Helper()
	out := e.request(method, path, map[string]string{"X-User": strconv.FormatInt(user, 10), "Content-Type": "application/json"}, jsonBytes(input))
	if out.status != want {
		e.t.Fatalf("%s %s as %d = %d %s, want %d", method, path, user, out.status, out.raw, want)
	}
	return out
}

func (e *env) public(method, token, path string, input any, want int) response {
	e.t.Helper()
	out := e.request(method, "/api/public/sign/"+token+path, map[string]string{"Content-Type": "application/json"}, jsonBytes(input))
	if out.status != want {
		e.t.Fatalf("%s sign%s = %d %s, want %d", method, path, out.status, out.raw, want)
	}
	return out
}

func (e *env) agentCall(user int64, method, path string, input any, want int) response {
	e.t.Helper()
	out := e.request(method, path, map[string]string{"Authorization": "Bearer " + strconv.FormatInt(user, 10), "Content-Type": "application/json"}, jsonBytes(input))
	if out.status != want {
		e.t.Fatalf("%s %s = %d %s, want %d", method, path, out.status, out.raw, want)
	}
	return out
}

func (e *env) upload(token, fieldID, query, contentType string, content []byte, want int) response {
	e.t.Helper()
	out := e.request("POST", "/api/public/sign/"+token+"/fields/"+fieldID+"/upload?"+query, map[string]string{"Content-Type": contentType}, content)
	if out.status != want {
		e.t.Fatalf("upload = %d %s, want %d", out.status, out.raw, want)
	}
	return out
}

func samplePDF(t *testing.T) []byte {
	t.Helper()
	pdf, _, err := signpdf.RenderMarkdown("Contract", "Please sign this contract.", nil, "en")
	if err != nil {
		t.Fatal(err)
	}
	return pdf
}

func samplePNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 300, 100))
	for x := range 300 {
		for y := range 100 {
			img.Set(x, y, color.NRGBA{20, 20, 90, 255})
		}
	}
	var out bytes.Buffer
	_ = png.Encode(&out, img)
	return out.Bytes()
}

func sampleGIF() []byte {
	var out bytes.Buffer
	_ = gif.Encode(&out, image.NewPaletted(image.Rect(0, 0, 40, 20), color.Palette{color.Black, color.White}), nil)
	return out.Bytes()
}

func (e *env) file(user int64, path string, content []byte) string {
	e.t.Helper()
	id, err := e.agent.CreateAt(context.Background(), user, path, content, false)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

type draft struct {
	id      string
	parties []string
	fields  []string
}

type fieldSpec struct {
	party int
	spec  map[string]any
}

func area(x, y float64) []any {
	return []any{map[string]any{"page": 0, "x": x, "y": y, "w": 0.3, "h": 0.05}}
}

func signatureField(party int, y float64) fieldSpec {
	return fieldSpec{party, map[string]any{"type": "signature", "name": "Signature", "areas": area(0.1, y)}}
}

func textField(party int, name string, y float64) fieldSpec {
	return fieldSpec{party, map[string]any{"type": "text", "name": name, "areas": area(0.5, y)}}
}

// draft makes a draft of a fresh PDF with parties and fields.
func (e *env) draft(user int64, name string, parties []map[string]any, specs ...fieldSpec) draft {
	e.t.Helper()
	node := e.file(user, "/"+name+".pdf", samplePDF(e.t))
	created := e.as(user, "POST", "/api/signing/documents", map[string]any{"nodeId": node}, 201)
	d := draft{id: str(obj(created.body["document"])["id"])}
	saved := e.as(user, "PUT", "/api/signing/documents/"+d.id+"/parties", map[string]any{"parties": parties}, 200)
	for _, p := range list(saved.body["parties"]) {
		d.parties = append(d.parties, str(obj(p)["id"]))
	}
	var fields []any
	for _, f := range specs {
		f.spec["partyId"] = d.parties[f.party]
		fields = append(fields, f.spec)
	}
	stored := e.as(user, "PUT", "/api/signing/documents/"+d.id+"/fields", map[string]any{"fields": fields}, 200)
	for _, f := range list(stored.body["fields"]) {
		d.fields = append(d.fields, str(obj(f)["id"]))
	}
	return d
}

// send sends a draft and returns each party's token.
func (e *env) send(user int64, d draft, options map[string]any) []string {
	e.t.Helper()
	out := e.as(user, "POST", "/api/signing/documents/"+d.id+"/send", options, 200)
	var tokens []string
	for _, link := range list(out.body["links"]) {
		url := str(obj(link)["url"])
		tokens = append(tokens, url[strings.LastIndexByte(url, '/')+1:])
	}
	return tokens
}

func (e *env) consent(token, name string) {
	e.t.Helper()
	e.public("POST", token, "/consent", map[string]any{"accepted": true, "signerName": name}, 200)
}

func (e *env) status(d draft) string {
	e.t.Helper()
	return str(obj(e.as(owner, "GET", "/api/signing/documents/"+d.id, nil, 200).body["document"])["status"])
}

// deliverMail runs the mail worker's send pass and returns what went out.
func (e *env) deliverMail() []sentMail {
	e.t.Helper()
	e.clock = e.clock.Add(time.Second)
	e.h.processMail(context.Background())
	e.mu.Lock()
	defer e.mu.Unlock()
	out := e.mails
	e.mails = nil
	return out
}

func mailBody(m servicemail.Message) string {
	return servicemail.RenderText(m) + servicemail.RenderHTML(m)
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.W.QueryRow(query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}
