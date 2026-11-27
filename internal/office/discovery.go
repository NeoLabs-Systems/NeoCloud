package office

// The editor's WOPI discovery, fetched from the address the admin configured
// and cached: which extensions it opens and at which page.

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	fetchTimeout      = 8 * time.Second
	maxDiscoveryBytes = 1 << 20
	discoveryRefresh  = time.Hour
	discoveryRetry    = time.Minute
)

// The formats offered for editing. Text and CSV stay with the built-in editor.
var editableExtensions = []string{"docx", "xlsx", "pptx", "odt", "ods", "odp"}

var (
	templatePlaceholder = regexp.MustCompile(`<[^>]*>`)
	extensionPattern    = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
)

var errNotDiscovery = errors.New("this is not a WOPI discovery document")

type action struct{ Edit, View string }

// discovery is the last answer for one editor address.
type discovery struct {
	url     string
	actions map[string]action
	failure string
	checked time.Time
}

// parseDiscovery maps each extension to its edit and view pages. The first
// action listed for an extension wins; other actions, odd extensions and
// addresses that are not http(s) are ignored.
func parseDiscovery(document []byte) (map[string]action, error) {
	decoder := xml.NewDecoder(strings.NewReader(string(document)))
	actions := map[string]action{}
	found := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errNotDiscovery
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		found = found || start.Name.Local == "wopi-discovery"
		if start.Name.Local != "action" {
			continue
		}
		attributes := map[string]string{}
		for _, attribute := range start.Attr {
			attributes[attribute.Name.Local] = attribute.Value
		}
		ext, name, urlsrc := strings.ToLower(attributes["ext"]), strings.ToLower(attributes["name"]), attributes["urlsrc"]
		if name != "edit" && name != "view" || !extensionPattern.MatchString(ext) || actionOrigin(urlsrc) == "" {
			continue
		}
		current := actions[ext]
		if name == "edit" && current.Edit == "" {
			current.Edit = urlsrc
		}
		if name == "view" && current.View == "" {
			current.View = urlsrc
		}
		actions[ext] = current
	}
	if !found {
		return nil, errNotDiscovery
	}
	return actions, nil
}

// actionOrigin is the origin of an http(s) editor address, else "".
func actionOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(templatePlaceholder.ReplaceAllString(raw, "")))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(u.Host, " ;,'\"") {
		return ""
	}
	return u.Scheme + "://" + strings.ToLower(u.Host)
}

// editorPageURL adds WOPISrc to an action's address, which may carry
// placeholders like <ui=UI_LLCC&>.
func editorPageURL(urlsrc, wopiSrc string) string {
	base := templatePlaceholder.ReplaceAllString(urlsrc, "")
	joiner := "?"
	if strings.Contains(base, "?") {
		joiner = "&"
		if strings.HasSuffix(base, "?") || strings.HasSuffix(base, "&") {
			joiner = ""
		}
	}
	return base + joiner + "WOPISrc=" + url.QueryEscape(wopiSrc)
}

func extensionsOf(actions map[string]action) []string {
	extensions := []string{}
	for _, ext := range editableExtensions {
		if _, ok := actions[ext]; ok {
			extensions = append(extensions, ext)
		}
	}
	return extensions
}

// fetchActions asks the editor for its discovery. Failures are short reasons
// written for the admin.
func (h *Handler) fetchActions(ctx context.Context, editorURL string) (map[string]action, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	unreachable := func() error {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("The editor did not answer in time. Is it running and reachable from this server?")
		}
		return errors.New("This server could not connect to the editor. Check the address and that the editor is running.")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, editorURL+"/hosting/discovery", nil)
	if err != nil {
		return nil, unreachable()
	}
	request.Header.Set("Accept", "text/xml, application/xml")
	response, err := h.client.Do(request)
	if err != nil {
		return nil, unreachable()
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return nil, errors.New("The address redirects somewhere else. Enter the final address (for example the https one).")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("The editor answered with status %d. Check the address.", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxDiscoveryBytes+1))
	if err != nil {
		return nil, unreachable()
	}
	if len(body) > maxDiscoveryBytes {
		return nil, errors.New("The editor’s answer is far larger than a discovery document.")
	}
	actions, err := parseDiscovery(body)
	if err != nil {
		return nil, errors.New("That address is not a Collabora Online or ONLYOFFICE editor (no WOPI discovery).")
	}
	if !slices.ContainsFunc(editableExtensions, func(ext string) bool { _, ok := actions[ext]; return ok }) {
		return nil, errors.New("The editor does not offer any office formats.")
	}
	return actions, nil
}

// checkEditor fetches past the cache and remembers the answer.
func (h *Handler) checkEditor(ctx context.Context, editorURL string) (map[string]action, error) {
	actions, err := h.fetchActions(ctx, editorURL)
	answer := discovery{url: editorURL, actions: actions, checked: h.now()}
	if err != nil {
		answer.failure = err.Error()
	}
	h.mu.Lock()
	h.cache = answer
	h.mu.Unlock()
	return actions, err
}

// currentActions is the cached discovery, refreshed when stale; nil when the
// editor is off or was never reachable.
func (h *Handler) currentActions(ctx context.Context) map[string]action {
	h.mu.RLock()
	settings, known := h.settings, h.cache
	h.mu.RUnlock()
	if !settings.Enabled {
		return nil
	}
	if known.url != settings.URL {
		known = discovery{}
	}
	age := h.now().Sub(known.checked)
	if known.actions != nil && age < discoveryRefresh {
		return known.actions
	}
	if known.failure != "" && age < discoveryRetry {
		return nil
	}
	actions, err := h.checkEditor(ctx, settings.URL)
	if err != nil {
		return known.actions
	}
	return actions
}

// EditorOrigins are the origins pages may frame and post to for editing: the
// configured address and where its actions point, only while editing is on.
func (h *Handler) EditorOrigins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.settings.Enabled {
		return nil
	}
	origins := []string{}
	add := func(raw string) {
		if origin := actionOrigin(raw); origin != "" && !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}
	add(h.settings.URL)
	if h.cache.url == h.settings.URL {
		for _, ext := range editableExtensions {
			add(h.cache.actions[ext].Edit)
			add(h.cache.actions[ext].View)
		}
	}
	return origins
}
