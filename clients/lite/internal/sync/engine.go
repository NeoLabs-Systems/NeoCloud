// Package sync is the lite client's sync engine (docs/clients.md §2): a
// three-way comparison of the local folder, the server tree and the state
// of the last sync, run per pair whenever either side may have changed.
package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	gosync "sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/neolabs/neocloud/clients/lite/internal/api"
	"github.com/neolabs/neocloud/clients/lite/internal/config"
	"github.com/neolabs/neocloud/clients/lite/internal/netstate"
	"github.com/neolabs/neocloud/clients/lite/internal/state"
)

// Engine states shown in the tray and settings page.
const (
	StateIdle       = "idle"
	StateSyncing    = "syncing"
	StatePaused     = "paused"
	StateError      = "error"
	StateOffline    = "offline"
	StateLoggedOut  = "logged-out"
	StateLocked     = "locked"
	StateConfirm    = "needs-confirmation"
	defaultPoll     = 15 * time.Second
	defaultRescan   = 5 * time.Minute
	settleDelay     = 1500 * time.Millisecond
	maxPassesPerRun = 5
)

// LockedMessage is the status while the security key lock holds the token.
const LockedMessage = "Locked — insert your security key"

// ErrLocked is returned while the security key lock holds the token.
var ErrLocked = errors.New("locked: insert your security key")

// Status is a snapshot for the UI.
type Status struct {
	State    string    `json:"state"`
	Message  string    `json:"message"`
	LastSync time.Time `json:"lastSync"`
	// Changes counts the files changed by the run in progress (or the last one).
	Changes   int                  `json:"changes"`
	PairState map[string]PairState `json:"pairs"`
}

// PairState is the outcome of a pair's last run. It is also stored in
// the state database so `status --json` can show it from another process.
type PairState struct {
	LastSync time.Time `json:"lastSync"`
	Error    string    `json:"error,omitempty"`
	// NeedsConfirmation is set while deletions wait for ConfirmDeletes.
	NeedsConfirmation *DeleteConfirmation `json:"needsConfirmation,omitempty"`
	// NotSynced lists what the last pass deliberately left alone.
	NotSynced []NotSynced `json:"notSynced"`
	// Failed lists the files the last pass could not sync.
	Failed []Issue `json:"failed"`
	// NewFolders wait for the user (new_folders "ask", big folders).
	NewFolders []NewFolder `json:"newFolders"`
	// Held says why the pair was not synced (a metered network…).
	Held string `json:"held,omitempty"`
}

// Engine syncs every configured pair of one account.
type Engine struct {
	cfg  *config.Store
	db   *state.DB
	log  *log.Logger
	up   *api.Limiter
	down *api.Limiter

	runMu gosync.Mutex // one sync run at a time

	mu       gosync.Mutex
	status   Status
	dirty    map[string]bool
	onStatus []func(Status)
	activity []Activity // oldest first, at most maxActivity
	wake     chan struct{}
	watcher  *fsnotify.Watcher
	watched  map[string]bool
	// stopRun cancels the sync run in progress, if any.
	stopRun context.CancelFunc
	// conditions reports a metered network or battery saver (a seam
	// for the tests).
	conditions func() netstate.Conditions
}

// New creates an engine. logger may be nil.
func New(cfg *config.Store, db *state.DB, logger *log.Logger) *Engine {
	if logger == nil {
		logger = log.Default()
	}
	return &Engine{
		cfg:    cfg,
		db:     db,
		log:    logger,
		up:     api.NewLimiter(0),
		down:   api.NewLimiter(0),
		status: Status{State: StateIdle, PairState: map[string]PairState{}},
		dirty:  map[string]bool{},
		wake:   make(chan struct{}, 1),

		conditions: netstate.Current,
	}
}

// OnStatus registers a callback for status changes (called synchronously).
func (e *Engine) OnStatus(fn func(Status)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onStatus = append(e.onStatus, fn)
}

// Status returns the current status.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	copied := e.status
	cfg := e.cfg.Get()
	if !cfg.LoggedIn() {
		copied.State, copied.Message = StateLoggedOut, "Not signed in"
	} else if cfg.Locked() && copied.State != StateLocked {
		copied.State, copied.Message = StateLocked, LockedMessage
	}
	copied.PairState = map[string]PairState{}
	for id, ps := range e.status.PairState {
		copied.PairState[id] = ps
	}
	for _, pair := range e.cfg.Get().Pairs {
		if _, ok := copied.PairState[pair.ID]; !ok {
			if stored, ok := e.PairReport(pair.ID); ok {
				copied.PairState[pair.ID] = stored
			}
		}
	}
	return copied
}

