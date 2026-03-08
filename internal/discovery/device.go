package discovery

import (
	"fmt"
	"strconv"
	"strings"
)

// AirPlayDevice represents a discovered AirPlay receiver.
type AirPlayDevice struct {
	// Identity
	ID   string // unique ID (MAC address from service name)
	Name string // user-friendly name

	// Network
	Host string
	Port int

	// Capabilities parsed from TXT records (_raop._tcp)
	SampleRate  int
	BitDepth    int
	Channels    int
	Codecs      []string // e.g. ["0"] = PCM, ["1"] = ALAC, ["2"] = AAC
	Encryption  []string // e.g. ["1"] = RSA
	Features    uint64
	Model       string
	FirmwareVer string

	// Group fields populated from _airplay._tcp TXT records.
	// Devices that form a HomePod stereo pair share the same GroupID and
	// TightSyncID. IsGroupLeader is true for the primary device in the pair.
	GroupID       string // gid — stereo pair members share this value
	GroupName     string // gpn — human-readable group name (e.g. "Living Room")
	IsGroupLeader bool   // igl — true if this device is the group leader
	TightSyncID   string // tsid — identical across stereo pair members
}

func (d AirPlayDevice) String() string {
	return fmt.Sprintf("%s (%s:%d)", d.Name, d.Host, d.Port)
}

// Addr returns "host:port".
func (d AirPlayDevice) Addr() string {
	return fmt.Sprintf("%s:%d", d.Host, d.Port)
}

// SupportsEncryption returns true if the device supports RSA encryption (type 1).
func (d AirPlayDevice) SupportsEncryption() bool {
	for _, e := range d.Encryption {
		if e == "1" {
			return true
		}
	}
	return false
}

// ParseTXTRecords parses mDNS TXT records into a key/value map.
func ParseTXTRecords(txt []string) map[string]string {
	m := make(map[string]string, len(txt))
	for _, entry := range txt {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// normaliseGroupID extracts the canonical group ID from a potentially compound
// value. The _airplay._tcp gid field may be formatted as "UUID1+n+UUID2"; only
// the first segment (before the first '+') is used for equality comparisons so
// that both members of a stereo pair resolve to the same ID.
func normaliseGroupID(raw string) string {
	if idx := strings.Index(raw, "+"); idx >= 0 {
		return raw[:idx]
	}
	return raw
}

// ParseAirPlayTXT parses _airplay._tcp TXT records and merges the group-related
// fields into dev. Fields that are absent from txt are left unchanged.
func ParseAirPlayTXT(txt []string, dev *AirPlayDevice) {
	records := ParseTXTRecords(txt)

	if gid, ok := records["gid"]; ok {
		dev.GroupID = normaliseGroupID(gid)
	}
	if gpn, ok := records["gpn"]; ok {
		dev.GroupName = gpn
	}
	if igl, ok := records["igl"]; ok {
		// The field is present and non-zero when this device is the leader.
		v, err := strconv.Atoi(igl)
		dev.IsGroupLeader = err == nil && v != 0
	}
	if tsid, ok := records["tsid"]; ok {
		dev.TightSyncID = tsid
	}
}

// NewDeviceFromMDNS creates an AirPlayDevice from _raop._tcp mDNS service entry
// fields. Group fields are left at their zero values; they are filled in later
// by a concurrent _airplay._tcp browse via ParseAirPlayTXT.
func NewDeviceFromMDNS(instanceName string, host string, port int, txt []string) AirPlayDevice {
	records := ParseTXTRecords(txt)

	// Instance name format: "MACADDR@DeviceName"
	name := instanceName
	id := instanceName
	if idx := strings.Index(instanceName, "@"); idx >= 0 {
		id = instanceName[:idx]
		name = instanceName[idx+1:]
	}

	dev := AirPlayDevice{
		ID:         id,
		Name:       name,
		Host:       host,
		Port:       port,
		SampleRate: 44100,
		BitDepth:   16,
		Channels:   2,
	}

	if sr, ok := records["sr"]; ok {
		if v, err := strconv.Atoi(sr); err == nil {
			dev.SampleRate = v
		}
	}
	if ss, ok := records["ss"]; ok {
		if v, err := strconv.Atoi(ss); err == nil {
			dev.BitDepth = v
		}
	}
	if ch, ok := records["ch"]; ok {
		if v, err := strconv.Atoi(ch); err == nil {
			dev.Channels = v
		}
	}
	if cn, ok := records["cn"]; ok {
		dev.Codecs = strings.Split(cn, ",")
	}
	if et, ok := records["et"]; ok {
		dev.Encryption = strings.Split(et, ",")
	}
	if ft, ok := records["ft"]; ok {
		if v, err := strconv.ParseUint(ft, 0, 64); err == nil {
			dev.Features = v
		}
	}
	if am, ok := records["am"]; ok {
		dev.Model = am
	}
	if vs, ok := records["vs"]; ok {
		dev.FirmwareVer = vs
	}

	return dev
}
