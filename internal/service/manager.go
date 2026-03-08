package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"airtune/internal/audio"
	"airtune/internal/codec"
	"airtune/internal/discovery"
	"airtune/internal/raop"
)

// Manager is the central orchestrator bridging the UI and backend.
// The UI calls Manager methods; Manager emits events consumed by the UI.
type Manager struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	events   chan Event
	state    AppState
	paused   bool

	// Discovery
	browser *discovery.Browser
	devices []discovery.AirPlayDevice

	// Audio pipeline
	pipeline *audio.Pipeline
	encoder  codec.Encoder

	// Active sessions (keyed by device ID)
	sessions map[string]*deviceSession

	// Per-device channel mode (set by UI before connecting)
	channelModes map[string]audio.ChannelMode

	// Audio capture device (WASAPI device ID, empty = system default)
	audioDevice string

	// Current system volume (0.0–1.0), synced from Windows
	sysVolume float32
	sysMuted  bool
}

type deviceSession struct {
	session *raop.Session
	device  discovery.AirPlayDevice
	cancel  context.CancelFunc
}

// NewManager creates a new Manager.
func NewManager() *Manager {
	cfg := LoadConfig()
	m := &Manager{
		events:        make(chan Event, 64),
		sessions:      make(map[string]*deviceSession),
		encoder:       codec.NewPCMEncoder(),
		channelModes: make(map[string]audio.ChannelMode),
		audioDevice:  cfg.AudioDevice,
	}
	for id, mode := range cfg.ChannelModes {
		m.channelModes[id] = mode
	}
	return m
}

// Events returns the event channel for UI consumption.
func (m *Manager) Events() <-chan Event {
	return m.events
}

// Start initializes the manager: starts discovery and prepares the pipeline.
func (m *Manager) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	// Start mDNS discovery
	m.browser = discovery.NewBrowser(func(devices []discovery.AirPlayDevice) {
		m.mu.Lock()
		m.devices = devices
		m.mu.Unlock()

		m.emit(Event{
			Type:    EventDevicesChanged,
			Devices: devices,
		})
	})

	go func() {
		if err := m.browser.Start(m.ctx); err != nil {
			log.Printf("manager: discovery error: %v", err)
		}
	}()

	// Monitor Windows system volume and sync to AirPlay devices
	volCh := audio.MonitorSystemVolume(m.ctx, 250*time.Millisecond)
	go m.syncSystemVolume(volCh)

	log.Println("manager: started")
}

// syncSystemVolume reads system volume changes and forwards them to all
// connected AirPlay devices.
func (m *Manager) syncSystemVolume(volCh <-chan audio.VolumeChange) {
	for vc := range volCh {
		m.mu.Lock()
		m.sysVolume = vc.Level
		m.sysMuted = vc.Muted
		// Snapshot current sessions
		sessions := make([]*deviceSession, 0, len(m.sessions))
		for _, ds := range m.sessions {
			sessions = append(sessions, ds)
		}
		m.mu.Unlock()

		var db float64
		if vc.Muted || vc.Level < 0.001 {
			db = -144 // AirPlay silence
		} else {
			db = raop.LinearToAirPlay(float64(vc.Level))
		}

		for _, ds := range sessions {
			if err := ds.session.SetVolume(db); err != nil {
				log.Printf("manager: sync volume: %v", err)
			}
		}
	}
}

// Stop shuts down the manager, disconnecting all devices.
func (m *Manager) Stop() {
	m.mu.Lock()
	for id := range m.sessions {
		m.disconnectLocked(id)
	}
	m.mu.Unlock()

	if m.pipeline != nil {
		m.pipeline.Stop()
	}
	if m.cancel != nil {
		m.cancel()
	}
	log.Println("manager: stopped")
}

// Devices returns the currently discovered devices.
func (m *Manager) Devices() []discovery.AirPlayDevice {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]discovery.AirPlayDevice, len(m.devices))
	copy(result, m.devices)
	return result
}

// ConnectDevice connects to the AirPlay device with the given ID.
func (m *Manager) ConnectDevice(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already connected
	if _, ok := m.sessions[id]; ok {
		return fmt.Errorf("device %s already connected", id)
	}

	// Find the device
	var dev *discovery.AirPlayDevice
	for i := range m.devices {
		if m.devices[i].ID == id {
			dev = &m.devices[i]
			break
		}
	}
	if dev == nil {
		return fmt.Errorf("device %s not found", id)
	}

	// Ensure pipeline is running
	if m.pipeline == nil {
		p := audio.NewPipeline(m.encoder, audio.NewWASAPILoopbackCapturer(m.audioDevice))
		if err := p.Start(m.ctx); err != nil {
			return fmt.Errorf("start pipeline: %w", err)
		}
		m.pipeline = p
	}

	// Create and connect session
	enc := m.encoder
	session := raop.NewSession(dev.Host, dev.Port, enc.CodecName(), enc.FmtpLine(), dev.SupportsEncryption())
	session.OnStateChange(func(state raop.SessionState) {
		m.emit(Event{
			Type:         EventSessionState,
			DeviceID:     id,
			SessionState: state,
		})
	})

	sessionCtx, sessionCancel := context.WithCancel(m.ctx)

	if err := session.Connect(sessionCtx); err != nil {
		sessionCancel()
		return fmt.Errorf("connect to %s: %w", dev.Name, err)
	}

	// Emit latency event so the UI/app can compensate (e.g. delay video).
	m.emit(Event{
		Type:     EventLatency,
		DeviceID: id,
		Latency:  session.LatencyDuration(),
	})
	log.Printf("manager: device %s audio latency: %v", dev.Name, session.LatencyDuration())

	m.sessions[id] = &deviceSession{
		session: session,
		device:  *dev,
		cancel:  sessionCancel,
	}

	// Subscribe to audio pipeline with the device's channel mode
	chMode := m.channelModes[id] // defaults to ChannelBoth (0)
	audioCh := m.pipeline.Subscribe(id, chMode)

	// Start streaming in a goroutine
	go session.Start(audioCh)

	// Sync the device's volume with the current Windows system volume.
	var db float64
	if m.sysMuted || m.sysVolume < 0.001 {
		db = -144
	} else {
		db = raop.LinearToAirPlay(float64(m.sysVolume))
	}
	if err := session.SetVolume(db); err != nil {
		log.Printf("manager: set initial volume: %v", err)
	}

	m.updateAppState()

	return nil
}

