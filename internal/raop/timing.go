package raop

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"
)

// TimingChannel handles the NTP-like timing synchronization with AirPlay devices.
// AirPlay devices send timing requests (PT 82) and we respond with timing responses (PT 83).
// We also proactively send timing packets every 3 seconds and measure network RTT.
type TimingChannel struct {
	conn       *net.UDPConn
	remoteAddr *net.UDPAddr
	mu         sync.Mutex

	// RTT measurement
	onRTT func(time.Duration) // callback when a new RTT measurement is available
}

// NewTimingChannel creates a timing channel bound to the given local connection
// that communicates with the given remote address. onRTT is called (if non-nil)
// whenever a new round-trip time measurement is available.
func NewTimingChannel(conn *net.UDPConn, remoteAddr *net.UDPAddr, onRTT func(time.Duration)) *TimingChannel {
	return &TimingChannel{
		conn:       conn,
		remoteAddr: remoteAddr,
		onRTT:      onRTT,
	}
}

// SetRemoteAddr updates the remote address (called after SETUP returns the real timing port).
func (t *TimingChannel) SetRemoteAddr(addr *net.UDPAddr) {
	t.mu.Lock()
	t.remoteAddr = addr
	t.mu.Unlock()
}

// Run starts the timing channel. It listens for timing requests and sends periodic sync packets.
func (t *TimingChannel) Run(ctx context.Context) {
	// Start listener for timing requests from the device
	go t.listenRequests(ctx)

	// Send periodic timing packets every 3 seconds
	go t.sendPeriodicSync(ctx)
}

// listenRequests listens for incoming timing request packets (PT 82) from the device
// and responds with timing response packets (PT 83). Also handles timing responses
// (PT 83) from the device to measure network RTT.
func (t *TimingChannel) listenRequests(ctx context.Context) {
	buf := make([]byte, 256)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		t.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, addr, err := t.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("timing: read error: %v", err)
				continue
			}
		}

		if n < 32 {
			continue
		}

		pt := buf[1] & 0x7F

		switch pt {
		case PayloadTiming:
			// Timing request from device — respond with PT 83
			refSecs := binary.BigEndian.Uint32(buf[24:28])
			refFrac := binary.BigEndian.Uint32(buf[28:32])
			resp := t.buildTimingResponse(refSecs, refFrac)
			t.conn.WriteToUDP(resp, addr)

		case PayloadTimingResponse:
			// Timing response to our request — measure RTT.
			// [8:16] = reference timestamp (our original send time, echoed back)
			if t.onRTT != nil {
				refSecs := binary.BigEndian.Uint32(buf[8:12])
				refFrac := binary.BigEndian.Uint32(buf[12:16])
				sendTime := ntpToTime(refSecs, refFrac)
				rtt := time.Since(sendTime)
				if rtt > 0 && rtt < 5*time.Second {
					t.onRTT(rtt)
				}
			}
		}
	}
}

// sendPeriodicSync sends timing packets to the device every 3 seconds.
func (t *TimingChannel) sendPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			addr := t.remoteAddr
			t.mu.Unlock()
			pkt := t.buildTimingRequest()
			t.conn.WriteToUDP(pkt, addr)
		}
	}
}

// buildTimingResponse creates a timing response packet (PT 83).
// Format (32 bytes):
//
//	[0]     0x80 (RTP v2)
//	[1]     0xD3 (marker + PT 83)
//	[2:4]   sequence (7)
//	[4:8]   0x00000000
//	[8:16]  reference timestamp (from request, NTP format)
//	[16:24] receive timestamp (when we got the request, NTP format)
//	[24:32] send timestamp (when we send this response, NTP format)
func (t *TimingChannel) buildTimingResponse(refSecs, refFrac uint32) []byte {
	pkt := make([]byte, 32)

	pkt[0] = 0x80
	pkt[1] = 0x80 | PayloadTimingResponse // 0xD3
	binary.BigEndian.PutUint16(pkt[2:4], 7)

	// Reference timestamp (echo back from request)
	binary.BigEndian.PutUint32(pkt[8:12], refSecs)
	binary.BigEndian.PutUint32(pkt[12:16], refFrac)

	// Receive timestamp (now)
	nowSecs, nowFrac := ntpTime(time.Now())
	binary.BigEndian.PutUint32(pkt[16:20], nowSecs)
	binary.BigEndian.PutUint32(pkt[20:24], nowFrac)

	// Send timestamp (also now, close enough)
	binary.BigEndian.PutUint32(pkt[24:28], nowSecs)
	binary.BigEndian.PutUint32(pkt[28:32], nowFrac)

	return pkt
}

// buildTimingRequest creates a timing request packet (PT 82).
func (t *TimingChannel) buildTimingRequest() []byte {
	pkt := make([]byte, 32)

	pkt[0] = 0x80
	pkt[1] = 0x80 | PayloadTiming // 0xD2
	binary.BigEndian.PutUint16(pkt[2:4], 7)

	// Send timestamp in bytes 24-31
	secs, frac := ntpTime(time.Now())
	binary.BigEndian.PutUint32(pkt[24:28], secs)
	binary.BigEndian.PutUint32(pkt[28:32], frac)

	return pkt
}

// ntpTime converts a time.Time to NTP timestamp (seconds since 1900 + fraction).
func ntpTime(t time.Time) (uint32, uint32) {
	// Seconds since NTP epoch (1900-01-01)
	secs := uint32(t.Unix()) + NTPEpochOffset
	// Fractional part: nanoseconds → fraction of 2^32
	frac := uint32(uint64(t.Nanosecond()) * (1 << 32) / 1e9)
	return secs, frac
}

// ntpToTime converts an NTP timestamp back to time.Time.
func ntpToTime(secs, frac uint32) time.Time {
	unixSecs := int64(secs) - int64(NTPEpochOffset)
	nanos := int64(frac) * 1e9 / (1 << 32)
	return time.Unix(unixSecs, nanos)
}
