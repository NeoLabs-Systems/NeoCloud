package imports

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// A read-only WebDAV client for imports: lists one folder at a time (PROPFIND
// Depth 1 -- Nextcloud often refuses infinity) and streams files down. It
// follows redirects itself, keeping credentials on the same origin only, and
// retries 401 once and 429, 5xx and network failures with backoff.

const (
	propfindBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns" xmlns:nc="http://nextcloud.org/ns"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/><d:quota-used-bytes/><oc:size/><oc:favorite/></d:prop></d:propfind>`

	maxRedirects      = 5
	maxRetryAfter     = time.Minute
	maxListingBytes   = 16 << 20
	listingTimeout    = 2 * time.Minute
	defaultRetries    = 4
	defaultRetryDelay = 500 * time.Millisecond
)

var (
	redirectStatuses = []int{301, 302, 303, 307, 308}
	retryStatuses    = []int{429, 500, 502, 503, 504}
)

type remoteItem struct {
	name     string
	folder   bool
	size     int64
	mtime    string // "YYYY-MM-DD HH:MM:SS" in UTC, empty when unknown
	favorite bool
}

type client struct {
	root, fallback     *url.URL
	sourcePath         []string
	username, password string
	http               *http.Client
	retries            int
	retryDelay         time.Duration
}

// clientFor builds a client under the account's network rules.
func (h *Handler) clientFor(ctx context.Context, userID int64, c connection, retries int) (*client, error) {
	allowed := h.allowPrivate
	if !allowed && h.isAdmin != nil {
		var err error
		if allowed, err = h.isAdmin(ctx, userID); err != nil {
			return nil, err
		}
	}
	guard := outbound.Guard{Resolve: h.resolveHost}
	if !allowed {
		guard.Check = ssrf.CheckAddr
	}
	rootAddress, fallbackAddress := c.roots()
	root, err := url.Parse(rootAddress)
	if err != nil {
		return nil, invalid("Enter the server address, like https://cloud.example.com.", "INVALID_URL")
	}
	built := &client{
		root: root, username: c.username, password: c.password, http: guard.HTTPClient(),
		sourcePath: strings.FieldsFunc(c.sourcePath, func(r rune) bool { return r == '/' }),
		retries:    retries, retryDelay: h.retryDelay,
	}
	if fallbackAddress != "" {
		if built.fallback, err = url.Parse(fallbackAddress); err != nil {
			return nil, err
		}
	}
	return built, nil
}

func (c *client) close() { c.http.CloseIdleConnections() }

func remoteError(message, code string) error {
	return apperr.New(http.StatusBadGateway, message, code)
}

func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return remoteError("The server refused the user name or app password.", "REMOTE_AUTH_FAILED")
	case http.StatusForbidden:
		return remoteError("The server denied access.", "REMOTE_FORBIDDEN")
	case http.StatusNotFound:
		return remoteError("Not found on the server.", "REMOTE_NOT_FOUND")
	case http.StatusTooManyRequests:
		return remoteError("The server is limiting requests; try again later.", "REMOTE_RATE_LIMITED")
	}
	return remoteError(fmt.Sprintf("The server answered %d.", status), "REMOTE_ERROR")
}

func (c *client) urlFor(relative string, folder bool) *url.URL {
	var escaped []string
	for _, segment := range slices.Concat(c.sourcePath, strings.FieldsFunc(relative, func(r rune) bool { return r == '/' })) {
		escaped = append(escaped, url.PathEscape(segment))
	}
	suffix := strings.Join(escaped, "/")
	if folder && suffix != "" {
		suffix += "/"
	}
	target := *c.root
	target.RawPath = c.root.EscapedPath() + suffix
	target.Path, _ = url.PathUnescape(target.RawPath)
	return &target
}

func origin(address *url.URL) string {
	port := address.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[address.Scheme]
	}
	return address.Scheme + "://" + strings.ToLower(address.Hostname()) + ":" + port
}

// send follows redirects by hand, so the password goes to the same origin only
// and the final address is known. The guard checks every connection.
func (c *client) send(ctx context.Context, method string, target *url.URL, depth string) (*http.Response, *url.URL, error) {
	for range maxRedirects + 1 {
		var body io.Reader
		if method == "PROPFIND" {
			body = strings.NewReader(propfindBody)
		}
		request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
		if err != nil {
			return nil, nil, err
		}
		if depth != "" {
			request.Header.Set("Depth", depth)
			request.Header.Set("Content-Type", "application/xml; charset=utf-8")
		}
		if origin(target) == origin(c.root) {
			request.SetBasicAuth(c.username, c.password)
		}
		response, err := c.http.Do(request)
		if err != nil {
			return nil, nil, err
		}
		if !slices.Contains(redirectStatuses, response.StatusCode) {
			return response, target, nil
		}
		response.Body.Close()
		location := response.Header.Get("Location")
		if location == "" {
			return nil, nil, statusError(response.StatusCode)
		}
		next, err := target.Parse(location)
		if err != nil || next.Scheme != "http" && next.Scheme != "https" {
			return nil, nil, remoteError("The server redirected somewhere unsupported.", "REMOTE_ERROR")
		}
		target = next
	}
	return nil, nil, remoteError("The server redirected too many times.", "REMOTE_TOO_MANY_REDIRECTS")
}

func (c *client) backoff(response *http.Response, attempt int) time.Duration {
	if response != nil {
		if header := response.Header.Get("Retry-After"); header != "" {
			if seconds, err := strconv.Atoi(header); err == nil {
				return min(max(time.Duration(seconds)*time.Second, 0), maxRetryAfter)
			}
			if when, err := http.ParseTime(header); err == nil {
				return min(max(time.Until(when), 0), maxRetryAfter)
			}
		}
	}
	return c.retryDelay<<attempt + rand.N(c.retryDelay+1)
}

