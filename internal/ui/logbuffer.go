//go:build cgo

package ui

import (
	"io"
	"sync"
)

// ringBuffer is a thread-safe ring buffer that implements io.Writer.
// It keeps the last maxLines lines of log output for the dev console.
type ringBuffer struct {
	mu       sync.Mutex
	lines    []string
	maxLines int
	current  []byte // partial line accumulator
	onChange func() // called (under lock released) when new lines arrive
}

var globalLogBuffer = &ringBuffer{
	maxLines: 500,
}

// LogBuffer returns the global log ring buffer as an io.Writer.
// Call this before any log output to capture logs for the dev console.
func LogBuffer() io.Writer {
	return globalLogBuffer
}

func (rb *ringBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()

	// Split input into lines
	for _, b := range p {
		if b == '\n' {
			rb.lines = append(rb.lines, string(rb.current))
			rb.current = rb.current[:0]
			if len(rb.lines) > rb.maxLines {
				rb.lines = rb.lines[len(rb.lines)-rb.maxLines:]
			}
		} else {
			rb.current = append(rb.current, b)
		}
	}

	cb := rb.onChange
	rb.mu.Unlock()

	if cb != nil {
		cb()
	}
	return len(p), nil
}

// Lines returns a snapshot of all buffered lines.
func (rb *ringBuffer) Lines() []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]string, len(rb.lines))
	copy(out, rb.lines)
	return out
}

// SetOnChange sets a callback that fires when new log lines arrive.
func (rb *ringBuffer) SetOnChange(fn func()) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.onChange = fn
}
