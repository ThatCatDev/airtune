package service

import (
	"time"

	"airtune/internal/audio"
	"airtune/internal/discovery"
	"airtune/internal/raop"
)

// EventType identifies the kind of event.
type EventType int

const (
	EventDevicesChanged EventType = iota
	EventSessionState
	EventAppState
	EventVisualizerData
	EventAudioDevices
	EventError
	EventLatency // fired after connect with the receiver's reported audio latency
	EventAVSync  // fired when A/V sync toggle changes
)

// Event is emitted by the Manager to notify the UI of state changes.
type Event struct {
	Type EventType

	// EventDevicesChanged
	Devices []discovery.AirPlayDevice

	// EventSessionState
	DeviceID     string
	SessionState raop.SessionState

	// EventAppState
	AppState AppState

	// EventVisualizerData
	Levels []float64 // per-channel RMS levels (0.0-1.0)

	// EventAudioDevices
	AudioDevices []audio.AudioDevice

	// EventError
	Error error

	// EventLatency
	Latency time.Duration // total audio output latency for this device

	// EventAVSync
	AVSync bool
}
