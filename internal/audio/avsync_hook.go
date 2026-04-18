package audio

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	syncShmName    = "Local\\AirTuneSyncLatency"
	syncShmMagic   = 0x41545359 // 'ATSY'
	syncShmVersion = 1
	syncShmSize    = 64
	syncDLLName    = "airtune_sync.dll"

	whGetMessage = 3 // WH_GETMESSAGE
)

// syncShmLayout mirrors AirTuneSyncData in airtune_sync.h.
type syncShmLayout struct {
	Magic      uint32
	Version    uint32
	LatencyHns int64
	SampleRate uint32
	Enabled    uint32
	Reserved   [40]byte
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")

	mapViewOfFile   = kernel32.NewProc("MapViewOfFile")
	unmapViewOfFile = kernel32.NewProc("UnmapViewOfFile")

	procCreateFileMappingW  = kernel32.NewProc("CreateFileMappingW")
	procGetProcAddress      = kernel32.NewProc("GetProcAddress")
	procOpenProcess         = kernel32.NewProc("OpenProcess")
	procVirtualAllocEx      = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx       = kernel32.NewProc("VirtualFreeEx")
	procWriteProcessMemory  = kernel32.NewProc("WriteProcessMemory")
	procCreateRemoteThread  = kernel32.NewProc("CreateRemoteThread")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW     = kernel32.NewProc("Process32FirstW")
	procProcess32NextW      = kernel32.NewProc("Process32NextW")

	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	regCreateKeyEx = advapi32.NewProc("RegCreateKeyExW")
	regSetValueEx  = advapi32.NewProc("RegSetValueExW")
	regOpenKeyEx   = advapi32.NewProc("RegOpenKeyExW")
	regDeleteValue = advapi32.NewProc("RegDeleteValueW")
)

const (
	hkcuHandle = uintptr(0x80000001) // HKEY_CURRENT_USER
)

const (
	fileMapRead           = 0x0004
	fileMapWrite          = 0x0002
	pageReadWrite         = 0x04
	invalidHandle         = ^uintptr(0)
	processAll            = 0x001FFFFF // PROCESS_ALL_ACCESS
	memCommit             = 0x1000
	memReserve            = 0x2000
	memRelease            = 0x8000
	th32csSnapProcess     = 0x00000002
)

type processEntry32W struct {
	Size              uint32
	CntUsage          uint32
	ProcessID         uint32
	DefaultHeapID     uintptr
	ModuleID          uint32
	CntThreads        uint32
	ParentProcessID   uint32
	PriClassBase      int32
	Flags             uint32
	ExeFile           [260]uint16
}

// AVSyncHook manages the system-wide IAudioClock::GetPosition() hook.
type AVSyncHook struct {
	mu       sync.Mutex
	active   bool
	hMapping syscall.Handle
	baseAddr unsafe.Pointer
	hDLL     syscall.Handle
	hHook    uintptr
	dllPath  string
	stopCh   chan struct{}

	// Track processes we've injected into (to avoid double-injection)
	injected map[uint32]bool
}

// NewAVSyncHook creates a new hook manager.
func NewAVSyncHook() *AVSyncHook {
	return &AVSyncHook{
		injected: make(map[uint32]bool),
	}
}

