// Package realtime is the web app's live connection: a Socket.IO server on
// which each signed-in browser session joins its account's room. Services
// publish events to an account; changes to its files and shares are read from
// the database, so those made by other processes arrive too.
package realtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/realtime"
)

// Session is the browser session a socket signed in with.
type Session struct {
	UserID int64
	// Hash is the keyed session-id hash stored in user_sessions.
	Hash      string
	UserAgent string
}

// Client is one connected socket.
type Client struct {
	Session
	Emit  func(event string, payload any)
	Close func()
}

// Feed reports what changed for one account since it was last asked.
type Feed interface {
	Folders(ctx context.Context) ([]string, error)
	SharesChanged(ctx context.Context) (bool, error)
}

// Options configure a Server.
type Options struct {
	DB *sql.DB
	// Session resolves the signed-in browser session of a handshake request.
	Session     func(*http.Request) (Session, bool)
	AllowOrigin func(*http.Request) bool
	// Follow starts a change feed at the account's current position.
	Follow       func(ctx context.Context, userID int64) (Feed, error)
	PollInterval time.Duration
}

// Server serves "/socket.io/".
type Server struct {
	options Options
	hub     *realtime.Hub

	mu        sync.Mutex
	accounts  map[int64]*account
	connected []func(Client) (disconnected func())

	stop context.CancelFunc
	done chan struct{}
}

type account struct {
	feed    Feed
	sockets map[*realtime.Socket]Session
}

const sessionKey = "session"

var errUnauthorized = errors.New("Unauthorized")

// New starts the server; Close stops it.
func New(options Options) (*Server, error) {
	if options.DB == nil || options.Session == nil || options.AllowOrigin == nil || options.Follow == nil {
		return nil, errors.New("realtime: database, session resolver, origin check and change feed are required")
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{
		options:  options,
		hub:      realtime.New(realtime.Options{AllowOrigin: func(string) bool { return true }}),
		accounts: map[int64]*account{},
		stop:     cancel,
		done:     make(chan struct{}),
	}
	s.hub.Use(s.authorize)
	s.hub.OnConnection(s.accept)
	go s.poll(ctx)
	return s, nil
}

// OnConnection registers a handler for every socket that joins; the function it
// returns runs when that socket is gone.
func (s *Server) OnConnection(fn func(Client) func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected = append(s.connected, fn)
}

// Publish sends an event to every socket of the account.
func (s *Server) Publish(userID int64, event string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	s.hub.EmitToUser(userID, event, data)
}

// Close disconnects every client.
func (s *Server) Close() {
	s.stop()
	<-s.done
	s.hub.Close()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" && !s.options.AllowOrigin(r) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}
	s.hub.ServeHTTP(w, r)
}

// authorize admits a signed-in session and starts the account's feed before
// the client hears that it is connected, so no later change is missed.
func (s *Server) authorize(socket *realtime.Socket, _ json.RawMessage) error {
	r := socket.Request()
	session, ok := s.options.Session(r)
	if !ok || !s.active(r.Context(), session) {
		return errUnauthorized
	}
	s.mu.Lock()
	known := s.accounts[session.UserID] != nil
	s.mu.Unlock()
	if !known {
		feed, err := s.options.Follow(r.Context(), session.UserID)
		if err != nil {
			slog.Error("start the live change feed", "error", err)
			return errors.New("Server error")
		}
		s.mu.Lock()
		if s.accounts[session.UserID] == nil {
			s.accounts[session.UserID] = &account{feed: feed, sockets: map[*realtime.Socket]Session{}}
		}
		s.mu.Unlock()
	}
	socket.Data.Store(sessionKey, session)
	return nil
}

func (s *Server) accept(socket *realtime.Socket) {
	value, ok := socket.Data.Load(sessionKey)
	session, valid := value.(Session)
	if !ok || !valid {
		socket.Disconnect(true)
		return
	}
	s.mu.Lock()
	entry := s.accounts[session.UserID]
	if entry == nil {
		s.mu.Unlock()
		socket.Disconnect(true)
		return
	}
	entry.sockets[socket] = session
	handlers := append([]func(Client) func(){}, s.connected...)
	s.mu.Unlock()
	socket.Join(realtime.UserRoom(session.UserID))
	client := Client{
		Session: session,
		Emit: func(event string, payload any) {
			if err := socket.Emit(event, payload); err != nil {
				slog.Warn("realtime emit failed", "event", event, "error", err)
			}
		},
		Close: func() { socket.Disconnect(true) },
	}
	var ends []func()
	for _, handler := range handlers {
		if end := handler(client); end != nil {
			ends = append(ends, end)
		}
	}
	socket.OnDisconnect(func(string) {
		s.mu.Lock()
		if entry := s.accounts[session.UserID]; entry != nil {
			delete(entry.sockets, socket)
			if len(entry.sockets) == 0 {
				delete(s.accounts, session.UserID)
			}
		}
		s.mu.Unlock()
		for _, end := range ends {
			end()
		}
	})
}

// active reports whether the account and the session may still be connected.
func (s *Server) active(ctx context.Context, session Session) bool {
	var active bool
	err := s.options.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)
		AND NOT EXISTS(SELECT 1 FROM user_sessions WHERE session_hash=? AND revoked_at IS NOT NULL)`,
		session.UserID, session.Hash).Scan(&active)
	if err != nil && ctx.Err() == nil {
		slog.Error("check a live session", "error", err)
	}
	return err == nil && active
}

func (s *Server) poll(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.options.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		type snapshot struct {
			userID  int64
			feed    Feed
			sockets map[*realtime.Socket]Session
		}
		s.mu.Lock()
		accounts := make([]snapshot, 0, len(s.accounts))
		for userID, entry := range s.accounts {
			sockets := make(map[*realtime.Socket]Session, len(entry.sockets))
			for socket, session := range entry.sockets {
				sockets[socket] = session
			}
			accounts = append(accounts, snapshot{userID, entry.feed, sockets})
		}
		s.mu.Unlock()
		for _, entry := range accounts {
			// A signed-out session or disabled account loses its connection.
			for socket, session := range entry.sockets {
				if !s.active(ctx, session) && ctx.Err() == nil {
					socket.Disconnect(true)
				}
			}
			s.publishChanges(ctx, entry.userID, entry.feed)
		}
	}
}

func (s *Server) publishChanges(ctx context.Context, userID int64, feed Feed) {
	folders, err := feed.Folders(ctx)
	if err == nil && len(folders) > 0 {
		s.Publish(userID, "files:changed", map[string]any{"folderIds": folders})
	}
	changed, shareErr := feed.SharesChanged(ctx)
	if shareErr == nil && changed {
		s.Publish(userID, "shares:changed", nil)
	}
	if failure := errors.Join(err, shareErr); failure != nil && ctx.Err() == nil && !strings.Contains(failure.Error(), "closed") {
		slog.Error("read live changes", "error", failure)
	}
}
