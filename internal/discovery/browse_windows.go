//go:build windows

package discovery

import (
	"context"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/grandcat/zeroconf"
)

// Windows native DNS-SD browser using DnsServiceBrowse/DnsServiceResolve
// from dnsapi.dll. This works through the OS DNS client service, avoiding
// port 5353 conflicts with Chrome and other apps.

var (
	modDnsapi                  = syscall.NewLazyDLL("dnsapi.dll")
	procDnsServiceBrowse       = modDnsapi.NewProc("DnsServiceBrowse")
	procDnsServiceBrowseCancel = modDnsapi.NewProc("DnsServiceBrowseCancel")
	procDnsServiceResolve      = modDnsapi.NewProc("DnsServiceResolve")
	procDnsServiceResolveCancel = modDnsapi.NewProc("DnsServiceResolveCancel")
	procDnsFree                = modDnsapi.NewProc("DnsFree")
	procDnsFreeServiceInstance = modDnsapi.NewProc("DnsFreeServiceInstance")
)

const (
	dnsRequestPending       = 9506
	dnsQueryRequestVersion1 = 1
	dnsFreeRecordList       = 1
	dnsTypePTR       uint16 = 0x000C
)

// Struct layouts matching Windows API (amd64)

type dnsServiceBrowseRequest struct {
	Version        uint32
	InterfaceIndex uint32
	QueryName      *uint16
	Callback       uintptr
	QueryContext   uintptr
}

type dnsServiceResolveRequest struct {
	Version        uint32
	InterfaceIndex uint32
	QueryName      *uint16
	Callback       uintptr
	QueryContext   uintptr
}

type dnsServiceCancel struct {
	Reserved uintptr
}

// Callback registry — callbacks are singletons, dispatched via QueryContext ID.

var (
	cbMu     sync.Mutex
	cbNextID uintptr

	browseCallbackOnce sync.Once
	browseCallbackPtr  uintptr
	browseHandlers     = make(map[uintptr]*browseState)

	resolveCallbackOnce sync.Once
	resolveCallbackPtr  uintptr
	resolveHandlers     = make(map[uintptr]chan uintptr)
)

type browseState struct {
	names chan string
	done  int32 // atomic: 1 = closed
}

func allocCallbackID() uintptr {
	cbMu.Lock()
	cbNextID++
	id := cbNextID
	cbMu.Unlock()
	return id
}

// --- Browse callback (called from Windows thread pool) ---

func getBrowseCallback() uintptr {
	browseCallbackOnce.Do(func() {
		browseCallbackPtr = syscall.NewCallback(onBrowseCallback)
	})
	return browseCallbackPtr
}

func onBrowseCallback(status uintptr, queryCtx uintptr, pRecord uintptr) uintptr {
	cbMu.Lock()
	state, ok := browseHandlers[queryCtx]
	cbMu.Unlock()

	if !ok {
		if pRecord != 0 {
			procDnsFree.Call(pRecord, dnsFreeRecordList)
		}
		return 0
	}

	if status != 0 || pRecord == 0 {
		// Browse ended or cancelled
		if atomic.CompareAndSwapInt32(&state.done, 0, 1) {
			close(state.names)
		}
		return 0
	}

	// Walk DNS_RECORD linked list for PTR records
	// DNS_RECORD layout (amd64): pNext @0, pName @8, wType @16, Data @32
	// PTR Data: pNameHost @0 (a PWSTR)
	for rec := pRecord; rec != 0; {
		wType := *(*uint16)(unsafe.Pointer(rec + 16))
		if wType == dnsTypePTR {
			pName := *(*uintptr)(unsafe.Pointer(rec + 32))
			if pName != 0 {
				name := utf16PtrToGoString(pName)
				select {
				case state.names <- name:
				default:
				}
			}
		}
		rec = *(*uintptr)(unsafe.Pointer(rec)) // pNext
	}
	procDnsFree.Call(pRecord, dnsFreeRecordList)
	return 0
}

// --- Resolve callback ---

func getResolveCallback() uintptr {
	resolveCallbackOnce.Do(func() {
		resolveCallbackPtr = syscall.NewCallback(onResolveCallback)
	})
	return resolveCallbackPtr
}

