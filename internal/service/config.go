package service

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"airtune/internal/audio"
)

// Config holds persistent user preferences saved across restarts.
type Config struct {
	ChannelModes  map[string]audio.ChannelMode `json:"channel_modes,omitempty"`  // keyed by device ID
	AudioDevice string `json:"audio_device,omitempty"` // WASAPI device ID for capture
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		ChannelModes: make(map[string]audio.ChannelMode),
	}
}

// configPath returns the path to the config file (%APPDATA%/airtune/config.json).
func configPath() string {
	dir := os.Getenv("APPDATA")
	if dir == "" {
		dir, _ = os.UserConfigDir()
	}
	return filepath.Join(dir, "airtune", "config.json")
}

// LoadConfig reads the config from disk. Returns defaults if the file doesn't exist.
func LoadConfig() Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("config: parse error, using defaults: %v", err)
		return DefaultConfig()
	}
	// Ensure map is never nil after loading
	if cfg.ChannelModes == nil {
		cfg.ChannelModes = make(map[string]audio.ChannelMode)
	}
	return cfg
}

// SaveConfig writes the config to disk.
func SaveConfig(cfg Config) {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Printf("config: mkdir error: %v", err)
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Printf("config: marshal error: %v", err)
		return
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		log.Printf("config: write error: %v", err)
	}
}
