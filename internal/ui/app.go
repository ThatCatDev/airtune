//go:build cgo

package ui

import (
	"log"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"airtune/internal/service"
)

// App wraps the GTK4 Application and manages the main window.
type App struct {
	gtkApp  *gtk.Application
	window  *MainWindow
	manager *service.Manager
}

// NewApp creates a new GTK4 application.
func NewApp(manager *service.Manager) *App {
	app := &App{
		manager: manager,
	}

	app.gtkApp = gtk.NewApplication("com.airtune.app", gio.ApplicationFlagsNone)
	app.gtkApp.ConnectActivate(func() {
		app.onActivate()
	})

	return app
}

// Run starts the GTK4 application main loop.
func (a *App) Run() int {
	return a.gtkApp.Run(nil)
}

func (a *App) onActivate() {
	// Load CSS and custom device icons
	loadCSS(a.gtkApp)
	setupCustomIcons()

	// Create main window
	a.window = NewMainWindow(a.gtkApp, a.manager)
	a.window.Show()

	// Start consuming manager events
	go a.consumeEvents()

	log.Println("ui: activated")
}

func loadCSS(app *gtk.Application) {
	provider := gtk.NewCSSProvider()
	provider.LoadFromData(cssStyles)

	display := gdk.DisplayGetDefault()
	gtk.StyleContextAddProviderForDisplay(display, provider, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}

// cssStyles — Apple Human Interface Guidelines dark theme.
// Colors: iOS dark mode system palette. Typography: macOS/iOS SF Pro scale.
// Layout: 8pt grid, 16pt margins, 10-12pt corner radii.
const cssStyles = `
/* ── Base ── */
window {
    background-color: #000000;
    color: rgba(255, 255, 255, 0.85);
    font-size: 13px;
}

/* ── Grouped list (iOS Settings / Home app style) ── */
.device-list {
    background-color: #1c1c1e;
    border-radius: 10px;
    padding: 0;
}

.device-list row {
    background: transparent;
    padding: 0;
    margin: 0;
    outline: none;
    border-bottom: 1px solid rgba(84, 84, 88, 0.36);
}

.device-list row:last-child {
    border-bottom: none;
}

.device-list row:hover {
    background-color: rgba(255, 255, 255, 0.05);
}

.device-list row:selected {
    background-color: transparent;
}

/* ── Device row (hbox inside each list row) ── */
.device-row {
    padding: 12px 16px;
    min-height: 44px;
}

.pair-row {
    background-color: rgba(120, 120, 128, 0.08);
}

/* ── Device icon ── */
.device-icon {
    color: rgba(235, 235, 245, 0.60);
    margin-right: 4px;
}

/* ── Typography ── */
.device-name {
    font-size: 15px;
    font-weight: 600;
    color: #ffffff;
}

.device-info {
    font-size: 13px;
    font-weight: 400;
    color: rgba(235, 235, 245, 0.60);
}

.section-label {
    font-size: 13px;
    font-weight: 400;
    color: rgba(235, 235, 245, 0.60);
    padding: 0 4px;
    margin-bottom: 6px;
    margin-top: 4px;
}

/* ── Buttons (filled, pill-shaped) ── */
.connect-btn {
    background: #0a84ff;
    color: #ffffff;
    border-radius: 14px;
    padding: 6px 14px;
    font-weight: 500;
    font-size: 13px;
    border: none;
    min-height: 28px;
}

.connect-btn:hover {
    background: #3a9aff;
}

.connect-btn:active {
    background: #0070e0;
}

.disconnect-btn {
    background: rgba(255, 69, 58, 0.15);
    color: #ff453a;
    border-radius: 14px;
    padding: 6px 14px;
    font-weight: 500;
    font-size: 13px;
    border: none;
    min-height: 28px;
}

.disconnect-btn:hover {
    background: rgba(255, 69, 58, 0.25);
}

.disconnect-btn:active {
    background: rgba(255, 69, 58, 0.35);
}

/* ── Controls panel ── */
.controls-box {
    background-color: #1c1c1e;
    border-radius: 10px;
    padding: 16px;
    margin-top: 8px;
}

.play-btn {
    background: #0a84ff;
    color: #ffffff;
    border-radius: 20px;
    padding: 8px 28px;
    font-size: 15px;
    font-weight: 600;
    border: none;
    min-height: 40px;
}

.play-btn:hover {
    background: #3a9aff;
}

.play-btn:active {
    background: #0070e0;
}

/* ── Channel dropdown (segmented-control feel) ── */
.channel-drop {
    font-size: 13px;
    font-weight: 400;
    background-color: rgba(120, 120, 128, 0.24);
    color: rgba(235, 235, 245, 0.60);
    border-radius: 8px;
    padding: 2px 6px;
    border: none;
    min-height: 28px;
}

/* ── Title & status (iOS Large Title style) ── */
.title-label {
    font-size: 26px;
    font-weight: 700;
    color: #ffffff;
}

.status-label {
    font-size: 13px;
    font-weight: 400;
    color: rgba(235, 235, 245, 0.60);
}

/* ── Visualizer ── */
.visualizer {
    background-color: #1c1c1e;
    border-radius: 10px;
}

/* ── Scrollbar (thin, muted) ── */
scrollbar slider {
    background-color: rgba(255, 255, 255, 0.15);
    border-radius: 4px;
    min-width: 6px;
}

scrollbar trough {
    background-color: transparent;
}
`

func (a *App) consumeEvents() {
	for evt := range a.manager.Events() {
		evt := evt // capture for closure
		a.window.HandleEvent(evt)
	}
}
