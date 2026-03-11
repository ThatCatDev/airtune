package audio

import (
	"context"
	"log"
	"math"
	"sync"
	"time"

	"airtune/internal/codec"
)

// Pipeline captures system audio, resamples to 44.1kHz, encodes, and
// distributes encoded packets to subscribers (one per connected AirPlay device).
type Pipeline struct {
	capturer  Capturer
	resampler *Resampler
	encoder   codec.Encoder

	mu          sync.Mutex
	subscribers map[string]*subscriber
	running     bool
	cancel      context.CancelFunc
	done        chan struct{}
}

type subscriber struct {
	ch   chan EncodedPacket
	mode ChannelMode
}

// NewPipeline creates a new audio pipeline.
// capturer provides the audio source (WASAPI loopback).
func NewPipeline(encoder codec.Encoder, capturer Capturer) *Pipeline {
	return &Pipeline{
		capturer:    capturer,
		encoder:     encoder,
		subscribers: make(map[string]*subscriber),
	}
}

// Start begins the pipeline: capture → resample → encode → fan-out.
func (p *Pipeline) Start(ctx context.Context) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = true
	p.mu.Unlock()

	pipeCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})

	// Start WASAPI capture
	rawCh, err := p.capturer.Start(pipeCtx)
	if err != nil {
		cancel()
		p.mu.Lock()
		p.running = false
		p.mu.Unlock()
		return err
	}

	// Determine if resampling is needed
	captureFormat := p.capturer.Format()
	log.Printf("pipeline: capture format: %dHz %dch %dbit",
		captureFormat.SampleRate, captureFormat.Channels, captureFormat.BitDepth)

	if captureFormat.SampleRate != AirPlayFormat.SampleRate {
		p.resampler = NewResampler(captureFormat.SampleRate, AirPlayFormat.SampleRate)
		log.Printf("pipeline: resampling %d → %d Hz",
			captureFormat.SampleRate, AirPlayFormat.SampleRate)
	}

	go p.processLoop(pipeCtx, rawCh)

	return nil
}

// Stop stops the pipeline and releases resources.
func (p *Pipeline) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()

	if p.cancel != nil {
		p.cancel()
	}
	if p.done != nil {
		<-p.done
	}
	p.capturer.Close()

	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

// Subscribe creates a new subscriber channel for the given device ID.
// mode controls which stereo channel(s) the device receives.
// Returns a channel that receives encoded audio packets.
func (p *Pipeline) Subscribe(deviceID string, mode ChannelMode) <-chan EncodedPacket {
	ch := make(chan EncodedPacket, 8)
	p.mu.Lock()
	p.subscribers[deviceID] = &subscriber{ch: ch, mode: mode}
	p.mu.Unlock()
	log.Printf("pipeline: subscribed device %s (channel=%s)", deviceID, mode)
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (p *Pipeline) Unsubscribe(deviceID string) {
	p.mu.Lock()
	sub, ok := p.subscribers[deviceID]
	if ok {
		delete(p.subscribers, deviceID)
		close(sub.ch)
	}
	p.mu.Unlock()
	log.Printf("pipeline: unsubscribed device %s", deviceID)
}

// processLoop reads captured audio, resamples, encodes, and fans out.
func (p *Pipeline) processLoop(ctx context.Context, rawCh <-chan AudioChunk) {
	defer close(p.done)

	// Accumulation buffer for packetizing into 352-frame packets
	packetBytes := AirPlayFormat.FramesPerPkt * AirPlayFormat.FrameSize // 352 * 4 = 1408
	var accumBuf []byte

	var diagChunks int
	var diagNonSilent int
	diagTicker := time.NewTicker(3 * time.Second)
	defer diagTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-diagTicker.C:
			log.Printf("pipeline: diag: %d chunks received, %d non-silent", diagChunks, diagNonSilent)
			diagChunks = 0
			diagNonSilent = 0
			continue
		case chunk, ok := <-rawCh:
			if !ok {
				return
			}

			// Diagnostic: check if chunk has non-zero data
			diagChunks++
			for i := 0; i < len(chunk.Data); i++ {
				if chunk.Data[i] != 0 {
					diagNonSilent++
					break
				}
			}

			// Convert to 16-bit PCM first — WASAPI captures in 32-bit float
			// and the resampler expects 16-bit PCM.
			pcm16Chunk := AudioChunk{
				Data:      convertTo16BitPCM(chunk),
				Format:    chunk.Format,
				Timestamp: chunk.Timestamp,
			}
			pcm16Chunk.Format.BitDepth = 16
			pcm16Chunk.Format.FrameSize = pcm16Chunk.Format.Channels * 2

			// Resample if needed (operates on 16-bit PCM)
			if p.resampler != nil {
				pcm16Chunk = p.resampler.Resample(pcm16Chunk)
			}

			pcmData := pcm16Chunk.Data

			// Accumulate
			accumBuf = append(accumBuf, pcmData...)

			// Packetize into 352-frame chunks
			for len(accumBuf) >= packetBytes {
				pktData := make([]byte, packetBytes)
				copy(pktData, accumBuf[:packetBytes])
				accumBuf = accumBuf[packetBytes:]

				// Check which channel modes are needed
				p.mu.Lock()
				needBoth, needLeft, needRight := false, false, false
				for _, sub := range p.subscribers {
					switch sub.mode {
					case ChannelLeft:
						needLeft = true
					case ChannelRight:
						needRight = true
					default:
						needBoth = true
					}
				}
				p.mu.Unlock()

				// Encode each needed version (only compute what we need)
				var encodedBoth, encodedLeft, encodedRight []byte
				now := time.Now()

				if needBoth {
					enc, err := p.encoder.Encode(pktData)
					if err != nil {
						log.Printf("pipeline: encode error: %v", err)
						continue
					}
					encodedBoth = enc
				}
				if needLeft {
					leftPCM := extractChannel(pktData, ChannelLeft)
					enc, err := p.encoder.Encode(leftPCM)
					if err != nil {
						log.Printf("pipeline: encode L error: %v", err)
						continue
					}
					encodedLeft = enc
				}
				if needRight {
					rightPCM := extractChannel(pktData, ChannelRight)
					enc, err := p.encoder.Encode(rightPCM)
					if err != nil {
						log.Printf("pipeline: encode R error: %v", err)
						continue
					}
					encodedRight = enc
				}

				// Fan out to all subscribers
				p.mu.Lock()
				for id, sub := range p.subscribers {
					var data []byte
					switch sub.mode {
					case ChannelLeft:
						data = encodedLeft
					case ChannelRight:
						data = encodedRight
					default:
						data = encodedBoth
					}

					pkt := EncodedPacket{
						Data:      data,
						Frames:    AirPlayFormat.FramesPerPkt,
						Timestamp: now,
					}
					select {
					case sub.ch <- pkt:
					default:
						log.Printf("pipeline: subscriber %s buffer full, dropping packet", id)
					}
				}
				p.mu.Unlock()
			}
		}
	}
}

