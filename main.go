//go:build cgo

package main

import (
	"io"
	"log"
	"os"
	"runtime"

	"airtune/internal/ui"
)

var version = "dev"

func main() {
	runtime.LockOSThread()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Log to file + in-memory ring buffer for the dev console
	logFile, err := os.OpenFile("airtune.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile, ui.LogBuffer()))
		defer logFile.Close()
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, ui.LogBuffer()))
	}

	ui.SetVersion(version)

	// Launch GTK immediately — manager starts in background after window is visible
	app := ui.NewApp(nil)
	os.Exit(app.Run())
}
