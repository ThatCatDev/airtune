package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Raw mDNS query for _raop._tcp.local
	// mDNS multicast group: 224.0.0.251:5353
	mdnsAddr := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

	// Build a minimal DNS query for _raop._tcp.local PTR
	query := buildMDNSQuery("_raop._tcp.local")

	// Open a UDP socket
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	p := ipv4.NewPacketConn(conn)

	// Find Ethernet interface
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Name == "Ethernet" {
			fmt.Printf("Joining multicast on %s\n", iface.Name)
			p.JoinGroup(&iface, mdnsAddr)
			break
		}
	}

	// Send query
	fmt.Println("Sending mDNS query for _raop._tcp.local...")
	_, err = conn.WriteTo(query, mdnsAddr)
	if err != nil {
		log.Fatalf("send: %v", err)
	}

	// Listen for responses
	buf := make([]byte, 65536)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			fmt.Printf("Read done: %v\n", err)
			break
		}
		fmt.Printf("RESPONSE from %s (%d bytes): %x\n", addr, n, buf[:min(n, 64)])
	}
	fmt.Println("Done.")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildMDNSQuery builds a minimal DNS query packet for a PTR record
func buildMDNSQuery(name string) []byte {
	// DNS header: ID=0, Flags=0 (standard query), QDCOUNT=1
	header := []byte{
		0x00, 0x00, // ID
		0x00, 0x00, // Flags (standard query)
		0x00, 0x01, // QDCOUNT = 1
		0x00, 0x00, // ANCOUNT = 0
		0x00, 0x00, // NSCOUNT = 0
		0x00, 0x00, // ARCOUNT = 0
	}

	// Encode name
	qname := encodeDNSName(name)

	// QTYPE=PTR(12), QCLASS=IN(1) with unicast-response bit
	qtype := []byte{0x00, 0x0c, 0x00, 0x01}

	pkt := make([]byte, 0, len(header)+len(qname)+len(qtype))
	pkt = append(pkt, header...)
	pkt = append(pkt, qname...)
	pkt = append(pkt, qtype...)
	return pkt
}

func encodeDNSName(name string) []byte {
	var buf []byte
	labels := splitLabels(name)
	for _, label := range labels {
		buf = append(buf, byte(len(label)))
		buf = append(buf, []byte(label)...)
	}
	buf = append(buf, 0x00) // root label
	return buf
}

func splitLabels(name string) []string {
	var labels []string
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			if i > start {
				labels = append(labels, name[start:i])
			}
			start = i + 1
		}
	}
	if start < len(name) {
		labels = append(labels, name[start:])
	}
	return labels
}
