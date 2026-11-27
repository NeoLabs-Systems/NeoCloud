package webdav

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxLocksPerAccount = 1000

var ErrTooManyLocks = errors.New("too many WebDAV locks are held")

type Lock struct {
	Token, Key, Href, Depth, Scope, Owner string
	Timeout                               time.Duration
	ExpiresAt                             time.Time
}

type LockManager struct{ db *sql.DB }

func NewLockManager(db *sql.DB) *LockManager { return &LockManager{db: db} }

func LockKey(segments []string) string {
	parts := make([]string, len(segments))
	for i, segment := range segments {
		parts[i] = strings.ToLower(segment)
	}
	return strings.Join(parts, "/")
}

func SubmittedTokens(header string) []string {
	var result []string
	for _, part := range strings.Split(header, "<") {
		if end := strings.IndexByte(part, '>'); end > 0 {
			token := part[:end]
			if strings.HasPrefix(token, "opaquelocktoken:") {
				result = append(result, token)
			}
		}
	}
	return result
}

var timeoutPattern = regexp.MustCompile(`(?i)^Second-(\d+)$`)

// ParseTimeout reads "Second-600", "Infinite" or a list of them, capped to an hour.
func ParseTimeout(header string) time.Duration {
	first, _, _ := strings.Cut(header, ",")
	match := timeoutPattern.FindStringSubmatch(strings.TrimSpace(first))
	if match == nil {
		return time.Hour
	}
	seconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil || seconds > 3600 {
		return time.Hour
	}
	return time.Duration(max(seconds, 1)) * time.Second
}

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadLocks(ctx context.Context, queryer rowQueryer, user int64, now int64) ([]Lock, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT token,key,href,depth,scope,owner,timeout_ms,expires_at
		FROM webdav_locks WHERE user_id=? AND expires_at>? ORDER BY key,token`, user, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var locks []Lock
	for rows.Next() {
		var lock Lock
		var timeoutMs, expiresMs int64
		if err := rows.Scan(&lock.Token, &lock.Key, &lock.Href, &lock.Depth, &lock.Scope, &lock.Owner, &timeoutMs, &expiresMs); err != nil {
			return nil, err
		}
		lock.Timeout = time.Duration(timeoutMs) * time.Millisecond
		lock.ExpiresAt = time.UnixMilli(expiresMs)
		locks = append(locks, lock)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return locks, nil
}

func below(key, ancestor string) bool {
	if ancestor == "" {
		return key != ""
	}
	return strings.HasPrefix(key, ancestor+"/")
}

func covers(lock Lock, key string) bool {
	return lock.Key == key || lock.Depth == "infinity" && below(key, lock.Key)
}

// All returns the account's live locks.
func (m *LockManager) All(ctx context.Context, user int64) ([]Lock, error) {
	return loadLocks(ctx, m.db, user, time.Now().UnixMilli())
}

// Covering picks the locks that apply to key.
func Covering(locks []Lock, key string) []Lock {
	result := make([]Lock, 0, len(locks))
	for _, lock := range locks {
		if covers(lock, key) {
			result = append(result, lock)
		}
	}
	return result
}

// Create serializes conflict checking with a write statement inside the same
// SQLite transaction. That makes simultaneous LOCK requests on separate Go
// server instances see each other's committed rows before inserting.
func (m *LockManager) Create(ctx context.Context, user int64, lock Lock) (Lock, bool, error) {
	if user < 1 || (lock.Depth != "0" && lock.Depth != "infinity") || (lock.Scope != "exclusive" && lock.Scope != "shared") {
		return Lock{}, false, nil
	}
	if lock.Timeout <= 0 || lock.Timeout > time.Hour {
		lock.Timeout = time.Hour
	}
	lock.Token = "opaquelocktoken:" + uuid.NewString()
	now := time.Now()
	lock.ExpiresAt = now.Add(lock.Timeout)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return Lock{}, false, err
	}
	defer tx.Rollback()
	// Even when this deletes no rows, SQLite treats the statement as a write
	// and obtains the database writer reservation before conflict reads.
	if _, err = tx.ExecContext(ctx, `DELETE FROM webdav_locks WHERE expires_at<=?`, now.UnixMilli()); err != nil {
		return Lock{}, false, err
	}
	locks, err := loadLocks(ctx, tx, user, now.UnixMilli())
	if err != nil {
		return Lock{}, false, err
	}
	if len(locks) >= maxLocksPerAccount {
		return Lock{}, false, ErrTooManyLocks
	}
	for _, existing := range locks {
		conflict := covers(existing, lock.Key) || lock.Depth == "infinity" && below(existing.Key, lock.Key)
		if conflict && (existing.Scope == "exclusive" || lock.Scope == "exclusive") {
			return Lock{}, false, nil
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO webdav_locks
		(token,user_id,key,href,depth,scope,owner,timeout_ms,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		lock.Token, user, lock.Key, lock.Href, lock.Depth, lock.Scope, lock.Owner,
		lock.Timeout.Milliseconds(), lock.ExpiresAt.UnixMilli()); err != nil {
		return Lock{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Lock{}, false, err
	}
	return lock, true, nil
}

// Unlocked reports whether no lock the request does not hold applies to key
// or, with deep, to anything below it (DELETE and MOVE of a folder).
func (m *LockManager) Unlocked(ctx context.Context, user int64, key string, tokens []string, deep bool) (bool, error) {
	locks, err := m.All(ctx, user)
	if err != nil {
		return false, err
	}
	for _, lock := range locks {
		if (covers(lock, key) || deep && below(lock.Key, key)) && !slices.Contains(tokens, lock.Token) {
			return false, nil
		}
	}
	return true, nil
}

func (m *LockManager) Refresh(ctx context.Context, user int64, key string, tokens []string, timeout time.Duration) (Lock, bool, error) {
	locks, err := loadLocks(ctx, m.db, user, time.Now().UnixMilli())
	if err != nil {
		return Lock{}, false, err
	}
	if timeout <= 0 || timeout > time.Hour {
		timeout = time.Hour
	}
	now := time.Now()
	for _, lock := range locks {
		if covers(lock, key) && slices.Contains(tokens, lock.Token) {
			lock.Timeout, lock.ExpiresAt = timeout, now.Add(timeout)
			result, err := m.db.ExecContext(ctx, `UPDATE webdav_locks SET timeout_ms=?,expires_at=? WHERE user_id=? AND token=? AND expires_at>?`,
				timeout.Milliseconds(), lock.ExpiresAt.UnixMilli(), user, lock.Token, now.UnixMilli())
			if err != nil {
				return Lock{}, false, err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return Lock{}, false, err
			}
			return lock, changed == 1, nil
		}
	}
	return Lock{}, false, nil
}

func (m *LockManager) Remove(ctx context.Context, user int64, key, token string) (bool, error) {
	locks, err := loadLocks(ctx, m.db, user, time.Now().UnixMilli())
	if err != nil {
		return false, err
	}
	for _, lock := range locks {
		if lock.Token != token || !covers(lock, key) {
			continue
		}
		result, err := m.db.ExecContext(ctx, `DELETE FROM webdav_locks WHERE user_id=? AND token=? AND expires_at>?`, user, token, time.Now().UnixMilli())
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		return changed == 1, err
	}
	return false, nil
}

func (m *LockManager) ReleaseTree(ctx context.Context, user int64, key string) error {
	locks, err := loadLocks(ctx, m.db, user, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, lock := range locks {
		if lock.Key == key || below(lock.Key, key) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM webdav_locks WHERE user_id=? AND token=?`, user, lock.Token); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