// PairReport is the stored outcome of a pair's last run.
func (e *Engine) PairReport(pairID string) (PairState, bool) {
	value, err := e.db.Meta(pairID, metaReport)
	if err != nil || value == "" {
		return PairState{}, false
	}
	var ps PairState
	if json.Unmarshal([]byte(value), &ps) != nil {
		return PairState{}, false
	}
	return ps, true
}

// ConfirmDeletes approves the deletions the guard held back for a pair;
// the next pass carries them out (never more than were shown).
func (e *Engine) ConfirmDeletes(pairID string) (DeleteConfirmation, error) {
	if _, ok := e.cfg.Get().Pair(pairID); !ok {
		return DeleteConfirmation{}, fmt.Errorf("no pair %q", pairID)
	}
	report, ok := e.PairReport(pairID)
	if !ok || report.NeedsConfirmation == nil {
		return DeleteConfirmation{}, errors.New("this pair has no deletions waiting for a confirmation")
	}
	pending := *report.NeedsConfirmation
	if err := e.db.SetMeta(pairID, metaConfirm, encodeJSON(pending)); err != nil {
		return DeleteConfirmation{}, err
	}
	e.markDirty(pairID)
	e.poke()
	return pending, nil
}

func (e *Engine) setStatus(update func(*Status)) {
	e.mu.Lock()
	update(&e.status)
	snapshot := e.status
	listeners := append([]func(Status){}, e.onStatus...)
	e.mu.Unlock()
	for _, fn := range listeners {
		fn(snapshot)
	}
}

// Client builds an API client from the current config, with the shared
// bandwidth limiters set to the configured caps.
func (e *Engine) Client() (*api.Client, error) {
	cfg := e.cfg.Get()
	if !cfg.LoggedIn() {
		return nil, errors.New("not signed in; run `neocloud-lite login --server URL` first")
	}
	if cfg.Locked() {
		return nil, ErrLocked
	}
	e.up.SetRate(cfg.UploadKiBps * 1024)
	e.down.SetRate(cfg.DownloadKiBps * 1024)
	client := api.New(cfg.Server, cfg.Token)
	client.Up, client.Down = e.up, e.down
	return client, nil
}

// Locked is called by the security key lock when the key goes away (or
// to update the message while locked): the run in progress stops and
// nothing syncs until Unlocked.
func (e *Engine) Locked(message string) {
	e.mu.Lock()
	if e.stopRun != nil {
		e.stopRun()
	}
	e.mu.Unlock()
	e.setStatus(func(s *Status) { s.State, s.Message = StateLocked, message })
}

// Unlocked is called by the security key lock once the token is back in
// memory: syncing resumes.
func (e *Engine) Unlocked() {
	e.setStatus(func(s *Status) { s.State, s.Message = StateIdle, "Unlocked" })
	e.SyncNow()
}

// SignIn stores a freshly issued token. Signing in to a different account
// forgets all sync state first, so the old account's files are never
// compared with the new one's (that would read as mass deletions). A
// security key lock is turned off: it sealed the old token.
func (e *Engine) SignIn(server string, token *api.DeviceToken) error {
	cfg := e.cfg.Get()
	if cfg.UserID != 0 && cfg.UserID != token.User.ID || cfg.Server != "" && cfg.Server != server {
		if err := e.db.Reset(); err != nil {
			return err
		}
	}
	err := e.cfg.Update(func(c *config.Config) error {
		c.Server = strings.TrimRight(server, "/")
		c.Token = token.Token
		c.KeyLock = nil
		c.UserID = token.User.ID
		c.Username = token.User.Username
		if c.DeviceName == "" {
			c.DeviceName = config.DefaultDeviceName()
		}
		return nil
	})
	if err == nil {
		e.SyncNow()
	}
	return err
}

// SignOut forgets the token and all sync state; pairs stay configured.
func (e *Engine) SignOut() error {
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Token, c.Username, c.UserID = "", "", 0
		c.KeyLock = nil
		return nil
	}); err != nil {
		return err
	}
	e.setStatus(func(s *Status) { s.State, s.Message = StateLoggedOut, "Not signed in" })
	return e.db.Reset()
}

