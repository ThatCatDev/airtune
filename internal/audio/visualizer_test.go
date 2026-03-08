package audio

import "testing"

func TestComputeRMSLevels(t *testing.T) {
	silence := make([]byte, 1024)
	levels := ComputeRMSLevels(silence, 2)
	if len(levels) != 2 {
		t.Fatalf("levels count = %d, want 2", len(levels))
	}
	for i, l := range levels {
		if l > 0.001 {
			t.Errorf("silence level[%d] = %f, want ~0", i, l)
		}
	}
}

func TestComputePeakLevels(t *testing.T) {
	silence := make([]byte, 512)
	peaks := ComputePeakLevels(silence, 2)
	if len(peaks) != 2 {
		t.Fatalf("peaks count = %d, want 2", len(peaks))
	}
	for i, p := range peaks {
		if p > 0.001 {
			t.Errorf("silence peak[%d] = %f, want ~0", i, p)
		}
	}
}
