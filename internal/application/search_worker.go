package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neocloud/internal/searchindex"
)

const (
	searchFirstPassDelay = 15 * time.Second
	searchIdleDelay      = 5 * time.Second
	searchBusyDelay      = 50 * time.Millisecond
)

// StartWorkers starts the process-owned Go workers after the HTTP application
// has been composed. It is safe to call more than once; Close stops and joins
// every worker before closing SQLite.
func (a *Application) StartWorkers(ctx context.Context) {
	if a == nil || a.searchIndexer == nil && a.aiHandler == nil && a.photos == nil && a.imports == nil && a.handoff == nil && a.signing == nil && a.backupRunner == nil && a.maintenanceRunner == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.workerMu.Lock()
	if a.workerCancel != nil {
		a.workerMu.Unlock()
		return
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	a.workerCancel = cancel
	a.workerDone = done
	a.workerMu.Unlock()
	if a.backupRunner != nil {
		a.backupRunner.Start(workerCtx)
	}
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		if a.maintenanceRunner != nil {
			workers.Add(1)
			go func() { defer workers.Done(); a.maintenanceRunner.Run(workerCtx) }()
		}
		if a.searchIndexer != nil {
			workers.Add(1)
			go func() { defer workers.Done(); a.runSearchIndexer(workerCtx) }()
		}
		if a.aiHandler != nil {
			workers.Add(1)
			go func() { defer workers.Done(); a.aiHandler.Run(workerCtx) }()
		}
		if a.photos != nil {
			workers.Add(1)
			go func() {
				defer workers.Done()
				stop := a.photos.StartWorker(workerCtx)
				<-workerCtx.Done()
				stop()
			}()
		}
		if a.imports != nil {
			workers.Add(1)
			go func() {
				defer workers.Done()
				a.imports.StartWorker(workerCtx)
				<-workerCtx.Done()
				a.imports.Close()
			}()
		}
		if a.handoff != nil {
			workers.Add(1)
			go func() { defer workers.Done(); a.runHandoffMaintenance(workerCtx) }()
		}
		if a.signing != nil {
			workers.Add(1)
			go func() { defer workers.Done(); a.signing.RunWorker(workerCtx) }()
			workers.Add(1)
			go func() { defer workers.Done(); a.signing.RunMailWorker(workerCtx) }()
		}
		workers.Wait()
	}()
}

func (a *Application) runSearchIndexer(ctx context.Context) {
	timer := time.NewTimer(searchFirstPassDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		done, err := a.searchIndexer.RunPass(ctx, searchindex.MaxFilesPerPass)
		if err != nil && ctx.Err() == nil {
			slog.Error("Go full-text index pass failed", "error", err)
		}
		delay := searchIdleDelay
		if done > 0 && err == nil {
			delay = searchBusyDelay
		}
		timer.Reset(delay)
	}
}

func (a *Application) stopWorkers() {
	if a == nil {
		return
	}
	a.workerMu.Lock()
	cancel, done := a.workerCancel, a.workerDone
	if cancel != nil {
		cancel()
	}
	a.workerMu.Unlock()
	if done != nil {
		<-done
	}
	a.workerMu.Lock()
	if a.workerDone == done {
		a.workerCancel = nil
		a.workerDone = nil
	}
	a.workerMu.Unlock()
}
