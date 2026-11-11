// Package settingsui is the lite client's app window: a tiny HTTP server
// on 127.0.0.1 with a random port.
//
// Other local users can reach that port, so every request needs a session.
// The tray opens a one-time launch URL; its code is short-lived, works once
// and is exchanged for an HttpOnly, SameSite=Strict session cookie. No
// lasting secret ever appears on a command line (an opener's or browser's
// argv is readable by other users on many systems). API calls also need
// the X-Requested-With header, which a page from another origin cannot
// send without a CORS preflight this server never grants. The Host check
// keeps out DNS rebinding.
package settingsui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	gosync "sync"
	"time"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/autostart"
	"github.com/neolabs/neocloud/clients/lite/internal/browser"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/keylock"
	"github.com/neolabs/neocloud/clients/lite/internal/qr"
	"github.com/neolabs/neocloud/clients/lite/internal/sync"
)

// ui is the page: index.html plus the stylesheet, script and logo it loads.
//
//go:embed ui
var ui embed.FS

// assetTypes are the files under ui/ served at /assets/<name>.
var assetTypes = map[string]string{
	"app.css":  "text/css; charset=utf-8",
	"app.js":   "text/javascript; charset=utf-8",
	"logo.svg": "image/svg+xml",
}

// pageCSP allows nothing but this server's own files (and the QR code's
// data: URI): no inline script, nothing from the network.
const pageCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Options are the page's ties to the rest of the app.
type Options struct {
	// LogPath is the log file the page offers to open ("" when the
	// client logs to a terminal).
	LogPath string
	// WindowProfile is the browser profile folder of the app window.
	WindowProfile string
}

// Server serves the app window's page and API.
type Server struct {
	engine *sync.Engine
	cfg    *config.Store
	lock   *keylock.Lock
	opts   Options
	host   string
	cookie string // the session cookie's name, unique per port
	http   *http.Server

	// Seams for the tests: the OS dialogs and openers.
	run      commandRunner
	lookPath func(string) (string, error)
	open     func(string) error
	openApp  func(url, profileDir string) (bool, error)
	closeApp func(profileDir string) error
	picking  gosync.Mutex // one folder dialog at a time

	mu       gosync.Mutex
	login    loginState
	launches map[string]time.Time // unused launch codes → expiry (zero: none)
	sessions map[string]int       // session → its number; the highest is the newest window
	seq      int
	openedAt time.Time
	windows  int       // pages holding /api/window
	goneAt   time.Time // when the last of them let go
	quota    map[string]int64
	quotaAt  time.Time
}

const (
	// LaunchTTL is how long a launch URL opened from the tray stays valid.
	LaunchTTL = 2 * time.Minute
	// apiHeader must be present (with any value) on API calls.
	apiHeader = "X-Requested-With"
)

