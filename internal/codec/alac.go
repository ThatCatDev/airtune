//go:build cgo && alac

package codec

// ALACEncoder wraps a CGo ALAC encoder (Phase 3).
// This is a placeholder — implement by wrapping mikebrady/alac.
type ALACEncoder struct{}

func NewALACEncoder() *ALACEncoder {
	return &ALACEncoder{}
}

func (e *ALACEncoder) Encode(pcm []byte) ([]byte, error) {
	// TODO: CGo ALAC encoding
	return pcm, nil
}

func (e *ALACEncoder) CodecName() string {
	return "AppleLossless"
}

func (e *ALACEncoder) FmtpLine() string {
	return "96 352 0 16 40 10 14 2 255 0 0 44100"
}