// extractChannel extracts one channel from interleaved 16-bit stereo PCM
// and duplicates it to both L and R (so the device plays mono from both speakers).
func extractChannel(stereo []byte, mode ChannelMode) []byte {
	if mode == ChannelBoth {
		return stereo
	}
	numFrames := len(stereo) / 4 // 4 bytes per frame (2ch × 2 bytes)
	out := make([]byte, len(stereo))

	srcOffset := 0 // left channel
	if mode == ChannelRight {
		srcOffset = 2 // right channel starts at byte 2 of each frame
	}

	for i := 0; i < numFrames; i++ {
		s0 := stereo[i*4+srcOffset]
		s1 := stereo[i*4+srcOffset+1]
		// Write to both L and R in output
		out[i*4] = s0
		out[i*4+1] = s1
		out[i*4+2] = s0
		out[i*4+3] = s1
	}
	return out
}

// convertTo16BitPCM converts audio data to 16-bit signed LE PCM.
// WASAPI typically captures in 32-bit float; we need 16-bit for AirPlay.
func convertTo16BitPCM(chunk AudioChunk) []byte {
	if chunk.Format.BitDepth == 16 {
		return chunk.Data
	}

	if chunk.Format.BitDepth == 32 {
		// Assume 32-bit IEEE float (WASAPI default)
		// Each sample is 4 bytes (float32), convert to 2 bytes (int16)
		numSamples := len(chunk.Data) / 4
		out := make([]byte, numSamples*2)
		for i := 0; i < numSamples; i++ {
			// Read float32 LE
			bits := uint32(chunk.Data[i*4]) |
				uint32(chunk.Data[i*4+1])<<8 |
				uint32(chunk.Data[i*4+2])<<16 |
				uint32(chunk.Data[i*4+3])<<24
			f := float32FromBits(bits)

			// Clamp and convert to int16
			if f > 1.0 {
				f = 1.0
			} else if f < -1.0 {
				f = -1.0
			}
			s := int16(f * 32767)

			// Write int16 LE
			out[i*2] = byte(s)
			out[i*2+1] = byte(s >> 8)
		}
		return out
	}

	// Unsupported bit depth, return as-is
	return chunk.Data
}

// float32FromBits converts a uint32 bit pattern to float32.
func float32FromBits(bits uint32) float32 {
	return math.Float32frombits(bits)
}
