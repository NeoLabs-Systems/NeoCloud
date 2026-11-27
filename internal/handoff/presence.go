package handoff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"
)

// Who is online. A device is online while at least one live connection (a
// browser's socket or a signed-in app's event stream) is open for it. Presence
// is held in memory per account: it is never visible to another account, and a
// restart empties it until the clients reconnect.

const (
	heartbeat   = 25 * time.Second
	streamQueue = 64
)

type connection struct {
	key        string
	userAgent  string
	canReceive bool
	// push must not block.
	push  func(event string, payload any)
	close func()
}

type presence struct {
	mu       sync.Mutex
	accounts map[int64][]*connection
}

func newPresence() *presence { return &presence{accounts: map[int64][]*connection{}} }

func (p *presence) connections(userID int64) []*connection {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.accounts[userID])
}

// online has one entry per online device.
func (p *presence) online(userID int64) []connection {
	var out []connection
	index := map[string]int{}
	for _, c := range p.connections(userID) {
		if at, known := index[c.key]; known {
			out[at].canReceive = out[at].canReceive || c.canReceive
			continue
		}
		index[c.key] = len(out)
		out = append(out, *c)
	}
	return out
}

func (p *presence) isOnline(userID int64, key string) bool {
	return slices.ContainsFunc(p.connections(userID), func(c *connection) bool { return c.key == key })
}

func (p *presence) pushToDevice(userID int64, key, event string, payload any) {
	for _, c := range p.connections(userID) {
		if c.key == key {
			c.push(event, payload)
		}
	}
}

func (p *presence) announceDevices(userID int64) {
	for _, c := range p.connections(userID) {
		c.push("handoff:devices", map[string]any{})
	}
}

// connect registers a live connection and returns what to call when it ends.
func (p *presence) connect(userID int64, c *connection) func() {
	wasOnline := p.isOnline(userID, c.key)
	p.mu.Lock()
	p.accounts[userID] = append(p.accounts[userID], c)
	p.mu.Unlock()
	if !wasOnline {
		p.announceDevices(userID)
	}
	return func() {
		p.mu.Lock()
		list := p.accounts[userID]
		at := slices.Index(list, c)
		if at >= 0 {
			list = slices.Delete(list, at, at+1)
			if len(list) == 0 {
				delete(p.accounts, userID)
			} else {
				p.accounts[userID] = list
			}
		}
		p.mu.Unlock()
		if at >= 0 && !p.isOnline(userID, c.key) {
			p.announceDevices(userID)
		}
	}
}

func (p *presence) dropDevice(userID int64, key string) {
	for _, c := range p.connections(userID) {
		if c.key == key {
			c.close()
		}
	}
}

// deliverWaiting hands a device the offers that arrived while it was away.
func (h *Handler) deliverWaiting(ctx context.Context, userID int64, key string) {
	waiting, err := h.offers(ctx, "receiver", `user_id=? AND target_key=? AND status='pending' ORDER BY created_at,rowid`, userID, key)
	if err != nil {
		slog.Error("deliver waiting sends", "error", err)
		return
	}
	for _, o := range waiting {
		h.presence.pushToDevice(userID, key, "handoff:offer", o)
	}
}

// AttachBrowser registers a web session's socket: one device per browser
// session, however many tabs it has. The returned function ends it.
func (h *Handler) AttachBrowser(userID int64, sessionHash, userAgent string, push func(string, any), close func()) func() {
	if len(sessionHash) < sessionKeyLength {
		return nil
	}
	key := SessionKey(sessionHash)
	disconnect := h.presence.connect(userID, &connection{key: key, userAgent: userAgent, canReceive: true, push: push, close: close})
	h.deliverWaiting(context.Background(), userID, key)
	return disconnect
}

const sessionKeyLength = 24

// SessionKey addresses a browser by a prefix of its keyed session hash, which
// reveals nothing about the session cookie.
func SessionKey(sessionHash string) string {
	return "s:" + sessionHash[:min(len(sessionHash), sessionKeyLength)]
}

// signedIn reports whether the caller behind an open stream still is.
func (h *Handler) signedIn(ctx context.Context, c caller) bool {
	query, id := `SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND disabled_at IS NULL)`, []any{c.UserID}
	if c.IsDevice {
		query = `SELECT EXISTS(SELECT 1 FROM device_tokens d JOIN users u ON u.id=d.user_id AND u.disabled_at IS NULL WHERE d.id=? AND d.revoked_at IS NULL)`
		id = []any{c.DeviceID}
	}
	var active bool
	return h.db.QueryRowContext(ctx, query, id...).Scan(&active) == nil && active
}

// events is the server-sent event stream of the request's own device, for
// signed-in apps; ?receive=0 stays out of the devices that can be sent to.
func (h *Handler) events(w http.ResponseWriter, r *http.Request, c caller) error {
	receive := r.URL.Query().Get("receive") != "0"
	response := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	if _, err := io.WriteString(w, "retry: 3000\n: connected\n\n"); err != nil || response.Flush() != nil {
		return nil
	}
	queue := make(chan string, streamQueue)
	closed := make(chan struct{})
	end := sync.OnceFunc(func() { close(closed) })
	disconnect := h.presence.connect(c.UserID, &connection{
		key: c.key, canReceive: receive, close: end,
		push: func(event string, payload any) {
			data, err := json.Marshal(payload)
			if err != nil {
				slog.Error("encode a live event", "event", event, "error", err)
				return
			}
			select {
			case queue <- "event: " + event + "\ndata: " + string(data) + "\n\n":
			default:
				end()
			}
		},
	})
	defer disconnect()
	if receive {
		h.deliverWaiting(r.Context(), c.UserID, c.key)
	}
	ping := time.NewTicker(heartbeat)
	defer ping.Stop()
	check := time.NewTicker(h.recheck)
	defer check.Stop()
	write := func(frame string) bool {
		_, err := fmt.Fprint(w, frame)
		return err == nil && response.Flush() == nil
	}
	for {
		select {
		case <-r.Context().Done():
			return nil
		case <-closed:
			return nil
		case frame := <-queue:
			if !write(frame) {
				return nil
			}
		case <-ping.C:
			if !write(": ping\n\n") {
				return nil
			}
		case <-check.C:
			if !h.signedIn(r.Context(), c) {
				return nil
			}
		}
	}
}
