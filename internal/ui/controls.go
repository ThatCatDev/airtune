//go:build cgo

package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"airtune/internal/service"
)

// Controls manages the play/pause button and A/V sync toggle.
type Controls struct {
	Box       *gtk.Box
	playBtn   *gtk.Button
	avSwitch  *gtk.Switch
	manager   *service.Manager
}

// NewControls creates the controls widget.
func NewControls(manager *service.Manager) *Controls {
	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.AddCSSClass("controls-box")

	c := &Controls{
		Box:     box,
		manager: manager,
	}

	// Play/Pause button
	c.playBtn = gtk.NewButtonWithLabel("Play / Pause")
	c.playBtn.AddCSSClass("play-btn")
	c.playBtn.SetHAlign(gtk.AlignCenter)
	c.playBtn.ConnectClicked(func() {
		manager.PlayPause()
	})
	box.Append(c.playBtn)

	// A/V Sync toggle
	syncRow := gtk.NewBox(gtk.OrientationHorizontal, 8)
	syncRow.SetMarginTop(8)

	syncLabel := gtk.NewLabel("A/V Sync")
	syncLabel.AddCSSClass("device-name")
	syncLabel.SetHExpand(true)
	syncLabel.SetHAlign(gtk.AlignStart)
	syncRow.Append(syncLabel)

	c.avSwitch = gtk.NewSwitch()
	c.avSwitch.SetActive(manager.AVSync())
	c.avSwitch.SetVAlign(gtk.AlignCenter)
	c.avSwitch.ConnectStateSet(func(state bool) bool {
		go manager.SetAVSync(state)
		return false // let GTK update the switch state
	})
	syncRow.Append(c.avSwitch)

	box.Append(syncRow)

	syncDesc := gtk.NewLabel("Delays video playback to match AirPlay audio latency")
	syncDesc.AddCSSClass("device-info")
	syncDesc.SetHAlign(gtk.AlignStart)
	syncDesc.SetWrap(true)
	box.Append(syncDesc)

	return c
}

// UpdateState updates the controls based on the app state.
func (c *Controls) UpdateState(state service.AppState) {
	switch state {
	case service.AppStateStreaming:
		c.playBtn.SetLabel("Pause")
	case service.AppStatePaused:
		c.playBtn.SetLabel("Resume")
	default:
		c.playBtn.SetLabel("Play / Pause")
	}
}
