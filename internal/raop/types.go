package raop

import "net"

// SessionState represents the state of a RAOP session.
type SessionState int

const (
	StateDisconnected SessionState = iota
	StateConnecting
	StateConnected
	StateStreaming
	StatePaused
	StateError
)

func (s SessionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateStreaming:
		return "streaming"
	case StatePaused:
		return "paused"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// SessionConfig holds parameters for a RAOP session.
type SessionConfig struct {
	// Device address (host:port from mDNS)
	Host string
	Port int

	// Encryption
	AESKey    []byte // 16-byte AES key
	AESIV     []byte // 16-byte AES IV
	Encrypted bool   // whether to use AES encryption

	// Codec info for SDP
	CodecName string
	FmtpLine  string

	// Audio format
	SampleRate   int
	Channels     int
	BitDepth     int
	FramesPerPkt int
}

// ServerPorts holds the UDP ports returned by SETUP.
type ServerPorts struct {
	Audio   int
	Control int
	Timing  int
}

// ClientPorts holds the local UDP ports we bind.
type ClientPorts struct {
	Audio   int
	Control int
	Timing  int
}

// UDPChannels holds the active UDP connections for a session.
type UDPChannels struct {
	AudioConn   *net.UDPConn
	ControlConn *net.UDPConn
	TimingConn  *net.UDPConn

	AudioAddr   *net.UDPAddr
	ControlAddr *net.UDPAddr
	TimingAddr  *net.UDPAddr
}

// RAOP constants
const (
	FramesPerPacket = 352
	SampleRate      = 44100
	Channels        = 2
	BitDepth        = 16
	BytesPerFrame   = Channels * (BitDepth / 8) // 4

	// LatencyFrames is the sender-side latency offset in audio frames.
	// This controls how far ahead of real-time we send audio, which determines
	// how much the receiver buffers before playing. Lower values = less delay.
	// Apple defaults: 88200 (2s) for music, 11025 (250ms) for low-latency.
	LatencyFrames = 11025 // 250ms at 44100Hz

	// RTP payload types
	PayloadTiming          = 82
	PayloadTimingResponse  = 83
	PayloadSync            = 84
	PayloadRetransmitReq   = 85
	PayloadRetransmitReply = 86
	PayloadAudio           = 96

	// NTP epoch offset: seconds between 1900-01-01 and 1970-01-01
	NTPEpochOffset = 2208988800
)

// AirPlay RSA public key (from Apple's published key, used by all AirPlay 1 devices)
const AirPlayRSAPublicKeyPEM = `-----BEGIN RSA PUBLIC KEY-----
MIGJAoGBANS+ws5JIxCk/lmVc89FHnhB08kRIkPoYVEMubM9JGfxJo/S4sFSJd+J
Gi+ONRP4c9cBnzMUMA0JsaFw7h0i9j1RlWn/cUk9b+OVU1G7tdryJCLNzG0lvnnM
QijlVQBfW09bElbWqRY2dGuaAOsiB1VQ8p9UlGsjuiwGY/fibhQPAgMBAAE=
-----END RSA PUBLIC KEY-----`
