//go:build cgo

package ui

import (
	"log"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"airtune/internal/audio"
	"airtune/internal/service"
)

// MainWindow is the primary application window.
type MainWindow struct {
	*gtk.ApplicationWindow
	manager    *service.Manager
	stack      *gtk.Stack
	splash     *Splash
	mainBox    *gtk.Box
	deviceList *DeviceList
	controls   *Controls
	visualizer *Visualizer
	devConsole *DevConsole
	statusBar  *gtk.Label
	sourceDrop     *gtk.DropDown
	sourceIDs      []string
	sourceUpdating bool
	splashDone     bool
}

// NewMainWindow creates the main window with all UI components.
func NewMainWindow(app *gtk.Application, manager *service.Manager) *MainWindow {
	win := gtk.NewApplicationWindow(app)
	win.SetTitle("AirTune")
	win.SetDefaultSize(400, 600)

	w := &MainWindow{
		ApplicationWindow: win,
		manager:           manager,
	}

	w.buildUI()
	return w
}

func (w *MainWindow) buildUI() {
	// Stack to switch between splash and main view
	w.stack = gtk.NewStack()
	w.stack.SetTransitionType(gtk.StackTransitionTypeCrossfade)
	w.stack.SetTransitionDuration(300)

	// Splash screen
	w.splash = NewSplash()
	w.stack.AddNamed(w.splash.Box, "splash")

	// Main UI
	w.mainBox = w.buildMainUI()
	w.stack.AddNamed(w.mainBox, "main")

	// Start on splash
	w.stack.SetVisibleChildName("splash")

	w.SetChild(w.stack)
}

func (w *MainWindow) buildMainUI() *gtk.Box {
	vbox := gtk.NewBox(gtk.OrientationVertical, 8)
	vbox.SetMarginTop(20)
	vbox.SetMarginBottom(16)
	vbox.SetMarginStart(16)
	vbox.SetMarginEnd(16)

	// Title
	title := gtk.NewLabel("AirTune")
	title.AddCSSClass("title-label")
	title.SetHAlign(gtk.AlignStart)
	vbox.Append(title)

	// Status
	w.statusBar = gtk.NewLabel("Searching for AirPlay devices...")
	w.statusBar.AddCSSClass("status-label")
	w.statusBar.SetHAlign(gtk.AlignStart)
	w.statusBar.SetMarginBottom(8)
	vbox.Append(w.statusBar)

	// Visualizer
	w.visualizer = NewVisualizer()
	vbox.Append(w.visualizer.DrawingArea)

	// Audio source section
	sourceLabel := gtk.NewLabel("SOURCE")
	sourceLabel.AddCSSClass("section-label")
	sourceLabel.SetHAlign(gtk.AlignStart)
	vbox.Append(sourceLabel)

	w.sourceDrop = gtk.NewDropDown(gtk.NewStringList([]string{"Default"}), nil)
	w.sourceDrop.AddCSSClass("channel-drop")
	w.sourceDrop.SetMarginBottom(8)
	w.sourceIDs = []string{""}
	w.sourceDrop.NotifyProperty("selected", func() {
		if w.sourceUpdating {
			return
		}
		idx := w.sourceDrop.Selected()
		if int(idx) < len(w.sourceIDs) {
			w.manager.SetAudioDevice(w.sourceIDs[idx])
		}
	})
	vbox.Append(w.sourceDrop)

	go w.loadAudioDevices()

	// Device list header
	devicesHeader := gtk.NewBox(gtk.OrientationHorizontal, 0)
	devicesLabel := gtk.NewLabel("SPEAKERS & TVS")
	devicesLabel.AddCSSClass("section-label")
	devicesLabel.SetHAlign(gtk.AlignStart)
	devicesLabel.SetHExpand(true)
	devicesHeader.Append(devicesLabel)

	refreshBtn := gtk.NewButtonFromIconName("view-refresh-symbolic")
	refreshBtn.AddCSSClass("refresh-btn")
	refreshBtn.SetVAlign(gtk.AlignCenter)
	refreshBtn.SetTooltipText("Refresh devices")
	refreshBtn.ConnectClicked(func() {
		w.statusBar.SetText("Searching for AirPlay devices...")
		go w.manager.RefreshDevices()
	})
	devicesHeader.Append(refreshBtn)
	vbox.Append(devicesHeader)

	// Device list
	w.deviceList = NewDeviceList(w.manager)
	scrolled := gtk.NewScrolledWindow()
	scrolled.SetVExpand(true)
	scrolled.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scrolled.SetChild(w.deviceList.ListBox)
	vbox.Append(scrolled)

	// Controls
	w.controls = NewControls(w.manager)
	vbox.Append(w.controls.Box)

	// Dev console (collapsible)
	w.devConsole = NewDevConsole()
	vbox.Append(w.devConsole.Expander)

	return vbox
}

// transitionToMain switches from splash to the main UI.
func (w *MainWindow) transitionToMain() {
	if w.splashDone {
		return
	}
	w.splashDone = true
	w.stack.SetVisibleChildName("main")
}

// loadAudioDevices enumerates audio devices and populates the source dropdown.
func (w *MainWindow) loadAudioDevices() {
	devices, err := w.manager.GetAudioDevices()
	if err != nil {
		log.Printf("ui: enumerate audio devices: %v", err)
		return
	}
	glib.IdleAdd(func() {
		w.updateSourceDropdown(devices)
	})
}

// updateSourceDropdown rebuilds the source dropdown with the given devices.
func (w *MainWindow) updateSourceDropdown(devices []audio.AudioDevice) {
	configuredID := w.manager.GetAudioDevice()

	names := []string{"Default"}
	ids := []string{""}
	selectedIdx := uint(0)

	for _, dev := range devices {
		names = append(names, dev.Name)
		ids = append(ids, dev.ID)
		if dev.ID == configuredID {
			selectedIdx = uint(len(names) - 1)
		}
	}

	w.sourceUpdating = true
	w.sourceIDs = ids
	model := gtk.NewStringList(names)
	w.sourceDrop.SetModel(model)
	w.sourceDrop.SetSelected(selectedIdx)
	w.sourceUpdating = false
}

// HandleEvent processes a manager event on the GTK main thread.
func (w *MainWindow) HandleEvent(evt service.Event) {
	glib.IdleAdd(func() {
		switch evt.Type {
		case service.EventDevicesChanged:
			w.deviceList.UpdateDevices(evt.Devices)
			if len(evt.Devices) == 0 {
				w.statusBar.SetText("Searching for AirPlay devices...")
			} else {
				w.statusBar.SetText("")
				// First devices arrived — leave the splash
				w.transitionToMain()
			}

		case service.EventSessionState:
			w.deviceList.UpdateSessionState(evt.DeviceID, evt.SessionState)

		case service.EventAppState:
			w.controls.UpdateState(evt.AppState)
			switch evt.AppState {
			case service.AppStateIdle:
				w.statusBar.SetText("Not connected")
			case service.AppStateStreaming:
				w.statusBar.SetText("Streaming")
			case service.AppStatePaused:
				w.statusBar.SetText("Paused")
			}

		case service.EventAudioDevices:
			w.updateSourceDropdown(evt.AudioDevices)

		case service.EventVisualizerData:
			w.visualizer.SetLevels(evt.Levels)

		case service.EventAVSync:
			w.controls.avSwitch.SetActive(evt.AVSync)

		case service.EventError:
			w.statusBar.SetText("Error: " + evt.Error.Error())
		}
	})
}
