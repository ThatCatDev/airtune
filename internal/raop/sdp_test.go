package raop

import (
	"strings"
	"testing"
)

func TestBuildSDP(t *testing.T) {
	cfg := SessionConfig{
		Host:       "192.168.1.50",
		CodecName:  "AppleLossless",
		FmtpLine:   "96 352 0 16 40 10 14 2 255 0 0 44100",
		SampleRate: 44100,
		Channels:   2,
		AESIV:      []byte("0123456789abcdef"),
	}

	sdp := BuildSDP(cfg, "RSA_ENCRYPTED_KEY_BASE64", "")

	checks := []string{
		"v=0",
		"o=iTunes",
		"s=iTunes",
		"m=audio 0 RTP/AVP 96",
		"a=rtpmap:96 AppleLossless",
		"a=fmtp:96 352 0 16 40 10 14 2 255 0 0 44100",
		"a=rsaaeskey:RSA_ENCRYPTED_KEY_BASE64",
		"a=aesiv:",
	}

	for _, check := range checks {
		if !strings.Contains(sdp, check) {
			t.Errorf("SDP missing %q", check)
		}
	}
}
