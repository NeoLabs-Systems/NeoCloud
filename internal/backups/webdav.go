package backups

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

const (
	webDAVListingLimit = 64 << 20
	webDAVMaxDepth     = 16
	webDAVMaxEntries   = 2_000_000
)

// WebDAVConfig describes a backup folder on an HTTP(S) WebDAV server.
type WebDAVConfig struct {
	URL      string
	Username string
	Password string
}

// WebDAVTarget implements the snapshot Target interface with bounded,
// non-redirecting requests. The supplied client is useful for protocol tests;
// production callers should use the default SSRF-protected transport.
type WebDAVTarget struct {
	root   *url.URL
	auth   string
	client *http.Client
}

func NewWebDAVTarget(config WebDAVConfig) (*WebDAVTarget, error) {
	client := ssrf.NewClient(ssrf.Options{AllowPrivate: true, Timeout: 2 * time.Minute})
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return NewWebDAVTargetWithClient(config, client)
}

func NewWebDAVTargetWithClient(config WebDAVConfig, client *http.Client) (*WebDAVTarget, error) {
	root, err := url.ParseRequestURI(strings.TrimSpace(config.URL))
	if err != nil || root.Host == "" || (root.Scheme != "http" && root.Scheme != "https") || root.User != nil || root.RawQuery != "" || root.Fragment != "" {
		return nil, errors.New("enter an http or https WebDAV folder URL without credentials, query, or fragment")
	}
	if client == nil {
		return nil, errors.New("WebDAV HTTP client is required")
	}
	root.Path = strings.TrimRight(root.Path, "/") + "/"
	return &WebDAVTarget{root: root, auth: basicAuth(config.Username, config.Password), client: client}, nil
}

func basicAuth(username, password string) string {
	if username == "" {
		return ""
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func (t *WebDAVTarget) targetURL(key string, folder bool) (*url.URL, error) {
	if err := validTargetKey(key, true); err != nil {
		return nil, err
	}
	u := *t.root
	segments := strings.Split(strings.Trim(key, "/"), "/")
	decoded := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment != "" {
			decoded = append(decoded, segment)
		}
	}
	u.Path = strings.TrimRight(t.root.Path, "/")
	if len(decoded) > 0 {
		u.Path += "/" + strings.Join(decoded, "/")
	}
	if folder && !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	u.RawPath = ""
	return &u, nil
}

func validTargetKey(key string, allowEmpty bool) error {
	if strings.ContainsRune(key, 0) || strings.Contains(key, "\\") || strings.HasPrefix(key, "/") {
		return errors.New("invalid WebDAV backup key")
	}
	if key == "" && allowEmpty {
		return nil
	}
	if key == "" || path.Clean(key) != key || key == "." || key == ".." {
		return errors.New("invalid WebDAV backup key")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("invalid WebDAV backup key")
		}
	}
	return nil
}

func (t *WebDAVTarget) request(ctx context.Context, method, key string, folder bool, body io.Reader, headers map[string]string) (*http.Response, error) {
	target, err := t.targetURL(key, folder)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if t.auth != "" {
		req.Header.Set("Authorization", t.auth)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WebDAV %s request failed: %w", method, err)
	}
	return response, nil
}

func (t *WebDAVTarget) Open(ctx context.Context) error {
	response, err := t.request(ctx, "MKCOL", "", true, nil, nil)
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("WebDAV MKCOL answered HTTP %d", response.StatusCode)
	}
	response, err = t.request(ctx, "PROPFIND", "", true, strings.NewReader(webDAVPropfind), map[string]string{"Depth": "0", "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != 207 {
		return fmt.Errorf("WebDAV PROPFIND answered HTTP %d", response.StatusCode)
	}
	return nil
}

func (*WebDAVTarget) Close() error { return nil }

func (t *WebDAVTarget) Exists(ctx context.Context, key string) (bool, error) {
	response, err := t.request(ctx, http.MethodHead, key, false, nil, nil)
	if err != nil {
		return false, err
	}
	closeResponse(response)
	if response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented {
		response, err = t.request(ctx, "PROPFIND", key, false, strings.NewReader(webDAVPropfind), map[string]string{"Depth": "0", "Content-Type": "application/xml; charset=utf-8"})
		if err != nil {
			return false, err
		}
		status := response.StatusCode
		closeResponse(response)
		if status == http.StatusNotFound {
			return false, nil
		}
		if status == 207 {
			return true, nil
		}
		return false, fmt.Errorf("WebDAV PROPFIND answered HTTP %d", status)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return false, fmt.Errorf("WebDAV HEAD answered HTTP %d", response.StatusCode)
	}
	return true, nil
}