// AddPair saves a new pair. For a pair the app creates the folder of
// (pair.Created), the folder is made once the pair was accepted; the
// folders made are returned, outermost first.
func (e *Engine) AddPair(pair config.Pair) ([]string, error) {
	var made []string
	err := e.cfg.Update(func(c *config.Config) error {
		if err := c.AddPair(pair); err != nil {
			return err
		}
		if !pair.Created {
			return nil
		}
		var err error
		made, err = config.CreateLocal(pair.Local)
		return err
	})
	if err != nil {
		return nil, err
	}
	e.SyncNow()
	return made, nil
}

// RemovePair drops a pair from the config and forgets its state. Files
// stay where they are on both sides.
func (e *Engine) RemovePair(key string) error {
	var removed config.Pair
	err := e.cfg.Update(func(c *config.Config) error {
		pair, ok := c.RemovePair(key)
		if !ok {
			return fmt.Errorf("no pair %q", key)
		}
		removed = pair
		return nil
	})
	if err != nil {
		return err
	}
	return e.db.ForgetPair(removed.ID)
}

// excludeFolders adds folders to a pair's exclude_paths.
func (e *Engine) excludeFolders(pairID string, folders []string) error {
	return e.cfg.Update(func(c *config.Config) error {
		for i := range c.Pairs {
			if c.Pairs[i].ID == pairID {
				c.Pairs[i].ExcludePaths = append(c.Pairs[i].ExcludePaths, folders...)
				return c.Pairs[i].Validate()
			}
		}
		return fmt.Errorf("no pair %q", pairID)
	})
}

// DecideNewFolder answers a new-folder prompt: sync the folder from now
// on, or exclude it.
func (e *Engine) DecideNewFolder(pairID, folder string, sync bool) error {
	folder = config.CleanRemote(folder)
	if folder == "" || strings.Contains(folder, "/") {
		return errors.New("not a top-level folder")
	}
	if !sync {
		if err := e.excludeFolders(pairID, []string{folder}); err != nil {
			return err
		}
	} else {
		value, err := e.db.Meta(pairID, metaAllowed)
		if err != nil {
			return err
		}
		var allowed []string
		json.Unmarshal([]byte(value), &allowed)
		if err := e.db.SetMeta(pairID, metaAllowed, encodeJSON(append(allowed, nfc(folder)))); err != nil {
			return err
		}
	}
	e.dropNewFolder(pairID, folder)
	return e.SyncPair(pairID)
}

// dropNewFolder takes an answered prompt out of the shown status.
func (e *Engine) dropNewFolder(pairID, folder string) {
	e.setStatus(func(s *Status) {
		ps := s.PairState[pairID]
		var kept []NewFolder
		for _, item := range ps.NewFolders {
			if item.Path != folder {
				kept = append(kept, item)
			}
		}
		ps.NewFolders = kept
		s.PairState[pairID] = ps
	})
}

// Resolve stores the issues view's choice for a file that failed to sync
// (ResolveLocal, ResolveRemote or ResolveBoth; "" just retries) and syncs
// the pair again.
func (e *Engine) Resolve(pairID, rel, choice string) error {
	if _, ok := e.cfg.Get().Pair(pairID); !ok {
		return fmt.Errorf("no pair %q", pairID)
	}
	if choice != "" {
		if choice != ResolveLocal && choice != ResolveRemote && choice != ResolveBoth {
			return errors.New("choice must be local, remote or both")
		}
		value, err := e.db.Meta(pairID, metaResolve)
		if err != nil {
			return err
		}
		choices := map[string]string{}
		json.Unmarshal([]byte(value), &choices)
		choices[rel] = choice
		if err := e.db.SetMeta(pairID, metaResolve, encodeJSON(choices)); err != nil {
			return err
		}
	}
	return e.SyncPair(pairID)
}

// SetPaused pauses or resumes all syncing.
func (e *Engine) SetPaused(paused bool) error {
	err := e.cfg.Update(func(c *config.Config) error { c.Paused = paused; return nil })
	if err != nil {
		return err
	}
	if paused {
		e.setStatus(func(s *Status) { s.State, s.Message = StatePaused, "Paused" })
	} else {
		e.SyncNow()
	}
	return nil
}

// SyncPair marks one pair for a run and wakes the loop.
func (e *Engine) SyncPair(pairID string) error {
	if _, ok := e.cfg.Get().Pair(pairID); !ok {
		return fmt.Errorf("no pair %q", pairID)
	}
	e.markDirty(pairID)
	e.poke()
	return nil
}

