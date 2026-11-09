//go:build desktop

package main

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// emitter adapts runtime.EventsEmit to gui.Emitter. The Wails context only
// exists after OnStartup, so earlier events are dropped. It must only call
// runtime.EventsEmit: gui.App emits while holding its lock.
type emitter struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (e *emitter) setContext(ctx context.Context) {
	e.mu.Lock()
	e.ctx = ctx
	e.mu.Unlock()
}

func (e *emitter) Emit(event string, data any) {
	e.mu.RLock()
	ctx := e.ctx
	e.mu.RUnlock()
	if ctx == nil {
		return
	}
	runtime.EventsEmit(ctx, event, data)
}

// Dialogs is bound as window.go.main.Dialogs.
type Dialogs struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (d *Dialogs) setContext(ctx context.Context) {
	d.mu.Lock()
	d.ctx = ctx
	d.mu.Unlock()
}

// PickQRFile asks for a QR code image; cancelling returns "" and no error.
func (d *Dialogs) PickQRFile() (string, error) {
	d.mu.RLock()
	ctx := d.ctx
	d.mu.RUnlock()
	if ctx == nil {
		return "", nil
	}
	return runtime.OpenFileDialog(ctx, runtime.OpenDialogOptions{
		Title: "Choose a QR code image",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.png;*.jpg;*.jpeg)", Pattern: "*.png;*.jpg;*.jpeg"},
		},
	})
}
