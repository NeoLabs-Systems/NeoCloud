package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
)

// fakeSendServer speaks the sender's side of the send protocol: devices,
// create, one PUT per file, status, cancel.
type fakeSendServer struct {
	mu       sync.Mutex
	devices  []api.HandoffDevice
	status   string // what GET /offers/:id answers (after the upload)
	names    []string
	sizes    []int64
	uploaded map[int64]string
	cancels  int
	creates  int
	headers  []http.Header // of the uploads
	statuses int           // GET /offers/:id calls

	createFail func() (int, string, string) // status, code, message; status 0: fine
	putFail    func(fileID int64) int       // status for this upload; 0: fine
	statusFail func(call int) bool          // drop this status check
	// hold blocks an upload (after reading its body) until released or the request ends.
	hold chan struct{}
}

func newFakeSend() *fakeSendServer {
	return &fakeSendServer{
		status:   "pending",
		uploaded: map[int64]string{},
		devices: []api.HandoffDevice{
			{ID: "d:1", Name: "Pixel 8", Platform: "android", Kind: "phone", CanReceive: true, Online: true},
		},
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (f *fakeSendServer) offerJSON(status string) map[string]any {
	files := []map[string]any{}
	for i, name := range f.names {
		files = append(files, map[string]any{"id": 101 + i, "name": name, "size": f.sizes[i]})
	}
	return map[string]any{"id": "o1", "role": "sender", "status": status, "files": files, "to": map[string]string{"name": "Pixel 8"}}
}

func (f *fakeSendServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/handoff/devices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"devices": f.devices})
	})
	mux.HandleFunc("POST /api/handoff/offers", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.creates++
		if f.createFail != nil {
			if status, code, message := f.createFail(); status != 0 {
				writeJSON(w, status, map[string]string{"error": message, "code": code})
				return
			}
		}
		var body struct {
			TargetID string `json:"targetId"`
			Files    []struct {
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"files"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.names, f.sizes = nil, nil
		for _, file := range body.Files {
			f.names = append(f.names, file.Name)
			f.sizes = append(f.sizes, file.Size)
		}
		writeJSON(w, 201, f.offerJSON("uploading"))
	})
	mux.HandleFunc("PUT /api/handoff/offers/{id}/files/{fid}", func(w http.ResponseWriter, r *http.Request) {
		fid, _ := strconv.ParseInt(r.PathValue("fid"), 10, 64)
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.headers = append(f.headers, r.Header.Clone())
		f.headers[len(f.headers)-1].Set("X-Length", strconv.FormatInt(r.ContentLength, 10))
		f.headers[len(f.headers)-1].Set("X-Encoding", strings.Join(r.TransferEncoding, ","))
		hold, fail := f.hold, f.putFail
		f.mu.Unlock()
		if hold != nil {
			select {
			case <-hold:
			case <-r.Context().Done():
				return
			}
		}
		if fail != nil {
			if status := fail(fid); status != 0 {
				writeJSON(w, status, map[string]string{"error": "boom", "code": "INTERNAL"})
				return
			}
		}
		f.mu.Lock()
		f.uploaded[fid] = string(body)
		f.mu.Unlock()
		writeJSON(w, 200, f.offerJSON("pending"))
	})
	mux.HandleFunc("GET /api/handoff/offers/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.statuses++
		call, status, drop := f.statuses, f.status, f.statusFail
		f.mu.Unlock()
		if drop != nil && drop(call) {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		writeJSON(w, 200, f.offerJSON(status))
	})
	mux.HandleFunc("DELETE /api/handoff/offers/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.cancels++
		f.status = "cancelled"
		f.mu.Unlock()
		writeJSON(w, 200, f.offerJSON("cancelled"))
	})
	return mux
}

func (f *fakeSendServer) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeSendServer) cancelled() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cancels
}

type sendEnv struct {
	server  *fakeSendServer
	sender  *Sender
	notices func() []string
	dir     string
}

func newSendEnv(t *testing.T, receive bool) *sendEnv {
	t.Helper()
	fake := newFakeSend()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error {
		c.Server, c.Token = srv.URL, "nc_token"
		c.ReceiveFromDevices = &receive
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	sender := NewSender(ctx, store, log.New(io.Discard, "", 0))
	sender.poll = 10 * time.Millisecond
	var mu sync.Mutex
	var list []string
	sender.Notify = func(title, body string) {
		mu.Lock()
		list = append(list, title+": "+body)
		mu.Unlock()
	}
	return &sendEnv{server: fake, sender: sender, dir: t.TempDir(), notices: func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), list...)
	}}
}