// SyncNow marks every pair for a run and wakes the loop.
func (e *Engine) SyncNow() {
	e.mu.Lock()
	for _, pair := range e.cfg.Get().Pairs {
		e.dirty[pair.ID] = true
	}
	e.mu.Unlock()
	e.poke()
}

func (e *Engine) markDirty(pairID string) {
	e.mu.Lock()
	e.dirty[pairID] = true
	e.mu.Unlock()
}

func (e *Engine) poke() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

func (e *Engine) takeDirty() map[string]bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	taken := e.dirty
	e.dirty = map[string]bool{}
	return taken
}

// SyncOnce runs every active pair until it settles. Used by `sync --once`
// and by the loop.
func (e *Engine) SyncOnce(ctx context.Context) error {
	return e.syncPairs(ctx, nil)
}

// syncPairs runs the pairs in only (all when nil). It returns the joined
// per-pair errors so a broken pair never stops the others.
func (e *Engine) syncPairs(ctx context.Context, only map[string]bool) error {
	e.runMu.Lock()
	defer e.runMu.Unlock()
	cfg := e.cfg.Get()
	if cfg.Paused {
		e.setStatus(func(s *Status) { s.State, s.Message = StatePaused, "Paused" })
		return nil
	}
	if cfg.Locked() {
		e.setStatus(func(s *Status) {
			if s.State != StateLocked {
				s.State, s.Message = StateLocked, LockedMessage
			}
		})
		return nil
	}
	ctx, cancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.stopRun = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.stopRun = nil
		e.mu.Unlock()
		cancel()
	}()
	client, err := e.Client()
	if err != nil {
		e.setStatus(func(s *Status) { s.State, s.Message = StateLoggedOut, "Not signed in" })
		return err
	}
	e.setStatus(func(s *Status) { s.State, s.Message, s.Changes = StateSyncing, "Syncing…", 0 })

	var errs []error
	held, ran := e.heldBy(cfg), 0
	for _, pair := range cfg.Pairs {
		if pair.Paused || (only != nil && !only[pair.ID]) {
			continue
		}
		if held != "" && pair.FollowsNetworkRules() {
			e.setStatus(func(s *Status) {
				ps := s.PairState[pair.ID]
				ps.Held = held
				s.PairState[pair.ID] = ps
			})
			continue
		}
		ran++
		ps, pairErr := e.syncPair(ctx, client, cfg, pair)
		if ctx.Err() != nil {
			break // stopped (locked or quitting): keep the last real report
		}
		ps.LastSync = time.Now()
		var confirm *NeedsConfirmationError
		if pairErr != nil && !errors.As(pairErr, &confirm) {
			ps.Error = pairErr.Error()
		}
		if err := e.db.SetMeta(pair.ID, metaReport, encodeJSON(ps)); err != nil {
			e.log.Printf("storing the status of %s: %v", pair.Local, err)
		}
		e.setStatus(func(s *Status) { s.PairState[pair.ID] = ps })
		if pairErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", pair.Local, pairErr))
		}
	}
	joined := errors.Join(errs...)
	if e.cfg.Get().Locked() {
		return joined // Locked set the status
	}
	e.setStatus(func(s *Status) {
		s.LastSync = time.Now()
		switch {
		case joined == nil && ran == 0 && held != "":
			s.State, s.Message = StatePaused, held
		case joined == nil:
			s.State, s.Message = StateIdle, "Up to date"
		case isOffline(joined):
			s.State, s.Message = StateOffline, "Server unreachable"
		case onlyConfirmations(errs):
			s.State, s.Message = StateConfirm, "Deletions are waiting for your confirmation"
		default:
			s.State, s.Message = StateError, firstLine(joined.Error())
		}
	})
	return joined
}

// heldBy says why pairs that follow the network rules wait, or "".
func (e *Engine) heldBy(cfg config.Config) string {
	if !cfg.MeteredPause() && !cfg.BatteryPause() {
		return ""
	}
	now := e.conditions()
	switch {
	case cfg.MeteredPause() && now.Metered:
		return "Paused on a metered connection"
	case cfg.BatteryPause() && now.BatterySaver:
		return "Paused while the battery saver is on"
	}
	return ""
}

