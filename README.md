# AirTune

AirTune streams your PC's audio to AirPlay speakers over the network. It captures system audio via WASAPI loopback, resamples to 44.1kHz, and sends it to one or more AirPlay 1 (RAOP) receivers.

## Features

- **Device Discovery** — Automatic mDNS/Zeroconf discovery of AirPlay speakers on your network
- **Multi-Device Streaming** — Stream to multiple AirPlay speakers simultaneously
- **L/R Channel Split** — Send left channel to one speaker and right to another for stereo separation
- **Volume Sync** — Syncs Windows system volume to AirPlay devices in real time
- **GUI & CLI** — GTK4 GUI mode or interactive CLI mode

## Prerequisites

- **Go 1.26+**
- **Windows 10/11** (x64)
- **MSYS2 with mingw64** — for GTK4 GUI builds (not needed for CLI-only)

## Build

### CLI only (no CGo required)

```
make build
```

### GUI mode (requires MSYS2 mingw64 + GTK4)

```
make build-gui
```

## Usage

### CLI Mode

```
airtune --cli
```

Discovers AirPlay speakers and lets you choose which one to stream to interactively.

### GUI Mode

```
airtune
```

Opens the GTK4 GUI with device list, volume control, and channel mode selection.

## Configuration

Settings are stored in `%APPDATA%/airtune/config.json`:

| Field | Values | Description |
|-------|--------|-------------|
| `audio_device` | Device ID string | WASAPI device ID for loopback capture (empty = system default) |
| `channel_modes` | Map of device ID → mode | Per-device channel mode (0=Both, 1=Left, 2=Right) |

## Architecture

```
Windows App → [Default Audio Output] → WASAPI Loopback → AirTune
                                                            ↓
                                                         Pipeline
                                                    (resample → encode)
                                                           ↓
                                                     RAOP Session
                                                    (RTSP + RTP)
                                                           ↓
                                                    AirPlay Speaker
```

## License

See [LICENSE](LICENSE).