// setChromeAudioSandbox sets or removes the Chrome policy to disable audio sandbox.
// When disabled=true, Chrome's audio process runs unsandboxed so we can inject.
func setChromeAudioSandbox(disable bool) {
	key := `SOFTWARE\Policies\Google\Chrome`

	var k syscall.Handle
	keyPtr, _ := syscall.UTF16PtrFromString(key)

	if disable {
		// Create key and set AudioSandboxEnabled = 0
		var disp uint32
		ret, _, _ := regCreateKeyEx.Call(hkcuHandle, uintptr(unsafe.Pointer(keyPtr)),
			0, 0, 0, 0x20006, /* KEY_WRITE */ 0, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&disp)))
		if ret != 0 {
			log.Printf("avsync: RegCreateKeyEx: error %d", ret)
			return
		}
		defer syscall.RegCloseKey(k)

		valName, _ := syscall.UTF16PtrFromString("AudioSandboxEnabled")
		val := uint32(0)
		regSetValueEx.Call(uintptr(k), uintptr(unsafe.Pointer(valName)),
			0, 4 /* REG_DWORD */, uintptr(unsafe.Pointer(&val)), 4)
		log.Println("avsync: Chrome audio sandbox disabled via policy")
	} else {
		// Delete the value (re-enable sandbox)
		ret, _, _ := regOpenKeyEx.Call(hkcuHandle, uintptr(unsafe.Pointer(keyPtr)),
			0, 0x20006, uintptr(unsafe.Pointer(&k)))
		if ret != 0 {
			return // Key doesn't exist, nothing to do
		}
		defer syscall.RegCloseKey(k)

		valName, _ := syscall.UTF16PtrFromString("AudioSandboxEnabled")
		regDeleteValue.Call(uintptr(k), uintptr(unsafe.Pointer(valName)))
		log.Println("avsync: Chrome audio sandbox policy removed")
	}
}

// Enable activates the system-wide A/V sync hook with the given latency.
func (h *AVSyncHook) Enable(latencyHNS int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		h.writeLatency(latencyHNS, true)
		return nil
	}

	// 0. Disable Chrome audio sandbox so we can inject into its audio process
	setChromeAudioSandbox(true)

	// 1. Create shared memory
	namePtr, err := syscall.UTF16PtrFromString(syncShmName)
	if err != nil {
		return fmt.Errorf("avsync: UTF16: %w", err)
	}

	r, _, e := procCreateFileMappingW.Call(
		invalidHandle, 0, pageReadWrite, 0, syncShmSize,
		uintptr(unsafe.Pointer(namePtr)),
	)
	if r == 0 {
		return fmt.Errorf("avsync: CreateFileMapping: %v", e)
	}
	h.hMapping = syscall.Handle(r)

	addr, _, e2 := mapViewOfFile.Call(
		uintptr(h.hMapping), fileMapWrite|fileMapRead, 0, 0, syncShmSize,
	)
	if addr == 0 {
		syscall.CloseHandle(h.hMapping)
		h.hMapping = 0
		return fmt.Errorf("avsync: MapViewOfFile: %v", e2)
	}
	h.baseAddr = unsafe.Add(nil, int(addr))

	layout := (*syncShmLayout)(h.baseAddr)
	layout.Magic = syncShmMagic
	layout.Version = syncShmVersion
	layout.SampleRate = 44100
	atomic.StoreInt64(&layout.LatencyHns, latencyHNS)
	atomic.StoreUint32(&layout.Enabled, 1)

	// 2. Find DLL path
	dllPath, err := findDLL()
	if err != nil {
		h.cleanupShm()
		return fmt.Errorf("avsync: find DLL: %w", err)
	}
	h.dllPath = dllPath

	// 3. Load DLL in our process
	hDLL, err := syscall.LoadLibrary(dllPath)
	if err != nil {
		h.cleanupShm()
		return fmt.Errorf("avsync: LoadLibrary(%s): %w", dllPath, err)
	}
	h.hDLL = hDLL

	// 4. Install system-wide WH_GETMESSAGE hook (covers GUI processes)
	procName, _ := syscall.BytePtrFromString("AirTuneCBTProc")
	hookProc, _, _ := procGetProcAddress.Call(uintptr(hDLL), uintptr(unsafe.Pointer(procName)))
	if hookProc != 0 {
		hHook, _, _ := procSetWindowsHookExW.Call(whGetMessage, hookProc, uintptr(hDLL), 0)
		h.hHook = hHook
	}

	// 5. Start background goroutine to inject into sandboxed processes
	h.stopCh = make(chan struct{})
	h.active = true

	go h.injectionLoop()

	return nil
}

