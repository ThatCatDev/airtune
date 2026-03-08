package service

// AppState represents the global application state.
type AppState int

const (
	AppStateIdle      AppState = iota // No devices connected
	AppStateStreaming                  // Streaming to at least one device
	AppStatePaused                    // Streaming paused
)

func (s AppState) String() string {
	switch s {
	case AppStateIdle:
		return "idle"
	case AppStateStreaming:
		return "streaming"
	case AppStatePaused:
		return "paused"
	default:
		return "unknown"
	}
}
