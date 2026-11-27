package httppolicy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDrainRefusesNewUploadsAndEndsStreams(t *testing.T) {
	drain := NewDrain()
	streaming := make(chan struct{})
	ended := make(chan struct{})
	handler := drain.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/sync/watch" {
			close(streaming)
			<-r.Context().Done()
			close(ended)
		}
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/sync/watch", nil))
	<-streaming
	drain.Start()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the event stream outlived the drain")
	}
	for path, want := range map[string]int{"/api/upload": 503, "/api/uploads": 503, "/api/uploads/x/parts/1": 503, "/api/files": 200} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != want || recorder.Header().Get("Connection") != "close" {
			t.Errorf("%s = %d %v", path, recorder.Code, recorder.Header())
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/dav/files/ann/a.txt", nil))
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "SERVER_STOPPING") {
		t.Errorf("DAV upload = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestLoggedPathHidesSecretsAndFileNames(t *testing.T) {
	for in, want := range map[string]string{
		"/s/AbC123":                      "/s/[redacted]",
		"/api/public/shares/tok/upload":  "/api/public/shares/[redacted]/upload",
		"/app/sign/tok":                  "/app/sign/[redacted]",
		"/dav/files/ann/Private/tax.pdf": "/dav/files/ann/[path]",
		"/remote.php/webdav/x":           "/remote.php/webdav/[path]",
		"/api/files/123":                 "/api/files/123",
	} {
		if got := LoggedPath(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}