func onResolveCallback(status uintptr, queryCtx uintptr, pInstance uintptr) uintptr {
	cbMu.Lock()
	ch, ok := resolveHandlers[queryCtx]
	if ok {
		delete(resolveHandlers, queryCtx)
	}
	cbMu.Unlock()

	if ok {
		if status == 0 && pInstance != 0 {
			ch <- pInstance
		}
		close(ch)
	} else if pInstance != 0 {
		// Nobody listening — free the instance ourselves
		safeFreeServiceInstance(pInstance)
	}
	return 0
}

// --- browseOnce (platform entry point) ---

func nativeAPIAvailable() bool {
	return procDnsServiceBrowse.Find() == nil &&
		procDnsServiceResolve.Find() == nil &&
		procDnsFree.Find() == nil
}

// safeFreeServiceInstance frees a DNS_SERVICE_INSTANCE if the API is available,
// otherwise does nothing (minor leak, acceptable for discovery).
func safeFreeServiceInstance(ptr uintptr) {
	if procDnsFreeServiceInstance.Find() == nil {
		safeFreeServiceInstance(ptr)
	}
}

func (b *Browser) browseOnce(ctx context.Context, duration time.Duration) {
	if !nativeAPIAvailable() {
		log.Println("discovery: native DNS-SD API unavailable, using zeroconf fallback")
		b.browseOnceZeroconf(ctx, duration)
		return
	}

	scanCtx, scanCancel := context.WithTimeout(ctx, duration)
	defer scanCancel()

	raopEntries := make(chan *zeroconf.ServiceEntry, 16)
	go b.processRAOPEntries(raopEntries)

	airplayEntries := make(chan *zeroconf.ServiceEntry, 16)
	go b.processAirPlayEntries(airplayEntries)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		nativeBrowseService(scanCtx, raopService+".local", raopService, raopEntries)
		close(raopEntries)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		nativeBrowseService(scanCtx, airplayService+".local", airplayService, airplayEntries)
		close(airplayEntries)
	}()

	wg.Wait()
}

