//go:build cgo

package ui

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

var iconSetupOnce sync.Once

// setupCustomIcons writes Apple-device SVG icons to the user config dir
// and registers them with the GTK icon theme.
func setupCustomIcons() {
	iconSetupOnce.Do(func() {
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			log.Printf("ui: icons: config dir: %v", err)
			return
		}

		iconBase := filepath.Join(cfgDir, "airtune", "icons")
		svgDir := filepath.Join(iconBase, "hicolor", "scalable", "actions")
		if err := os.MkdirAll(svgDir, 0755); err != nil {
			log.Printf("ui: icons: mkdir: %v", err)
			return
		}

		for name, svg := range deviceSVGs {
			p := filepath.Join(svgDir, name+".svg")
			if err := os.WriteFile(p, []byte(svg), 0644); err != nil {
				log.Printf("ui: icons: write %s: %v", name, err)
			}
		}

		display := gdk.DisplayGetDefault()
		theme := gtk.IconThemeGetForDisplay(display)
		theme.AddSearchPath(iconBase)

		log.Printf("ui: installed %d custom icons to %s", len(deviceSVGs), iconBase)
	})
}

// deviceIcon returns the icon name for a given AirPlay device model string.
func deviceIcon(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.HasPrefix(m, "audioaccessory"):
		return "airtune-homepod-symbolic"
	case strings.HasPrefix(m, "appletv"):
		return "airtune-appletv-symbolic"
	case strings.HasPrefix(m, "macbookpro"), strings.HasPrefix(m, "macbookair"),
		strings.HasPrefix(m, "macbook"):
		return "airtune-macbook-symbolic"
	case strings.HasPrefix(m, "macmini"), strings.HasPrefix(m, "macpro"),
		strings.HasPrefix(m, "imac"), strings.HasPrefix(m, "mac"):
		return "airtune-mac-symbolic"
	case strings.HasPrefix(m, "iphone"):
		return "airtune-iphone-symbolic"
	case strings.HasPrefix(m, "ipad"):
		return "airtune-ipad-symbolic"
	case strings.HasPrefix(m, "airportexpress"), strings.HasPrefix(m, "airport"):
		return "airtune-airport-symbolic"
	default:
		return "airtune-speaker-symbolic"
	}
}

// unescapeDNS removes mDNS/DNS name escaping (backslash-prefixed characters).
func unescapeDNS(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// deviceSVGs maps icon names to SVG data.
// These are 16×16 symbolic icons using #bebebe fill (GTK recolors for context).
var deviceSVGs = map[string]string{
	// HomePod — tall rounded pill shape with circular touch surface at top.
	"airtune-homepod-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" fill-rule="evenodd" d="M5 4c0-1.66 1.34-3 3-3s3 1.34 3 3v8c0 1.66-1.34 3-3 3s-3-1.34-3-3V4zm3 1.5a1.25 1.25 0 1 0 0-2.5 1.25 1.25 0 0 0 0 2.5z"/>
</svg>`,

	// Apple TV — flat wide rounded rectangle (set-top box from front).
	"airtune-appletv-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <rect x="2" y="5" width="12" height="6" rx="1.5" fill="#bebebe"/>
</svg>`,

	// MacBook — laptop with screen and keyboard base.
	"airtune-macbook-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" d="M3 3a1 1 0 0 1 1-1h8a1 1 0 0 1 1 1v7H3V3z"/>
  <path fill="#bebebe" d="M1 11.5h14c0 .83-.67 1.5-1.5 1.5h-11C1.67 13 1 12.33 1 11.5z"/>
</svg>`,

	// Mac desktop — rounded square with circle (Mac mini top-down / iMac front).
	"airtune-mac-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" fill-rule="evenodd" d="M2 4a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V4zm6 5.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z"/>
</svg>`,

	// iPhone — tall rounded rectangle with home indicator line at bottom.
	"airtune-iphone-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" fill-rule="evenodd" d="M4 3a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V3zm2.5 10h3a.5.5 0 0 1 0 1h-3a.5.5 0 0 1 0-1z"/>
</svg>`,

	// iPad — wider rounded rectangle with home indicator.
	"airtune-ipad-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" fill-rule="evenodd" d="M2 3.5A1.5 1.5 0 0 1 3.5 2h9A1.5 1.5 0 0 1 14 3.5v9a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 12.5v-9zM8 13a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5z"/>
</svg>`,

	// AirPort Express — flat disc with antenna bump.
	"airtune-airport-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <rect x="3" y="6" width="10" height="6" rx="3" fill="#bebebe"/>
  <rect x="7" y="3" width="2" height="4" rx="1" fill="#bebebe"/>
</svg>`,

	// Generic AirPlay speaker — speaker cone shape.
	"airtune-speaker-symbolic": `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">
  <path fill="#bebebe" fill-rule="evenodd" d="M3 4a2 2 0 0 1 2-2h6a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V4zm5 6a2 2 0 1 0 0-4 2 2 0 0 0 0 4z"/>
</svg>`,
}
