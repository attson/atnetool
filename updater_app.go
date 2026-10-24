package main

import (
	"context"
	"fmt"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) startUpdater(ctx context.Context) {
	if a.updater == nil {
		return
	}
	a.updater.SetEmitter(func(state UpdateState) {
		if a.ctx != nil {
			wailsruntime.EventsEmit(a.ctx, updateEventName, state)
		}
	})
	if !a.updater.State().Enabled {
		return
	}
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		checkContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		_, _ = a.updater.Check(checkContext)
	}()
}

func (a *App) GetUpdateState() UpdateState {
	if a.updater == nil {
		return UpdateState{Phase: updatePhaseDisabled, CurrentVersion: appVersion}
	}
	return a.updater.State()
}

func (a *App) CheckForUpdates() (UpdateState, error) {
	if a.updater == nil {
		return UpdateState{}, fmt.Errorf("更新服务尚未初始化")
	}
	ctx, cancel := context.WithTimeout(a.baseContext(), 20*time.Second)
	defer cancel()
	return a.updater.Check(ctx)
}

func (a *App) DownloadUpdate() (UpdateState, error) {
	if a.updater == nil {
		return UpdateState{}, fmt.Errorf("更新服务尚未初始化")
	}
	return a.updater.Download(a.baseContext())
}

func (a *App) CancelUpdate() UpdateState {
	if a.updater == nil {
		return UpdateState{Phase: updatePhaseDisabled, CurrentVersion: appVersion}
	}
	return a.updater.Cancel()
}

func (a *App) ApplyUpdate() (UpdateState, error) {
	if a.updater == nil {
		return UpdateState{}, fmt.Errorf("更新服务尚未初始化")
	}
	state, err := a.updater.Apply()
	if err != nil {
		return state, err
	}
	go func() {
		time.Sleep(350 * time.Millisecond)
		if a.ctx != nil {
			wailsruntime.Quit(a.ctx)
		}
	}()
	return state, nil
}