// DisconnectDevice disconnects from the AirPlay device with the given ID.
func (m *Manager) DisconnectDevice(id string) {
	m.mu.Lock()
	m.disconnectLocked(id)
	m.updateAppState()
	m.mu.Unlock()
}

func (m *Manager) disconnectLocked(id string) {
	ds, ok := m.sessions[id]
	if !ok {
		return
	}

	if m.pipeline != nil {
		m.pipeline.Unsubscribe(id)
	}
	ds.cancel()
	ds.session.Close()
	delete(m.sessions, id)

	log.Printf("manager: disconnected device %s", id)
}

// IsConnected returns whether the given device is connected.
func (m *Manager) IsConnected(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[id]
	return ok
}

// SetChannelMode sets the stereo channel mode for a device (Both/Left/Right).
// If called while connected, the device will be disconnected and reconnected
// with the new mode. The new mode is persisted to config.
func (m *Manager) SetChannelMode(id string, mode audio.ChannelMode) {
	m.mu.Lock()
	m.channelModes[id] = mode
	_, connected := m.sessions[id]
	m.mu.Unlock()

	// Save config
	go m.saveConfig()

	if connected {
		m.DisconnectDevice(id)
		if err := m.ConnectDevice(id); err != nil {
			log.Printf("manager: reconnect with new channel mode: %v", err)
		}
	}
}

// GetAudioDevices returns the list of available audio capture devices.
func (m *Manager) GetAudioDevices() ([]audio.AudioDevice, error) {
	return audio.EnumerateAudioDevices()
}

// GetAudioDevice returns the currently configured audio device ID.
func (m *Manager) GetAudioDevice() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.audioDevice
}

// SetAudioDevice changes the audio capture device and restarts the pipeline if running.
func (m *Manager) SetAudioDevice(deviceID string) {
	m.mu.Lock()
	m.audioDevice = deviceID
	needRestart := m.pipeline != nil
	// Snapshot connected device IDs + modes for reconnection
	var connectedIDs []string
	var connectedModes []audio.ChannelMode
	if needRestart {
		for id := range m.sessions {
			connectedIDs = append(connectedIDs, id)
			connectedModes = append(connectedModes, m.channelModes[id])
		}
	}
	m.mu.Unlock()

	go m.saveConfig()

	if needRestart {
		// Disconnect all, stop pipeline, restart with new device
		for _, id := range connectedIDs {
			m.DisconnectDevice(id)
		}
		m.mu.Lock()
		if m.pipeline != nil {
			m.pipeline.Stop()
			m.pipeline = nil
		}
		m.mu.Unlock()

		// Reconnect all previously connected devices
		for i, id := range connectedIDs {
			m.mu.Lock()
			m.channelModes[id] = connectedModes[i]
			m.mu.Unlock()
			if err := m.ConnectDevice(id); err != nil {
				log.Printf("manager: reconnect %s after audio device change: %v", id, err)
			}
		}
	}

	// Emit updated audio devices list so UI can update selection state
	if devices, err := audio.EnumerateAudioDevices(); err == nil {
		m.emit(Event{
			Type:         EventAudioDevices,
			AudioDevices: devices,
		})
	}
}

// GetChannelMode returns the channel mode for a device (defaults to ChannelBoth).
func (m *Manager) GetChannelMode(id string) audio.ChannelMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.channelModes[id]
}

// PlayPause toggles play/pause for all connected devices.
func (m *Manager) PlayPause() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.paused {
		// Resume: reconnect pipeline subscribers
		// For simplicity, we just update state — the pipeline continues running
		m.paused = false
		m.updateAppState()
	} else {
		// Pause: flush all sessions
		for _, ds := range m.sessions {
			ds.session.Flush()
		}
		m.paused = true
		m.updateAppState()
	}
}

// AppState returns the current application state.
func (m *Manager) AppState() AppState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *Manager) updateAppState() {
	var newState AppState
	if len(m.sessions) == 0 {
		newState = AppStateIdle
	} else if m.paused {
		newState = AppStatePaused
	} else {
		newState = AppStateStreaming
	}

	if newState != m.state {
		m.state = newState
		m.emit(Event{
			Type:     EventAppState,
			AppState: newState,
		})
	}
}

// saveConfig writes the current settings to disk.
func (m *Manager) saveConfig() {
	m.mu.Lock()
	cfg := Config{
		ChannelModes: make(map[string]audio.ChannelMode, len(m.channelModes)),
		AudioDevice:  m.audioDevice,
	}
	for id, mode := range m.channelModes {
		cfg.ChannelModes[id] = mode
	}
	m.mu.Unlock()
	SaveConfig(cfg)
}

func (m *Manager) emit(evt Event) {
	select {
	case m.events <- evt:
	default:
		log.Printf("manager: event channel full, dropping event %d", evt.Type)
	}
}
