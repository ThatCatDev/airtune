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
	sysVolume    float32
	sysMuted     bool
	volReady     bool // true after first system volume reading

	// A/V sync: hooks IAudioClock::GetPosition() system-wide to delay video
	avSync     bool
	avSyncHook *audio.AVSyncHook
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
		events:       make(chan Event, 64),
		sessions:     make(map[string]*deviceSession),
		encoder:      codec.NewPCMEncoder(),
		channelModes: make(map[string]audio.ChannelMode),
		audioDevice:  cfg.AudioDevice,
		avSync:       cfg.AVSync,
		avSyncHook:   audio.NewAVSyncHook(),
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

// RefreshDevices restarts the mDNS browser to re-discover devices.
func (m *Manager) RefreshDevices() {
	log.Println("manager: refreshing device discovery")
	m.mu.Lock()
	oldBrowser := m.browser
	m.mu.Unlock()

	// Stop old browser
	if oldBrowser != nil {
		oldBrowser.Stop()
	}

	// Clear device list
	m.mu.Lock()
	m.devices = nil
	m.mu.Unlock()
	m.emit(Event{Type: EventDevicesChanged, Devices: nil})

	// Start new browser
	newBrowser := discovery.NewBrowser(func(devices []discovery.AirPlayDevice) {
		m.mu.Lock()
		m.devices = devices
		m.mu.Unlock()
		m.emit(Event{
			Type:    EventDevicesChanged,
			Devices: devices,
		})
	})

	m.mu.Lock()
	m.browser = newBrowser
	m.mu.Unlock()

	go func() {
		if err := newBrowser.Start(m.ctx); err != nil {
			log.Printf("manager: discovery error: %v", err)
		}
	}()
}

