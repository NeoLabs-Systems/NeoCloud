package externalstorage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/outbound"
	"github.com/NeoLabs-Systems/neolabs-go/apperr"
)

// A WebDAV share spoken directly: PROPFIND, GET (with Range), PUT, MKCOL, MOVE,
// DELETE. Redirects are never followed -- the password would travel with them
// -- the owner is told where the server wants to go instead.

const (
	propfindBody    = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`
	maxListingBytes = 16 << 20
	listingTimeout  = time.Minute
)

type webdavAdapter struct {
	root               *url.URL
	username, password string
	client             *http.Client
}

func newWebDAV(config settings, guard outbound.Guard) (adapter, error) {
	root, err := url.Parse(config.text("url"))
	if err != nil {
		return nil, invalidConfig("The address must be a full address like https://example.com.")
	}
	if !strings.HasSuffix(root.Path, "/") {
		root.Path += "/"
		if root.RawPath != "" {
			root.RawPath += "/"
		}
	}
	root.RawQuery, root.Fragment = "", ""
	return &webdavAdapter{root: root, username: config.text("username"), password: config.text("password"), client: guard.HTTPClient()}, nil
}

func (a *webdavAdapter) urlFor(path string, folder bool) *url.URL {
	var escaped []string
	for _, segment := range strings.Split(path, "/") {
		if segment != "" {
			escaped = append(escaped, url.PathEscape(segment))
		}
	}
	suffix := strings.Join(escaped, "/")
	if folder && suffix != "" {
		suffix += "/"
	}
	target := *a.root
	target.RawPath = a.root.EscapedPath() + suffix
	target.Path, _ = url.PathUnescape(target.RawPath)
	return &target
}

func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return apperr.New(http.StatusBadGateway, "The server refused the user name or password.", "EXTERNAL_AUTH_FAILED")
	case http.StatusForbidden:
		return apperr.New(http.StatusBadGateway, "The server denied access.", "EXTERNAL_FORBIDDEN")
	case http.StatusNotFound:
		return notFound()
	case http.StatusConflict:
		return apperr.New(http.StatusConflict, "The folder does not exist at the provider.", "EXTERNAL_CONFLICT")
	case http.StatusPreconditionFailed, http.StatusMethodNotAllowed:
		return nameExists()
	case http.StatusLocked:
		return apperr.New(http.StatusConflict, "The item is locked at the provider.", "EXTERNAL_LOCKED")
	case http.StatusInsufficientStorage:
		return apperr.New(http.StatusInsufficientStorage, "The provider is out of space.", "EXTERNAL_FULL")
	}
	return apperr.New(http.StatusBadGateway, fmt.Sprintf("The server answered %d.", status), "EXTERNAL_ERROR")
}

// request sends one request and returns the response only for an expected status.
func (a *webdavAdapter) request(ctx context.Context, method string, target *url.URL, header map[string]string, body io.Reader, size int64, expect ...int) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.ContentLength = size
		if size == 0 {
			request.Body = http.NoBody
		}
	}
	request.SetBasicAuth(a.username, a.password)
	for key, value := range header {
		request.Header.Set(key, value)
	}
	response, err := a.client.Do(request)
	if errors.Is(err, outbound.ErrBlocked) {
		return nil, hostBlocked()
	}
	if err != nil {
		return nil, unreachable()
	}
	for _, status := range expect {
		if response.StatusCode == status {
			return response, nil
		}
	}
	response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		message := "The server redirected the request. Enter the address it redirects to."
		if location := response.Header.Get("Location"); location != "" {
			message += " It points to " + location + "."
		}
		return nil, apperr.New(http.StatusBadGateway, message, "EXTERNAL_REDIRECT")
	}
	return nil, statusError(response.StatusCode)
}

func (a *webdavAdapter) send(ctx context.Context, method string, target *url.URL, header map[string]string, body io.Reader, size int64, expect ...int) error {
	response, err := a.request(ctx, method, target, header, body, size, expect...)
	if err == nil {
		response.Body.Close()
	}
	return err
}

func (a *webdavAdapter) propfind(ctx context.Context, target *url.URL, depth int) ([]outbound.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, listingTimeout)
	defer cancel()
	header := map[string]string{"Depth": strconv.Itoa(depth), "Content-Type": "application/xml; charset=utf-8"}
	response, err := a.request(ctx, "PROPFIND", target, header, strings.NewReader(propfindBody), int64(len(propfindBody)), http.StatusMultiStatus)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	listing, err := outbound.ReadMultistatus(response.Body, maxListingBytes)
	switch {
	case errors.Is(err, outbound.ErrListingTooLarge):
		return nil, apperr.New(http.StatusBadGateway, "The folder listing is too large.", "EXTERNAL_TOO_LARGE")
	case errors.Is(err, outbound.ErrNotWebDAV):
		return nil, apperr.New(http.StatusBadGateway, "This does not look like a WebDAV server.", "EXTERNAL_NOT_WEBDAV")
	case err != nil:
		return nil, unreachable()
	}
	return listing, nil
}

func davEntry(name string, resource outbound.Response) entry {
	found := entry{name: name, folder: resource.IsCollection()}
	if !found.folder {
		if size, err := strconv.ParseInt(resource.Text("getcontentlength"), 10, 64); err == nil && size >= 0 {
			found.size = size
		}
	}
	found.modified, _ = http.ParseTime(resource.Text("getlastmodified"))
	return found
}

func (a *webdavAdapter) open(ctx context.Context) error {
	_, err := a.propfind(ctx, a.root, 0)
	return err
}

func (a *webdavAdapter) close() { a.client.CloseIdleConnections() }

func (a *webdavAdapter) list(ctx context.Context, path string, limit int) ([]entry, bool, error) {
	target := a.urlFor(path, true)
	listing, err := a.propfind(ctx, target, 1)
	if err != nil {
		return nil, false, err
	}
	base := outbound.Segments(target.EscapedPath())
	entries := []entry{}
	for _, resource := range listing {
		href, err := url.Parse(resource.Href)
		if err != nil || resource.Href == "" {
			continue
		}
		rest, ok := outbound.Below(base, outbound.Segments(target.ResolveReference(href).EscapedPath()))
		if !ok || len(rest) != 1 || !safeListedName(rest[0]) {
			continue
		}
		if len(entries) == limit {
			return entries, true, nil
		}
		entries = append(entries, davEntry(rest[0], resource))
	}
	return entries, false, nil
}

func missing(err error) bool {
	known, ok := apperr.As(err)
	return ok && known.Code == "EXTERNAL_NOT_FOUND"
}

// stat asks for the folder first (servers redirect a folder named without its
// slash), then for the file (strict servers know no file with one).
func (a *webdavAdapter) stat(ctx context.Context, path string) (*entry, error) {
	listing, err := a.propfind(ctx, a.urlFor(path, true), 0)
	if missing(err) && path != "" {
		listing, err = a.propfind(ctx, a.urlFor(path, false), 0)
	}
	if missing(err) {
		return nil, nil
	}
	if err != nil || len(listing) == 0 {
		return nil, err
	}
	found := davEntry(baseName(path), listing[0])
	return &found, nil
}

func (a *webdavAdapter) read(ctx context.Context, path string, part *byteRange) (io.ReadCloser, error) {
	header := map[string]string{}
	expect := []int{http.StatusOK}
	if part != nil {
		header["Range"] = fmt.Sprintf("bytes=%d-%d", part.start, part.end)
		expect = append(expect, http.StatusPartialContent)
	}
	response, err := a.request(ctx, http.MethodGet, a.urlFor(path, false), header, nil, 0, expect...)
	if err != nil {
		return nil, err
	}
	if part == nil || response.StatusCode == http.StatusPartialContent {
		return response.Body, nil
	}
	// The server ignored the range: cut it out here.
	if _, err = io.CopyN(io.Discard, response.Body, part.start); err != nil {
		response.Body.Close()
		return nil, unreachable()
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(response.Body, part.end-part.start+1), response.Body}, nil
}

func (a *webdavAdapter) write(ctx context.Context, path string, body io.Reader, size int64) error {
	// NopCloser: the transport must not close the caller's stream.
	return a.send(ctx, http.MethodPut, a.urlFor(path, false), nil, io.NopCloser(body), size, http.StatusOK, http.StatusCreated, http.StatusNoContent)
}

func (a *webdavAdapter) mkdir(ctx context.Context, path string) error {
	return a.send(ctx, "MKCOL", a.urlFor(path, true), nil, nil, 0, http.StatusCreated)
}

func (a *webdavAdapter) move(ctx context.Context, from, to string) error {
	header := map[string]string{"Destination": a.urlFor(to, false).String(), "Overwrite": "F"}
	return a.send(ctx, "MOVE", a.urlFor(from, false), header, nil, 0, http.StatusCreated, http.StatusNoContent)
}

func (a *webdavAdapter) remove(ctx context.Context, path string) error {
	return a.send(ctx, http.MethodDelete, a.urlFor(path, false), nil, nil, 0, http.StatusOK, http.StatusNoContent)
}
