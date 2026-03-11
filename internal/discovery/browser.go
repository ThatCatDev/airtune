package discovery

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	raopService    = "_raop._tcp"
	airplayService = "_airplay._tcp"
)

// Browser discovers AirPlay devices on the local network via mDNS.
//
// It browses two service types concurrently:
//   - _raop._tcp  — primary source for device identity and audio capabilities.
//   - _airplay._tcp — supplementary source for HomePod stereo-pair group fields
//     (GroupID, GroupName, IsGroupLeader, TightSyncID).
//
// Entries from _airplay._tcp are matched to existing _raop._tcp devices by IP
// address. When a match is found the group fields are merged into the stored
// device and onChange is fired if anything changed.
type Browser struct {
	mu              sync.Mutex
	devices         map[string]AirPlayDevice // keyed by device ID (MAC)
	ipToID          map[string]string        // IPv4 → device ID; used to match _airplay entries
	pendingAirplay  map[string][]string      // IPv4 → TXT records from _airplay._tcp (buffered until RAOP match)
	onChange        func([]AirPlayDevice)
	cancel          context.CancelFunc
	done            chan struct{}
}

// NewBrowser creates a Browser that calls onChange whenever the discovered
// device list changes. onChange must not be nil.
func NewBrowser(onChange func([]AirPlayDevice)) *Browser {
	return &Browser{
		devices:  make(map[string]AirPlayDevice),
		ipToID:   make(map[string]string),
		onChange: onChange,
		done:     make(chan struct{}),
	}
}

// Start begins browsing for AirPlay devices. It blocks until Stop is called
// or the parent context is cancelled. Call Start in a goroutine.
//
// Two independent zeroconf resolvers are created — one per service type — so
// that they can be browsed concurrently on the same context.
func (b *Browser) Start(ctx context.Context) error {
	ctx, b.cancel = context.WithCancel(ctx)

	// The grandcat/zeroconf library uses exponential backoff that eventually
	// stops querying. To work around this, we restart resolvers periodically
	// so that mDNS queries keep flowing and new/returning devices are found.
	const browseInterval = 30 * time.Second
	const browseDuration = 10 * time.Second

	// Do one immediate scan, then loop
	for {
		b.browseOnce(ctx, browseDuration)

		select {
		case <-ctx.Done():
			close(b.done)
			return nil
		case <-time.After(browseInterval):
		}
	}
}

// browseOnceZeroconf creates fresh zeroconf resolvers that run for the given duration.
// This is the fallback when the native platform API is unavailable.
func (b *Browser) browseOnceZeroconf(ctx context.Context, duration time.Duration) {
	scanCtx, scanCancel := context.WithTimeout(ctx, duration)
	defer scanCancel()

	// --- _raop._tcp ---
	raopResolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("discovery: failed to create _raop._tcp resolver: %v", err)
		return
	}

	raopEntries := make(chan *zeroconf.ServiceEntry)
	go b.processRAOPEntries(raopEntries)

	if err = raopResolver.Browse(scanCtx, raopService, "local.", raopEntries); err != nil {
		log.Printf("discovery: failed to browse %s: %v", raopService, err)
		return
	}

	// --- _airplay._tcp ---
	airplayResolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Printf("discovery: failed to create _airplay._tcp resolver: %v", err)
	} else {
		airplayEntries := make(chan *zeroconf.ServiceEntry)
		go b.processAirPlayEntries(airplayEntries)

		if err = airplayResolver.Browse(scanCtx, airplayService, "local.", airplayEntries); err != nil {
			log.Printf("discovery: failed to browse %s: %v", airplayService, err)
		}
	}

	<-scanCtx.Done()
}

// Stop stops the mDNS browser. It is safe to call multiple times.
func (b *Browser) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	<-b.done
}

// Devices returns a snapshot of the currently discovered devices.
func (b *Browser) Devices() []AirPlayDevice {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.deviceList()
}

