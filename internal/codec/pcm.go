package codec

import "encoding/binary"

// ALACUncompressedEncoder wraps raw PCM data in uncompressed ALAC frames.
// Uses proper bit-packed ALAC framing with the escape flag set.
type ALACUncompressedEncoder struct{}

// NewPCMEncoder creates an encoder that wraps PCM in uncompressed ALAC frames.
func NewPCMEncoder() *ALACUncompressedEncoder {
	return &ALACUncompressedEncoder{}
}

// Encode wraps 16-bit LE interleaved PCM in a bit-packed uncompressed ALAC frame.
//
// ALAC uncompressed frame bit layout (MSB-first bitstream):
//
//	[3]  tag = 1 (CPE, channel pair element for stereo)
//	[4]  element_instance_tag = 0
//	[12] unused = 0
//	[1]  partialFrame = 0
//	[2]  bytesShifted = 0
//	[1]  isNotCompressed = 1  (escape flag)
//	--- 23 bits of header ---
//	[16 × numSamples × numChannels] sample data (signed 16-bit, MSB first)
func (e *ALACUncompressedEncoder) Encode(pcm []byte) ([]byte, error) {
	numSamples := len(pcm) / 2 // total individual samples (L+R interleaved)

	// Total bits: 23-bit header + 16 bits per sample
	totalBits := 23 + numSamples*16
	totalBytes := (totalBits + 7) / 8
	out := make([]byte, totalBytes)

	// Write 23-bit header via bitstream
	var bs bitWriter
	bs.buf = out

	// tag = 1 (CPE for stereo)
	bs.writeBits(1, 3)
	// element_instance_tag = 0
	bs.writeBits(0, 4)
	// unused header = 0
	bs.writeBits(0, 12)
	// partialFrame = 0
	bs.writeBits(0, 1)
	// bytesShifted = 0
	bs.writeBits(0, 2)
	// isNotCompressed = 1 (escape/uncompressed)
	bs.writeBits(1, 1)
	// Now at bit offset 23

	// Write interleaved 16-bit signed samples, MSB first
	for i := 0; i < numSamples; i++ {
		sample := binary.LittleEndian.Uint16(pcm[i*2 : i*2+2])
		bs.writeBits(uint32(sample), 16)
	}

	return out, nil
}

// CodecName returns "AppleLossless" for SDP rtpmap.
func (e *ALACUncompressedEncoder) CodecName() string {
	return "AppleLossless"
}

// FmtpLine returns the ALAC SDP fmtp parameters.
func (e *ALACUncompressedEncoder) FmtpLine() string {
	return "96 352 0 16 40 10 14 2 255 0 0 44100"
}

// bitWriter writes bits MSB-first into a byte slice.
type bitWriter struct {
	buf    []byte
	bitPos int // current bit position in the buffer
}

// writeBits writes the lowest `n` bits of val into the bitstream, MSB first.
func (w *bitWriter) writeBits(val uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		if val&(1<<uint(i)) != 0 {
			byteIdx := w.bitPos / 8
			bitIdx := 7 - (w.bitPos % 8) // MSB first within each byte
			w.buf[byteIdx] |= 1 << uint(bitIdx)
		}
		w.bitPos++
	}
}
