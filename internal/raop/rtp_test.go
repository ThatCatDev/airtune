package raop

import (
	"encoding/binary"
	"testing"
)

func TestBuildAudioPacket(t *testing.T) {
	hdr := RTPHeader{
		Marker:      false,
		PayloadType: PayloadAudio,
		Sequence:    42,
		Timestamp:   1000,
		SSRC:        0xDEADBEEF,
	}
	payload := []byte{1, 2, 3, 4}
	pkt := BuildAudioPacket(hdr, payload)

	if pkt[0] != 0x80 {
		t.Errorf("version byte = 0x%02x, want 0x80", pkt[0])
	}
	if pkt[1] != 0x60 {
		t.Errorf("PT byte = 0x%02x, want 0x60", pkt[1])
	}
	if seq := binary.BigEndian.Uint16(pkt[2:4]); seq != 42 {
		t.Errorf("sequence = %d, want 42", seq)
	}
	if ts := binary.BigEndian.Uint32(pkt[4:8]); ts != 1000 {
		t.Errorf("timestamp = %d, want 1000", ts)
	}
	if ssrc := binary.BigEndian.Uint32(pkt[8:12]); ssrc != 0xDEADBEEF {
		t.Errorf("SSRC = 0x%08x, want 0xDEADBEEF", ssrc)
	}
}

func TestBuildAudioPacketMarker(t *testing.T) {
	hdr := RTPHeader{Marker: true, PayloadType: PayloadAudio}
	pkt := BuildAudioPacket(hdr, nil)
	if pkt[1]&0x80 == 0 {
		t.Error("marker bit not set")
	}
}

func TestAudioPacketBuilder(t *testing.T) {
	b := NewAudioPacketBuilder(123)

	pkt1 := b.Build([]byte{0xFF}, 352)
	if pkt1[1]&0x80 == 0 {
		t.Error("first packet should have marker bit")
	}

	pkt2 := b.Build([]byte{0xFF}, 352)
	if pkt2[1]&0x80 != 0 {
		t.Error("second packet should not have marker bit")
	}

	seq1 := binary.BigEndian.Uint16(pkt1[2:4])
	seq2 := binary.BigEndian.Uint16(pkt2[2:4])
	if seq2 != seq1+1 {
		t.Errorf("sequence: %d -> %d, want +1", seq1, seq2)
	}

	b.SetMarker()
	pkt3 := b.Build([]byte{0xFF}, 352)
	if pkt3[1]&0x80 == 0 {
		t.Error("packet after SetMarker should have marker bit")
	}
}
