package raop

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// BuildSDP constructs the SDP payload for an encrypted RTSP ANNOUNCE request.
func BuildSDP(cfg SessionConfig, rsaAESKey, rsaAESIV string) string {
	var b strings.Builder

	b.WriteString("v=0\r\n")
	b.WriteString("o=iTunes 0 0 IN IP4 127.0.0.1\r\n")
	b.WriteString("s=iTunes\r\n")
	b.WriteString("c=IN IP4 " + cfg.Host + "\r\n")
	b.WriteString("t=0 0\r\n")
	b.WriteString("m=audio 0 RTP/AVP 96\r\n")
	b.WriteString(fmt.Sprintf("a=rtpmap:96 %s\r\n", cfg.CodecName))
	b.WriteString(fmt.Sprintf("a=fmtp:%s\r\n", cfg.FmtpLine))
	b.WriteString(fmt.Sprintf("a=rsaaeskey:%s\r\n", rsaAESKey))
	b.WriteString(fmt.Sprintf("a=aesiv:%s\r\n", base64.StdEncoding.EncodeToString(cfg.AESIV)))

	return b.String()
}

// BuildSDPUnencrypted constructs an SDP payload without encryption keys.
// Used when the device doesn't support RSA encryption (et≠1).
func BuildSDPUnencrypted(cfg SessionConfig) string {
	var b strings.Builder

	b.WriteString("v=0\r\n")
	b.WriteString("o=iTunes 0 0 IN IP4 127.0.0.1\r\n")
	b.WriteString("s=iTunes\r\n")
	b.WriteString("c=IN IP4 " + cfg.Host + "\r\n")
	b.WriteString("t=0 0\r\n")
	b.WriteString("m=audio 0 RTP/AVP 96\r\n")
	b.WriteString(fmt.Sprintf("a=rtpmap:96 %s\r\n", cfg.CodecName))
	b.WriteString(fmt.Sprintf("a=fmtp:%s\r\n", cfg.FmtpLine))

	return b.String()
}
