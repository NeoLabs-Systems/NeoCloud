package application

import (
	"context"
	"log/slog"
	"time"
)

const (
	handoffFirstMaintenanceDelay = 30 * time.Second
	handoffMaintenanceInterval   = time.Hour
)

func (a *Application) runHandoffMaintenance(ctx context.Context) {
	timer := time.NewTimer(handoffFirstMaintenanceDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if _, _, err := a.handoff.RunMaintenance(ctx); err != nil && ctx.Err() == nil {
			slog.Error("Go handoff maintenance failed", "error", err)
		}
		timer.Reset(handoffMaintenanceInterval)
	}
}
