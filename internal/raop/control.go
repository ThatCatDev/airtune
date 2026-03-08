package raop

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"time"
)

// ControlChannel handles the RAOP control channel (sync and retransmit).
// It sends periodic sync packets (PT 84) and handles retransmit requests (PT 85).
type ControlChannel struct {
	conn       *net.UDPConn
	remoteAddr *net.UDPAddr
	rtpBuilder *AudioPacketBuilder
	latency    uint32 // total latency in audio frames
}

// NewControlChannel creates a new control channel.
// latencyFrames is the total buffer depth (sender latency + receiver Audio-Latency).
func NewControlChannel(conn *net.UDPConn, remoteAddr *net.UDPAddr, rtpBuilder *AudioPacketBuilder, latencyFrames uint32) *ControlChannel {
	return &ControlChannel{
		conn:       conn,
		remoteAddr: remoteAddr,
		rtpBuilder: rtpBuilder,
		latency:    latencyFrames,
	}
}

// Run starts the control channel. It sends sync packets and listens for retransmit requests.
func (c *ControlChannel) Run(ctx context.Context) {
	// Listen for retransmit requests
	go c.listenRetransmit(ctx)

	// Send an initial sync packet immediately so the receiver has timing info
	pkt := c.buildSyncPacket()
	c.conn.WriteToUDP(pkt, c.remoteAddr)

	// Send periodic sync packets every 1 second
	go c.sendPeriodicSync(ctx)
}

// sendPeriodicSync sends sync packets (PT 84) every second.
// Sync packet format (20 bytes):
//
//	[0]     0x90 (RTP v2, extension bit set)
//	[1]     0xD4 (marker + PT 84)
//	[2:4]   sequence (7)
//	[4:8]   current RTP timestamp (where we are now)
//	[8:16]  NTP timestamp (current wall clock)
//	[16:20] next RTP timestamp (next packet to send)
func (c *ControlChannel) sendPeriodicSync(ctx context.Context) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pkt := c.buildSyncPacket()
			c.conn.WriteToUDP(pkt, c.remoteAddr)
		}
	}
}

// buildSyncPacket creates a sync packet (PT 84).
//
// Per the RAOP protocol (libraop/shairport-sync):
//   [4:8]   = current RTP timestamp MINUS latency (what the DAC outputs now)
//   [8:16]  = NTP wall-clock time for this moment
//   [16:20] = current RTP timestamp (head of stream / latest sent)
//
// The receiver calculates buffer depth as [16:20] - [4:8] = latency frames.
func (c *ControlChannel) buildSyncPacket() []byte {
	pkt := make([]byte, 20)

	pkt[0] = 0x90               // v2, extension bit
	pkt[1] = 0x80 | PayloadSync // 0xD4, marker + PT 84
	binary.BigEndian.PutUint16(pkt[2:4], 7)

	curTS := c.rtpBuilder.CurrentTimestamp()

	// [4:8] = curTS - latency: tells receiver "this is what should be playing now"
	binary.BigEndian.PutUint32(pkt[4:8], curTS-c.latency)

	// [8:16] = NTP wall clock
	secs, frac := ntpTime(time.Now())
	binary.BigEndian.PutUint32(pkt[8:12], secs)
	binary.BigEndian.PutUint32(pkt[12:16], frac)

	// [16:20] = current RTP timestamp (head of stream)
	binary.BigEndian.PutUint32(pkt[16:20], curTS)

	return pkt
}

// listenRetransmit listens for retransmit requests (PT 85) from the device.
// In Phase 1, we just log them — no retransmit buffer is maintained.
func (c *ControlChannel) listenRetransmit(ctx context.Context) {
	buf := make([]byte, 256)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, err := c.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("control: read error: %v", err)
				continue
			}
		}

		if n < 8 {
			continue
		}

		pt := buf[1] & 0x7F
		if pt == PayloadRetransmitReq {
			// Retransmit request format:
			// [2:4] missed sequence number
			// [4:6] count of missed packets
			missedSeq := binary.BigEndian.Uint16(buf[4:6])
			missedCount := binary.BigEndian.Uint16(buf[6:8])
			log.Printf("control: retransmit request seq=%d count=%d (not implemented)", missedSeq, missedCount)
		}
	}
}
