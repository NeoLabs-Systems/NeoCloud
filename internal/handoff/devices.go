package handoff

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// The devices an account can send to: those online now (browsers and apps) and
// signed-in apps seen in the last week, which get an offer when they next
// connect.

const recentDevices = "-7 days"

type device struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	Kind       string `json:"kind"`
	CanReceive bool   `json:"canReceive"`
	Online     bool   `json:"online"`
	Current    *bool  `json:"current,omitempty"`
}

var deviceKeyPattern = regexp.MustCompile(`^d:(\d+)$`)

func deviceKey(id int64) string { return "d:" + strconv.FormatInt(id, 10) }

// deviceID is the token id of a "d:" key; false for a browser.
func deviceID(key string) (int64, bool) {
	match := deviceKeyPattern.FindStringSubmatch(key)
	if match == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(match[1], 10, 64)
	return id, err == nil
}

func kindOf(platform string) string {
	if platform == "android" || platform == "ios" {
		return "phone"
	}
	return "desktop"
}

func (h *Handler) token(ctx context.Context, userID, id int64) (name, platform string, found bool, err error) {
	err = h.db.QueryRowContext(ctx, `SELECT name,COALESCE(platform,'') FROM device_tokens WHERE id=? AND user_id=? AND revoked_at IS NULL AND platform!='webdav'`, id, userID).Scan(&name, &platform)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return name, platform, err == nil, err
}

func (h *Handler) sessionUserAgent(ctx context.Context, userID int64, key string) (string, error) {
	prefix := strings.TrimPrefix(key, "s:")
	var agent sql.NullString
	err := h.db.QueryRowContext(ctx, `SELECT user_agent FROM user_sessions WHERE user_id=? AND revoked_at IS NULL AND substr(session_hash,1,?)=?`, userID, len(prefix), prefix).Scan(&agent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return agent.String, err
}

// devices lists online devices first, then recently seen apps.
func (h *Handler) devices(ctx context.Context, userID int64) ([]device, error) {
	out := []device{}
	known := map[string]bool{}
	for _, entry := range h.presence.online(userID) {
		d := device{ID: entry.key, CanReceive: entry.canReceive, Online: true}
		if id, isApp := deviceID(entry.key); isApp {
			name, platform, found, err := h.token(ctx, userID, id)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			d.Name, d.Platform, d.Kind = name, platform, kindOf(platform)
		} else {
			agent, err := h.sessionUserAgent(ctx, userID, entry.key)
			if err != nil {
				return nil, err
			}
			if agent == "" {
				agent = entry.userAgent
			}
			d.Name, d.Platform, d.Kind = browserLabel(agent), browserPlatform(agent), "browser"
		}
		known[d.ID] = true
		out = append(out, d)
	}
	rows, err := h.db.QueryContext(ctx, `SELECT id,name,COALESCE(platform,'') FROM device_tokens
		WHERE user_id=? AND revoked_at IS NULL AND platform!='webdav' AND last_seen_at IS NOT NULL AND datetime(last_seen_at)>datetime('now',?)
		ORDER BY id DESC`, userID, recentDevices)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		d := device{CanReceive: true}
		if err := rows.Scan(&id, &d.Name, &d.Platform); err != nil {
			return nil, err
		}
		d.ID, d.Kind = deviceKey(id), kindOf(d.Platform)
		if !known[d.ID] {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// resolveTarget is the device to send to; another account's keys never resolve.
func (h *Handler) resolveTarget(ctx context.Context, userID int64, key string) (device, error) {
	list, err := h.devices(ctx, userID)
	if err != nil {
		return device{}, err
	}
	for _, d := range list {
		if d.ID != key {
			continue
		}
		if !d.CanReceive {
			return device{}, failure(http.StatusConflict, "That device is not accepting files right now.", "DEVICE_NOT_RECEIVING")
		}
		return d, nil
	}
	return device{}, failure(http.StatusNotFound, "That device is not available.", "DEVICE_NOT_FOUND")
}

// describeSender is the sending device's name for the offer the receiver sees.
func (h *Handler) describeSender(ctx context.Context, c caller, userAgent string) (name, platform string, err error) {
	if id, isApp := deviceID(c.key); isApp {
		name, platform, found, err := h.token(ctx, c.UserID, id)
		if !found {
			name = "Device"
		}
		return name, platform, err
	}
	return browserLabel(userAgent), browserPlatform(userAgent), nil
}

type userAgentRule struct {
	pattern *regexp.Regexp
	name    string
}

func rules(pairs ...string) []userAgentRule {
	out := make([]userAgentRule, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, userAgentRule{regexp.MustCompile(pairs[i]), pairs[i+1]})
	}
	return out
}

var (
	browsers = rules(`Edg/`, "Edge", `OPR/|Opera`, "Opera", `Firefox/`, "Firefox", `Chrome/`, "Chrome", `Safari/`, "Safari")
	systems  = rules(`Windows`, "Windows", `Android`, "Android", `iPhone|iPad|iOS`, "iOS", `Mac OS X|Macintosh`, "macOS", `Linux|X11`, "Linux")
)

func firstMatch(table []userAgentRule, text, fallback string) string {
	for _, rule := range table {
		if rule.pattern.MatchString(text) {
			return rule.name
		}
	}
	return fallback
}

// browserLabel names a browser, which has no name of its own.
func browserLabel(userAgent string) string {
	return firstMatch(browsers, userAgent, "Browser") + " on " + firstMatch(systems, userAgent, "this device")
}

func browserPlatform(userAgent string) string {
	return strings.ToLower(firstMatch(systems, userAgent, ""))
}
