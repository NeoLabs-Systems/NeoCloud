package backups

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	storageClassStateFile   = "backup-storage-class.json"
	storageClassStaleAfter  = 5 * time.Minute
	storageClassHeartbeat   = 30 * time.Second
	storageClassConcurrency = 4
)

var storageClassAreas = []string{"blobs/", "snapshots/"}

type StorageClassMigration struct {
	Status          string `json:"status"`
	StorageClass    string `json:"storageClass"`
	StartedAt       string `json:"startedAt"`
	FinishedAt      any    `json:"finishedAt"`
	ObjectsTotal    *int64 `json:"objectsTotal"`
	ObjectsDone     int64  `json:"objectsDone"`
	BytesTotal      *int64 `json:"bytesTotal"`
	BytesDone       int64  `json:"bytesDone"`
	ObjectsSkipped  int64  `json:"objectsSkipped"`
	ObjectsArchived int64  `json:"objectsArchived"`
	Error           any    `json:"error"`
	HeartbeatAt     string `json:"heartbeatAt"`
}

// StorageClassMigrator copies existing backup objects into the S3 storage
// class configured on their target. Progress is stored in the same state file
// as the Node implementation so rolling deployment status remains readable.
type StorageClassMigrator struct {
	path   string
	mu     sync.Mutex
	active map[int64]bool
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewStorageClassMigrator(stateDir string) (*StorageClassMigrator, error) {
	if stateDir == "" {
		return nil, errors.New("backup storage migration state directory is required")
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &StorageClassMigrator{path: filepath.Join(stateDir, storageClassStateFile), active: map[int64]bool{}, ctx: ctx, cancel: cancel}, nil
}

func (m *StorageClassMigrator) Start(targetID int64, config S3Config) error {
	if targetID < 1 {
		return errors.New("invalid backup target id")
	}
	if config.StorageClass == "" {
		config.StorageClass = "STANDARD"
	}
	probe, err := NewS3Target(config)
	if err != nil {
		return err
	}
	_ = probe.Close()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active[targetID] {
		return ErrStorageMigrationRunning
	}
	all, err := m.readStateLocked()
	if err != nil {
		return err
	}
	if state, ok := all[strconv.FormatInt(targetID, 10)]; ok && state.Status == "running" && time.Since(parseStorageMigrationTime(state.HeartbeatAt)) <= storageClassStaleAfter {
		return ErrStorageMigrationRunning
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	state := StorageClassMigration{Status: "running", StorageClass: config.StorageClass, StartedAt: started, HeartbeatAt: started}
	all[strconv.FormatInt(targetID, 10)] = state
	if err = m.writeStateLocked(all); err != nil {
		return err
	}
	m.active[targetID] = true
	m.wg.Add(1)
	go m.run(targetID, config, state)
	return nil
}

var ErrStorageMigrationRunning = errors.New("storage class migration is already running")

func (m *StorageClassMigrator) Status(targetID int64) (any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := m.readStateLocked()
	if err != nil {
		return nil, err
	}
	state, ok := all[strconv.FormatInt(targetID, 10)]
	if !ok {
		return nil, nil
	}
	if state.Status == "running" && time.Since(parseStorageMigrationTime(state.HeartbeatAt)) > storageClassStaleAfter {
		state.Status = "interrupted"
	}
	return state, nil
}

func (m *StorageClassMigrator) run(targetID int64, config S3Config, state StorageClassMigration) {
	defer m.wg.Done()
	defer func() {
		m.mu.Lock()
		delete(m.active, targetID)
		m.mu.Unlock()
	}()
	heartbeatStop := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(storageClassHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatStop:
				return
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.touch(targetID)
			}
		}
	}()
	defer func() {
		close(heartbeatStop)
		<-heartbeatDone
	}()
	ctx, cancel := context.WithCancel(m.ctx)
	defer cancel()
	target, err := NewS3Target(config)
	if err == nil {
		err = target.Open(ctx)
	}
	if err != nil {
		state.Status = "failed"
		state.Error = "The S3 backup target could not be opened."
		state.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		m.save(targetID, state)
		return
	}
	defer target.Close()
	objects := make([]S3Object, 0)
	for _, area := range storageClassAreas {
		listed, listErr := target.ListObjects(ctx, area)
		if listErr != nil {
			state.Status = "failed"
			state.Error = "Could not list backup objects on the S3 target."
			state.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
			m.save(targetID, state)
			slog.Error("S3 storage-class migration listing failed", "targetId", targetID, "error", listErr)
			return
		}
		for _, object := range listed {
			switch {
			case object.StorageClass == config.StorageClass:
				state.ObjectsSkipped++
			case object.StorageClass == "GLACIER" || object.StorageClass == "DEEP_ARCHIVE":
				state.ObjectsArchived++
			default:
				objects = append(objects, object)
			}
		}
	}
	totalObjects, totalBytes := int64(len(objects)), int64(0)
	for _, object := range objects {
		totalBytes += object.Size
	}
	state.ObjectsTotal = &totalObjects
	state.BytesTotal = &totalBytes
	m.save(targetID, state)
	type result struct {
		object S3Object
		err    error
	}
	jobs := make(chan S3Object)
	results := make(chan result, storageClassConcurrency)
	var workers sync.WaitGroup
	for i := 0; i < storageClassConcurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for object := range jobs {
				copyErr := target.ChangeStorageClass(ctx, object.Key, object.Size)
				results <- result{object: object, err: copyErr}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, object := range objects {
			select {
			case jobs <- object:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	lastSaved := time.Now()
	var firstError error
	for item := range results {
		if item.err != nil {
			if s3ObjectNotFound(item.err) {
				state.ObjectsDone++
				state.BytesDone += item.object.Size
			} else if firstError == nil {
				firstError = item.err
				cancel()
			}
		} else {
			state.ObjectsDone++
			state.BytesDone += item.object.Size
		}
		if time.Since(lastSaved) >= time.Second {
			m.save(targetID, state)
			lastSaved = time.Now()
		}
	}
	if firstError == nil && m.ctx.Err() != nil {
		firstError = m.ctx.Err()
	}
	if firstError != nil {
		state.Status = "failed"
		state.Error = "Some backup objects could not be moved to the configured storage class."
		if errors.Is(firstError, context.Canceled) {
			state.Status = "interrupted"
			state.Error = "The migration was interrupted. Start it again to resume."
		}
		slog.Error("S3 storage-class migration failed", "targetId", targetID, "error", firstError)
	} else {
		state.Status = "ok"
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	state.FinishedAt = finishedAt
	state.HeartbeatAt = finishedAt
	m.save(targetID, state)
}

func (m *StorageClassMigrator) save(targetID int64, state StorageClassMigration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := m.readStateLocked()
	if err != nil {
		slog.Error("Could not read S3 storage migration state", "targetId", targetID, "error", err)
		return
	}
	state.HeartbeatAt = time.Now().UTC().Format(time.RFC3339Nano)
	all[strconv.FormatInt(targetID, 10)] = state
	if err = m.writeStateLocked(all); err != nil {
		slog.Error("Could not persist S3 storage migration state", "targetId", targetID, "error", err)
	}
}

func (m *StorageClassMigrator) touch(targetID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all, err := m.readStateLocked()
	if err != nil {
		slog.Error("Could not read S3 storage migration state", "targetId", targetID, "error", err)
		return
	}
	key := strconv.FormatInt(targetID, 10)
	state, ok := all[key]
	if !ok || state.Status != "running" {
		return
	}
	state.HeartbeatAt = time.Now().UTC().Format(time.RFC3339Nano)
	all[key] = state
	if err = m.writeStateLocked(all); err != nil {
		slog.Error("Could not persist S3 storage migration heartbeat", "targetId", targetID, "error", err)
	}
}

func (m *StorageClassMigrator) readStateLocked() (map[string]StorageClassMigration, error) {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]StorageClassMigration{}, nil
	}
	if err != nil {
		return nil, err
	}
	state := map[string]StorageClassMigration{}
	if err = json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode backup storage migration state: %w", err)
	}
	return state, nil
}

func (m *StorageClassMigrator) writeStateLocked(state map[string]StorageClassMigration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".backup-storage-class-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, m.path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(m.path))
}

func parseStorageMigrationTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func (m *StorageClassMigrator) Close() {
	if m == nil {
		return
	}
	m.cancel()
	m.wg.Wait()
}