// syncPair repeats passes until one changes nothing: a conflict copy, a
// file that changed mid-transfer or our own uploads all settle that way.
// Deletions held back by the guard come back as *NeedsConfirmationError.
func (e *Engine) syncPair(ctx context.Context, client *api.Client, cfg config.Config, pair config.Pair) (PairState, error) {
	var ps PairState
	release, err := lockPair(e.db.Dir(), pair.ID)
	if err != nil {
		return ps, err
	}
	defer release()
	var lastErr error
	var pending *DeleteConfirmation
	for i := 0; i < maxPassesPerRun; i++ {
		tree, err := client.Tree(ctx)
		if err != nil {
			return ps, err
		}
		files, percent := cfg.Guard(pair)
		p := &pass{
			ctx: ctx, client: client, db: e.db, pair: pair, device: cfg.DeviceName,
			logf:        func(format string, args ...any) { e.log.Printf("[%s] "+format, append([]any{pair.Remote}, args...)...) },
			note:        func(kind, rel string) { e.recordActivity(pair.ID, kind, rel) },
			guardLimits: guardLimits{files: files, percent: percent},
			bigFolder:   int64(cfg.BigFolderMB(pair)) << 20,
		}
		lastErr = p.run(tree)
		e.watchDirs(pair.ID, p.dirs)
		ps.NotSynced, pending = p.notSynced, p.pending
		ps.Failed, ps.NewFolders = p.failed, p.asks
		if len(p.newExcludes) > 0 {
			pair.ExcludePaths = append(pair.ExcludePaths, p.newExcludes...)
			if err := e.excludeFolders(pair.ID, p.newExcludes); err != nil {
				return ps, err
			}
		}
		if lastErr == nil || errors.Is(lastErr, context.Canceled) {
			if err := e.db.SetCursor(pair.ID, tree.Cursor); err != nil {
				return ps, err
			}
		}
		if p.actions == 0 && !p.again {
			break
		}
	}
	if ps.NotSynced == nil {
		ps.NotSynced = []NotSynced{}
	}
	if ps.Failed == nil {
		ps.Failed = []Issue{}
	}
	if ps.NewFolders == nil {
		ps.NewFolders = []NewFolder{}
	}
	ps.NeedsConfirmation = pending
	if lastErr == nil && pending != nil {
		lastErr = &NeedsConfirmationError{Pair: pair.ID, Pending: *pending}
	}
	return ps, lastErr
}

func onlyConfirmations(errs []error) bool {
	for _, err := range errs {
		var confirm *NeedsConfirmationError
		if !errors.As(err, &confirm) {
			return false
		}
	}
	return len(errs) > 0
}

// remoteChanged asks the journal whether anything happened inside the
// pair's folder since its cursor. A change counts when it concerns a node
// the pair tracks, or lands in a tracked folder (or the pair's root).
// Changes elsewhere in the account only move the cursor on, so they never
// cost a full pass. The pass itself still compares full snapshots, which
// also covers subtrees that one folder trash/restore entry stands for.
func (e *Engine) remoteChanged(ctx context.Context, client *api.Client, pairID string) (bool, error) {
	cursor, err := e.db.Cursor(pairID)
	if err != nil {
		return false, err
	}
	root, err := e.db.Meta(pairID, metaRootID)
	if err != nil || root == "" {
		return true, err
	}
	entries, err := e.db.Entries(pairID)
	if err != nil {
		return false, err
	}
	tracked := map[string]bool{root: true}
	for _, entry := range entries {
		tracked[entry.NodeID] = true
	}
	for {
		page, err := client.Changes(ctx, cursor, 1000)
		if err != nil {
			return false, err
		}
		for _, change := range page.Changes {
			if changeTouches(change, tracked) {
				return true, nil
			}
		}
		cursor = page.Cursor
		if !page.HasMore {
			break
		}
	}
	return false, e.db.SetCursor(pairID, cursor)
}

// changeTouches reports whether a journal entry concerns the pair whose
// node ids (root included) are in tracked: the node itself is tracked
// (edited, moved away, trashed) or it now lives in a tracked folder.
func changeTouches(change api.Change, tracked map[string]bool) bool {
	return tracked[change.NodeID] || (change.Node != nil && tracked[change.Node.ParentID])
}

func isOffline(err error) bool {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "connection refused") || strings.Contains(text, "no such host") ||
		strings.Contains(text, "timeout") || strings.Contains(text, "network is unreachable")
}

func firstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}
