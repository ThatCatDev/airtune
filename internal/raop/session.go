package raop

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"airtune/internal/audio"
)

// Session manages the full lifecycle of a RAOP connection to one AirPlay device.
// It handles RTSP handshake, UDP channel setup, audio streaming, volume, and teardown.
type Session struct {
	mu    sync.Mutex
	state SessionState

	config SessionConfig

	// RTSP
	rtsp *RTSPClient

	// UDP channels
	udp         UDPChannels
	serverPorts ServerPorts
	clientPorts ClientPorts

	// Audio streaming
	rtpBuilder *AudioPacketBuilder
	timing     *TimingChannel
	control    *ControlChannel

	// syncLatency: buffer depth in frames used in sync packets (PT 84).
	// This tells the receiver how far ahead of real-time we're sending.
	syncLatency uint32

	// hwLatency: receiver's hardware/DAC buffer in frames (from Audio-Latency header).
	hwLatency uint32

	// networkRTT: measured round-trip time to the receiver (from NTP timing).
	networkRTT time.Duration

	// Lifecycle
	cancel context.CancelFunc
	done   chan struct{}

	// Callback for state changes
	onStateChange func(SessionState)
}

// NewSession creates a new RAOP session for the given device.
// Set encrypted=true if the device supports RSA encryption (et contains "1").
func NewSession(host string, port int, codecName, fmtpLine string, encrypted bool) *Session {
	return &Session{
		state: StateDisconnected,
		config: SessionConfig{
			Host:         host,
			Port:         port,
			CodecName:    codecName,
			FmtpLine:     fmtpLine,
			SampleRate:   SampleRate,
			Channels:     Channels,
			BitDepth:     BitDepth,
			FramesPerPkt: FramesPerPacket,
			Encrypted:    encrypted,
		},
	}
}

// OnStateChange sets a callback that fires whenever the session state changes.
func (s *Session) OnStateChange(fn func(SessionState)) {
	s.mu.Lock()
	s.onStateChange = fn
	s.mu.Unlock()
}

// State returns the current session state.
func (s *Session) State() SessionState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Latency returns the sync buffer depth in frames (used in sync packets).
func (s *Session) Latency() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.syncLatency
}

// LatencyDuration returns the estimated total audio latency from capture to speaker.
// This is the amount video needs to be delayed to stay in sync.
// Components: sync buffer depth + hardware buffer + network one-way transit.
func (s *Session) LatencyDuration() time.Duration {
	s.mu.Lock()
	syncFrames := s.syncLatency
	hwFrames := s.hwLatency
	rtt := s.networkRTT
	s.mu.Unlock()

	syncDur := time.Duration(float64(syncFrames) / float64(SampleRate) * float64(time.Second))
	hwDur := time.Duration(float64(hwFrames) / float64(SampleRate) * float64(time.Second))
	networkOneWay := rtt / 2

	return syncDur + hwDur + networkOneWay
}

// SetNetworkRTT updates the measured network round-trip time.
func (s *Session) SetNetworkRTT(rtt time.Duration) {
	s.mu.Lock()
	s.networkRTT = rtt
	s.mu.Unlock()
}

func (s *Session) setState(state SessionState) {
	s.mu.Lock()
	s.state = state
	fn := s.onStateChange
	s.mu.Unlock()

	log.Printf("session: state → %s", state)
	if fn != nil {
		fn(state)
	}
}

