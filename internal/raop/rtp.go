package raop

import (
	"encoding/binary"
)

// RTPHeader is a minimal RTP header for RAOP audio packets.
// RAOP uses a 12-byte RTP header (no CSRC, no extensions for audio).
type RTPHeader struct {
	// Byte 0: V=2, P=0, X=0, CC=0 → 0x80
	// For the first packet after RECORD/FLUSH, set marker bit: 0x80 | 0x60 = 0xE0
	// Byte 1: M=0/1, PT=96 → 0x60 or 0xE0
	Marker     bool
	PayloadType uint8
	Sequence    uint16
	Timestamp   uint32
	SSRC        uint32
}

// BuildAudioPacket creates a complete RAOP audio RTP packet.
// The packet format is:
//   [0]    0x80 (RTP v2)
//   [1]    0x60 (PT 96) or 0xE0 (PT 96 + marker)
//   [2:4]  sequence number (big-endian)
//   [4:8]  timestamp (big-endian)
//   [8:12] SSRC (big-endian)
//   [12:]  encrypted audio payload
func BuildAudioPacket(hdr RTPHeader, payload []byte) []byte {
	pkt := make([]byte, 12+len(payload))

	// Byte 0: version 2, no padding, no extension, no CSRC
	pkt[0] = 0x80

	// Byte 1: marker + payload type
	pkt[1] = hdr.PayloadType & 0x7F
	if hdr.Marker {
		pkt[1] |= 0x80
	}

	// Bytes 2-3: sequence number
	binary.BigEndian.PutUint16(pkt[2:4], hdr.Sequence)

	// Bytes 4-7: timestamp
	binary.BigEndian.PutUint32(pkt[4:8], hdr.Timestamp)

	// Bytes 8-11: SSRC
	binary.BigEndian.PutUint32(pkt[8:12], hdr.SSRC)

	// Payload (already encrypted)
	copy(pkt[12:], payload)

	return pkt
}

// AudioPacketBuilder manages RTP sequence numbers and timestamps for audio streaming.
type AudioPacketBuilder struct {
	sequence  uint16
	timestamp uint32
	ssrc      uint32
	marker    bool // set on first packet or after flush
}

// NewAudioPacketBuilder creates a new builder with a random-ish SSRC.
func NewAudioPacketBuilder(ssrc uint32) *AudioPacketBuilder {
	return &AudioPacketBuilder{
		sequence:  0,
		timestamp: 0,
		ssrc:      ssrc,
		marker:    true, // first packet gets marker bit
	}
}

// Build creates an RTP packet with the given encrypted audio payload.
// It auto-increments sequence and advances timestamp by framesPerPacket.
func (b *AudioPacketBuilder) Build(encryptedPayload []byte, framesPerPacket int) []byte {
	hdr := RTPHeader{
		Marker:      b.marker,
		PayloadType: PayloadAudio,
		Sequence:    b.sequence,
		Timestamp:   b.timestamp,
		SSRC:        b.ssrc,
	}

	pkt := BuildAudioPacket(hdr, encryptedPayload)

	b.sequence++
	b.timestamp += uint32(framesPerPacket)
	b.marker = false

	return pkt
}

// SetMarker sets the marker bit for the next packet (used after FLUSH/RECORD).
func (b *AudioPacketBuilder) SetMarker() {
	b.marker = true
}

// CurrentTimestamp returns the current RTP timestamp.
func (b *AudioPacketBuilder) CurrentTimestamp() uint32 {
	return b.timestamp
}

// CurrentSequence returns the current sequence number.
func (b *AudioPacketBuilder) CurrentSequence() uint16 {
	return b.sequence
}