func (e *sendEnv) file(t *testing.T, name, content string) Item {
	t.Helper()
	path := filepath.Join(e.dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return Item{Path: path, Name: name, Size: int64(len(content))}
}

func (e *sendEnv) device() api.HandoffDevice { return e.server.devices[0] }

func TestSendDeliversAndStreamsExactBytes(t *testing.T) {
	env := newSendEnv(t, true)
	item := env.file(t, "report.pdf", strings.Repeat("x", 300_000))
	env.server.set(func() { env.server.status = "delivered" })
	var seen []Transfer
	var mu sync.Mutex
	final := env.sender.Send(context.Background(), env.device(), []Item{item}, Options{Update: func(tr Transfer) {
		mu.Lock()
		seen = append(seen, tr)
		mu.Unlock()
	}})

	if final.State != StateDelivered || final.Active {
		t.Fatalf("final: %+v", final)
	}
	if got := env.server.uploaded[101]; len(got) != 300_000 {
		t.Errorf("uploaded %d bytes", len(got))
	}
	h := env.server.headers[0]
	if h.Get("X-Length") != "300000" || h.Get("X-Encoding") != "" {
		t.Errorf("the body must go out with a known length, not chunked: %v", h)
	}
	if h.Get("Authorization") != "Bearer nc_token" || h.Get("Content-Type") != "application/octet-stream" {
		t.Errorf("headers: %v", h)
	}
	var last int64
	mu.Lock()
	for _, tr := range seen {
		if tr.Done < last || tr.Done > tr.Size {
			t.Errorf("progress went from %d to %d of %d", last, tr.Done, tr.Size)
		}
		last = tr.Done
	}
	mu.Unlock()
	if last != 300_000 {
		t.Errorf("progress ended at %d", last)
	}
	if env.server.cancelled() != 0 {
		t.Error("a delivered send was cancelled")
	}
	if n := env.notices(); len(n) != 1 || n[0] != "Delivered to Pixel 8: report.pdf" {
		t.Errorf("notices: %q", n)
	}
	if list := env.sender.Transfers(); len(list) != 1 || list[0].State != StateDelivered {
		t.Errorf("transfers: %+v", list)
	}
}

func TestSendIsIndependentOfTheReceiveSwitch(t *testing.T) {
	env := newSendEnv(t, false)
	env.server.devices = append(env.server.devices,
		api.HandoffDevice{ID: "d:2", Name: "Me", Online: true, CanReceive: true, Current: true},
		api.HandoffDevice{ID: "d:3", Name: "Off", Online: true, CanReceive: false},
		api.HandoffDevice{ID: "d:4", Name: "Asleep", Online: false, CanReceive: true},
		api.HandoffDevice{ID: "s:5", Name: "Firefox", Online: true, CanReceive: true, Kind: "browser"})
	devices, err := env.sender.Devices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "Pixel 8" || devices[1].Name != "Firefox" {
		t.Fatalf("only online devices that accept files, never this one: %+v", devices)
	}
	env.server.set(func() { env.server.status = "delivered" })
	final := env.sender.Send(context.Background(), devices[0], []Item{env.file(t, "a.txt", "hi")}, Options{})
	if final.State != StateDelivered {
		t.Errorf("with receiving switched off: %+v", final)
	}
}

func TestSendSeveralFilesMapsEachToItsServerId(t *testing.T) {
	env := newSendEnv(t, true)
	items := []Item{env.file(t, "a.txt", "alpha"), env.file(t, "b.txt", ""), env.file(t, "c.txt", "gamma!")}
	env.server.set(func() { env.server.status = "delivered" })
	final := env.sender.Send(context.Background(), env.device(), items, Options{})
	if final.State != StateDelivered || final.Label != "3 files" || final.Size != 11 {
		t.Fatalf("final: %+v", final)
	}
	if env.server.uploaded[101] != "alpha" || env.server.uploaded[102] != "" || env.server.uploaded[103] != "gamma!" || len(env.server.uploaded) != 3 {
		t.Errorf("uploads: %v", env.server.uploaded)
	}
	if env.server.headers[1].Get("X-Length") != "0" {
		t.Error("an empty file is sent with length 0")
	}
}

func TestUploadFailureCancelsTheOffer(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.putFail = func(id int64) int {
		if id == 102 {
			return 500
		}
		return 0
	}
	final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1"), env.file(t, "b", "2")}, Options{})
	if final.State != StateFailed || !strings.Contains(final.Message, "problem") {
		t.Fatalf("final: %+v", final)
	}
	if env.server.cancelled() != 1 {
		t.Errorf("the half-sent offer must be cancelled; cancels: %d", env.server.cancelled())
	}
	if n := env.notices(); len(n) != 1 || !strings.HasPrefix(n[0], "Could not send 2 files") {
		t.Errorf("notices: %q", n)
	}
}

func TestOutcomesDeclinedAndExpired(t *testing.T) {
	for status, want := range map[string]struct{ state, notice string }{
		"declined": {StateDeclined, "Declined: Pixel 8 declined a.txt"},
		"expired":  {StateExpired, "Not received: Pixel 8 did not receive a.txt in time"},
	} {
		env := newSendEnv(t, true)
		env.server.set(func() { env.server.status = status })
		final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a.txt", "x")}, Options{})
		if final.State != want.state || final.Active {
			t.Errorf("%s: %+v", status, final)
		}
		if n := env.notices(); len(n) != 1 || n[0] != want.notice {
			t.Errorf("%s notices: %q", status, n)
		}
		if env.server.cancelled() != 0 {
			t.Errorf("%s: nothing to cancel", status)
		}
	}
}

func TestWaitsThroughAcceptedUntilDelivered(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.set(func() { env.server.status = "pending" })
	var mu sync.Mutex
	var states []string
	done := make(chan Transfer, 1)
	go func() {
		done <- env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a.txt", "x")}, Options{Update: func(tr Transfer) {
			mu.Lock()
			if len(states) == 0 || states[len(states)-1] != tr.State {
				states = append(states, tr.State)
			}
			mu.Unlock()
		}})
	}()
	waitFor(t, "waiting", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(states, ","), StateWaiting)
	})
	env.server.set(func() { env.server.status = "accepted" })
	waitFor(t, "receiving", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(states, ","), StateReceiving)
	})
	env.server.set(func() { env.server.status = "delivered" })
	if final := <-done; final.State != StateDelivered {
		t.Fatalf("final: %+v", final)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(states, ",") != "uploading,waiting,receiving,delivered" {
		t.Errorf("states: %v", states)
	}
}

