package audio

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

var (
	kernel32    = syscall.NewLazyDLL("kernel32.dll")
	createEvent = kernel32.NewProc("CreateEventW")
	closeHandle = kernel32.NewProc("CloseHandle")
)

type initResult struct {
	err error
}

// AudioDevice represents a Windows audio render device.
type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// EnumerateAudioDevices lists all active audio render devices.
func EnumerateAudioDevices() ([]AudioDevice, error) {
	type result struct {
		devices []AudioDevice
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
			ch <- result{err: fmt.Errorf("CoInitializeEx: %w", err)}
			return
		}
		defer ole.CoUninitialize()

		var mmde *wca.IMMDeviceEnumerator
		if err := wca.CoCreateInstance(
			wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL,
			wca.IID_IMMDeviceEnumerator, &mmde,
		); err != nil {
			ch <- result{err: fmt.Errorf("CoCreateInstance: %w", err)}
			return
		}
		defer mmde.Release()

		// Get default device ID for marking IsDefault
		var defaultID string
		var defaultDev *wca.IMMDevice
		if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &defaultDev); err == nil {
			defaultDev.GetId(&defaultID)
			defaultDev.Release()
		}

		// Enumerate all active render endpoints
		var dc *wca.IMMDeviceCollection
		if err := mmde.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &dc); err != nil {
			ch <- result{err: fmt.Errorf("EnumAudioEndpoints: %w", err)}
			return
		}
		defer dc.Release()

		var count uint32
		if err := dc.GetCount(&count); err != nil {
			ch <- result{err: fmt.Errorf("GetCount: %w", err)}
			return
		}

		devices := make([]AudioDevice, 0, count)
		for i := uint32(0); i < count; i++ {
			var dev *wca.IMMDevice
			if err := dc.Item(i, &dev); err != nil {
				continue
			}

			var devID string
			if err := dev.GetId(&devID); err != nil {
				dev.Release()
				continue
			}

			name := devID // fallback
			var ps *wca.IPropertyStore
			if err := dev.OpenPropertyStore(wca.STGM_READ, &ps); err == nil {
				var pv wca.PROPVARIANT
				if err := ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err == nil {
					name = pv.String()
				}
				ps.Release()
			}

			dev.Release()

			devices = append(devices, AudioDevice{
				ID:        devID,
				Name:      name,
				IsDefault: devID == defaultID,
			})
		}

		ch <- result{devices: devices}
	}()
	r := <-ch
	return r.devices, r.err
}