// processRAOPEntries reads _raop._tcp entries, converts them to AirPlayDevice
// values, and fires onChange on every update.
func (b *Browser) processRAOPEntries(entries <-chan *zeroconf.ServiceEntry) {
	for entry := range entries {
		host := resolveHost(entry)
		dev := NewDeviceFromMDNS(entry.Instance, host, entry.Port, entry.Text)

		b.mu.Lock()
		prev, existed := b.devices[dev.ID]
		// Preserve any group fields already merged from _airplay._tcp.
		if existed {
			dev.GroupID = prev.GroupID
			dev.GroupName = prev.GroupName
			dev.IsGroupLeader = prev.IsGroupLeader
			dev.TightSyncID = prev.TightSyncID
		}
		changed := !existed || !deviceEqual(prev, dev)
		b.devices[dev.ID] = dev
		// Keep the reverse IP → ID index current.
		if host != "" {
			b.ipToID[host] = dev.ID
		}
		var list []AirPlayDevice
		if changed {
			list = b.deviceList()
		}
		b.mu.Unlock()

		if changed {
			log.Printf("discovery: found device %s", dev)
			b.onChange(list)
		}
	}
}

// processAirPlayEntries reads _airplay._tcp entries and merges group fields
// into the matching _raop._tcp device (matched by IP address). onChange is
// fired only when the group information actually changes.
func (b *Browser) processAirPlayEntries(entries <-chan *zeroconf.ServiceEntry) {
	for entry := range entries {
		host := resolveHost(entry)

		b.mu.Lock()

		// Look up the RAOP device that lives at this IP.
		id, ok := b.ipToID[host]
		if !ok {
			// The _airplay._tcp entry arrived before the matching _raop._tcp
			// entry. There is nothing to merge into yet; the next RAOP entry
			// for this host will preserve whatever was already stored, so
			// we can safely skip for now.
			b.mu.Unlock()
			continue
		}

		dev, ok := b.devices[id]
		if !ok {
			b.mu.Unlock()
			continue
		}

		// Snapshot group fields before the merge so we can detect changes.
		prevGroupID := dev.GroupID
		prevGroupName := dev.GroupName
		prevIsGroupLeader := dev.IsGroupLeader
		prevTightSyncID := dev.TightSyncID

		ParseAirPlayTXT(entry.Text, &dev)

		changed := dev.GroupID != prevGroupID ||
			dev.GroupName != prevGroupName ||
			dev.IsGroupLeader != prevIsGroupLeader ||
			dev.TightSyncID != prevTightSyncID

		var list []AirPlayDevice
		if changed {
			b.devices[id] = dev
			list = b.deviceList()
		}
		b.mu.Unlock()

		if changed {
			log.Printf("discovery: updated group info for device %s (gid=%s gpn=%q igl=%v tsid=%s)",
				dev, dev.GroupID, dev.GroupName, dev.IsGroupLeader, dev.TightSyncID)
			b.onChange(list)
		}
	}
}

// resolveHost picks the best host string from an mDNS entry.
// It prefers the first IPv4 address; falls back to the hostname
// with any trailing dot removed.
func resolveHost(entry *zeroconf.ServiceEntry) string {
	if len(entry.AddrIPv4) > 0 {
		return entry.AddrIPv4[0].String()
	}
	return strings.TrimRight(entry.HostName, ".")
}

// deviceList returns a slice of all devices in the map (caller must hold mu).
func (b *Browser) deviceList() []AirPlayDevice {
	list := make([]AirPlayDevice, 0, len(b.devices))
	for _, d := range b.devices {
		list = append(list, d)
	}
	return list
}

// deviceEqual returns true if two device structs are functionally identical
// for change-detection purposes.
func deviceEqual(a, b AirPlayDevice) bool {
	return a.Host == b.Host &&
		a.Port == b.Port &&
		a.Name == b.Name &&
		a.GroupID == b.GroupID
}

// RemoveStale removes devices that have not been re-discovered within the
// given duration. This can be called periodically to prune vanished devices.
func (b *Browser) RemoveStale(maxAge time.Duration) {
	// The zeroconf library does not surface TTL information, so a practical
	// approach is to periodically clear the map and let active devices
	// re-appear on the next browse cycle. This helper clears all devices
	// and fires onChange with an empty list; the browse loop will repopulate
	// it as entries arrive.
	b.mu.Lock()
	if len(b.devices) == 0 {
		b.mu.Unlock()
		return
	}
	b.devices = make(map[string]AirPlayDevice)
	b.ipToID = make(map[string]string)
	b.mu.Unlock()

	b.onChange(nil)
}
