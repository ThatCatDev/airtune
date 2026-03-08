package audio

import (
	"context"
	"log"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// VolumeChange represents a system volume change event.
type VolumeChange struct {
	Level float32 // 0.0–1.0 scalar
	Muted bool
}

// ── COM callback implementation for IAudioEndpointVolumeCallback ──

// volumeCallbackVtbl is the COM vtable layout.
type volumeCallbackVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	OnNotify       uintptr
}

// volumeCallback implements IAudioEndpointVolumeCallback.
// First field MUST be the vtable pointer (COM ABI).
type volumeCallback struct {
	vtbl     *volumeCallbackVtbl
	refCount int32
	closed   int32 // atomic; set to 1 before channel close
	ch       chan<- VolumeChange
}

// AUDIO_VOLUME_NOTIFICATION_DATA from the Windows SDK.
type audioVolumeNotificationData struct {
	GuidEventContext [16]byte // GUID
	BMuted           int32    // BOOL
	FMasterVolume    float32
	NChannels        uint32
	// afChannelVolumes[1] follows (variable-length, ignored)
}

// Shared vtable — callbacks created via syscall.NewCallback are process-lifetime.
var cbVtbl = &volumeCallbackVtbl{
	QueryInterface: syscall.NewCallback(cbQueryInterface),
	AddRef:         syscall.NewCallback(cbAddRef),
	Release:        syscall.NewCallback(cbRelease),
	OnNotify:       syscall.NewCallback(cbOnNotify),
}

func cbQueryInterface(this, riid, ppvObject uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(ppvObject)) = this
	cb := (*volumeCallback)(unsafe.Pointer(this))
	atomic.AddInt32(&cb.refCount, 1)
	return 0 // S_OK
}

func cbAddRef(this uintptr) uintptr {
	cb := (*volumeCallback)(unsafe.Pointer(this))
	return uintptr(atomic.AddInt32(&cb.refCount, 1))
}

func cbRelease(this uintptr) uintptr {
	cb := (*volumeCallback)(unsafe.Pointer(this))
	return uintptr(atomic.AddInt32(&cb.refCount, -1))
}

func cbOnNotify(this, pNotify uintptr) uintptr {
	cb := (*volumeCallback)(unsafe.Pointer(this))
	if atomic.LoadInt32(&cb.closed) != 0 {
		return 0
	}
	data := (*audioVolumeNotificationData)(unsafe.Pointer(pNotify))
	select {
	case cb.ch <- VolumeChange{Level: data.FMasterVolume, Muted: data.BMuted != 0}:
	default:
	}
	return 0 // S_OK
}

// ── Public API ──

// MonitorSystemVolume registers a Windows IAudioEndpointVolumeCallback to
// receive volume change events without polling. The interval parameter is
// ignored (kept for API compatibility). Sends an initial reading immediately.
// Runs until ctx is cancelled.
func MonitorSystemVolume(ctx context.Context, _ time.Duration) <-chan VolumeChange {
	ch := make(chan VolumeChange, 8)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(ch)

		if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
			log.Printf("sysvolume: CoInitializeEx: %v", err)
			return
		}
		defer ole.CoUninitialize()

		var mmde *wca.IMMDeviceEnumerator
		if err := wca.CoCreateInstance(
			wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL,
			wca.IID_IMMDeviceEnumerator, &mmde,
		); err != nil {
			log.Printf("sysvolume: CoCreateInstance: %v", err)
			return
		}
		defer mmde.Release()

		var mmd *wca.IMMDevice
		if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &mmd); err != nil {
			log.Printf("sysvolume: GetDefaultAudioEndpoint: %v", err)
			return
		}
		defer mmd.Release()

		var aev *wca.IAudioEndpointVolume
		if err := mmd.Activate(wca.IID_IAudioEndpointVolume, wca.CLSCTX_ALL, nil, &aev); err != nil {
			log.Printf("sysvolume: Activate IAudioEndpointVolume: %v", err)
			return
		}
		defer aev.Release()

		// Create COM callback object.
		cb := &volumeCallback{
			vtbl:     cbVtbl,
			refCount: 1,
			ch:       ch,
		}

		// Call RegisterControlChangeNotify via vtable directly
		// (go-wca's wrapper is a stub that returns E_NOTIMPL).
		hr, _, _ := syscall.SyscallN(
			aev.VTable().RegisterControlChangeNotify,
			uintptr(unsafe.Pointer(aev)),
			uintptr(unsafe.Pointer(cb)),
		)
		if hr != 0 {
			log.Printf("sysvolume: RegisterControlChangeNotify failed: 0x%x", hr)
			return
		}

		// Send the current volume as the initial event.
		var level float32
		var muted bool
		if err := aev.GetMasterVolumeLevelScalar(&level); err == nil {
			_ = aev.GetMute(&muted)
			ch <- VolumeChange{Level: level, Muted: muted}
		}

		log.Println("sysvolume: listening for volume changes (event-based)")

		// Block until the context is cancelled.
		// Windows delivers OnNotify callbacks on a COM thread automatically.
		<-ctx.Done()

		// Stop receiving callbacks before closing the channel.
		atomic.StoreInt32(&cb.closed, 1)
		syscall.SyscallN(
			aev.VTable().UnregisterControlChangeNotify,
			uintptr(unsafe.Pointer(aev)),
			uintptr(unsafe.Pointer(cb)),
		)

		runtime.KeepAlive(cb)
	}()

	return ch
}
