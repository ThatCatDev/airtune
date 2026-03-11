//go:build cgo

package main

import (
	"context"
	"io"
	"log"
	"os"
	"runtime"

	"airtune/internal/service"
	"airtune/internal/ui"
)

var version = "dev"

func main() {
	runtime.LockOSThread()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Log to file + in-memory ring buffer for the dev console
	logFile, err := os.OpenFile("airtune.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile, ui.LogBuffer()))
		defer logFile.Close()
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, ui.LogBuffer()))
	}

	ui.SetVersion(version)

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
