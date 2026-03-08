package audio

import (
	"encoding/binary"
	"log"
	"math"
	"time"

	"github.com/zeozeozeo/gomplerate"
)

// Resampler converts PCM audio between sample rates using sinc interpolation.
type Resampler struct {
	fromRate int
	toRate   int
	channels int
	inner    *gomplerate.Resampler
}

// NewResampler creates a resampler that converts from fromRate to toRate.
// Assumes stereo (2 channels).
func NewResampler(fromRate, toRate int) *Resampler {
	channels := 2
	inner, err := gomplerate.NewResampler(channels, fromRate, toRate)
	if err != nil {
		log.Printf("resampler: failed to create resampler: %v, will pass through", err)
		return &Resampler{fromRate: fromRate, toRate: fromRate, channels: channels}
	}
	return &Resampler{
		fromRate: fromRate,
		toRate:   toRate,
		channels: channels,
		inner:    inner,
	}
}

// Resample converts a chunk from the source sample rate to the target sample
// rate. If the rates are identical the chunk is returned unmodified.
// Assumes 16-bit signed little-endian PCM.
func (r *Resampler) Resample(chunk AudioChunk) AudioChunk {
	if r.fromRate == r.toRate || r.inner == nil {
		return chunk
	}

	// Convert 16-bit LE PCM bytes to float64 samples.
	samples := pcm16ToFloat64(chunk.Data)

	// Run the resampler.
	resampled := r.inner.ResampleFloat64(samples)

	// Convert float64 samples back to 16-bit LE PCM bytes.
	outBytes := float64ToPCM16(resampled)

	outFormat := chunk.Format
	outFormat.SampleRate = r.toRate

	return AudioChunk{
		Data:      outBytes,
		Format:    outFormat,
		Timestamp: chunk.Timestamp,
	}
}

// ResampleChunks is a convenience helper that resamples a stream of chunks.
func (r *Resampler) ResampleChunks(in <-chan AudioChunk) <-chan AudioChunk {
	out := make(chan AudioChunk, cap(in))
	go func() {
		defer close(out)
		for chunk := range in {
			out <- r.Resample(chunk)
		}
	}()
	return out
}

// Duration returns how long the given chunk's audio data represents.
func Duration(chunk AudioChunk) time.Duration {
	if chunk.Format.FrameSize == 0 || chunk.Format.SampleRate == 0 {
		return 0
	}
	frames := len(chunk.Data) / chunk.Format.FrameSize
	return time.Duration(frames) * time.Second / time.Duration(chunk.Format.SampleRate)
}

// pcm16ToFloat64 decodes 16-bit signed little-endian PCM into normalised
// float64 samples in the range [-1.0, 1.0].
func pcm16ToFloat64(data []byte) []float64 {
	numSamples := len(data) / 2
	out := make([]float64, numSamples)
	for i := 0; i < numSamples; i++ {
		sample := int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
		out[i] = float64(sample) / math.MaxInt16
	}
	return out
}

// float64ToPCM16 encodes normalised float64 samples back into 16-bit signed
// little-endian PCM bytes. Values are clamped to [-1.0, 1.0].
func float64ToPCM16(samples []float64) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		sample := int16(s * math.MaxInt16)
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], uint16(sample))
	}
	return out
}