// nativeBrowseService calls DnsServiceBrowse and resolves each discovered instance.
func nativeBrowseService(ctx context.Context, queryName string, service string, entries chan<- *zeroconf.ServiceEntry) {
	state := &browseState{
		names: make(chan string, 32),
	}

	id := allocCallbackID()
	cbMu.Lock()
	browseHandlers[id] = state
	cbMu.Unlock()

	defer func() {
		cbMu.Lock()
		delete(browseHandlers, id)
		cbMu.Unlock()
	}()

	qn, _ := syscall.UTF16PtrFromString(queryName)
	req := dnsServiceBrowseRequest{
		Version:      dnsQueryRequestVersion1,
		QueryName:    qn,
		Callback:     getBrowseCallback(),
		QueryContext: id,
	}

	var cancel dnsServiceCancel

	ret, _, _ := procDnsServiceBrowse.Call(
		uintptr(unsafe.Pointer(&req)),
		uintptr(unsafe.Pointer(&cancel)),
	)

	if ret != dnsRequestPending {
		log.Printf("discovery: DnsServiceBrowse(%s) failed: %d", queryName, ret)
		return
	}

	// Cancel browse when context is done
	go func() {
		<-ctx.Done()
		procDnsServiceBrowseCancel.Call(uintptr(unsafe.Pointer(&cancel)))
	}()

	seen := make(map[string]bool)
	for {
		select {
		case name, ok := <-state.names:
			if !ok {
				return
			}
			if seen[name] {
				continue
			}
			seen[name] = true

			entry := resolveNativeInstance(ctx, name, service)
			if entry != nil {
				select {
				case entries <- entry:
				case <-ctx.Done():
					return
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// resolveNativeInstance resolves a service instance name to get host, port, IP, TXT.
func resolveNativeInstance(ctx context.Context, instanceName string, service string) *zeroconf.ServiceEntry {
	id := allocCallbackID()
	ch := make(chan uintptr, 1)

	cbMu.Lock()
	resolveHandlers[id] = ch
	cbMu.Unlock()

	qn, _ := syscall.UTF16PtrFromString(instanceName)
	req := dnsServiceResolveRequest{
		Version:      dnsQueryRequestVersion1,
		QueryName:    qn,
		Callback:     getResolveCallback(),
		QueryContext: id,
	}

	var cancel dnsServiceCancel

	ret, _, _ := procDnsServiceResolve.Call(
		uintptr(unsafe.Pointer(&req)),
		uintptr(unsafe.Pointer(&cancel)),
	)

	if ret != dnsRequestPending {
		cbMu.Lock()
		delete(resolveHandlers, id)
		cbMu.Unlock()
		return nil
	}

	select {
	case ptr, ok := <-ch:
		if !ok || ptr == 0 {
			return nil
		}
		entry := parseNativeServiceInstance(ptr, instanceName, service)
		safeFreeServiceInstance(ptr)
		return entry

	case <-ctx.Done():
		procDnsServiceResolveCancel.Call(uintptr(unsafe.Pointer(&cancel)))
		cbMu.Lock()
		delete(resolveHandlers, id)
		cbMu.Unlock()
		return nil

	case <-time.After(5 * time.Second):
		procDnsServiceResolveCancel.Call(uintptr(unsafe.Pointer(&cancel)))
		cbMu.Lock()
		delete(resolveHandlers, id)
		cbMu.Unlock()
		return nil
	}
}

// parseNativeServiceInstance extracts data from a DNS_SERVICE_INSTANCE.
//
// DNS_SERVICE_INSTANCE layout (amd64):
//
//	 0: pszInstanceName  *uint16
//	 8: pszHostName      *uint16
//	16: ip4Address       *[4]byte
//	24: ip6Address       *[16]byte
//	32: wPort            uint16
//	34: wPriority        uint16
//	36: wWeight          uint16
//	38: (pad)            uint16
//	40: dwPropertyCount  uint32
//	44: (pad)            uint32
//	48: keys             **uint16
//	56: values           **uint16
//	64: dwInterfaceIndex uint32
func parseNativeServiceInstance(ptr uintptr, instanceName string, service string) *zeroconf.ServiceEntry {
	entry := zeroconf.NewServiceEntry(instanceName, service, "local.")

	// Host name
	pHost := *(*uintptr)(unsafe.Pointer(ptr + 8))
	if pHost != 0 {
		entry.HostName = utf16PtrToGoString(pHost)
	}

	// IPv4 address
	pIP4 := *(*uintptr)(unsafe.Pointer(ptr + 16))
	if pIP4 != 0 {
		ip := make(net.IP, 4)
		copy(ip, (*[4]byte)(unsafe.Pointer(pIP4))[:])
		entry.AddrIPv4 = []net.IP{ip}
	}

	// Port
	entry.Port = int(*(*uint16)(unsafe.Pointer(ptr + 32)))

	// TXT properties (key=value pairs)
	propCount := *(*uint32)(unsafe.Pointer(ptr + 40))
	if propCount > 0 && propCount < 256 {
		keysBase := *(*uintptr)(unsafe.Pointer(ptr + 48))
		valsBase := *(*uintptr)(unsafe.Pointer(ptr + 56))
		if keysBase != 0 {
			ptrSize := unsafe.Sizeof(uintptr(0))
			entry.Text = make([]string, 0, propCount)
			for i := uint32(0); i < propCount; i++ {
				kp := *(*uintptr)(unsafe.Pointer(keysBase + uintptr(i)*ptrSize))
				key := ""
				if kp != 0 {
					key = utf16PtrToGoString(kp)
				}
				val := ""
				if valsBase != 0 {
					vp := *(*uintptr)(unsafe.Pointer(valsBase + uintptr(i)*ptrSize))
					if vp != 0 {
						val = utf16PtrToGoString(vp)
					}
				}
				if val != "" {
					entry.Text = append(entry.Text, key+"="+val)
				} else if key != "" {
					entry.Text = append(entry.Text, key)
				}
			}
		}
	}

	return entry
}

// utf16PtrToGoString converts a null-terminated UTF-16 pointer to a Go string.
func utf16PtrToGoString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	n := 0
	for {
		ch := *(*uint16)(unsafe.Pointer(ptr + uintptr(n)*2))
		if ch == 0 {
			break
		}
		n++
		if n > 4096 {
			break
		}
	}
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	for i := 0; i < n; i++ {
		buf[i] = *(*uint16)(unsafe.Pointer(ptr + uintptr(i)*2))
	}
	return syscall.UTF16ToString(buf)
}
