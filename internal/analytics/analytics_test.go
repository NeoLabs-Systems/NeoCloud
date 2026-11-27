package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScriptAndInjection(t *testing.T) {
	recorder := httptest.NewRecorder()
	Script()(recorder, httptest.NewRequest(http.MethodGet, ScriptPath, nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `"appKey":"`+appKey+`"`) || strings.Contains(body, "COUNTLY_CONFIG") || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("script not configured: %v", recorder.Header())
	}
	page := Inject("<html><HEAD></HEAD><body></body></html>")
	if !strings.Contains(page, `<script defer src="/_neolabs/analytics.js"></script>`+"\n</HEAD>") {
		t.Fatalf("injected page = %s", page)
	}
}

func TestHostReportCarriesOnlyBands(t *testing.T) {
	var form map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
	}))
	defer server.Close()
	if err := report(context.Background(), server.Client(), server.URL, hostSnapshot("1.2.3", time.Now().Add(-3*time.Hour))); err != nil {
		t.Fatal(err)
	}
	var events []struct {
		Key          string         `json:"key"`
		Segmentation map[string]any `json:"segmentation"`
	}
	if err := json.Unmarshal([]byte(form["events"][0]), &events); err != nil || len(events) != 1 || events[0].Key != "host-snapshot" {
		t.Fatalf("events = %v, %v", form["events"], err)
	}
	if form["device_id"][0] != "aggregate-server" || events[0].Segmentation["app_version"] != "1.2.3" || events[0].Segmentation["uptime_hours_range"] != "2-4" {
		t.Fatalf("report = %v", form)
	}
	if _, found := events[0].Segmentation["hostname"]; found {
		t.Fatal("report names the host")
	}
}