// Connect performs the RTSP handshake: OPTIONS → ANNOUNCE → SETUP → RECORD.
// After Connect returns successfully, the session is ready for streaming.
func (s *Session) Connect(ctx context.Context) error {
	s.setState(StateConnecting)

	// Generate AES key and IV for encrypted mode
	aesKey, aesIV, err := GenerateAESKey()
	if err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: generate AES key: %w", err)
	}
	s.config.AESKey = aesKey
	s.config.AESIV = aesIV

	// Create RTSP client and connect
	s.rtsp = NewRTSPClient(s.config.Host, s.config.Port)
	if err := s.rtsp.Connect(); err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: RTSP connect: %w", err)
	}

	// OPTIONS
	if _, err := s.rtsp.Options(); err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: OPTIONS: %w", err)
	}

	// Try encrypted ANNOUNCE first, fall back to unencrypted
	var sdp string
	if s.config.Encrypted {
		rsaAESKey, err := EncryptAESKey(aesKey)
		if err != nil {
			s.setState(StateError)
			return fmt.Errorf("session: encrypt AES key: %w", err)
		}
		sdp = BuildSDP(s.config, rsaAESKey, "")
	} else {
		sdp = BuildSDPUnencrypted(s.config)
	}

	log.Printf("session: SDP:\n%s", sdp)

	_, err = s.rtsp.Announce(sdp)
	if err != nil && s.config.Encrypted {
		// Encrypted failed, try unencrypted
		log.Printf("session: encrypted ANNOUNCE failed, trying unencrypted")
		s.config.Encrypted = false

		// Reconnect (some devices close after 406)
		s.rtsp.Close()
		s.rtsp = NewRTSPClient(s.config.Host, s.config.Port)
		if err := s.rtsp.Connect(); err != nil {
			s.setState(StateError)
			return fmt.Errorf("session: RTSP reconnect: %w", err)
		}
		if _, err := s.rtsp.Options(); err != nil {
			s.setState(StateError)
			return fmt.Errorf("session: OPTIONS retry: %w", err)
		}

		sdp = BuildSDPUnencrypted(s.config)
		log.Printf("session: unencrypted SDP:\n%s", sdp)
		if _, err := s.rtsp.Announce(sdp); err != nil {
			s.setState(StateError)
			return fmt.Errorf("session: ANNOUNCE: %w", err)
		}
	} else if err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: ANNOUNCE: %w", err)
	}

	// Bind local UDP ports for control and timing
	if err := s.bindUDPPorts(); err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: bind UDP: %w", err)
	}

	// Start timing listener BEFORE SETUP — some devices (e.g. HomePod) send
	// timing requests during the SETUP handshake and return 500 if nobody responds.
	sessionCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.done = make(chan struct{})

	// Use a temporary remote address for timing (device's RTSP port as placeholder;
	// the real timing port comes back from SETUP). The listener doesn't need the
	// remote address — it replies to whatever address sent the request.
	s.timing = NewTimingChannel(s.udp.TimingConn, &net.UDPAddr{IP: net.ParseIP(s.config.Host), Port: s.config.Port}, func(rtt time.Duration) {
		s.mu.Lock()
		s.networkRTT = rtt
		s.mu.Unlock()
	})
	s.timing.Run(sessionCtx)

	// SETUP - negotiate transport
	resp, serverPorts, err := s.rtsp.Setup(s.clientPorts)
	if err != nil {
		cancel()
		s.setState(StateError)
		return fmt.Errorf("session: SETUP: %w", err)
	}
	_ = resp
	s.serverPorts = serverPorts

	// Set up remote UDP addresses
	s.udp.AudioAddr = &net.UDPAddr{IP: net.ParseIP(s.config.Host), Port: s.serverPorts.Audio}
	s.udp.ControlAddr = &net.UDPAddr{IP: net.ParseIP(s.config.Host), Port: s.serverPorts.Control}
	s.udp.TimingAddr = &net.UDPAddr{IP: net.ParseIP(s.config.Host), Port: s.serverPorts.Timing}

	// Update timing channel with the real remote address
	s.timing.SetRemoteAddr(s.udp.TimingAddr)

	// Create RTP builder with random SSRC
	ssrc := rand.Uint32()
	s.rtpBuilder = NewAudioPacketBuilder(ssrc)

	// RECORD
	recordResp, err := s.rtsp.Record(s.rtpBuilder.CurrentSequence(), s.rtpBuilder.CurrentTimestamp())
	if err != nil {
		s.setState(StateError)
		return fmt.Errorf("session: RECORD: %w", err)
	}

	// Parse the receiver's Audio-Latency from the RECORD response.
	// This is the receiver's hardware/DAC buffer depth — NOT the sync latency.
	// Sync packets use LatencyFrames (our sender-side buffer depth) which tells
	// the receiver how far ahead of real-time we're sending audio.
	s.syncLatency = uint32(LatencyFrames)
	s.hwLatency = 0
	if al, ok := recordResp.Headers["Audio-Latency"]; ok {
		if parsed, err := strconv.ParseUint(al, 10, 32); err == nil && parsed > 0 {
			s.hwLatency = uint32(parsed)
			log.Printf("session: receiver Audio-Latency: %d frames (%.0fms)",
				s.hwLatency, float64(s.hwLatency)/float64(SampleRate)*1000)
			// If the receiver needs MORE buffer than our default, increase sync latency
			if s.hwLatency > s.syncLatency {
				s.syncLatency = s.hwLatency
			}
		}
	} else {
		log.Printf("session: no Audio-Latency in RECORD response, using default sync latency %d frames", s.syncLatency)
	}
	log.Printf("session: sync latency: %d frames (%.0fms), hw latency: %d frames (%.0fms), total: %v",
		s.syncLatency, float64(s.syncLatency)/float64(SampleRate)*1000,
		s.hwLatency, float64(s.hwLatency)/float64(SampleRate)*1000,
		s.LatencyDuration())

	// Start control channel with the sync latency (timing is already running)
	s.control = NewControlChannel(s.udp.ControlConn, s.udp.ControlAddr, s.rtpBuilder, s.syncLatency)
	s.control.Run(sessionCtx)

	// Start RTSP keep-alive to prevent the device from dropping the TCP connection
	go s.keepAlive(sessionCtx)

	s.setState(StateConnected)
	return nil
}

