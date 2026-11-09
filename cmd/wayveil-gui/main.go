//go:build desktop

// Command wayveil-gui is the Wayveil desktop app (Wails v2 + Svelte).
package main

import (
	"context"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	err := wails.Run(&options.App{
		Title:            "Wayveil",
		Width:            380,
		Height:           640,
		DisableResize:    true,
		BackgroundColour: &options.RGBA{R: 11, G: 15, B: 26, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        func(ctx context.Context) {},
	})
	if err != nil {
		log.Fatal(err)
	}
}
