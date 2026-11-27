package webdav

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Credentials struct{ Username, Password string }

var basicPattern = regexp.MustCompile(`(?i)^Basic\s+([A-Za-z0-9+/]+={0,2})\s*$`)

func BasicCredentials(header string) (Credentials, bool) {
	match := basicPattern.FindStringSubmatch(header)
	if match == nil {
		return Credentials{}, false
	}
	decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(match[1], "="))
	if err != nil {
		return Credentials{}, false
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found {
		return Credentials{}, false
	}
	return Credentials{Username: strings.TrimSpace(username), Password: password}, true
}

// AppPasswords signs in with an account's username and a device token of
// platform `webdav`. Revoked tokens and disabled accounts resolve to nothing.
func AppPasswords(db *sql.DB) AuthenticateFunc {
	return func(ctx context.Context, credentials Credentials) (Principal, bool, error) {
		if !strings.HasPrefix(credentials.Password, "nc_") {
			return Principal{}, false, nil
		}
		sum := sha256.Sum256([]byte(credentials.Password))
		var principal Principal
		var deviceID int64
		err := db.QueryRowContext(ctx, `SELECT d.id,d.user_id,u.username FROM device_tokens d JOIN users u ON u.id=d.user_id
			WHERE d.token_hash=? AND d.platform='webdav' AND d.revoked_at IS NULL AND u.disabled_at IS NULL`, hex.EncodeToString(sum[:])).
			Scan(&deviceID, &principal.UserID, &principal.Username)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !strings.EqualFold(principal.Username, credentials.Username) {
			return Principal{}, false, nil
		}
		if err != nil {
			return Principal{}, false, err
		}
		_, err = db.ExecContext(ctx, `UPDATE device_tokens SET last_seen_at=datetime('now') WHERE id=? AND (last_seen_at IS NULL OR last_seen_at<datetime('now','-1 minute'))`, deviceID)
		return principal, true, err
	}
}

const (
	failureWindow      = 15 * time.Minute
	maxFailuresPerIP   = 60
	maxFailuresPerUser = 30
)

type attempts struct {
	count int
	reset time.Time
}

// failureLimiter counts wrong app passwords per fixed window. An attempt is
// reserved before the password is checked, so parallel guesses cannot pass the
// limit, and given back when it turns out not to be a failure.
type failureLimiter struct {
	mu    sync.Mutex
	seen  map[string]*attempts
	swept time.Time
}

func newFailureLimiter() *failureLimiter { return &failureLimiter{seen: map[string]*attempts{}} }

// reserve takes one attempt on both keys. When either is used up nothing is
// taken and retry says how long its window still runs.
func (l *failureLimiter) reserve(ip, username string, now time.Time) (release func(), retry time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) >= failureWindow {
		for key, entry := range l.seen {
			if !now.Before(entry.reset) {
				delete(l.seen, key)
			}
		}
		l.swept = now
	}
	limits := []struct {
		key string
		max int
	}{{"ip:" + ip, maxFailuresPerIP}, {"user:" + strings.ToLower(username), maxFailuresPerUser}}
	taken := make([]*attempts, 0, len(limits))
	for _, limit := range limits {
		entry := l.seen[limit.key]
		if entry == nil || !now.Before(entry.reset) {
			entry = &attempts{reset: now.Add(failureWindow)}
			l.seen[limit.key] = entry
		}
		if entry.count >= limit.max {
			return nil, entry.reset.Sub(now)
		}
		taken = append(taken, entry)
	}
	for _, entry := range taken {
		entry.count++
	}
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, entry := range taken {
			entry.count--
		}
	}, 0
}
