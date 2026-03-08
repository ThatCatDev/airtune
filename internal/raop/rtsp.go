package raop

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// RTSPClient implements the RAOP RTSP handshake protocol.
// It manages a TCP connection to the AirPlay device and handles
// the full RTSP sequence: OPTIONS → ANNOUNCE → SETUP → RECORD → SET_PARAMETER → FLUSH → TEARDOWN.
type RTSPClient struct {
	conn   net.Conn
	reader *textproto.Reader
	cseq   int
	host   string
	port   int
	url    string
	closed bool // set when connection is known dead

	// Persistent headers sent with every request
	clientInstance string // unique client identifier
	dacpID         string // DACP identifier
	activeRemote   string // Active-Remote for control

	// Session state
	sessionID string
}

// NewRTSPClient creates a new RTSP client for the given host:port.
func NewRTSPClient(host string, port int) *RTSPClient {
	// Generate random identifiers for this client session
	instanceBytes := make([]byte, 8)
	rand.Read(instanceBytes)
	clientInstance := hex.EncodeToString(instanceBytes)

	dacpBytes := make([]byte, 8)
	rand.Read(dacpBytes)
	dacpID := hex.EncodeToString(dacpBytes)

	activeRemote, _ := rand.Int(rand.Reader, big.NewInt(1<<32))

	return &RTSPClient{
		host:           host,
		port:           port,
		url:            fmt.Sprintf("rtsp://%s/%d", net.JoinHostPort(host, strconv.Itoa(port)), time.Now().UnixNano()),
		cseq:           0,
		clientInstance: clientInstance,
		dacpID:         dacpID,
		activeRemote:   activeRemote.String(),
	}
}

// RTSPResponse holds a parsed RTSP response.
type RTSPResponse struct {
	StatusCode int
	Status     string
	Headers    map[string]string
	Body       string
}

// Connect establishes the TCP connection to the RTSP server.
func (c *RTSPClient) Connect() error {
	addr := net.JoinHostPort(c.host, strconv.Itoa(c.port))
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("rtsp connect: %w", err)
	}
	c.conn = conn
	c.reader = textproto.NewReader(bufio.NewReader(conn))
	return nil
}

