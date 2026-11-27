// Package analytics is NeoCloud's opt-out, aggregate-only Countly reporting:
// the browser script and an hourly host snapshot. ANALYTICS_ENABLED=false turns
// both off.
package analytics

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

const (
	// Origin is the only host a browser or server report goes to.
	Origin   = "https://data.neo.eu.com"
	endpoint = Origin + "/i"
	appKey   = "b2c25a128d98c32f5fcf7e1b06a5f15433d458b4"
	// ScriptPath is where pages load the browser script from.
	ScriptPath = "/_neolabs/analytics.js"
)

//go:embed client.js
var clientSource string

var headEnd = regexp.MustCompile(`(?i)</head>`)

// Inject adds the browser script to an HTML page.
func Inject(page string) string {
	location := headEnd.FindStringIndex(page)
	if location == nil {
		return page
	}
	return page[:location[0]] + `<script defer src="` + ScriptPath + `"></script>` + "\n" + page[location[0]:]
}

// Script serves the browser script with this deployment's configuration.
func Script() http.HandlerFunc {
	config, _ := json.Marshal(map[string]string{"endpoint": endpoint, "appKey": appKey})
	script := strings.Replace(clientSource, "/* COUNTLY_CONFIG */ null", string(config), 1)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(w, script)
	}
}

// ReportHosts reports this machine once and then hourly until ctx ends.
// Analytics never affect the server, so failures are dropped.
func ReportHosts(ctx context.Context, version string) {
	client := ssrf.NewClient(ssrf.Options{Timeout: 5 * time.Second, MaxRedirects: -1})
	started := time.Now()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		_ = report(ctx, client, endpoint, hostSnapshot(version, started))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func report(ctx context.Context, client *http.Client, target string, snapshot map[string]any) error {
	events, err := json.Marshal([]map[string]any{{"key": "host-snapshot", "count": 1, "segmentation": snapshot}})
	if err != nil {
		return err
	}
	// A stream label satisfies Countly's protocol without identifying an
	// installation.
	form := url.Values{"app_key": {appKey}, "device_id": {"aggregate-server"}, "events": {string(events)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	return response.Body.Close()
}
