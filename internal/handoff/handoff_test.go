package handoff

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/NeoLabs-Systems/neocloud/internal/cryptox"
	"github.com/NeoLabs-Systems/neocloud/internal/files"
	"github.com/NeoLabs-Systems/neocloud/internal/schema"
	"github.com/NeoLabs-Systems/neolabs-go/dbx"
)

type identityKey struct{}
type testIdentity struct {
	principal Principal
	key       string
}

var (
	laptop  = testIdentity{Principal{UserID: 1, DeviceID: 1, IsDevice: true, Platform: "macos"}, "d:1"}
	phone   = testIdentity{Principal{UserID: 1, DeviceID: 2, IsDevice: true, Platform: "ios"}, "d:2"}
	tablet  = testIdentity{Principal{UserID: 1, DeviceID: 3, IsDevice: true, Platform: "android"}, "d:3"}
	browser = testIdentity{Principal{UserID: 1}, "s:abcdefabcdefabcdefabcdef"}
	foreign = testIdentity{Principal{UserID: 2, DeviceID: 4, IsDevice: true, Platform: "linux"}, "d:4"}
)

func setup(t *testing.T) (*dbx.DB, *Handler, *http.ServeMux, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := dbx.Open(ctx, filepath.Join(dir, "handoff.sqlite"), dbx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = schema.Migrate(ctx, db, filepath.Join(dir, "backup")); err != nil {
		t.Fatal(err)
	}
	keys, err := cryptox.NewKeyring(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2} {
		wrapped, err := keys.WrapDEK(id, bytes.Repeat([]byte{byte(40 + id)}, 32))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.W.Exec(`INSERT INTO users(id,username,password,wrapped_dek,quota_bytes) VALUES(?,?,'x',?,10000000)`, id, "user"+strconv.FormatInt(id, 10), wrapped); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.W.Exec(`INSERT INTO device_tokens(id,user_id,name,platform,token_hash,last_seen_at) VALUES
		(1,1,'Laptop','macos','h1',datetime('now')),(2,1,'Phone','ios','h2',datetime('now')),
		(3,1,'Tablet','android','h3',datetime('now')),(4,2,'Theirs','linux','h4',datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	blobs := filepath.Join(dir, "blobs")
	if err = os.MkdirAll(blobs, 0o700); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{
		DB: db.W, Keys: keys, BlobDir: blobs,
		Files: files.New(files.Options{DB: db.W, Keys: keys, BlobDir: blobs, DefaultQuotaBytes: 10000000, MaxNodes: 1000}),
		ResolvePrincipal: func(r *http.Request) (Principal, bool) {
			v, ok := r.Context().Value(identityKey{}).(testIdentity)
			return v.principal, ok
		},
		RequestKey: func(r *http.Request, _ Principal) string {
			return r.Context().Value(identityKey{}).(testIdentity).key
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	return db, h, mux, blobs
}

func call(mux *http.ServeMux, who testIdentity, method, path, body string, header ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, who))
	for i := 0; i+1 < len(header); i += 2 {
		r.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func expect(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || code != "" && !strings.Contains(w.Body.String(), `"code":"`+code+`"`) {
		t.Fatalf("status=%d body=%s, want %d %s", w.Code, w.Body.String(), status, code)
	}
}

func decodeOffer(t *testing.T, w *httptest.ResponseRecorder) offer {
	t.Helper()
	var o offer
	if err := json.Unmarshal(w.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	return o
}

type received struct {
	mu     sync.Mutex
	events []string
	closed int
}

func (r *received) all() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.events, "\n")
}

func online(h *Handler, userID int64, key string, receive bool) (*received, func()) {
	log := &received{}
	return log, h.presence.connect(userID, &connection{
		key: key, canReceive: receive,
		push: func(event string, payload any) {
			data, _ := json.Marshal(payload)
			log.mu.Lock()
			defer log.mu.Unlock()
			log.events = append(log.events, event+" "+string(data))
		},
		close: func() { log.closed++ },
	})
}

// send uploads contents from the laptop to the phone and returns the ready send.
func send(t *testing.T, mux *http.ServeMux, contents ...string) offer {
	t.Helper()
	declared := []string{}
	for i, content := range contents {
		declared = append(declared, `{"name":"`+string(rune('a'+i))+`.txt","size":`+strconv.Itoa(len(content))+`}`)
	}
	created := call(mux, laptop, "POST", "/api/handoff/offers", `{"targetId":"d:2","files":[`+strings.Join(declared, ",")+`]}`)
	expect(t, created, 201, "")
	o := decodeOffer(t, created)
	for i, content := range contents {
		expect(t, call(mux, laptop, "PUT", "/api/handoff/offers/"+o.ID+"/files/"+strconv.FormatInt(o.Files[i].ID, 10), content), 200, "")
	}
	return decodeOffer(t, call(mux, laptop, "GET", "/api/handoff/offers/"+o.ID, ""))
}

func status(t *testing.T, mux *http.ServeMux, id string) string {
	t.Helper()
	return decodeOffer(t, call(mux, laptop, "GET", "/api/handoff/offers/"+id, "")).Status
}

func used(t *testing.T, db *dbx.DB) int64 {
	t.Helper()
	var n int64
	if err := db.W.QueryRow(`SELECT used_bytes FROM users WHERE id=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSendReachesOnlyItsTargetAndIsReleasedWhenDelivered(t *testing.T) {
	db, h, mux, _ := setup(t)
	sender, _ := online(h, 1, "d:1", true)
	receiver, _ := online(h, 1, "d:2", true)
	bystander, _ := online(h, 1, "d:3", true)
	stranger, _ := online(h, 2, "d:4", true)

	o := send(t, mux, "hello world", "second")
	if o.Status != "pending" || used(t, db) == 0 {
		t.Fatalf("offer=%+v used=%d", o, used(t, db))
	}
	if got := receiver.all(); strings.Count(got, "handoff:offer ") != 1 || !strings.Contains(got, `"role":"receiver"`) {
		t.Fatalf("receiver events: %s", got)
	}
	if strings.Contains(sender.all(), "handoff:offer ") || strings.Contains(bystander.all(), o.ID) || strings.Contains(stranger.all(), o.ID) {
		t.Fatal("the send leaked to another device")
	}
	base := "/api/handoff/offers/" + o.ID
	file := base + "/files/" + strconv.FormatInt(o.Files[0].ID, 10)
	expect(t, call(mux, tablet, "GET", base, ""), 404, "NOT_FOUND")
	expect(t, call(mux, tablet, "POST", base+"/accept", ""), 404, "NOT_FOUND")
	expect(t, call(mux, phone, "GET", file, ""), 409, "NOT_ACCEPTED")
	expect(t, call(mux, phone, "POST", base+"/accept", ""), 200, "")
	for _, other := range []testIdentity{tablet, laptop, browser, foreign} {
		expect(t, call(mux, other, "GET", file, ""), 404, "")
		expect(t, call(mux, other, "GET", base+"/zip", ""), 404, "")
	}
	whole := call(mux, phone, "GET", file, "", "Range", "bytes=6-")
	if whole.Code != 206 || whole.Body.String() != "world" || whole.Header().Get("Content-Range") != "bytes 6-10/11" {
		t.Fatalf("range=%d %q %v", whole.Code, whole.Body.String(), whole.Header())
	}
	expect(t, call(mux, phone, "GET", file, "", "Range", "bytes=50-"), 416, "")
	if status(t, mux, o.ID) != "accepted" {
		t.Fatal("one of two files fetched must not deliver the send")
	}
	archive := call(mux, phone, "GET", base+"/zip", "")
	expect(t, archive, 200, "")
	reader, err := zip.NewReader(bytes.NewReader(archive.Body.Bytes()), int64(archive.Body.Len()))
	if err != nil || len(reader.File) != 2 || reader.File[1].Name != "b.txt" {
		t.Fatalf("zip=%v %v", reader, err)
	}
	entry, err := reader.File[1].Open()
	if err != nil {
		t.Fatal(err)
	}
	if data, err := io.ReadAll(entry); err != nil || string(data) != "second" {
		t.Fatalf("zip entry=%q %v", data, err)
	}
	if status(t, mux, o.ID) != "delivered" {
		t.Fatal("every file fetched must deliver the send")
	}
	expect(t, call(mux, phone, "POST", base+"/complete", ""), 200, "")
	if used(t, db) != 0 {
		t.Fatalf("staged bytes were not released: %d", used(t, db))
	}
	if !strings.Contains(sender.all(), `handoff:updated {"offer":`) || !strings.Contains(sender.all(), `"status":"delivered"`) {
		t.Fatalf("sender events: %s", sender.all())
	}
}

func TestDeclineCancelExpiryAndUploadRules(t *testing.T) {
	db, h, mux, _ := setup(t)
	declined := send(t, mux, "no")
	expect(t, call(mux, phone, "POST", "/api/handoff/offers/"+declined.ID+"/decline", ""), 200, "")
	if status(t, mux, declined.ID) != "declined" || used(t, db) != 0 {
		t.Fatal("decline must release the staged files")
	}
	cancelled := send(t, mux, "x")
	expect(t, call(mux, tablet, "DELETE", "/api/handoff/offers/"+cancelled.ID, ""), 404, "")
	if o := decodeOffer(t, call(mux, laptop, "DELETE", "/api/handoff/offers/"+cancelled.ID, "")); o.Status != "cancelled" {
		t.Fatalf("cancel=%+v", o)
	}
	expect(t, call(mux, phone, "POST", "/api/handoff/offers/"+cancelled.ID+"/accept", ""), 409, "OFFER_CLOSED")

	created := call(mux, laptop, "POST", "/api/handoff/offers", `{"targetId":"d:2","files":[{"name":"big.bin","size":5}]}`)
	expect(t, created, 201, "")
	o := decodeOffer(t, created)
	file := "/api/handoff/offers/" + o.ID + "/files/" + strconv.FormatInt(o.Files[0].ID, 10)
	expect(t, call(mux, phone, "GET", "/api/handoff/offers/"+o.ID, ""), 404, "")
	expect(t, call(mux, phone, "PUT", file, "12345"), 404, "")
	expect(t, call(mux, foreign, "PUT", file, "12345"), 404, "")
	expect(t, call(mux, laptop, "PUT", file, "1234"), 400, "SIZE_MISMATCH")
	expect(t, call(mux, laptop, "PUT", file, "123456"), 400, "SIZE_MISMATCH")
	expect(t, call(mux, laptop, "PUT", file, "12345"), 200, "")
	expect(t, call(mux, laptop, "PUT", file, "12345"), 409, "OFFER_CLOSED")
	expect(t, call(mux, phone, "POST", "/api/handoff/offers/"+o.ID+"/accept", ""), 200, "")
	if _, err := db.W.Exec(`UPDATE handoff_offers SET expires_at=datetime('now','-1 minute') WHERE id=?`, o.ID); err != nil {
		t.Fatal(err)
	}
	expect(t, call(mux, phone, "GET", file, ""), 410, "OFFER_EXPIRED")
	if expired, _, err := h.RunMaintenance(context.Background()); err != nil || expired != 1 {
		t.Fatalf("expired=%d %v", expired, err)
	}
	if status(t, mux, o.ID) != "expired" || used(t, db) != 0 {
		t.Fatal("expiry must release the staged files")
	}
	expect(t, call(mux, phone, "POST", "/api/handoff/offers/"+o.ID+"/complete", ""), 409, "NOT_ACCEPTED")
}

func TestFilesAlreadyStoredAreSentAsSharedCopies(t *testing.T) {
	db, h, mux, _ := setup(t)
	receiver, _ := online(h, 1, "d:2", true)
	blob, err := h.writeBlob(context.Background(), 1, 9, strings.NewReader("quarterly"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.W.Exec(`INSERT INTO blobs(id,user_id,size,sha256,enc_version,ref_count) VALUES(?,1,9,?,1,1);
		UPDATE users SET used_bytes=9 WHERE id=1;
		INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('root',1,NULL,'folder','','r');
		INSERT INTO nodes(id,user_id,parent_id,type,name,size,mime,etag,blob_id) VALUES('file',1,'root','file','report.txt',9,'text/plain','f','`+blob.id+`');
		INSERT INTO nodes(id,user_id,parent_id,type,name,etag) VALUES('theirs-root',2,NULL,'folder','','r2')`, blob.id, blob.sha256); err != nil {
		t.Fatal(err)
	}
	created := call(mux, laptop, "POST", "/api/handoff/offers", `{"targetId":"d:2","nodeIds":["file","file"]}`)
	expect(t, created, 201, "")
	o := decodeOffer(t, created)
	if o.Status != "pending" || len(o.Files) != 1 || used(t, db) != 9 || !strings.Contains(receiver.all(), "handoff:offer ") {
		t.Fatalf("offer=%+v used=%d events=%s", o, used(t, db), receiver.all())
	}
	expect(t, call(mux, phone, "POST", "/api/handoff/offers/"+o.ID+"/accept", ""), 200, "")
	got := call(mux, phone, "GET", "/api/handoff/offers/"+o.ID+"/files/"+strconv.FormatInt(o.Files[0].ID, 10), "")
	if got.Body.String() != "quarterly" || used(t, db) != 9 {
		t.Fatalf("download=%q used=%d", got.Body.String(), used(t, db))
	}
	var references int
	if err = db.W.QueryRow(`SELECT ref_count FROM blobs WHERE id=?`, blob.id).Scan(&references); err != nil || references != 1 {
		t.Fatalf("references=%d %v", references, err)
	}
	expect(t, call(mux, foreign, "POST", "/api/handoff/offers", `{"targetId":"d:4","nodeIds":["file"]}`), 400, "SAME_DEVICE")
	_, _ = db.W.Exec(`INSERT INTO device_tokens(id,user_id,name,platform,token_hash,last_seen_at) VALUES(5,2,'Second','linux','h5',datetime('now'))`)
	expect(t, call(mux, foreign, "POST", "/api/handoff/offers", `{"targetId":"d:5","nodeIds":["file"]}`), 404, "")
}

func TestCapsNamesAndTargets(t *testing.T) {
	_, h, mux, _ := setup(t)
	post := func(who testIdentity, body string) *httptest.ResponseRecorder {
		return call(mux, who, "POST", "/api/handoff/offers", body)
	}
	many := strings.TrimSuffix(strings.Repeat(`{"name":"f","size":1},`, 51), ",")
	expect(t, post(laptop, `{"targetId":"d:2","files":[`+many+`]}`), 400, "TOO_MANY_FILES")
	expect(t, post(laptop, `{"targetId":"d:2","files":[{"name":"huge","size":3221225472}]}`), 413, "SEND_TOO_LARGE")
	expect(t, post(laptop, `{"targetId":"d:2","files":[]}`), 400, "NOTHING_SELECTED")
	expect(t, post(laptop, `{"targetId":"d:2","files":[{"name":"a","size":-1}]}`), 400, "INVALID_SIZE")
	expect(t, post(laptop, `{"targetId":"d:1","files":[{"name":"a","size":1}]}`), 400, "SAME_DEVICE")
	expect(t, post(laptop, `{"targetId":"d:999","files":[{"name":"a","size":1}]}`), 404, "DEVICE_NOT_FOUND")
	expect(t, post(foreign, `{"targetId":"d:2","files":[{"name":"a","size":1}]}`), 404, "DEVICE_NOT_FOUND")
	expect(t, post(laptop, `{"targetId":"d:2","files":[{"name":"a","size":20000000}]}`), 413, "")
	_, off := online(h, 1, "d:3", false)
	expect(t, post(laptop, `{"targetId":"d:3","files":[{"name":"a","size":1}]}`), 409, "DEVICE_NOT_RECEIVING")
	off()
	for range maxOpenOffers {
		expect(t, post(laptop, `{"targetId":"d:2","files":[{"name":"a","size":1}]}`), 201, "")
	}
	expect(t, post(laptop, `{"targetId":"d:2","files":[{"name":"a","size":1}]}`), 429, "TOO_MANY_OPEN_SENDS")
	empty := call(mux, foreign, "GET", "/api/handoff/offers", "")
	if strings.TrimSpace(empty.Body.String()) != `{"inbox":[],"sent":[]}` {
		t.Fatalf("another account sees %s", empty.Body.String())
	}
}

func TestDevicesListOnlineBrowsersAndRecentApps(t *testing.T) {
	db, h, mux, _ := setup(t)
	hash := strings.TrimPrefix(browser.key, "s:") + "0000"
	if _, err := db.W.Exec(`INSERT INTO user_sessions(user_id,session_hash,user_agent,expires_at) VALUES(1,?,'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari/605.1.15',datetime('now','+1 day'))`, hash); err != nil {
		t.Fatal(err)
	}
	log := &received{}
	end := h.AttachBrowser(1, hash, "", func(event string, _ any) { log.events = append(log.events, event) }, func() {})
	defer end()
	var list struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(call(mux, phone, "GET", "/api/handoff/devices", "").Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, d := range list.Devices {
		names = append(names, d["name"].(string))
		if d["id"] == "d:2" && d["current"] != true || d["id"] == browser.key && (d["online"] != true || d["kind"] != "browser") {
			t.Fatalf("device=%v", d)
		}
	}
	if !slices.Equal(names, []string{"Safari on macOS", "Tablet", "Phone", "Laptop"}) {
		t.Fatalf("names=%v", names)
	}
	if !slices.Contains(log.events, "handoff:devices") {
		t.Fatalf("browser events=%v", log.events)
	}
	// A send waiting for the browser reaches it when it reconnects.
	created := call(mux, phone, "POST", "/api/handoff/offers", `{"targetId":"`+browser.key+`","files":[{"name":"a","size":1}]}`)
	expect(t, created, 201, "")
	o := decodeOffer(t, created)
	expect(t, call(mux, phone, "PUT", "/api/handoff/offers/"+o.ID+"/files/"+strconv.FormatInt(o.Files[0].ID, 10), "x"), 200, "")
	again := &received{}
	defer h.AttachBrowser(1, hash, "", func(event string, _ any) { again.events = append(again.events, event) }, func() {})()
	if !slices.Contains(again.events, "handoff:offer") {
		t.Fatalf("reconnect events=%v", again.events)
	}
}

func TestRevokingADeviceCancelsItsSendsAndClosesItsConnection(t *testing.T) {
	db, h, mux, _ := setup(t)
	doomed, _ := online(h, 1, "d:2", true)
	o := send(t, mux, "bye")
	if _, err := db.W.Exec(`UPDATE device_tokens SET revoked_at=datetime('now') WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if err := h.CancelRevokedDeviceOffers(context.Background(), 1, 2, 0); err != nil {
		t.Fatal(err)
	}
	if doomed.closed != 1 || status(t, mux, o.ID) != "cancelled" || used(t, db) != 0 {
		t.Fatalf("closed=%d status=%s used=%d", doomed.closed, status(t, mux, o.ID), used(t, db))
	}
}

func TestEventStreamAnnouncesPresenceDeliversAndEndsOnRevocation(t *testing.T) {
	db, h, mux, _ := setup(t)
	h.recheck = 20 * time.Millisecond
	waiting := send(t, mux, "later")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, phone)))
	}))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(server.URL + "/api/handoff/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var stream strings.Builder
	buffer := make([]byte, 4096)
	for !strings.Contains(stream.String(), "event: handoff:offer\ndata: ") || !strings.Contains(stream.String(), waiting.ID) {
		n, err := response.Body.Read(buffer)
		stream.Write(buffer[:n])
		if err != nil {
			t.Fatalf("stream ended: %v\n%s", err, stream.String())
		}
	}
	if !h.presence.isOnline(1, "d:2") {
		t.Fatal("the stream must put its device online")
	}
	if _, err = db.W.Exec(`UPDATE device_tokens SET revoked_at=datetime('now') WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadAll(response.Body); err != nil {
		t.Fatalf("a revoked device's stream must end: %v", err)
	}
}

func TestNames(t *testing.T) {
	for raw, want := range map[string]string{
		"../../etc/passwd": "_.._etc_passwd", "a\\b/c.txt": "a_b_c.txt", "inv\u202eoice.exe": "invoice.exe",
		"bad\x00name\n.txt": "badname.txt", "report. . ": "report", ".hidden": "hidden", "   ": "file", "..": "file", "nul.txt": "_nul.txt",
	} {
		if got := sanitiseFileName(raw); got != want {
			t.Errorf("%q = %q, want %q", raw, got, want)
		}
	}
	long := sanitiseFileName(strings.Repeat("ä", 300) + ".pdf")
	if len(long) > 200 || !strings.HasSuffix(long, ".pdf") || !utf8.ValidString(long) {
		t.Errorf("long name %q", long)
	}
	if got := uniqueFileNames([]string{"a.txt", "A.txt", "a.txt", "b"}); !slices.Equal(got, []string{"a.txt", "A (1).txt", "a (2).txt", "b"}) {
		t.Errorf("unique=%v", got)
	}
	if browserLabel("") != "Browser on this device" {
		t.Error("empty user agent")
	}
}
