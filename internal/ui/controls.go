//go:build cgo

package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"airtune/internal/service"
)

// Controls manages the play/pause button.
type Controls struct {
	Box     *gtk.Box
	playBtn *gtk.Button
	manager *service.Manager
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