func TestCancelMidUploadCancelsTheOffer(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.hold = make(chan struct{})
	id := env.sender.Start(env.device(), []Item{env.file(t, "big.bin", "data")})
	waitFor(t, "the upload to start", func() bool {
		env.server.mu.Lock()
		defer env.server.mu.Unlock()
		return len(env.server.headers) > 0
	})
	if !env.sender.Cancel(id) {
		t.Fatal("Cancel found nothing running")
	}
	waitFor(t, "cancelled", func() bool { l := env.sender.Transfers(); return len(l) == 1 && l[0].State == StateCancelled })
	if env.server.cancelled() != 1 {
		t.Errorf("server cancels: %d", env.server.cancelled())
	}
	if n := env.notices(); len(n) != 0 {
		t.Errorf("a cancel the person asked for is not announced: %q", n)
	}
}

func TestCancelWhileWaitingForTheAnswer(t *testing.T) {
	env := newSendEnv(t, true)
	id := env.sender.Start(env.device(), []Item{env.file(t, "a.txt", "x")})
	waitFor(t, "waiting", func() bool { l := env.sender.Transfers(); return len(l) == 1 && l[0].State == StateWaiting })
	env.sender.Cancel(id)
	waitFor(t, "cancelled", func() bool { l := env.sender.Transfers(); return len(l) == 1 && l[0].State == StateCancelled })
	if env.server.cancelled() != 1 {
		t.Errorf("server cancels: %d", env.server.cancelled())
	}
}

func TestServerRefusalsAreShownInPlainWords(t *testing.T) {
	cases := []struct {
		status        int
		code, message string
		want          string
	}{
		{409, "DEVICE_NOT_RECEIVING", "x", "not accepting files"},
		{404, "DEVICE_NOT_FOUND", "x", "no longer available"},
		{413, "QUOTA_EXCEEDED", "x", "storage is full"},
		{413, "SEND_TOO_LARGE", "x", "more than can be sent"},
		{429, "TOO_MANY_OPEN_SENDS", "x", "Too many sends are waiting"},
		{429, "", "slow down", "Too many sends in a short time"},
		{401, "", "x", "signed out"},
		{503, "", "x", "problem"},
	}
	for _, c := range cases {
		env := newSendEnv(t, true)
		env.server.createFail = func() (int, string, string) { return c.status, c.code, c.message }
		final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{})
		if final.State != StateFailed || !strings.Contains(final.Message, c.want) {
			t.Errorf("%d %s: %+v", c.status, c.code, final)
		}
	}
}

func TestNoWaitReturnsAfterUpload(t *testing.T) {
	env := newSendEnv(t, true)
	final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{NoWait: true})
	if final.State != StateWaiting || final.Active {
		t.Errorf("final: %+v", final)
	}
	if env.server.statuses != 0 {
		t.Error("it must not poll when not waiting")
	}
}