// WASAPILoopbackCapturer captures all system audio via WASAPI loopback.
type WASAPILoopbackCapturer struct {
	mu       sync.Mutex
	format   AudioFormat
	deviceID string // empty = default device

	started bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewWASAPILoopbackCapturer(deviceID string) *WASAPILoopbackCapturer {
	return &WASAPILoopbackCapturer{deviceID: deviceID}
}

func (c *WASAPILoopbackCapturer) Format() AudioFormat {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.format
}

func (c *WASAPILoopbackCapturer) Start(ctx interface{}) (<-chan AudioChunk, error) {
	realCtx, ok := ctx.(context.Context)
	if !ok {
		return nil, fmt.Errorf("capture: ctx must be context.Context, got %T", ctx)
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil, fmt.Errorf("capture: already started")
	}
	c.started = true
	c.mu.Unlock()

	initCh := make(chan initResult, 1)
	out := make(chan AudioChunk, 8)
	derivedCtx, cancel := context.WithCancel(realCtx)
	c.cancel = cancel
	c.done = make(chan struct{})

	go c.captureLoop(derivedCtx, out, initCh)

	res := <-initCh
	if res.err != nil {
		cancel()
		<-c.done
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return nil, fmt.Errorf("capture: init failed: %w", res.err)
	}

	return out, nil
}

func (c *WASAPILoopbackCapturer) captureLoop(ctx context.Context, out chan<- AudioChunk, initCh chan<- initResult) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(c.done)
	defer close(out)

	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		initCh <- initResult{err: fmt.Errorf("CoInitializeEx: %w", err)}
		return
	}
	defer ole.CoUninitialize()

	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator, &mmde,
	); err != nil {
		initCh <- initResult{err: fmt.Errorf("CoCreateInstance: %w", err)}
		return
	}
	defer mmde.Release()

	var mmd *wca.IMMDevice
	if c.deviceID != "" {
		if err := getDeviceByID(mmde, c.deviceID, &mmd); err != nil {
			initCh <- initResult{err: fmt.Errorf("GetDevice(%s): %w", c.deviceID, err)}
			return
		}
	} else {
		if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &mmd); err != nil {
			initCh <- initResult{err: fmt.Errorf("GetDefaultAudioEndpoint: %w", err)}
			return
		}
	}
	defer mmd.Release()

	var ac *wca.IAudioClient
	if err := mmd.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
		initCh <- initResult{err: fmt.Errorf("Activate: %w", err)}
		return
	}
	defer ac.Release()

	var wfx *wca.WAVEFORMATEX
	if err := ac.GetMixFormat(&wfx); err != nil {
		initCh <- initResult{err: fmt.Errorf("GetMixFormat: %w", err)}
		return
	}

	log.Printf("capture: mix format: %dHz %dch %dbit blockAlign=%d formatTag=%d cbSize=%d",
		wfx.NSamplesPerSec, wfx.NChannels, wfx.WBitsPerSample, wfx.NBlockAlign, wfx.WFormatTag, wfx.CbSize)

	// Try to initialize with the mix format. Some drivers (especially multi-channel
	// HDMI/DisplayPort) fail even with their own mix format due to driver bugs.
	// Strategy: try mix format → try stereo float32 → try stereo int16.
	var usedFormat *wca.WAVEFORMATEX
	var bufDuration wca.REFERENCE_TIME = 100_000 // 10ms

	initErr := ac.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK),
		bufDuration, 0, wfx, nil,
	)

	if initErr == nil {
		usedFormat = wfx
		log.Printf("capture: initialized with mix format")
	} else {
		log.Printf("capture: mix format failed, trying stereo float32")

		// Re-activate to get a fresh IAudioClient
		ac.Release()
		if err := mmd.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
			initCh <- initResult{err: fmt.Errorf("Activate retry: %w", err)}
			return
		}

		// Try stereo 32-bit float
		stereoFloat := &wca.WAVEFORMATEX{
			WFormatTag:      3, // WAVE_FORMAT_IEEE_FLOAT
			NChannels:       2,
			NSamplesPerSec:  wfx.NSamplesPerSec,
			WBitsPerSample:  32,
			NBlockAlign:     8, // 2ch * 4bytes
			NAvgBytesPerSec: wfx.NSamplesPerSec * 8,
			CbSize:          0,
		}

		initErr = ac.Initialize(
			wca.AUDCLNT_SHAREMODE_SHARED,
			uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK),
			bufDuration, 0, stereoFloat, nil,
		)

		if initErr == nil {
			usedFormat = stereoFloat
			log.Printf("capture: initialized with stereo float32")
		} else {
			log.Printf("capture: stereo float32 failed, trying stereo int16")

			ac.Release()
			if err := mmd.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
				initCh <- initResult{err: fmt.Errorf("Activate retry2: %w", err)}
				return
			}

			// Try stereo 16-bit PCM
			stereoPCM := &wca.WAVEFORMATEX{
				WFormatTag:      1, // WAVE_FORMAT_PCM
				NChannels:       2,
				NSamplesPerSec:  wfx.NSamplesPerSec,
				WBitsPerSample:  16,
				NBlockAlign:     4, // 2ch * 2bytes
				NAvgBytesPerSec: wfx.NSamplesPerSec * 4,
				CbSize:          0,
			}

			initErr = ac.Initialize(
				wca.AUDCLNT_SHAREMODE_SHARED,
				uint32(wca.AUDCLNT_STREAMFLAGS_LOOPBACK),
				bufDuration, 0, stereoPCM, nil,
			)

			if initErr == nil {
				usedFormat = stereoPCM
				log.Printf("capture: initialized with stereo int16")
			} else {
				initCh <- initResult{err: fmt.Errorf("all format attempts failed (last: %w)", initErr)}
				return
			}
		}
	}

	// Store the format we actually got
	c.mu.Lock()
	c.format = AudioFormat{
		SampleRate:   int(usedFormat.NSamplesPerSec),
		Channels:     int(usedFormat.NChannels),
		BitDepth:     int(usedFormat.WBitsPerSample),
		FrameSize:    int(usedFormat.NBlockAlign),
		FramesPerPkt: 352,
	}
	c.mu.Unlock()

	var acc *wca.IAudioCaptureClient
	if err := ac.GetService(wca.IID_IAudioCaptureClient, &acc); err != nil {
		initCh <- initResult{err: fmt.Errorf("GetService: %w", err)}
		return
	}
	defer acc.Release()

	if err := ac.Start(); err != nil {
		initCh <- initResult{err: fmt.Errorf("Start: %w", err)}
		return
	}
	defer ac.Stop()

	initCh <- initResult{err: nil}
	log.Printf("capture: started (%dHz %dch %dbit)",
		usedFormat.NSamplesPerSec, usedFormat.NChannels, usedFormat.WBitsPerSample)

	blockAlign := int(usedFormat.NBlockAlign)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Millisecond):
		}

		for {
			var data *byte
			var frames uint32
			var flags uint32

			if err := acc.GetBuffer(&data, &frames, &flags, nil, nil); err != nil {
				break
			}

			if frames == 0 {
				acc.ReleaseBuffer(frames)
				break
			}

			size := int(frames) * blockAlign

			const bufferFlagsSilent = 0x2
			var buf []byte
			if flags&bufferFlagsSilent != 0 {
				buf = make([]byte, size)
			} else {
				buf = make([]byte, size)
				src := unsafe.Slice((*byte)(unsafe.Pointer(data)), size)
				copy(buf, src)
			}

			acc.ReleaseBuffer(frames)

			chunk := AudioChunk{
				Data:      buf,
				Format:    c.Format(),
				Timestamp: time.Now(),
			}

			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// getDeviceByID calls IMMDeviceEnumerator::GetDevice via syscall.
// go-wca's GetDevice() is a stub (E_NOTIMPL), so we invoke the vtable directly.
func getDeviceByID(mmde *wca.IMMDeviceEnumerator, deviceID string, mmd **wca.IMMDevice) error {
	utf16ID, err := syscall.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	hr, _, _ := syscall.Syscall(
		mmde.VTable().GetDevice,
		3,
		uintptr(unsafe.Pointer(mmde)),
		uintptr(unsafe.Pointer(utf16ID)),
		uintptr(unsafe.Pointer(mmd)),
	)
	if hr != 0 {
		return ole.NewError(hr)
	}
	return nil
}

func (c *WASAPILoopbackCapturer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return nil
	}

	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}

	c.started = false
	return nil
}