// keepAlive sends periodic OPTIONS requests on the RTSP TCP connection
// to prevent the AirPlay device from timing out and dropping the session.
func (s *Session) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.rtsp == nil {
				return
			}
			if _, err := s.rtsp.Options(); err != nil {
				log.Printf("session: keep-alive failed: %v", err)
				return
			}
		}
	}
}

// Start begins streaming audio from the provided channel.
// This blocks until the channel is closed or the session is closed.
// Packets are sent as soon as they arrive from the pipeline — the WASAPI
// capture rate naturally paces delivery at real-time.
func (s *Session) Start(audioIn <-chan audio.EncodedPacket) {
	s.setState(StateStreaming)

	defer close(s.done)

	for pkt := range audioIn {
		// Encrypt (skip if unencrypted mode)
		var encrypted []byte
		if s.config.Encrypted {
			encrypted = EncryptAudio(pkt.Data, s.config.AESKey, s.config.AESIV)
		} else {
			encrypted = pkt.Data
		}

		// Build RTP packet
		rtpPkt := s.rtpBuilder.Build(encrypted, FramesPerPacket)

		// Send via UDP
		_, err := s.udp.AudioConn.WriteToUDP(rtpPkt, s.udp.AudioAddr)
		if err != nil {
			log.Printf("session: audio send error: %v", err)
			s.setState(StateError)
			return
		}
	}
}

// SetVolume sets the volume in dB (use LinearToAirPlay to convert from linear).
func (s *Session) SetVolume(db float64) error {
	if s.rtsp == nil {
		return fmt.Errorf("session: not connected")
	}
	body := FormatVolume(db)
	_, err := s.rtsp.SetParameter(body)
	return err
}

// Flush sends a FLUSH to the device (clears audio buffer, used for pause).
func (s *Session) Flush() error {
	if s.rtsp == nil {
		return fmt.Errorf("session: not connected")
	}
	s.rtpBuilder.SetMarker()
	_, err := s.rtsp.Flush(s.rtpBuilder.CurrentSequence(), s.rtpBuilder.CurrentTimestamp())
	if err == nil {
		s.setState(StatePaused)
	}
	return err
}

// Close tears down the session: TEARDOWN, close UDP, close TCP.
func (s *Session) Close() error {
	log.Printf("session: closing")

	if s.cancel != nil {
		s.cancel()
	}

	// Send TEARDOWN
	if s.rtsp != nil {
		s.rtsp.Teardown()
		s.rtsp.Close()
	}

	// Close UDP connections
	if s.udp.AudioConn != nil {
		s.udp.AudioConn.Close()
	}
	if s.udp.ControlConn != nil {
		s.udp.ControlConn.Close()
	}
	if s.udp.TimingConn != nil {
		s.udp.TimingConn.Close()
	}

	s.setState(StateDisconnected)
	return nil
}

// bindUDPPorts binds three local UDP ports for audio, control, and timing.
func (s *Session) bindUDPPorts() error {
	var err error

	// Audio port
	s.udp.AudioConn, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return fmt.Errorf("bind audio UDP: %w", err)
	}

	// Control port
	s.udp.ControlConn, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return fmt.Errorf("bind control UDP: %w", err)
	}

	// Timing port
	s.udp.TimingConn, err = net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return fmt.Errorf("bind timing UDP: %w", err)
	}

	// Record the ports we bound
	s.clientPorts = ClientPorts{
		Audio:   localPort(s.udp.AudioConn),
		Control: localPort(s.udp.ControlConn),
		Timing:  localPort(s.udp.TimingConn),
	}

	log.Printf("session: bound UDP ports audio=%d control=%d timing=%d",
		s.clientPorts.Audio, s.clientPorts.Control, s.clientPorts.Timing)

	return nil
}

func localPort(conn *net.UDPConn) int {
	addr := conn.LocalAddr().(*net.UDPAddr)
	p, _ := strconv.Atoi(strconv.Itoa(addr.Port))
	return p
}