func TestLostConnectionDuringUploadIsAFailureAndCancels(t *testing.T) {
	env := newSendEnv(t, true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			writeJSON(w, 201, map[string]any{"id": "o1", "files": []map[string]any{{"id": 1, "name": "a", "size": 4}}})
		case r.Method == "PUT":
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close() // the network dropped
		default:
			w.Write([]byte("{}"))
		}
	}))
	defer srv.Close()
	env.sender.client = func(config.Config) *api.Client { return api.New(srv.URL, "t") }
	final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "data")}, Options{})
	if final.State != StateFailed || !strings.Contains(final.Message, "Could not reach NeoCloud") {
		t.Errorf("final: %+v", final)
	}
}

func TestFlakyStatusChecksDoNotLoseTheSend(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.statusFail = func(call int) bool { return call <= 3 }
	env.server.set(func() { env.server.status = "delivered" })
	final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{})
	if final.State != StateDelivered {
		t.Errorf("final: %+v", final)
	}
}

func TestEventKickAnswersWithoutWaitingForThePoll(t *testing.T) {
	env := newSendEnv(t, true)
	env.sender.poll = time.Hour
	done := make(chan Transfer, 1)
	go func() {
		done <- env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{})
	}()
	waitFor(t, "waiting", func() bool { l := env.sender.Transfers(); return len(l) == 1 && l[0].State == StateWaiting })
	time.Sleep(50 * time.Millisecond)
	env.server.set(func() { env.server.status = "delivered" })
	env.sender.Kick("o1")
	select {
	case final := <-done:
		if final.State != StateDelivered {
			t.Errorf("final: %+v", final)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the kick did not wake the send")
	}
}

func TestFileChangedWhileSendingIsReportedPlainly(t *testing.T) {
	env := newSendEnv(t, true)
	item := env.file(t, "log.txt", "0123456789")
	item.Size = 20 // it was 20 bytes when chosen, and shrank since
	final := env.sender.Send(context.Background(), env.device(), []Item{item}, Options{})
	if final.State != StateFailed || !strings.Contains(final.Message, "changed while it was being sent") {
		t.Errorf("final: %+v", final)
	}
	if env.server.cancelled() != 1 {
		t.Error("the offer must be cancelled")
	}
}

func TestNotificationsFollowTheSetting(t *testing.T) {
	for level, want := range map[string]int{config.NotifyOff: 0, config.NotifyErrors: 1, config.NotifyAll: 2} {
		env := newSendEnv(t, true)
		env.sender.Config.Update(func(c *config.Config) error { c.Notifications = level; return nil })
		env.server.set(func() { env.server.status = "delivered" })
		env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{})
		if n := env.notices(); len(n) != want {
			t.Errorf("%s: %q", level, n)
		}
	}
}

func TestSignedOutAndLocked(t *testing.T) {
	env := newSendEnv(t, true)
	env.sender.Config.Update(func(c *config.Config) error { c.Token = ""; return nil })
	if _, err := env.sender.Devices(context.Background()); err == nil || !strings.Contains(err.Error(), "security key") && !strings.Contains(err.Error(), "Sign in") {
		t.Errorf("no token: %v", err)
	}
	final := env.sender.Send(context.Background(), env.device(), []Item{env.file(t, "a", "1")}, Options{})
	if final.State != StateFailed || final.Message == "" {
		t.Errorf("final: %+v", final)
	}
}

func TestTemporaryFilesAreRemovedAfterwards(t *testing.T) {
	env := newSendEnv(t, true)
	item := env.file(t, "drop.txt", "x")
	item.Temporary = true
	env.server.set(func() { env.server.status = "declined" })
	env.sender.Send(context.Background(), env.device(), []Item{item}, Options{})
	if _, err := os.Stat(item.Path); err == nil {
		t.Error("the dropped copy was left behind")
	}
}

func TestFinishedListIsCappedAndDismissable(t *testing.T) {
	env := newSendEnv(t, true)
	env.server.set(func() { env.server.status = "declined" })
	for i := 0; i < keepFinished+3; i++ {
		env.sender.Send(context.Background(), env.device(), []Item{env.file(t, fmt.Sprintf("f%d", i), "x")}, Options{})
	}
	list := env.sender.Transfers()
	if len(list) != keepFinished {
		t.Fatalf("kept %d", len(list))
	}
	env.sender.Dismiss(list[0].ID)
	if len(env.sender.Transfers()) != keepFinished-1 {
		t.Error("dismiss did nothing")
	}
}