// Close closes the TCP connection.
func (c *RTSPClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Options sends the OPTIONS request with Apple-Challenge header.
func (c *RTSPClient) Options() (*RTSPResponse, error) {
	// Generate Apple-Challenge: 16 random bytes, base64-encoded
	challenge := make([]byte, 16)
	rand.Read(challenge)
	challengeB64 := base64.StdEncoding.EncodeToString(challenge)

	headers := map[string]string{
		"Apple-Challenge": challengeB64,
	}
	return c.sendRequest("OPTIONS", "*", headers, "")
}

// Announce sends the ANNOUNCE request with the SDP body containing
// stream description and encrypted AES keys.
func (c *RTSPClient) Announce(sdp string) (*RTSPResponse, error) {
	headers := map[string]string{
		"Content-Type": "application/sdp",
	}
	return c.sendRequest("ANNOUNCE", c.url, headers, sdp)
}

// Setup sends the SETUP request to negotiate UDP transport ports.
// It returns the server's chosen ports for audio, control, and timing channels.
func (c *RTSPClient) Setup(clientPorts ClientPorts) (*RTSPResponse, ServerPorts, error) {
	transport := fmt.Sprintf(
		"RTP/AVP/UDP;unicast;mode=record;control_port=%d;timing_port=%d",
		clientPorts.Control, clientPorts.Timing,
	)
	headers := map[string]string{
		"Transport": transport,
	}

	resp, err := c.sendRequest("SETUP", c.url, headers, "")
	if err != nil {
		return nil, ServerPorts{}, err
	}

	// Parse server ports from Transport header
	sp := ServerPorts{}
	if transportHeader, ok := resp.Headers["Transport"]; ok {
		sp = parseTransportHeader(transportHeader)
	}

	// Save session ID
	if sess, ok := resp.Headers["Session"]; ok {
		c.sessionID = sess
	}

	return resp, sp, nil
}

// Record sends the RECORD request to start streaming.
func (c *RTSPClient) Record(seqNum uint16, rtpTime uint32) (*RTSPResponse, error) {
	headers := map[string]string{
		"Range":    "npt=0-",
		"RTP-Info": fmt.Sprintf("seq=%d;rtptime=%d", seqNum, rtpTime),
	}
	if c.sessionID != "" {
		headers["Session"] = c.sessionID
	}
	return c.sendRequest("RECORD", c.url, headers, "")
}

// SetParameter sends a SET_PARAMETER request (used for volume control).
func (c *RTSPClient) SetParameter(body string) (*RTSPResponse, error) {
	headers := map[string]string{
		"Content-Type": "text/parameters",
	}
	if c.sessionID != "" {
		headers["Session"] = c.sessionID
	}
	return c.sendRequest("SET_PARAMETER", c.url, headers, body)
}

// Flush sends a FLUSH request to pause/clear the audio buffer.
func (c *RTSPClient) Flush(seqNum uint16, rtpTime uint32) (*RTSPResponse, error) {
	headers := map[string]string{
		"RTP-Info": fmt.Sprintf("seq=%d;rtptime=%d", seqNum, rtpTime),
	}
	if c.sessionID != "" {
		headers["Session"] = c.sessionID
	}
	return c.sendRequest("FLUSH", c.url, headers, "")
}

// Teardown sends the TEARDOWN request to end the session.
func (c *RTSPClient) Teardown() (*RTSPResponse, error) {
	headers := map[string]string{}
	if c.sessionID != "" {
		headers["Session"] = c.sessionID
	}
	return c.sendRequest("TEARDOWN", c.url, headers, "")
}

// sendRequest sends a generic RTSP request and reads the response.
func (c *RTSPClient) sendRequest(method, uri string, headers map[string]string, body string) (*RTSPResponse, error) {
	if c.closed {
		return nil, fmt.Errorf("rtsp connection closed")
	}
	c.cseq++

	// Build request
	var req strings.Builder
	req.WriteString(fmt.Sprintf("%s %s RTSP/1.0\r\n", method, uri))
	req.WriteString(fmt.Sprintf("CSeq: %d\r\n", c.cseq))
	req.WriteString("User-Agent: iTunes/7.6.2 (Windows; N;)\r\n")
	req.WriteString(fmt.Sprintf("Client-Instance: %s\r\n", c.clientInstance))
	req.WriteString(fmt.Sprintf("DACP-ID: %s\r\n", c.dacpID))
	req.WriteString(fmt.Sprintf("Active-Remote: %s\r\n", c.activeRemote))

	if headers != nil {
		for k, v := range headers {
			req.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
		}
	}

	if body != "" {
		req.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	}

	req.WriteString("\r\n")

	if body != "" {
		req.WriteString(body)
	}

	// Send
	reqStr := req.String()
	log.Printf("RTSP >>> %s %s (CSeq %d)", method, uri, c.cseq)
	_, err := c.conn.Write([]byte(reqStr))
	if err != nil {
		c.closed = true
		return nil, fmt.Errorf("rtsp write %s: %w", method, err)
	}

	// Read response
	return c.readResponse()
}

// readResponse reads and parses an RTSP response.
func (c *RTSPClient) readResponse() (*RTSPResponse, error) {
	// Read status line
	statusLine, err := c.reader.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("rtsp read status: %w", err)
	}

	log.Printf("RTSP <<< %s", statusLine)

	// Parse "RTSP/1.0 200 OK"
	parts := strings.SplitN(statusLine, " ", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("rtsp bad status line: %s", statusLine)
	}

	code, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("rtsp bad status code: %s", parts[1])
	}

	status := ""
	if len(parts) >= 3 {
		status = parts[2]
	}

	// Read headers
	headers := make(map[string]string)
	contentLength := 0
	for {
		line, err := c.reader.ReadLine()
		if err != nil {
			return nil, fmt.Errorf("rtsp read header: %w", err)
		}
		if line == "" {
			break
		}

		colonIdx := strings.IndexByte(line, ':')
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])
		headers[key] = val

		if strings.EqualFold(key, "Content-Length") {
			contentLength, _ = strconv.Atoi(val)
		}
	}

	// Read body if present
	body := ""
	if contentLength > 0 {
		bodyBuf := make([]byte, contentLength)
		n := 0
		for n < contentLength {
			nn, err := c.conn.Read(bodyBuf[n:])
			if err != nil {
				return nil, fmt.Errorf("rtsp read body: %w", err)
			}
			n += nn
		}
		body = string(bodyBuf)
	}

	resp := &RTSPResponse{
		StatusCode: code,
		Status:     status,
		Headers:    headers,
		Body:       body,
	}

	if code != 200 {
		return resp, fmt.Errorf("rtsp %s error: %d %s", method(statusLine), code, status)
	}

	return resp, nil
}

// method extracts a method name hint from context (for error messages).
func method(statusLine string) string {
	return "" // status line doesn't contain the method; just leave blank
}

// parseTransportHeader parses the SETUP response Transport header to extract server ports.
func parseTransportHeader(header string) ServerPorts {
	sp := ServerPorts{}
	parts := strings.Split(header, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		switch key {
		case "server_port":
			sp.Audio, _ = strconv.Atoi(val)
		case "control_port":
			sp.Control, _ = strconv.Atoi(val)
		case "timing_port":
			sp.Timing, _ = strconv.Atoi(val)
		}
	}
	return sp
}
