//go:build cgo

package ui

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// Visualizer renders audio level bars using a GTK DrawingArea.
type Visualizer struct {
	DrawingArea *gtk.DrawingArea
	levels      []float64 // current RMS levels per channel
	smoothed    []float64 // smoothed levels for display
	numBars     int
}

// NewVisualizer creates a new audio visualizer widget.
func NewVisualizer() *Visualizer {
	v := &Visualizer{
		numBars:  16,
		levels:   make([]float64, 2),
		smoothed: make([]float64, 16),
	}

	da := gtk.NewDrawingArea()
	da.AddCSSClass("visualizer")
	da.SetContentHeight(60)
	da.SetContentWidth(360)
	da.SetDrawFunc(func(da *gtk.DrawingArea, cr *cairo.Context, width, height int) {
		v.draw(cr, width, height)
	})

	v.DrawingArea = da
	return v
}

// SetLevels updates the visualizer with new audio levels and queues a redraw.
func (v *Visualizer) SetLevels(levels []float64) {
	v.levels = levels

	// Generate bar levels from the stereo RMS levels
	// Create a simple bar visualization from the stereo levels
	if len(levels) >= 2 {
		half := v.numBars / 2
		for i := 0; i < half; i++ {
			// Left channel bars (mirrored from center)
			target := levels[0] * (1.0 - float64(half-1-i)/float64(half)*0.7)
			v.smoothed[i] = v.smoothed[i]*0.7 + target*0.3
		}
		for i := half; i < v.numBars; i++ {
			// Right channel bars
			target := levels[1] * (1.0 - float64(i-half)/float64(half)*0.7)
			v.smoothed[i] = v.smoothed[i]*0.7 + target*0.3
		}
	}

	v.DrawingArea.QueueDraw()
}

func (v *Visualizer) draw(cr *cairo.Context, width, height int) {
	barWidth := float64(width) / float64(v.numBars)
	gap := 2.0

	for i := 0; i < v.numBars; i++ {
		level := v.smoothed[i]
		if level < 0.01 {
			level = 0.01 // minimum visible bar
		}

		barHeight := level * float64(height) * 0.9

		x := float64(i) * barWidth
		y := float64(height) - barHeight

		// Color: gradient from green to red based on level
		r, g, b := barColor(level)
		cr.SetSourceRGB(r, g, b)

		cr.Rectangle(x+gap/2, y, barWidth-gap, barHeight)
		cr.Fill()
	}
}

// barColor returns RGB values based on audio level (0-1).
// Uses an Apple-style blue gradient: soft cyan at low → vivid blue at high.
func barColor(level float64) (float64, float64, float64) {
	// Low:  #5ac8fa  (0.35, 0.78, 0.98)
	// High: #0a84ff  (0.04, 0.52, 1.00)
	t := math.Min(level, 1.0)
	r := 0.35 - t*0.31
	g := 0.78 - t*0.26
	b := 0.98 + t*0.02
	return r, g, b
}
