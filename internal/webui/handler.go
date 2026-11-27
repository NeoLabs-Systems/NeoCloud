// Package webui serves the pages NeoCloud ships: the Flutter web client under
// /app, the landing and status pages, the public page of a share link and the
// 404 page.
package webui

import (
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/analytics"
	"github.com/NeoLabs-Systems/neolabs-go/httpx"
)

const noStore = "no-store, no-cache, must-revalidate, proxy-revalidate"

const notBuilt = `The NeoCloud web app is not built yet. Run "flutter build web --no-web-resources-cdn" in flutter_app.`

var signingPath = regexp.MustCompile(`(?i)^/app/sign(?:/|$)`)
var noStoreFiles = map[string]bool{
	"index.html":                true,
	"flutter_bootstrap.js":      true,
	"flutter_service_worker.js": true,
	"version.json":              true,
}

// A signing link is its own credential: never indexed, never sent on in a
// Referer, never framed, and the page may only talk to this server.
const signingCSP = "default-src 'self'; script-src 'self' 'unsafe-inline' blob: 'wasm-unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; connect-src 'self' data: blob:; font-src 'self' data:; worker-src 'self' blob:; frame-src 'self' blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'"

type Options struct {
	// WebDir is the root of a Flutter web build produced with
	// --no-web-resources-cdn. It may be absent at startup; app navigation then
	// answers 503 until assets are installed.
	WebDir string
	// LandingDir holds the landing, share, status and 404 pages.
	LandingDir string
	// Analytics adds the browser analytics script to the landing and app pages.
	Analytics bool
}

type Handler struct{ options Options }

func New(options Options) (*Handler, error) {
	for _, dir := range []*string{&options.WebDir, &options.LandingDir} {
		if strings.TrimSpace(*dir) == "" {
			return nil, errors.New("webui: web and landing directories are required")
		}
		absolute, err := filepath.Abs(*dir)
		if err != nil {
			return nil, err
		}
		*dir = absolute
	}
	return &Handler{options: options}, nil
}

// Routes registers the pages. "/" also answers every path no other route took.
func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /app", h.app)
	mux.HandleFunc("GET /app/", h.app)
	mux.HandleFunc("GET /s/{token}", h.share)
	mux.HandleFunc("GET /s/{token}/{$}", h.share)
	mux.HandleFunc("GET /status", h.status)
	if h.options.Analytics {
		mux.Handle("GET "+analytics.ScriptPath, analytics.Script())
	}
	mux.HandleFunc("/", h.site)
}

func (h *Handler) app(w http.ResponseWriter, r *http.Request) {
	if signingPath.MatchString(r.URL.Path) {
		setNoStore(w.Header())
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Content-Security-Policy", signingCSP)
	}
	relative, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/app"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	relative = strings.TrimPrefix(relative, "/")
	if relative == "" || relative == "index.html" || !h.serveFile(w, r, h.options.WebDir, relative, noStoreFiles[relative]) {
		h.shell(w, r)
	}
}

// shell is the app's entry page. Flutter resolves its assets against
// <base href>; the app lives under /app/.
func (h *Handler) shell(w http.ResponseWriter, r *http.Request) {
	data, err := readUnder(h.options.WebDir, "index.html")
	if err != nil {
		http.Error(w, notBuilt, http.StatusServiceUnavailable)
		return
	}
	page := strings.Replace(string(data), `<base href="/">`, `<base href="/app/">`, 1)
	if !signingPath.MatchString(r.URL.Path) {
		page = injectNotice(page)
		if h.options.Analytics {
			page = analytics.Inject(page)
		}
	}
	writePage(w, r, page)
}

func injectNotice(page string) string {
	return strings.Replace(page, "</head>", `<script defer src="/js/cookie-notice.js"></script>`+"\n</head>", 1)
}

// share is the public page of a share link; it reads the token from its own
// URL. The token is the secret, so it must not travel on in a Referer.
func (h *Handler) share(w http.ResponseWriter, r *http.Request) {
	setNoStore(w.Header())
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if !h.serveFile(w, r, h.options.LandingDir, "share.html", false) {
		h.missing(w, r)
	}
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	if !h.serveFile(w, r, h.options.LandingDir, "status.html", false) {
		h.missing(w, r)
	}
}

// site serves the landing page and its assets, and answers everything no route
// took: JSON for the API, the 404 page for a browser.
func (h *Handler) site(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		httpx.ErrorCode(w, http.StatusNotFound, "Not found", "NOT_FOUND")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	landing, err := readUnder(h.options.LandingDir, "index.html")
	if err != nil && r.URL.Path == "/" {
		http.Redirect(w, r, "/app/", http.StatusFound)
		return
	}
	if err == nil && (r.URL.Path == "/" || r.URL.Path == "/index.html") {
		if h.options.Analytics {
			writePage(w, r, analytics.Inject(string(landing)))
			return
		}
		h.serveFile(w, r, h.options.LandingDir, "index.html", false)
		return
	}
	if h.serveFile(w, r, h.options.LandingDir, strings.TrimPrefix(r.URL.Path, "/"), false) {
		return
	}
	h.missing(w, r)
}

func (h *Handler) missing(w http.ResponseWriter, r *http.Request) {
	if prefersHTML(r.Header.Get("Accept")) {
		if page, err := readUnder(h.options.LandingDir, "404.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			if r.Method != http.MethodHead {
				_, _ = w.Write(page)
			}
			return
		}
	}
	http.NotFound(w, r)
}

// prefersHTML reports a browser navigation: HTML named before JSON.
func prefersHTML(accept string) bool {
	html := strings.Index(accept, "text/html")
	json := strings.Index(accept, "application/json")
	return html >= 0 && (json < 0 || html < json)
}

// serveFile serves one regular file below root and reports whether it did.
// Hidden files stay private.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, root, relative string, uncached bool) bool {
	clean := path.Clean(relative)
	if !fs.ValidPath(clean) || strings.HasPrefix(clean, ".") || strings.Contains(clean, "/.") || strings.Contains(clean, `\`) {
		return false
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return false
	}
	defer directory.Close()
	file, err := directory.Open(clean)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if uncached {
		setNoStore(w.Header())
	}
	if contentType := mime.TypeByExtension(path.Ext(clean)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, path.Base(clean), info.ModTime(), file)
	return true
}

func readUnder(root, name string) ([]byte, error) {
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer directory.Close()
	file, err := directory.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func writePage(w http.ResponseWriter, r *http.Request, page string) {
	setNoStore(w.Header())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(page))
}

func setNoStore(header http.Header) {
	header.Set("Cache-Control", noStore)
	header.Set("Pragma", "no-cache")
	header.Set("Expires", "0")
}
