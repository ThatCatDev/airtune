package audio

import "time"

// AudioFormat describes the PCM audio format.
type AudioFormat struct {
	SampleRate    int // e.g. 44100, 48000
	Channels      int // e.g. 2 (stereo)
	BitDepth      int // e.g. 16
	FrameSize     int // Channels * (BitDepth / 8)
	FramesPerPkt  int // frames per RAOP packet (352)
}

// CD-quality target for AirPlay
var AirPlayFormat = AudioFormat{
	SampleRate:   44100,
	Channels:     2,
	BitDepth:     16,
	FrameSize:    4, // 2ch * 2bytes
	FramesPerPkt: 352,
}

// AudioChunk is a timestamped block of PCM audio data.
type AudioChunk struct {
	Data      []byte
	Format    AudioFormat
	Timestamp time.Time
}

// EncodedPacket is an encoded audio packet ready for RTP wrapping.
type EncodedPacket struct {
	Data      []byte
	Frames    int       // number of audio frames in this packet
	Timestamp time.Time
}

// ChannelMode controls which stereo channels a subscriber receives.
type ChannelMode int

const (
	ChannelBoth  ChannelMode = iota // both L+R (normal stereo)
	ChannelLeft                     // left channel only (duplicated to both speakers)
	ChannelRight                    // right channel only (duplicated to both speakers)
)

func (m ChannelMode) String() string {
	switch m {
	case ChannelLeft:
		return "L"
	case ChannelRight:
		return "R"
	default:
		return "LR"
	}
}

// Capturer captures system audio.
type Capturer interface {
	// Start begins capturing audio. Returns a channel of PCM chunks.
	Start(ctx interface{}) (<-chan AudioChunk, error)
	// Format returns the native capture format.
	Format() AudioFormat
	// Close releases resources.
	Close() error
}