func (t *WebDAVTarget) List(ctx context.Context, prefix string) ([]string, error) {
	if err := validTargetKey(prefix, true); err != nil {
		return nil, err
	}
	keys := make([]string, 0)
	entries := 0
	var walk func(string, int) error
	walk = func(folder string, depth int) error {
		if depth > webDAVMaxDepth {
			return errors.New("WebDAV backup folder nesting is too deep")
		}
		response, err := t.request(ctx, "PROPFIND", folder, true, strings.NewReader(webDAVPropfind), map[string]string{"Depth": "1", "Content-Type": "application/xml; charset=utf-8"})
		if err != nil {
			return err
		}
		if response.StatusCode == http.StatusNotFound {
			closeResponse(response)
			return nil
		}
		if response.StatusCode != 207 {
			status := response.StatusCode
			closeResponse(response)
			return fmt.Errorf("WebDAV PROPFIND answered HTTP %d", status)
		}
		listing, readErr := io.ReadAll(io.LimitReader(response.Body, webDAVListingLimit+1))
		bodyCloseErr := response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if int64(len(listing)) > webDAVListingLimit {
			return errors.New("WebDAV listing is too large")
		}
		var result webDAVMultiStatus
		decodeErr := xml.Unmarshal(listing, &result)
		if decodeErr != nil {
			return fmt.Errorf("decode WebDAV listing: %w", decodeErr)
		}
		if bodyCloseErr != nil {
			return bodyCloseErr
		}
		base, _ := t.targetURL(folder, true)
		baseSegments := decodedURLSegments(base)
		for _, item := range result.Responses {
			entryURL, parseErr := url.Parse(item.Href)
			if parseErr != nil {
				continue
			}
			resolved := base.ResolveReference(entryURL)
			if resolved.Scheme != base.Scheme || !strings.EqualFold(resolved.Host, base.Host) {
				continue
			}
			segments := decodedURLSegments(resolved)
			if len(segments) != len(baseSegments)+1 {
				continue
			}
			matches := true
			for i := range baseSegments {
				if segments[i] != baseSegments[i] {
					matches = false
					break
				}
			}
			if !matches {
				continue
			}
			name := segments[len(baseSegments)]
			if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
				return errors.New("WebDAV listing contains an unsafe name")
			}
			entries++
			if entries > webDAVMaxEntries {
				return errors.New("WebDAV backup folder has too many entries")
			}
			key := path.Join(folder, name)
			if item.collection() {
				if err = walk(key, depth+1); err != nil {
					return err
				}
			} else if !strings.HasPrefix(name, ".tmp-") {
				keys = append(keys, key)
			}
		}
		return nil
	}
	if err := walk(prefix, 0); err != nil {
		return nil, err
	}
	return keys, nil
}

func (t *WebDAVTarget) Put(ctx context.Context, key, source string) error {
	if err := validTargetKey(key, false); err != nil {
		return err
	}
	parts := strings.Split(key, "/")
	folder := strings.Join(parts[:len(parts)-1], "/")
	if err := t.ensureFolder(ctx, folder); err != nil {
		return err
	}
	temporary := path.Join(folder, ".tmp-"+parts[len(parts)-1])
	moved := false
	defer func() {
		if !moved {
			_ = t.Delete(context.Background(), temporary)
		}
	}()
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	response, err := t.request(ctx, http.MethodPut, temporary, false, file, nil)
	_ = file.Close()
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("WebDAV PUT answered HTTP %d", response.StatusCode)
	}
	destination, err := t.targetURL(key, false)
	if err != nil {
		return err
	}
	response, err = t.request(ctx, "MOVE", temporary, false, nil, map[string]string{"Destination": destination.String(), "Overwrite": "T"})
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("WebDAV MOVE answered HTTP %d", response.StatusCode)
	}
	moved = true
	return nil
}

func (t *WebDAVTarget) ensureFolder(ctx context.Context, key string) error {
	current := ""
	for _, segment := range strings.Split(key, "/") {
		if segment == "" {
			continue
		}
		current = path.Join(current, segment)
		response, err := t.request(ctx, "MKCOL", current, true, nil, nil)
		if err != nil {
			return err
		}
		closeResponse(response)
		if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusMethodNotAllowed {
			return fmt.Errorf("WebDAV MKCOL answered HTTP %d", response.StatusCode)
		}
	}
	return nil
}

func (t *WebDAVTarget) Get(ctx context.Context, key, destination string) error {
	response, err := t.request(ctx, http.MethodGet, key, false, nil, nil)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		status := response.StatusCode
		closeResponse(response)
		return fmt.Errorf("WebDAV GET answered HTTP %d", status)
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		closeResponse(response)
		return err
	}
	tmp := destination + ".part"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		closeResponse(response)
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	bodyErr := response.Body.Close()
	if copyErr == nil {
		copyErr = bodyErr
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		copyErr = os.Rename(tmp, destination)
	}
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	return nil
}

func (t *WebDAVTarget) Delete(ctx context.Context, key string) error {
	response, err := t.request(ctx, http.MethodDelete, key, false, nil, nil)
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("WebDAV DELETE answered HTTP %d", response.StatusCode)
	}
	return nil
}

func (t *WebDAVTarget) DeletePrefix(ctx context.Context, prefix string) error {
	if err := validTargetKey(prefix, false); err != nil {
		return err
	}
	response, err := t.request(ctx, http.MethodDelete, prefix, true, nil, nil)
	if err != nil {
		return err
	}
	closeResponse(response)
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("WebDAV DELETE answered HTTP %d", response.StatusCode)
	}
	return nil
}

const webDAVPropfind = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`

type webDAVMultiStatus struct {
	Responses []webDAVResponse `xml:"response"`
}
type webDAVResponse struct {
	Href  string `xml:"href"`
	Props []struct {
		Status string `xml:"status"`
		Prop   struct {
			ResourceType struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
		} `xml:"prop"`
	} `xml:"propstat"`
	Status string `xml:"status"`
}

func (r webDAVResponse) collection() bool {
	for _, props := range r.Props {
		if strings.Contains(props.Status, " 200 ") && props.Prop.ResourceType.Collection != nil {
			return true
		}
	}
	return false
}

func decodedURLSegments(u *url.URL) []string {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func closeResponse(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
}
