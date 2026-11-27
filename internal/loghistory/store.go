// Package loghistory persists a bounded, redacted view of structured server
// logs for the admin console.
package loghistory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxEntries = 1000
	trimAt     = 1100
	fileName   = "server-logs.jsonl"
)

var (
	sensitiveKey = regexp.MustCompile(`(?i)(?:^|[_-])(?:token|secret|password|api[_-]?key|authorization|cookie|session(?:id)?|sid|key)(?:$|[_-])`)
	bearerValue  = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/-]+=*`)
	querySecret  = regexp.MustCompile(`(?i)([?&](?:token|access_token|api[_-]?key|secret|password|authorization|cookie|session|sid)=)([^&#\s]+)`)
	keySecret    = regexp.MustCompile(`(?i)\b(token|access_token|authorization|api[_-]?key|secret|password|cookie|session(?:id)?|sid)\b\s*[:=]\s*("[^"]*"|'[^']*'|[^,\s;]+)`)
	publicPath   = regexp.MustCompile(`(/(?:api/public/shares|api/public/sign|app/sign|s|devices/authorize)/)[^/?#\s]+`)
)

// Entry matches the admin log-history contract.
type Entry struct {
	Type      string `json:"type"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// Store holds and persists the newest entries. Its files are private to the
// current user, matching the other NeoCloud runtime data directories.
type Store struct {
	mu          sync.Mutex
	path        string
	entries     []Entry
	pending     []byte
	diskEntries int
}

// Open loads the last 1000 entries and creates the private log directory/file.
func Open(logDir string) (*Store, error) {
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	if err := os.Chmod(logDir, 0700); err != nil {
		return nil, fmt.Errorf("secure log directory: %w", err)
	}
	path := filepath.Join(logDir, fileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log history: %w", err)
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure log history: %w", err)
	}
	defer file.Close()

	store := &Store{path: path}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var entry Entry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.Type == "" || entry.Timestamp == "" {
			continue
		}
		entry.Message = redactText(entry.Message)
		store.entries = append(store.entries, entry)
		store.diskEntries++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read log history: %w", err)
	}
	if len(store.entries) > maxEntries {
		store.entries = append([]Entry(nil), store.entries[len(store.entries)-maxEntries:]...)
	}
	if store.diskEntries > maxEntries {
		if err := store.rewriteLocked(); err != nil {
			return nil, err
		}
	}
	return store, nil
}

// Entries returns a stable snapshot in chronological order.
func (s *Store) Entries() []Entry {
	if s == nil {
		return []Entry{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

// Writer returns a slog-compatible writer that tees JSON log lines to stderr
// and stores their redacted form in the private history file.
func (s *Store) Writer(stderr io.Writer) io.Writer {
	return &teeWriter{store: s, stderr: stderr}
}

type teeWriter struct {
	store  *Store
	stderr io.Writer
}

func (w *teeWriter) Write(data []byte) (int, error) {
	if w.stderr != nil {
		if _, err := w.stderr.Write(data); err != nil {
			return 0, err
		}
	}
	if w.store == nil {
		return len(data), nil
	}
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	w.store.pending = append(w.store.pending, data...)
	for {
		index := bytes.IndexByte(w.store.pending, '\n')
		if index < 0 {
			break
		}
		line := bytes.TrimSpace(w.store.pending[:index])
		w.store.pending = append(w.store.pending[:0], w.store.pending[index+1:]...)
		if len(line) == 0 {
			continue
		}
		if err := w.store.appendRecordLocked(line); err != nil {
			return 0, err
		}
	}
	return len(data), nil
}

func (s *Store) appendRecordLocked(line []byte) error {
	entry, err := parseRecord(line)
	if err != nil {
		return fmt.Errorf("parse structured log record: %w", err)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode log history entry: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return fmt.Errorf("append log history: %w", err)
	}
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(append(encoded, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write log history: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close log history: %w", closeErr)
	}
	s.entries = append(s.entries, entry)
	if len(s.entries) > maxEntries {
		s.entries = append([]Entry(nil), s.entries[len(s.entries)-maxEntries:]...)
	}
	s.diskEntries++
	if s.diskEntries > trimAt {
		return s.rewriteLocked()
	}
	return nil
}

func parseRecord(line []byte) (Entry, error) {
	var fields map[string]any
	if err := json.Unmarshal(line, &fields); err != nil {
		return Entry{}, err
	}
	level := strings.ToLower(stringField(fields, "level"))
	if level == "" {
		level = "info"
	}
	timestamp := stringField(fields, "time")
	if timestamp == "" {
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	message := redactText(stringField(fields, "msg"))
	attrs := make(map[string]any)
	for key, value := range fields {
		if key == "time" || key == "level" || key == "msg" || key == "source" {
			continue
		}
		attrs[key] = redactValue(key, value)
	}
	if len(attrs) != 0 {
		encoded, err := json.Marshal(attrs)
		if err != nil {
			return Entry{}, err
		}
		message += " " + string(encoded)
	}
	return Entry{Type: level, Message: message, Timestamp: timestamp}, nil
}

func stringField(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return value
}

func redactValue(key string, value any) any {
	if sensitiveKey.MatchString(key) {
		return "[redacted]"
	}
	switch typed := value.(type) {
	case string:
		return redactText(typed)
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = redactValue("", child)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, child := range typed {
			out[childKey] = redactValue(childKey, child)
		}
		return out
	default:
		return value
	}
}

func redactText(text string) string {
	text = publicPath.ReplaceAllString(text, `${1}[redacted]`)
	text = bearerValue.ReplaceAllString(text, `${1}[redacted]`)
	text = querySecret.ReplaceAllString(text, `${1}[redacted]`)
	return keySecret.ReplaceAllString(text, `${1}=[redacted]`)
}

func (s *Store) rewriteLocked() error {
	var data bytes.Buffer
	for _, entry := range s.entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encode bounded log history: %w", err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	directory := filepath.Dir(s.path)
	file, err := os.CreateTemp(directory, ".server-logs-*.tmp")
	if err != nil {
		return fmt.Errorf("create compacted log history: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data.Bytes())
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return fmt.Errorf("write compacted log history: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close compacted log history: %w", closeErr)
	}
	if err = os.Rename(temporary, s.path); err != nil {
		return fmt.Errorf("replace compacted log history: %w", err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	s.diskEntries = len(s.entries)
	return nil
}

var defaultMu sync.RWMutex
var defaultStore *Store

// SetDefault installs the process history store used by HTTP composition.
func SetDefault(store *Store) {
	defaultMu.Lock()
	defaultStore = store
	defaultMu.Unlock()
}

// Default returns the process history store, if one has been initialized.
func Default() *Store {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultStore
}

// Verify Store satisfies the structured record writer contract used above.
var _ io.Writer = (*teeWriter)(nil)
