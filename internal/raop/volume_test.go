package raop

import (
	"math"
	"strings"
	"testing"
)

func TestLinearToAirPlay(t *testing.T) {
	if v := LinearToAirPlay(0.0); v != -144.0 {
		t.Errorf("LinearToAirPlay(0.0) = %f, want -144.0", v)
	}
	if v := LinearToAirPlay(1.0); v != 0.0 {
		t.Errorf("LinearToAirPlay(1.0) = %f, want 0.0", v)
	}
	v := LinearToAirPlay(0.5)
	if v >= 0 || v <= -144 {
		t.Errorf("LinearToAirPlay(0.5) = %f, want between -144 and 0", v)
	}
}

func TestAirPlayToLinear(t *testing.T) {
	if v := AirPlayToLinear(0.0); v != 1.0 {
		t.Errorf("AirPlayToLinear(0.0) = %f, want 1.0", v)
	}
	if v := AirPlayToLinear(-144.0); v != 0.0 {
		t.Errorf("AirPlayToLinear(-144.0) = %f, want 0.0", v)
	}
}

func TestVolumeRoundTrip(t *testing.T) {
	for _, x := range []float64{0.1, 0.25, 0.5, 0.75, 1.0} {
		got := AirPlayToLinear(LinearToAirPlay(x))
		if math.Abs(got-x) > 0.001 {
			t.Errorf("round-trip(%f) = %f", x, got)
		}
	}
}

func TestFormatVolume(t *testing.T) {
	s := FormatVolume(-20.0)
	if !strings.Contains(s, "volume:") {
		t.Errorf("FormatVolume missing 'volume:': %q", s)
	}
	if !strings.HasSuffix(s, "\r\n") {
		t.Errorf("FormatVolume missing CRLF: %q", s)
	}
}