type loginState struct {
	Pending         bool   `json:"pending"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURL string `json:"verificationUrl,omitempty"`
	QRCode          string `json:"qrCode,omitempty"` // data: URI of an SVG of VerificationURL
	Error           string `json:"error,omitempty"`
	cancel          context.CancelFunc
}

// Start listens on a random loopback port.
func Start(engine *sync.Engine, cfg *config.Store, lock *keylock.Lock, opts Options) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{
		engine:   engine,
		cfg:      cfg,
		lock:     lock,
		opts:     opts,
		host:     listener.Addr().String(),
		cookie:   fmt.Sprintf("neocloud_lite_%d", listener.Addr().(*net.TCPAddr).Port),
		run:      runCommand,
		lookPath: exec.LookPath,
		open:     browser.Open,
		openApp:  browser.OpenApp,
		closeApp: browser.CloseApp,
		launches: map[string]time.Time{},
		sessions: map[string]int{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /launch", s.launch)
	mux.HandleFunc("GET /{$}", s.servePage)
	mux.HandleFunc("GET /assets/{name}", s.serveAsset)
	mux.HandleFunc("GET /api/state", s.api(s.state))
	mux.HandleFunc("GET /api/window", s.holdWindow)
	mux.HandleFunc("POST /api/login", s.api(s.startLogin))
	mux.HandleFunc("DELETE /api/login", s.api(s.cancelLogin))
	mux.HandleFunc("POST /api/logout", s.api(s.logout))
	mux.HandleFunc("POST /api/pairs", s.api(s.addPair))
	mux.HandleFunc("PUT /api/pairs/{id}", s.api(s.updatePair))
	mux.HandleFunc("DELETE /api/pairs/{id}", s.api(s.removePair))
	mux.HandleFunc("POST /api/pairs/{id}/confirm-deletes", s.api(s.confirmDeletes))
	mux.HandleFunc("PUT /api/settings", s.api(s.updateSettings))
	mux.HandleFunc("POST /api/key-lock", s.api(s.enrollKey))
	mux.HandleFunc("DELETE /api/key-lock", s.api(s.removeKey))
	mux.HandleFunc("POST /api/sync", s.api(func(*http.Request) (any, error) { engine.SyncNow(); return ok(), nil }))
	mux.HandleFunc("POST /api/pairs/{id}/sync", s.api(s.syncPair))
	mux.HandleFunc("POST /api/pairs/{id}/open", s.api(s.openPair))
	mux.HandleFunc("POST /api/pick-folder", s.api(s.pickFolder))
	mux.HandleFunc("GET /api/remote/folders", s.api(s.remoteFolders))
	mux.HandleFunc("POST /api/remote/folders", s.api(s.createRemoteFolder))
	mux.HandleFunc("GET /api/account", s.api(s.account))
	mux.HandleFunc("POST /api/open/{target}", s.api(s.openTarget))
	s.http = &http.Server{Handler: s.guard(mux), ReadHeaderTimeout: 10 * time.Second}
	go s.http.Serve(listener)
	return s, nil
}

// LaunchURL mints a one-time URL that opens the page. ttl bounds how long
// it stays valid unused; 0 keeps it until it is used.
func (s *Server) LaunchURL(ttl time.Duration) string {
	code := randomHex()
	var expiry time.Time
	if ttl > 0 {
		expiry = time.Now().Add(ttl)
	}
	s.mu.Lock()
	for other, at := range s.launches {
		if !at.IsZero() && time.Now().After(at) {
			delete(s.launches, other)
		}
	}
	s.launches[code] = expiry
	s.mu.Unlock()
	return "http://" + s.host + "/launch?code=" + code
}

// Close stops the server.
func (s *Server) Close() error { return s.http.Close() }

func randomHex() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		panic(err) // crypto/rand never fails on supported systems
	}
	return hex.EncodeToString(buf)
}

// launch spends a launch code and starts a session.
func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	s.mu.Lock()
	expiry, known := s.launches[code]
	delete(s.launches, code)
	valid := known && code != "" && (expiry.IsZero() || time.Now().Before(expiry))
	session := ""
	if valid {
		session = randomHex()
		s.seq++
		s.sessions[session] = s.seq
	}
	s.mu.Unlock()
	if !valid {
		http.Error(w, "This link was already used or has expired. Choose Open NeoCloud in the tray menu again.", http.StatusForbidden)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookie,
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// hasSession reports whether r carries a session cookie of this server.
func (s *Server) hasSession(r *http.Request) bool {
	cookie, err := r.Cookie(s.cookie)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for session := range s.sessions {
		if subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(session)) == 1 {
			return true
		}
	}
	return false
}

// guard rejects requests for another Host (DNS rebinding), requests
// without a session (the launch URL aside) and API calls without the
// API header or from another site.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != s.host {
			http.Error(w, "wrong host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/launch" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.hasSession(r) {
			http.Error(w, "Choose Open NeoCloud in the tray menu.", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			site := r.Header.Get("Sec-Fetch-Site")
			if r.Header.Get(apiHeader) == "" || (site != "" && site != "same-origin") {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) servePage(w http.ResponseWriter, _ *http.Request) {
	page, _ := ui.ReadFile("ui/index.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", pageCSP)
	w.Write(page)
}

func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	contentType, known := assetTypes[name]
	if !known {
		http.NotFound(w, r)
		return
	}
	body, _ := ui.ReadFile("ui/" + name)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(body)
}

func (s *Server) api(handler func(*http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := handler(r)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(result)
	}
}

func ok() map[string]bool { return map[string]bool{"success": true} }

func decode(r *http.Request, into any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(into)
}

func (s *Server) state(r *http.Request) (any, error) {
	cfg := s.cfg.Get()
	seq := s.sessionSeq(r)
	s.mu.Lock()
	login := s.login
	superseded := seq < s.seq
	s.mu.Unlock()
	home, _ := os.UserHomeDir()
	pairs := cfg.Pairs
	if pairs == nil {
		pairs = []config.Pair{}
	}
	return map[string]any{
		"loggedIn":      cfg.LoggedIn(),
		"server":        cfg.Server,
		"username":      cfg.Username,
		"deviceName":    cfg.DeviceName,
		"paused":        cfg.Paused,
		"uploadKiBps":   cfg.UploadKiBps,
		"downloadKiBps": cfg.DownloadKiBps,
		"autostart":     autostart.Enabled(),
		"pairs":         pairs,
		"status":        s.engine.Status(),
		"login":         login,
		"keyLock":       s.lock.Status(),
		"defaultIgnore": sync.DefaultIgnore,
		"activity":      s.engine.Activity(),
		"logs":          s.hasLogs(),
		"version":       version(),
		"os":            runtime.GOOS,
		"home":          home,
		"superseded":    superseded,
	}, nil
}

// buildVersion is the release tag, set by clients/lite/packaging
// (-ldflags -X …/settingsui.buildVersion=v1.2.3).
var buildVersion string

// version is the release tag, else the module version or VCS revision
// the binary was built from.
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
			return "dev " + setting.Value[:7]
		}
	}
	return "dev"
}

// startLogin begins the device-code flow and polls for the approval in
// the background; the page shows the code and watches /api/state.
func (s *Server) startLogin(r *http.Request) (any, error) {
	var body struct {
		Server string `json:"server"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	server, err := api.CleanServer(body.Server)
	if err != nil {
		return nil, err
	}
	client := api.New(server, "")
	auth, err := client.Authorize(r.Context(), config.DefaultDeviceName(), "lite")
	if err != nil {
		return nil, err
	}
	qrCode := ""
	if svg, err := qr.SVG(auth.VerificationURL); err == nil {
		qrCode = qr.DataURI(svg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.login.cancel != nil {
		s.login.cancel()
	}
	s.login = loginState{Pending: true, UserCode: auth.UserCode, VerificationURL: auth.VerificationURL, QRCode: qrCode, cancel: cancel}
	started := s.login
	s.mu.Unlock()
	go func() {
		token, err := client.WaitForToken(ctx, auth)
		if err == nil {
			err = s.engine.SignIn(server, token)
			s.forgetQuota()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.login.UserCode != auth.UserCode {
			return // cancelled, or replaced by a newer sign-in
		}
		s.login = loginState{}
		if err != nil && !errors.Is(err, context.Canceled) {
			s.login.Error = err.Error()
		}
	}()
	return started, nil
}

// cancelLogin stops waiting for a pending device approval.
func (s *Server) cancelLogin(*http.Request) (any, error) {
	s.mu.Lock()
	if s.login.cancel != nil {
		s.login.cancel()
	}
	s.login = loginState{}
	s.mu.Unlock()
	return ok(), nil
}

func (s *Server) logout(*http.Request) (any, error) {
	s.forgetQuota()
	return ok(), s.engine.SignOut()
}

func (s *Server) forgetQuota() {
	s.mu.Lock()
	s.quota = nil
	s.mu.Unlock()
}

type pairBody struct {
	Local     string   `json:"local"`
	Remote    string   `json:"remote"`
	Direction string   `json:"direction"`
	Ignore    []string `json:"ignore"`
	Paused    *bool    `json:"paused"`
}

func (s *Server) addPair(r *http.Request) (any, error) {
	var body pairBody
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	pair, err := config.NewPair(body.Local, body.Remote, body.Direction, cleanList(body.Ignore))
	if err != nil {
		return nil, err
	}
	if err := s.cfg.Update(func(c *config.Config) error { return c.AddPair(pair) }); err != nil {
		return nil, err
	}
	s.engine.SyncNow()
	return pair, nil
}

func (s *Server) updatePair(r *http.Request) (any, error) {
	var body pairBody
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	id := r.PathValue("id")
	err := s.cfg.Update(func(c *config.Config) error {
		for i := range c.Pairs {
			if c.Pairs[i].ID != id {
				continue
			}
			if body.Direction != "" {
				if body.Direction != config.Both && body.Direction != config.Up && body.Direction != config.Down {
					return errors.New("direction must be both, up or down")
				}
				c.Pairs[i].Direction = body.Direction
			}
			if body.Ignore != nil {
				c.Pairs[i].Ignore = cleanList(body.Ignore)
			}
			if body.Paused != nil {
				c.Pairs[i].Paused = *body.Paused
			}
			return nil
		}
		return errors.New("no such pair")
	})
	if err != nil {
		return nil, err
	}
	s.engine.SyncNow()
	return ok(), nil
}

func (s *Server) removePair(r *http.Request) (any, error) {
	id := r.PathValue("id")
	return ok(), s.engine.RemovePair(id)
}

func (s *Server) confirmDeletes(r *http.Request) (any, error) {
	return s.engine.ConfirmDeletes(r.PathValue("id"))
}

func (s *Server) updateSettings(r *http.Request) (any, error) {
	var body struct {
		Paused        *bool   `json:"paused"`
		Autostart     *bool   `json:"autostart"`
		UploadKiBps   *int64  `json:"uploadKiBps"`
		DownloadKiBps *int64  `json:"downloadKiBps"`
		DeviceName    *string `json:"deviceName"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	if body.Autostart != nil {
		var err error
		if *body.Autostart {
			err = autostart.Enable()
		} else {
			err = autostart.Disable()
		}
		if err != nil {
			return nil, err
		}
	}
	err := s.cfg.Update(func(c *config.Config) error {
		if body.UploadKiBps != nil {
			c.UploadKiBps = max(*body.UploadKiBps, 0)
		}
		if body.DownloadKiBps != nil {
			c.DownloadKiBps = max(*body.DownloadKiBps, 0)
		}
		if body.DeviceName != nil && strings.TrimSpace(*body.DeviceName) != "" {
			c.DeviceName = strings.TrimSpace(*body.DeviceName)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if body.Paused != nil {
		if err := s.engine.SetPaused(*body.Paused); err != nil {
			return nil, err
		}
	}
	return ok(), nil
}

// enrollKey turns on the security key lock. It answers once the key was
// touched (or the attempt timed out); the PIN, when the key needs one, is
// used for this request only.
func (s *Server) enrollKey(r *http.Request) (any, error) {
	var body struct {
		PIN string `json:"pin"`
	}
	if err := decode(r, &body); err != nil {
		return nil, err
	}
	return ok(), s.lock.Enroll(r.Context(), body.PIN)
}

func (s *Server) removeKey(r *http.Request) (any, error) {
	return ok(), s.lock.Remove(r.Context())
}

func cleanList(values []string) []string {
	var out []string
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
