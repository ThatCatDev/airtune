package audio

import (
	"encoding/binary"
	"math"
)

// ComputeRMSLevels computes per-channel RMS levels from 16-bit PCM data.
// Returns a slice of float64 values (one per channel) in the range [0.0, 1.0].
func ComputeRMSLevels(data []byte, channels int) []float64 {
	if channels <= 0 || len(data) < 2 {
		return make([]float64, max(channels, 1))
	}

	samplesPerChannel := len(data) / (2 * channels)
	if samplesPerChannel == 0 {
		return make([]float64, channels)
	}

	sumSquares := make([]float64, channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			offset := (i*channels + ch) * 2
			if offset+2 > len(data) {
				break
			}
			sample := int16(binary.LittleEndian.Uint16(data[offset : offset+2]))
			normalized := float64(sample) / 32768.0
			sumSquares[ch] += normalized * normalized
		}
	}

	levels := make([]float64, channels)
	for ch := 0; ch < channels; ch++ {
		levels[ch] = math.Sqrt(sumSquares[ch] / float64(samplesPerChannel))
		if levels[ch] > 1.0 {
			levels[ch] = 1.0
		}
	}

	return levels
}

// ComputePeakLevels computes per-channel peak levels from 16-bit PCM data.
func ComputePeakLevels(data []byte, channels int) []float64 {
	if channels <= 0 || len(data) < 2 {
		return make([]float64, max(channels, 1))
	}

	peaks := make([]float64, channels)
	samplesPerChannel := len(data) / (2 * channels)

	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			offset := (i*channels + ch) * 2
			if offset+2 > len(data) {
				break
			}
			sample := int16(binary.LittleEndian.Uint16(data[offset : offset+2]))
			abs := math.Abs(float64(sample) / 32768.0)
			if abs > peaks[ch] {
				peaks[ch] = abs
			}
		}
	}

	return peaks
}
