//go:build cgo

package ui

import (
	"log"

	"github.com/getlantern/systray"

	"airtune/internal/service"
)

// Tray manages the system tray icon and menu.
type Tray struct {
	manager   *service.Manager
	showFunc  func()
	quitFunc  func()
	menuItems struct {
		showHide *systray.MenuItem
		status   *systray.MenuItem
		quit     *systray.MenuItem
	}
}

// NewTray creates a new system tray handler.
func NewTray(manager *service.Manager, showFunc, quitFunc func()) *Tray {
	return &Tray{
		manager:  manager,
		showFunc: showFunc,
		quitFunc: quitFunc,
	}
}

// Run starts the system tray. This should be called from a goroutine
// as it blocks. Call systray.Quit() to stop.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	systray.SetTitle("AirTune")
	systray.SetTooltip("AirTune - AirPlay Audio Streamer")

	// Use a simple icon (1x1 transparent PNG as placeholder)
	// In production, load from assets/
	systray.SetIcon(trayIconData)

	t.menuItems.status = systray.AddMenuItem("Idle", "Current status")
	t.menuItems.status.Disable()

	systray.AddSeparator()

	t.menuItems.showHide = systray.AddMenuItem("Show Window", "Show or hide the main window")
	t.menuItems.quit = systray.AddMenuItem("Quit", "Quit AirTune")

	go func() {
		for {
			select {
			case <-t.menuItems.showHide.ClickedCh:
				if t.showFunc != nil {
					t.showFunc()
				}
			case <-t.menuItems.quit.ClickedCh:
				if t.quitFunc != nil {
					t.quitFunc()
				}
				systray.Quit()
				return
			}
		}
	}()

	log.Println("ui: system tray ready")
}

func (t *Tray) onExit() {
	log.Println("ui: system tray exiting")
}

// UpdateStatus updates the status menu item text.
func (t *Tray) UpdateStatus(status string) {
	if t.menuItems.status != nil {
		t.menuItems.status.SetTitle(status)
	}
}

// trayIconData is a minimal 16x16 PNG icon (blue circle).
// In production, replace with proper icon from assets/.
var trayIconData = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x10,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0xf3, 0xff, 0x61, 0x00, 0x00, 0x00,
	0x01, 0x73, 0x52, 0x47, 0x42, 0x00, 0xae, 0xce, 0x1c, 0xe9, 0x00, 0x00,
	0x00, 0x15, 0x49, 0x44, 0x41, 0x54, 0x38, 0xcb, 0x63, 0x60, 0x18, 0x05,
	0xa3, 0x60, 0x14, 0x8c, 0x82, 0x51, 0x40, 0x31, 0x00, 0x00, 0x04, 0x10,
	0x00, 0x01, 0x85, 0xa5, 0x20, 0x3b, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}
