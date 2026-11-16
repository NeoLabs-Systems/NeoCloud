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

// fakeServer streams the given offers on connect and records what the
// receiver did with them.
type fakeServer struct {
	mu      sync.Mutex
	offers  []api.HandoffOffer
	content map[int64]string
	calls   []string
	receive []string
	repeat  int
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/handoff/events", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.receive = append(f.receive, r.URL.Query().Get("receive"))
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte(": connected\n\n"))
		if r.URL.Query().Get("receive") == "1" {
			for i := 0; i <= f.repeat; i++ {
				for _, offer := range f.offers {
					data, _ := json.Marshal(offer)
					fmt.Fprintf(w, "event: handoff:offer\ndata: %s\n\n", data)
				}
			}
		}
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	mux.HandleFunc("/api/handoff/offers/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/api/handoff/offers/"))
		f.mu.Unlock()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/handoff/offers/"), "/")
		if len(parts) == 3 && parts[1] == "files" {
			id, _ := strconv.ParseInt(parts[2], 10, 64)
			io.WriteString(w, f.content[id])
			return
		}
		w.Write([]byte("{}"))
	})
	return mux
}

func (f *fakeServer) called(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type scripted struct {
	mu      sync.Mutex
	choice  Choice
	asked   int
	message string
}

func (s *scripted) Ask(_ context.Context, message string) (Choice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	s.message = message
	return s.choice, nil
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.asked
}

func start(t *testing.T, f *fakeServer, prompt Prompter, downloads string, receive bool) (notices func() string) {
	t.Helper()
	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *config.Config) error {
		c.Server, c.Token = server.URL, "nc_token"
		on := receive
		c.ReceiveFromDevices = &on
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var list []string
	r := New(store, log.New(io.Discard, "", 0))
	r.Prompt = prompt
	r.Downloads = func() string { return downloads }
	r.Notify = func(title, body string) {
		mu.Lock()
		list = append(list, title+": "+body)
		mu.Unlock()
	}
	r.wait = func(time.Duration) {}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	t.Cleanup(func() { stop(); <-done })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(list, "\n")
	}
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func offer(id string, files ...api.HandoffFile) api.HandoffOffer {
	o := api.HandoffOffer{ID: id, Files: files, Status: "pending"}
	o.From.Name = "Anna’s iPhone"
	for _, f := range files {
		o.TotalSize += f.Size
	}
	return o
}

func TestReceivesSavesAndConfirms(t *testing.T) {
	downloads := t.TempDir()
	os.WriteFile(filepath.Join(downloads, "a.txt"), []byte("existing"), 0o644)
	f := &fakeServer{
		offers:  []api.HandoffOffer{offer("o1", api.HandoffFile{ID: 1, Name: "a.txt", Size: 5}, api.HandoffFile{ID: 2, Name: "../../evil.sh", Size: 3})},
		content: map[int64]string{1: "hello", 2: "bad"},
		repeat:  2, // the server pushes the same offer again on reconnect
	}
	prompt := &scripted{choice: ChoiceDownloads}
	notices := start(t, f, prompt, downloads, true)
	waitFor(t, "complete", func() bool { return f.called("POST o1/complete") == 1 })
	time.Sleep(100 * time.Millisecond)

	if prompt.count() != 1 || f.called("POST o1/accept") != 1 {
		t.Errorf("the same offer was handled %d times (accepted %d)", prompt.count(), f.called("POST o1/accept"))
	}
	prompt.mu.Lock()
	message := prompt.message
	prompt.mu.Unlock()
	if !strings.Contains(message, "Anna’s iPhone") || !strings.Contains(message, "2 files") {
		t.Errorf("message: %q", message)
	}
	if got, _ := os.ReadFile(filepath.Join(downloads, "a.txt")); string(got) != "existing" {
		t.Error("an existing file was overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(downloads, "a (1).txt")); string(got) != "hello" {
		t.Errorf("a (1).txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(downloads, "_.._evil.sh")); string(got) != "bad" {
		t.Errorf("sanitised name missing: %v", listDir(downloads))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(downloads), "evil.sh")); err == nil {
		t.Error("a file escaped the folder")
	}
	if len(listDir(downloads)) != 3 {
		t.Errorf("unexpected files: %v", listDir(downloads))
	}
	joined := notices()
	if strings.Count(joined, "sent you") != 1 || !strings.Contains(joined, "Saved") {
		t.Errorf("notifications: %q", joined)
	}
}

func listDir(dir string) []string {
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDeclineSavesNothing(t *testing.T) {
	downloads := t.TempDir()
	f := &fakeServer{offers: []api.HandoffOffer{offer("o2", api.HandoffFile{ID: 1, Name: "x", Size: 1})}, content: map[int64]string{1: "x"}}
	start(t, f, &scripted{choice: ChoiceDecline}, downloads, true)
	waitFor(t, "decline", func() bool { return f.called("POST o2/decline") == 1 })
	if f.called("POST o2/accept") != 0 || len(listDir(downloads)) != 0 {
		t.Error("a declined send was saved")
	}
}

func TestLaterLeavesTheSendWaiting(t *testing.T) {
	downloads := t.TempDir()
	f := &fakeServer{offers: []api.HandoffOffer{offer("o3", api.HandoffFile{ID: 1, Name: "x", Size: 1})}, content: map[int64]string{1: "x"}}
	prompt := &scripted{choice: ChoiceLater}
	start(t, f, prompt, downloads, true)
	waitFor(t, "the dialog", func() bool { return prompt.count() == 1 })
	time.Sleep(100 * time.Millisecond)
	if f.called("POST") != 0 {
		t.Errorf("calls: %v", f.calls)
	}
}

func TestSizeMismatchIsNotKept(t *testing.T) {
	downloads := t.TempDir()
	f := &fakeServer{offers: []api.HandoffOffer{offer("o4", api.HandoffFile{ID: 1, Name: "x.bin", Size: 9})}, content: map[int64]string{1: "short"}}
	notices := start(t, f, &scripted{choice: ChoiceDownloads}, downloads, true)
	waitFor(t, "a failure notice", func() bool { return strings.Contains(notices(), "Could not save") })
	if f.called("POST o4/complete") != 0 {
		t.Error("an incomplete send was confirmed")
	}
	if names := listDir(downloads); len(names) != 0 {
		t.Errorf("leftovers: %v", names)
	}
}

func TestTurnedOffStaysConnectedButSilent(t *testing.T) {
	downloads := t.TempDir()
	f := &fakeServer{offers: []api.HandoffOffer{offer("o5", api.HandoffFile{ID: 1, Name: "x", Size: 1})}, content: map[int64]string{1: "x"}}
	prompt := &scripted{choice: ChoiceDownloads}
	start(t, f, prompt, downloads, false)
	waitFor(t, "connecting", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.receive) > 0 })
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.receive[0] != "0" || prompt.count() != 0 {
		t.Errorf("receive=%v asked=%d", f.receive, prompt.count())
	}
}

func TestPromptTextNamesDeviceAndCutsLongLists(t *testing.T) {
	var files []api.HandoffFile
	for i := 0; i < 6; i++ {
		files = append(files, api.HandoffFile{ID: int64(i), Name: fmt.Sprintf("f%d.txt", i), Size: 1500})
	}
	text := promptText(offer("o", files...))
	if !strings.Contains(text, "6 files (8.8 KiB)") || !strings.Contains(text, "and 3 more") || strings.Contains(text, "f5.txt") {
		t.Errorf("text: %q", text)
	}
	o := offer("o", api.HandoffFile{ID: 1, Name: "a.txt", Size: 1})
	o.From.Name = "Evil‮\x00 phone"
	if strings.ContainsAny(promptText(o), "‮\x00") {
		t.Error("control characters reached the dialog")
	}
}

func TestForwardsUpdatesAndDeviceChangesToTheSender(t *testing.T) {
	var mu sync.Mutex
	var updated []string
	var devices int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": connected\n\nevent: handoff:updated\ndata: {\"offer\":{\"id\":\"o9\",\"status\":\"delivered\"}}\n\nevent: handoff:devices\ndata: {}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	store, _ := config.Open(t.TempDir())
	store.Update(func(c *config.Config) error {
		c.Server, c.Token = server.URL, "t"
		off := false
		c.ReceiveFromDevices = &off // sending works with receiving off
		return nil
	})
	r := New(store, log.New(io.Discard, "", 0))
	r.Updated = func(id string) { mu.Lock(); updated = append(updated, id); mu.Unlock() }
	r.DevicesChanged = func() { mu.Lock(); devices++; mu.Unlock() }
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	defer func() { stop(); <-done }()
	waitFor(t, "the events", func() bool { mu.Lock(); defer mu.Unlock(); return len(updated) == 1 && devices == 1 })
	if updated[0] != "o9" {
		t.Errorf("updated: %v", updated)
	}
}
