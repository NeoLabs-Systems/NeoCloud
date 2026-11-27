package webdav

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const AllowedMethods = "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, COPY, MOVE, PROPFIND, PROPPATCH, LOCK, UNLOCK"

type Principal struct {
	UserID   int64
	Username string
}
type AuthenticateFunc func(context.Context, Credentials) (Principal, bool, error)
type DispatchFunc func(http.ResponseWriter, *http.Request, Principal, *Target) error

type Options struct {
	Authenticate AuthenticateFunc
	Dispatch     DispatchFunc
	// AllowedOrigin decides about requests that carry an Origin header.
	AllowedOrigin func(string) bool
	// ClientIP names the address failed sign-ins are counted against.
	ClientIP func(*http.Request) string
}

type Handler struct {
	options  Options
	failures *failureLimiter
}

func NewHandler(options Options) *Handler {
	if options.ClientIP == nil {
		options.ClientIP = func(r *http.Request) string {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				return r.RemoteAddr
			}
			return host
		}
	}
	return &Handler{options: options, failures: newFailureLimiter()}
}

// HTTPError is a failure answered with its own status. Condition names the
// DAV: precondition element of the error body, if any.
type HTTPError struct {
	Status    int
	Message   string
	Condition string
}

func (e HTTPError) Error() string   { return e.Message }
func (e HTTPError) HTTPStatus() int { return e.Status }

// NeoCloud's error codes where WebDAV expects a different status.
var statusForCode = map[string]int{
	"QUOTA_EXCEEDED": http.StatusInsufficientStorage,
	"DISK_FULL":      http.StatusInsufficientStorage,
	"NAME_EXISTS":    http.StatusConflict,
	"NOT_A_FOLDER":   http.StatusConflict,
	"NOT_A_FILE":     http.StatusConflict,
	"MOVE_INTO_SELF": http.StatusConflict,
	"COPY_INTO_SELF": http.StatusConflict,
	"ROOT_IMMUTABLE": http.StatusForbidden,
	"VAULT_BOUNDARY": http.StatusForbidden,
	"ETAG_MISMATCH":  http.StatusPreconditionFailed,
}

func wellKnown(path string) bool {
	return strings.EqualFold(strings.TrimSuffix(path, "/"), "/.well-known/webdav")
}

// RawPath is the request path as the client encoded it.
func RawPath(r *http.Request) string {
	if r.URL.RawPath != "" {
		return r.URL.RawPath
	}
	return r.URL.EscapedPath()
}

// Serves reports whether the request belongs to WebDAV and so bypasses the
// API middleware: clients send no cookie and no Origin, stream their bodies and
// use methods CORS would answer itself.
func Serves(r *http.Request) bool {
	path := RawPath(r)
	if wellKnown(path) {
		return true
	}
	target, err := ParsePath(path)
	return target != nil || err != nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := RawPath(r)
	if wellKnown(path) {
		http.Redirect(w, r, "/remote.php/webdav/", http.StatusMovedPermanently)
		return
	}
	target, err := ParsePath(path)
	if err != nil {
		fail(w, r, HTTPError{Status: http.StatusBadRequest, Message: err.Error()})
		return
	}
	if target == nil {
		http.NotFound(w, r)
		return
	}
	header := w.Header()
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cache-Control", "no-store")
	// WebDAV clients send no Origin; a browser page on another site does.
	if origin := r.Header.Get("Origin"); origin != "" && (h.options.AllowedOrigin == nil || !h.options.AllowedOrigin(origin)) {
		fail(w, r, HTTPError{Status: http.StatusForbidden, Message: "Origin not allowed."})
		return
	}
	// Windows asks OPTIONS before it sends credentials.
	if r.Method == http.MethodOptions {
		header.Set("Allow", AllowedMethods)
		header.Set("DAV", "1, 2")
		header.Set("MS-Author-Via", "DAV")
		header.Set("Accept-Ranges", "bytes")
		header.Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
		return
	}
	principal, ok := h.signIn(w, r)
	if !ok {
		return
	}
	if !slices.Contains(strings.Split(AllowedMethods, ", "), r.Method) {
		header.Set("Allow", AllowedMethods)
		fail(w, r, HTTPError{Status: http.StatusMethodNotAllowed, Message: "Method not allowed."})
		return
	}
	if target.Username != "" && !strings.EqualFold(target.Username, principal.Username) {
		fail(w, r, HTTPError{Status: http.StatusNotFound, Message: "File or folder not found"})
		return
	}
	if err := h.options.Dispatch(w, r, principal, target); err != nil {
		fail(w, r, err)
	}
}

// signIn checks the app password. Requests without credentials (the first leg
// of the Basic challenge) and successful ones don't count against the limit.
func (h *Handler) signIn(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	challenge := func() {
		w.Header().Set("WWW-Authenticate", `Basic realm="NeoCloud", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
	credentials, ok := BasicCredentials(r.Header.Get("Authorization"))
	if !ok {
		challenge()
		return Principal{}, false
	}
	release, retry := h.failures.reserve(h.options.ClientIP(r), credentials.Username, time.Now())
	if release == nil {
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
		fail(w, r, HTTPError{Status: http.StatusTooManyRequests, Message: "Too many failed sign-ins. Try again later."})
		return Principal{}, false
	}
	principal, ok, err := h.options.Authenticate(r.Context(), credentials)
	if err != nil {
		release()
		fail(w, r, err)
		return Principal{}, false
	}
	if !ok {
		challenge()
		return Principal{}, false
	}
	release()
	return principal, true
}

// fail writes a sabre-style error body, which Nextcloud clients can read.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	status, message, condition, code := http.StatusInternalServerError, err.Error(), "", ""
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		code = coded.ErrorCode()
	}
	var withStatus interface{ HTTPStatus() int }
	if errors.As(err, &withStatus) {
		status = withStatus.HTTPStatus()
	}
	if mapped, ok := statusForCode[code]; ok {
		status = mapped
	}
	var own HTTPError
	if errors.As(err, &own) {
		condition = own.Condition
	} else if status >= 500 && code == "" {
		slog.Error("WebDAV request failed", "method", r.Method, "error", err)
		status, message = http.StatusInternalServerError, "Something went wrong."
	}
	if status == http.StatusLocked && condition == "" {
		condition = "lock-token-submitted"
	}
	// A refused upload may still be sending its body; closing the connection
	// after the answer stops it without draining the socket.
	if r.Method == http.MethodPut && status >= 400 {
		w.Header().Set("Connection", "close")
	}
	body := Element(Sabre, "message", Escape(message))
	if condition != "" {
		body = Element(DAV, condition, "") + body
	}
	WriteXML(w, status, Document("error", body))
}
