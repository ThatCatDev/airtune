package codec

// Encoder encodes PCM audio data into a codec format.
type Encoder interface {
	// Encode encodes raw PCM data and returns the encoded bytes.
	Encode(pcm []byte) ([]byte, error)
	// CodecName returns the codec name for SDP (e.g. "AppleLossless", "L16").
	CodecName() string
	// FmtpLine returns the SDP fmtp attribute line.
	FmtpLine() string
}