// syncSystemVolume reads system volume changes and forwards them to all
// connected AirPlay devices.
func (m *Manager) syncSystemVolume(volCh <-chan audio.VolumeChange) {
	for vc := range volCh {
		m.mu.Lock()
		m.sysVolume = vc.Level
		m.sysMuted = vc.Muted
		m.volReady = true
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
	// Disable A/V sync hook if active
	m.avSyncHook.Disable()

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

	// Check if already connected
	if _, ok := m.sessions[id]; ok {
		m.mu.Unlock()
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
		m.mu.Unlock()
		return fmt.Errorf("device %s not found", id)
	}

	// Snapshot device info before releasing the lock
	devCopy := *dev

	// Ensure pipeline is running
	if m.pipeline == nil {
		capturer := m.createCapturer()
		p := audio.NewPipeline(m.encoder, capturer)
		if err := p.Start(m.ctx); err != nil {
			m.mu.Unlock()
			return fmt.Errorf("start pipeline: %w", err)
		}
		m.pipeline = p
	}

	// Create session while holding the lock (reads encoder + ctx)
	enc := m.encoder
	ctx := m.ctx
	session := raop.NewSession(devCopy.Host, devCopy.Port, enc.CodecName(), enc.FmtpLine(), devCopy.SupportsEncryption())
	session.OnStateChange(func(state raop.SessionState) {
		m.emit(Event{
			Type:         EventSessionState,
			DeviceID:     id,
			SessionState: state,
		})
	})

	sessionCtx, sessionCancel := context.WithCancel(ctx)

	// Release the lock during the blocking RTSP handshake so other
	// operations (volume sync, discovery, UI) aren't starved.
	m.mu.Unlock()

	if err := session.Connect(sessionCtx); err != nil {
		sessionCancel()
		return fmt.Errorf("connect to %s: %w", devCopy.Name, err)
	}

	// Re-acquire lock to register the session
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if device was disconnected/removed while we were connecting
	if _, ok := m.sessions[id]; ok {
		sessionCancel()
		return fmt.Errorf("device %s already connected (race)", id)
	}

	// Emit latency event so the UI/app can compensate (e.g. delay video).
	m.emit(Event{
		Type:     EventLatency,
		DeviceID: id,
		Latency:  session.LatencyDuration(),
	})
	log.Printf("manager: device %s audio latency: %v", devCopy.Name, session.LatencyDuration())

	m.sessions[id] = &deviceSession{
		session: session,
		device:  devCopy,
		cancel:  sessionCancel,
	}

	// Update A/V sync hook with latest latency
	m.updateAVSyncLatency()

	// Subscribe to audio pipeline with the device's channel mode
	chMode := m.channelModes[id] // defaults to ChannelBoth (0)
	audioCh := m.pipeline.Subscribe(id, chMode)

	// Start streaming in a goroutine
	go session.Start(audioCh)

	// Periodically update driver latency as NTP RTT measurements refine the estimate.
	// Runs until the session context is cancelled (disconnect).
	go func() {
		// Wait for first RTT measurement, then update once
		select {
		case <-time.After(5 * time.Second):
		case <-sessionCtx.Done():
			return
		}
		m.mu.Lock()
		m.updateAVSyncLatency()
		m.mu.Unlock()
		log.Printf("manager: device %s refined latency: %v", devCopy.Name, session.LatencyDuration())
	}()

	// Sync the device's volume with the current Windows system volume.
	// Only set if we've received at least one reading from the OS;
	// otherwise the volume sync goroutine will push it momentarily.
	if m.volReady {
		var db float64
		if m.sysMuted || m.sysVolume < 0.001 {
			db = -144
		} else {
			db = raop.LinearToAirPlay(float64(m.sysVolume))
		}
		if err := session.SetVolume(db); err != nil {
			log.Printf("manager: set initial volume: %v", err)
		}
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

	// Recalculate A/V sync latency after session removal
	m.updateAVSyncLatency()

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

// AVSync returns whether A/V sync mode is enabled.
func (m *Manager) AVSync() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.avSync
}

// SetAVSync enables or disables A/V sync mode.
// When enabled: hooks IAudioClock::GetPosition() system-wide so video renderers
// see a delayed audio position and hold video frames to match AirPlay latency.
func (m *Manager) SetAVSync(enabled bool) {
	m.mu.Lock()
	if m.avSync == enabled {
		m.mu.Unlock()
		return
	}
	m.avSync = enabled
	m.mu.Unlock()

	if enabled {
		m.enableAVSync()
	} else {
		m.disableAVSync()
	}

	m.emit(Event{Type: EventAVSync, AVSync: enabled})
	go m.saveConfig()
}

func (m *Manager) enableAVSync() {
	// Calculate latency from connected sessions
	m.mu.Lock()
	latencyHNS := m.maxSessionLatencyHNS()
	m.mu.Unlock()

	if latencyHNS == 0 {
		// No sessions connected yet — use a default estimate.
		// Will be updated when a device connects.
		latencyHNS = 5000000 // 500ms default
		log.Println("manager: A/V sync: no sessions connected, using 500ms default latency")
	}

	if err := m.avSyncHook.Enable(latencyHNS); err != nil {
		log.Printf("manager: A/V sync: enable hook: %v", err)
		m.mu.Lock()
		m.avSync = false
		m.mu.Unlock()
		m.emit(Event{Type: EventError, Error: fmt.Errorf("A/V sync hook failed: %v", err)})
		return
	}

	log.Printf("manager: A/V sync enabled — hook active, latency=%dms", latencyHNS/10000)
}

func (m *Manager) disableAVSync() {
	m.avSyncHook.Disable()
	log.Println("manager: A/V sync disabled — hook removed")
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
		ChannelModes:  make(map[string]audio.ChannelMode, len(m.channelModes)),
		AudioDevice: m.audioDevice,
		AVSync:      m.avSync,
	}
	for id, mode := range m.channelModes {
		cfg.ChannelModes[id] = mode
	}
	m.mu.Unlock()
	SaveConfig(cfg)
}

// createCapturer returns a WASAPI loopback capturer for the configured device.
// Must be called with m.mu held.
func (m *Manager) createCapturer() audio.Capturer {
	return audio.NewWASAPILoopbackCapturer(m.audioDevice)
}

// maxSessionLatencyHNS returns the maximum latency across all connected sessions
// in 100-nanosecond units. Must be called with m.mu held.
func (m *Manager) maxSessionLatencyHNS() int64 {
	var maxLatency time.Duration
	for _, ds := range m.sessions {
		lat := ds.session.LatencyDuration()
		if lat > maxLatency {
			maxLatency = lat
		}
	}
	// Convert to 100-nanosecond units
	return maxLatency.Nanoseconds() / 100
}

// updateAVSyncLatency pushes the current max session latency to the hook.
// Must be called with m.mu held.
func (m *Manager) updateAVSyncLatency() {
	if !m.avSync {
		return
	}
	hns := m.maxSessionLatencyHNS()
	if hns > 0 {
		m.avSyncHook.SetLatency(hns)
		log.Printf("manager: A/V sync latency updated to %dms", hns/10000)
	}
}

func (m *Manager) emit(evt Event) {
	select {
	case m.events <- evt:
	default:
		log.Printf("manager: event channel full, dropping event %d", evt.Type)
	}
}