func pause(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

// request returns the response with the expected status and the address it came
// from.
func (c *client) request(ctx context.Context, method string, target *url.URL, depth string, expect int) (*http.Response, *url.URL, error) {
	authRetried := false
	for attempt := 0; ; attempt++ {
		response, final, err := c.send(ctx, method, target, depth)
		if err != nil {
			if errors.Is(err, outbound.ErrBlocked) {
				return nil, nil, apperr.New(http.StatusForbidden, "Only an admin can import from a server on a private network.", "IMPORT_HOST_BLOCKED")
			}
			if _, ours := apperr.As(err); ours || ctx.Err() != nil {
				return nil, nil, err
			}
			if attempt >= c.retries {
				var failure *url.Error
				if errors.As(err, &failure) {
					err = failure.Err
				}
				return nil, nil, remoteError("Could not reach the server ("+err.Error()+").", "REMOTE_UNREACHABLE")
			}
			if err = pause(ctx, c.backoff(nil, attempt)); err != nil {
				return nil, nil, err
			}
			continue
		}
		if response.StatusCode == expect {
			return response, final, nil
		}
		response.Body.Close()
		retryAuth := response.StatusCode == http.StatusUnauthorized && !authRetried
		if !retryAuth && (!slices.Contains(retryStatuses, response.StatusCode) || attempt >= c.retries) {
			return nil, nil, statusError(response.StatusCode)
		}
		authRetried = authRetried || retryAuth
		if err = pause(ctx, c.backoff(response, attempt)); err != nil {
			return nil, nil, err
		}
	}
}

func (c *client) propfind(ctx context.Context, target *url.URL, depth string) ([]outbound.Response, *url.URL, error) {
	timed, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	var listing []outbound.Response
	response, final, err := c.request(timed, "PROPFIND", target, depth, http.StatusMultiStatus)
	if err == nil {
		defer response.Body.Close()
		listing, err = outbound.ReadMultistatus(response.Body, maxListingBytes)
	}
	switch {
	case err == nil:
		return listing, final, nil
	case ctx.Err() != nil:
		return nil, nil, context.Cause(ctx)
	case errors.Is(err, outbound.ErrNotWebDAV):
		return nil, nil, remoteError("This does not look like a WebDAV server.", "REMOTE_NOT_WEBDAV")
	case errors.Is(err, outbound.ErrListingTooLarge):
		return nil, nil, remoteError("The server sent a folder listing that is too large.", "REMOTE_ERROR")
	case timed.Err() != nil:
		return nil, nil, remoteError("The server took too long to answer.", "REMOTE_UNREACHABLE")
	}
	return nil, nil, err
}

// connect checks the account root, moving to where redirects lead (http to
// https, a sub-path) and to the fallback if the root does not exist.
func (c *client) connect(ctx context.Context) error {
	_, final, err := c.propfind(ctx, c.root, "0")
	if known, ok := apperr.As(err); ok && known.Code == "REMOTE_NOT_FOUND" && c.fallback != nil {
		c.root = c.fallback
		_, final, err = c.propfind(ctx, c.root, "0")
	}
	if err != nil {
		return err
	}
	root := *final
	if !strings.HasSuffix(root.Path, "/") {
		root.Path += "/"
		if root.RawPath != "" {
			root.RawPath += "/"
		}
	}
	root.RawQuery, root.Fragment = "", ""
	c.root = &root
	return nil
}

func count(value string) (int64, bool) {
	number, err := strconv.ParseInt(value, 10, 64)
	return number, err == nil && number >= 0
}

func toItem(resource outbound.Response) remoteItem {
	item := remoteItem{folder: resource.IsCollection(), favorite: resource.Text("favorite") == "1"}
	if item.folder {
		var ok bool
		if item.size, ok = count(resource.Text("size")); !ok {
			item.size, _ = count(resource.Text("quota-used-bytes"))
		}
	} else {
		item.size, _ = count(resource.Text("getcontentlength"))
	}
	if modified, err := http.ParseTime(resource.Text("getlastmodified")); err == nil {
		item.mtime = modified.UTC().Format(time.DateTime)
	}
	return item
}

// list returns the folder's own size and its direct children. Names stay as the
// server spells them, since requests must match byte for byte.
func (c *client) list(ctx context.Context, relative string) (int64, []remoteItem, error) {
	target := c.urlFor(relative, true)
	listing, final, err := c.propfind(ctx, target, "1")
	if err != nil {
		return 0, nil, err
	}
	base := outbound.Segments(target.EscapedPath())
	var own *remoteItem
	var children []remoteItem
	var sum int64
	for _, resource := range listing {
		href, err := url.Parse(resource.Href)
		if err != nil || resource.Href == "" {
			continue
		}
		rest, ok := outbound.Below(base, outbound.Segments(final.ResolveReference(href).EscapedPath()))
		if !ok {
			continue
		}
		item := toItem(resource)
		switch {
		case len(rest) == 0:
			own = &item
		case len(rest) == 1 && rest[0] != "." && rest[0] != ".." && !strings.ContainsAny(rest[0], "/\x00"):
			item.name = rest[0]
			sum += item.size
			children = append(children, item)
		}
	}
	if own != nil {
		sum = max(own.size, sum)
	}
	return sum, children, nil
}

// download opens a file; the size is -1 when the server does not say.
func (c *client) download(ctx context.Context, relative string) (io.ReadCloser, int64, error) {
	response, _, err := c.request(ctx, http.MethodGet, c.urlFor(relative, false), "", http.StatusOK)
	if err != nil {
		return nil, 0, err
	}
	return response.Body, response.ContentLength, nil
}