// injectionLoop periodically scans for audio-related processes that the
// SetWindowsHookEx approach can't reach (e.g., Chrome's sandboxed audio process)
// and injects the DLL via CreateRemoteThread + LoadLibraryW.
func (h *AVSyncHook) injectionLoop() {
	// Inject immediately, then periodically
	h.scanAndInject()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.scanAndInject()
		}
	}
}

func (h *AVSyncHook) scanAndInject() {
	pids := findTargetProcesses()
	h.mu.Lock()
	dllPath := h.dllPath
	h.mu.Unlock()

	for _, pid := range pids {
		h.mu.Lock()
		already := h.injected[pid]
		h.mu.Unlock()
		if already {
			continue
		}

		if err := injectDLL(pid, dllPath); err != nil {
			log.Printf("avsync: inject into PID %d: %v", pid, err)
		} else {
			log.Printf("avsync: injected DLL into PID %d", pid)
			h.mu.Lock()
			h.injected[pid] = true
			h.mu.Unlock()
		}
	}
}

// findTargetProcesses returns PIDs of processes that might handle audio
// but aren't reachable via SetWindowsHookEx (sandboxed/no message loop).
func findTargetProcesses() []uint32 {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snap == invalidHandle {
		return nil
	}
	defer syscall.CloseHandle(syscall.Handle(snap))

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	var pids []uint32
	myPID := uint32(os.Getpid())

	ok, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		name := syscall.UTF16ToString(entry.ExeFile[:])
		pid := entry.ProcessID

		// Target Chrome/Edge/Brave audio utility processes and other browsers
		if pid != myPID && pid != 0 && isAudioProcess(name) {
			pids = append(pids, pid)
		}

		entry.Size = uint32(unsafe.Sizeof(entry))
		ok, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}

	return pids
}

// isAudioProcess returns true for process names that commonly host audio.
// We inject into ALL chrome.exe/msedge.exe etc. because we can't tell which
// one is the audio utility process just from the name.
func isAudioProcess(name string) bool {
	switch name {
	case "chrome.exe", "msedge.exe", "brave.exe", "opera.exe", "vivaldi.exe",
		"firefox.exe", "vlc.exe", "mpc-hc64.exe", "mpc-hc.exe",
		"wmplayer.exe", "Video.UI.exe", "mpv.exe":
		return true
	}
	return false
}

// injectDLL injects a DLL into a remote process via CreateRemoteThread + LoadLibraryW.
func injectDLL(pid uint32, dllPath string) error {
	// Open process
	hProc, _, err := procOpenProcess.Call(processAll, 0, uintptr(pid))
	if hProc == 0 {
		return fmt.Errorf("OpenProcess: %v", err)
	}
	defer syscall.CloseHandle(syscall.Handle(hProc))

	// Convert DLL path to UTF-16 bytes
	dllPathUTF16, _ := syscall.UTF16FromString(dllPath)
	pathBytes := len(dllPathUTF16) * 2 // UTF-16 = 2 bytes per char

	// Allocate memory in remote process
	remoteMem, _, err := procVirtualAllocEx.Call(
		hProc, 0, uintptr(pathBytes),
		memCommit|memReserve, pageReadWrite,
	)
	if remoteMem == 0 {
		return fmt.Errorf("VirtualAllocEx: %v", err)
	}

	// Write DLL path to remote memory
	var written uintptr
	ok, _, err := procWriteProcessMemory.Call(
		hProc, remoteMem,
		uintptr(unsafe.Pointer(&dllPathUTF16[0])),
		uintptr(pathBytes), uintptr(unsafe.Pointer(&written)),
	)
	if ok == 0 {
		procVirtualFreeEx.Call(hProc, remoteMem, 0, memRelease)
		return fmt.Errorf("WriteProcessMemory: %v", err)
	}

	// Get LoadLibraryW address (same in all processes due to ASLR base sharing)
	kernel32Name, _ := syscall.UTF16PtrFromString("kernel32.dll")
	hKernel32, _, _ := procGetModuleHandleW.Call(uintptr(unsafe.Pointer(kernel32Name)))
	loadLibName, _ := syscall.BytePtrFromString("LoadLibraryW")
	loadLibAddr, _, _ := procGetProcAddress.Call(hKernel32, uintptr(unsafe.Pointer(loadLibName)))
	if loadLibAddr == 0 {
		procVirtualFreeEx.Call(hProc, remoteMem, 0, memRelease)
		return fmt.Errorf("GetProcAddress(LoadLibraryW) failed")
	}

	// Create remote thread that calls LoadLibraryW(dllPath)
	hThread, _, err := procCreateRemoteThread.Call(
		hProc, 0, 0, loadLibAddr, remoteMem, 0, 0,
	)
	if hThread == 0 {
		procVirtualFreeEx.Call(hProc, remoteMem, 0, memRelease)
		return fmt.Errorf("CreateRemoteThread: %v", err)
	}

	// Wait for the thread to complete (LoadLibrary to finish)
	syscall.WaitForSingleObject(syscall.Handle(hThread), 5000)
	syscall.CloseHandle(syscall.Handle(hThread))

	// Free remote memory (the DLL is loaded now, path string no longer needed)
	procVirtualFreeEx.Call(hProc, remoteMem, 0, memRelease)

	return nil
}

