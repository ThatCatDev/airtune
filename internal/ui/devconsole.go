//go:build cgo

package ui

import (
	"strings"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// DevConsole is a collapsible log viewer panel.
type DevConsole struct {
	Expander *gtk.Expander
	textView *gtk.TextView
	buffer   *gtk.TextBuffer
	endMark  *gtk.TextMark
}

// NewDevConsole creates the dev console widget.
func NewDevConsole() *DevConsole {
	dc := &DevConsole{}

	dc.buffer = gtk.NewTextBuffer(nil)
	// Create a mark at the end of the buffer for reliable auto-scroll
	endIter := dc.buffer.EndIter()
	dc.endMark = dc.buffer.CreateMark("end", endIter, false)
	dc.textView = gtk.NewTextViewWithBuffer(dc.buffer)
	dc.textView.SetEditable(false)
	dc.textView.SetCursorVisible(false)
	dc.textView.SetMonospace(true)
	dc.textView.SetWrapMode(gtk.WrapWordChar)
	dc.textView.AddCSSClass("dev-console-text")

	scrolled := gtk.NewScrolledWindow()
	scrolled.SetVExpand(true)
	scrolled.SetPolicy(gtk.PolicyAutomatic, gtk.PolicyAutomatic)
	scrolled.SetChild(dc.textView)
	scrolled.SetSizeRequest(-1, 200)
	scrolled.AddCSSClass("dev-console")

	dc.Expander = gtk.NewExpander("Developer")
	dc.Expander.AddCSSClass("dev-expander")
	dc.Expander.SetChild(scrolled)
	dc.Expander.SetExpanded(false)

	// Wire up live log updates
	globalLogBuffer.SetOnChange(func() {
		glib.IdleAdd(func() {
			dc.refresh()
		})
	})

	// Load existing lines
	dc.refresh()

	return dc
}

func (dc *DevConsole) refresh() {
	lines := globalLogBuffer.Lines()
	dc.buffer.SetText(strings.Join(lines, "\n"))

	// Auto-scroll to bottom using the end mark (more reliable than ScrollToIter)
	dc.textView.ScrollToMark(dc.endMark, 0, true, 0, 1.0)
}
