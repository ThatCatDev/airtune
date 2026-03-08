//go:build cgo

package main

import (
	"context"
	"os"
	"runtime"

	"airtune/internal/service"
	"airtune/internal/ui"
)

func runGUIOrCLI() {
	runGUI()
}

func runGUI() {
	runtime.LockOSThread()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := service.NewManager()
	manager.Start(ctx)
	defer manager.Stop()

	tray := ui.NewTray(manager, nil, func() {
		cancel()
		os.Exit(0)
	})
	go tray.Run()

	app := ui.NewApp(manager)
	os.Exit(app.Run())
}
