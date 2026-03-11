//go:build cgo

package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"airtune/internal/ui"
)

var version = "dev"

func main() {
	runtime.LockOSThread()

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Log to file in %APPDATA%/airtune/ + in-memory ring buffer for the dev console.
	// LogBuffer and logFile go first — os.Stderr may be invalid in -H windowsgui mode,
	// and io.MultiWriter short-circuits on the first write error.
	logDir := filepath.Join(os.Getenv("APPDATA"), "airtune")
	os.MkdirAll(logDir, 0755)
	writers := []io.Writer{ui.LogBuffer()}
	logFile, err := os.OpenFile(filepath.Join(logDir, "airtune.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err == nil {
		writers = append(writers, logFile)
		defer logFile.Close()
	}
	// Only add stderr if it's actually usable (not in -H windowsgui)
	if _, err := os.Stderr.Stat(); err == nil {
		writers = append(writers, os.Stderr)
	}
	log.SetOutput(io.MultiWriter(writers...))

	ui.SetVersion(version)

	// Launch GTK immediately — manager starts in background after window is visible
	app := ui.NewApp(nil)
	os.Exit(app.Run())
}