// SetLatency updates the latency value in shared memory.
func (h *AVSyncHook) SetLatency(latencyHNS int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active && h.baseAddr != nil {
		h.writeLatency(latencyHNS, true)
	}
}

// Disable deactivates the hook and cleans up.
func (h *AVSyncHook) Disable() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return
	}

	h.writeLatency(0, false)

	// Re-enable Chrome audio sandbox
	setChromeAudioSandbox(false)

	if h.stopCh != nil {
		close(h.stopCh)
		h.stopCh = nil
	}

	if h.hHook != 0 {
		procUnhookWindowsHookEx.Call(h.hHook)
		h.hHook = 0
	}

	h.cleanupDLL()
	h.cleanupShm()
	h.injected = make(map[uint32]bool)
	h.active = false
}

// IsActive reports whether the hook is currently active.
func (h *AVSyncHook) IsActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

func (h *AVSyncHook) writeLatency(hns int64, enabled bool) {
	if h.baseAddr == nil {
		return
	}
	layout := (*syncShmLayout)(h.baseAddr)
	atomic.StoreInt64(&layout.LatencyHns, hns)
	if enabled {
		atomic.StoreUint32(&layout.Enabled, 1)
	} else {
		atomic.StoreUint32(&layout.Enabled, 0)
	}
}

func (h *AVSyncHook) cleanupShm() {
	if h.baseAddr != nil {
		unmapViewOfFile.Call(uintptr(h.baseAddr))
		h.baseAddr = nil
	}
	if h.hMapping != 0 {
		syscall.CloseHandle(h.hMapping)
		h.hMapping = 0
	}
}

func (h *AVSyncHook) cleanupDLL() {
	if h.hDLL != 0 {
		syscall.FreeLibrary(h.hDLL)
		h.hDLL = 0
	}
}

// findDLL locates airtune_sync.dll next to the executable.
func findDLL() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dllPath := filepath.Join(filepath.Dir(exe), syncDLLName)
	if _, err := os.Stat(dllPath); err == nil {
		return dllPath, nil
	}

	if wd, err := os.Getwd(); err == nil {
		dllPath = filepath.Join(wd, syncDLLName)
		if _, err := os.Stat(dllPath); err == nil {
			return dllPath, nil
		}
	}

	if wd, err := os.Getwd(); err == nil {
		dllPath = filepath.Join(wd, "hook", syncDLLName)
		if _, err := os.Stat(dllPath); err == nil {
			return dllPath, nil
		}
	}

	return "", fmt.Errorf("%s not found", syncDLLName)
}
