//go:build desktop

// Command wayveil-gui is the Wayveil desktop app (Wails v2 + Svelte).
package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/theuittti/WayVeil/internal/gui"
)

func main() {
	// mihomo/xray are found next to the executable by their own Find functions.
	exeDir := "."
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	em := &emitter{}
	app := gui.NewDefault(em, exeDir)
	dialogs := &Dialogs{}

	err := wails.Run(&options.App{
		Title:            "Wayveil",
		Width:            380,
		Height:           640,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 11, G: 15, B: 26, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(ctx context.Context) {
			em.setContext(ctx)
			dialogs.setContext(ctx)
		},
		OnShutdown: func(context.Context) { app.Shutdown() },
		Bind:       []interface{}{app, dialogs},
	})
	if err != nil {
		log.Fatal(err)
	}
}
