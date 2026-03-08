package discovery

import "testing"

func TestParseTXTRecords(t *testing.T) {
	txt := []string{"sr=44100", "ch=2", "ss=16"}
	m := ParseTXTRecords(txt)
	if m["sr"] != "44100" {
		t.Errorf("sr = %q, want 44100", m["sr"])
	}
	if m["ch"] != "2" {
		t.Errorf("ch = %q, want 2", m["ch"])
	}
}

func TestNewDeviceFromMDNS(t *testing.T) {
	txt := []string{"sr=48000", "ss=24", "ch=2", "cn=0,1", "et=1", "am=AirPort10,115"}
	dev := NewDeviceFromMDNS("AABBCCDDEE@Living Room", "192.168.1.100", 7000, txt)

	if dev.ID != "AABBCCDDEE" {
		t.Errorf("ID = %q, want AABBCCDDEE", dev.ID)
	}
	if dev.Name != "Living Room" {
		t.Errorf("Name = %q, want Living Room", dev.Name)
	}
	if dev.Host != "192.168.1.100" {
		t.Errorf("Host = %q", dev.Host)
	}
	if dev.Port != 7000 {
		t.Errorf("Port = %d", dev.Port)
	}
	if dev.SampleRate != 48000 {
		t.Errorf("SampleRate = %d, want 48000", dev.SampleRate)
	}
	if dev.BitDepth != 24 {
		t.Errorf("BitDepth = %d, want 24", dev.BitDepth)
	}
	if dev.Model != "AirPort10,115" {
		t.Errorf("Model = %q", dev.Model)
	}
	if !dev.SupportsEncryption() {
		t.Error("should support encryption (et=1)")
	}
}

func TestNewDeviceFromMDNSDefaults(t *testing.T) {
	dev := NewDeviceFromMDNS("MyDevice", "10.0.0.1", 5000, nil)
	if dev.SampleRate != 44100 {
		t.Errorf("default SampleRate = %d, want 44100", dev.SampleRate)
	}
	if dev.BitDepth != 16 {
		t.Errorf("default BitDepth = %d, want 16", dev.BitDepth)
	}
	if dev.Channels != 2 {
		t.Errorf("default Channels = %d, want 2", dev.Channels)
	}
}
