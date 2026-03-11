//go:build cgo

package ui

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Splash is the initial loading screen shown while discovering devices.
type Splash struct {
	Box *gtk.Box
}

// NewSplash creates the splash/loader widget.
func NewSplash() *Splash {
	box := gtk.NewBox(gtk.OrientationVertical, 16)
	box.SetVAlign(gtk.AlignCenter)
	box.SetHAlign(gtk.AlignCenter)
	box.SetVExpand(true)

	// App icon / title
	title := gtk.NewLabel("AirTune")
	title.AddCSSClass("splash-title")
	box.Append(title)

	subtitle := gtk.NewLabel("v" + getVersion())
	subtitle.AddCSSClass("splash-version")
	box.Append(subtitle)

	// Spinner
	spinner := gtk.NewSpinner()
	spinner.SetSizeRequest(32, 32)
	spinner.Start()
	spinner.SetMarginTop(24)
	box.Append(spinner)

	// Status text
	status := gtk.NewLabel("Searching for AirPlay devices...")
	status.AddCSSClass("splash-status")
	status.SetMarginTop(8)
	box.Append(status)

	return &Splash{Box: box}
}

var appVersion = "dev"

// SetVersion sets the version string for the splash screen.
func SetVersion(v string) {
	appVersion = v
}

func getVersion() string {
	return appVersion
}
