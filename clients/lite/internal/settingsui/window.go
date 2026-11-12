package settingsui

import (
	"errors"
	"net/http"
	"os"
	"time"
)

// reopenGrace swallows a second "Open NeoCloud" right after the first
// (a double click in the tray menu, a slow browser start).
const reopenGrace = 3 * time.Second

// closedGrace is how long no page may hold /api/window before the window
// counts as closed rather than reloading.
var closedGrace = 5 * time.Second

// OpenWindow shows the page in an app window with a fresh launch code. A
// newer window supersedes the older ones: their pages see it in
// /api/state and close themselves, so there is one window at a time.
func (s *Server) OpenWindow() error {
	s.mu.Lock()
	if time.Since(s.openedAt) < reopenGrace {
		s.mu.Unlock()
		return nil
	}
	s.openedAt = time.Now()
	s.mu.Unlock()
	_, err := s.openApp(s.LaunchURL(LaunchTTL), s.opts.WindowProfile)
	return err
}

// holdWindow answers only when the page goes away: an open app window
// keeps one such request running. Once none has been running for
// closedGrace, the window was closed, and the app window's browser is
// told to quit; on macOS it would otherwise linger in the Dock.
func (s *Server) holdWindow(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.windows++
	s.mu.Unlock()
	<-r.Context().Done()
	s.mu.Lock()
	s.windows--
	if s.windows == 0 {
		s.goneAt = time.Now()
		time.AfterFunc(closedGrace, s.closeIfGone)
	}
	s.mu.Unlock()
}

func (s *Server) closeIfGone() {
	s.mu.Lock()
	gone := s.windows == 0 && time.Since(s.goneAt) >= closedGrace && time.Since(s.openedAt) >= closedGrace
	s.mu.Unlock()
	if gone {
		s.CloseWindow()
	}
}

// CloseWindow quits the app window's browser, if one is running.
func (s *Server) CloseWindow() {
	if s.opts.WindowProfile != "" {
		s.closeApp(s.opts.WindowProfile)
	}
}

// sessionSeq is the request's session number, 0 without a session.
func (s *Server) sessionSeq(r *http.Request) int {
	cookie, err := r.Cookie(s.cookie)
	if err != nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[cookie.Value]
}

func (s *Server) openPair(r *http.Request) (any, error) {
	pair, found := s.cfg.Get().Pair(r.PathValue("id"))
	if !found {
		return nil, errors.New("no such pair")
	}
	return ok(), s.open(pair.Local)
}

func (s *Server) syncPair(r *http.Request) (any, error) {
	return ok(), s.engine.SyncPair(r.PathValue("id"))
}

// openTarget opens one of a few fixed places with the desktop's default
// apps: the pending sign-in's approval page and the web app in the user's
// own browser (the app window's profile is not signed in to NeoCloud), or
// the log file. The page never chooses the URL or path itself.
func (s *Server) openTarget(r *http.Request) (any, error) {
	var target string
	switch r.PathValue("target") {
	case "approval":
		s.mu.Lock()
		target = s.login.VerificationURL
		s.mu.Unlock()
	case "web":
		if server := s.cfg.Get().Server; server != "" {
			target = server + "/app"
		}
	case "logs":
		if s.hasLogs() {
			target = s.opts.LogPath
		}
	default:
		return nil, errors.New("unknown target")
	}
	if target == "" {
		return nil, errors.New("nothing to open")
	}
	return ok(), s.open(target)
}

func (s *Server) hasLogs() bool {
	if s.opts.LogPath == "" {
		return false
	}
	_, err := os.Stat(s.opts.LogPath)
	return err == nil
}
