# go-slimvu

High-performance, pure Go virtual Squeezebox / Logitech Media Server (LMS) audio level provider, VU meter, and real-time spectrum analyzer engine.

![slimvu TUI](assets/screencast.webp)

`go-slimvu` emulates a hardware Squeezebox player over the **SlimProto** protocol, decodes incoming audio streams in real time with high-precision sample pacing, and exposes lock-free, zero-allocation left/right stereo RMS decibel levels and 16-band real-time frequency spectrum data for LED visualizers, displays, terminal visualizers, and audio monitors.

## Features

- **SlimProto Virtual Player**: Implements full SlimProto TCP handshaking (`HELO`/`STAT`/`STRM`), metadata exchange, and time-synchronized playback.
- **Multi-Codec Audio Decoding**:
  - **FLAC** (Native streaming chunk/frame decoding via `mewkiz/flac`)
  - **MP3** (Streaming MPEG decoding via `hajimehoshi/go-mp3`)
  - **AAC / ADTS** (High-efficiency decoding via `skrashevich/go-aac`)
  - **Ogg Vorbis** (Streaming Vorbis decoding via `jfreymuth/oggvorbis`)
  - **Opus** (Ogg/Opus container decoding via `pion/opus`)
  - **PCM / Raw** (Big/Little endian, 8/16/24/32-bit linear PCM)
- **High-Precision Clock Pacing**: Micro-paused sample consumption driven by system clock jiffies to stay in sync with multi-room audio zones.
- **Zero-Allocation Metering & Spectrum Analysis**: Lock-free atomic packed integers (`AtomicLevels`) and atomic 32-bit floats (`AtomicSpectrum`) for real-time reads at 30–60+ FPS without garbage collection pressure or heap allocations.
- **Real-Time 16-Band Spectrum Analyzer**: Fast FFT-based logarithmic frequency analysis (20 Hz – 20 kHz), Hann windowing, sample-rate adaptive FFT windows (2048 to 8192 points), ANSI fractional-octave energy aggregation, spectral tilt compensation (+0.5 dB/band), and smooth attack/decay ballistics.
- **LMS UDP Auto-Discovery**: Automatically locates Logitech Media Server instances on the local network (IPv4 UDP broadcast `e/E` probe).
- **Intelligent AutoSync & Sync Group Master Resolution**: Automatically slaves the virtual player to any active physical player or sync group in the house. When targeting a player that is part of a sync group, `go-slimvu` automatically resolves and slaves to the **sync master** of that group, dynamically tracking playlist changes and room migrations.
- **Direct Playback Command Forwarding**: Play, pause, previous, and next track commands are forwarded directly to the currently synced-to physical player or sync master.
- **Rich Terminal UI (`slimvu`)**:
  - Real-time 60 FPS stereo RMS decibel meter with smooth peak-hold decay, smoothed human-readable text decibel readouts, and 8× sub-pixel block resolution (`▏` through `█`).
  - Real-time 16-band frequency spectrum analyzer with 16-step vertical block resolution (` ` through `█`), logarithmic frequency labels (25 Hz to 20 kHz), and smooth ballistics decay.
  - Seamless toggle (`t`) between the Stereo VU Meter and Spectrum Analyzer visualization modes.
  - Full-color album cover art thumbnail rendered via 2×2 Unicode quadrant sub-pixel clustering with automatic terminal cell aspect ratio compensation.
  - Interactive popup modal (`s`) for manual multi-room zone targeting.
  - Live metadata tracking (`Artist · Album · Title`, elapsed/total duration, track number) with marquee scrolling.

## Used By

- [**GoLEDS**](https://github.com/jtl5770/goleds) — A flexible concurrent lighting system and reactive LED strip controller that uses `go-slimvu` to drive live stereo RMS decibel visualizers, spectrum visualizers, and multi-room audio sync.

## Installation

```bash
go get github.com/jtl5770/go-slimvu
```

To install the `slimvu` TUI binary directly:

```bash
go install github.com/jtl5770/go-slimvu/cmd/slimvu@latest
```

## Running the Terminal UI (`slimvu`)

Launch `slimvu` to automatically discover your LMS server, synchronize to the currently playing room, and display the live stereo VU meter or spectrum analyzer with album artwork:

```bash
slimvu
```

### Keyboard Controls

| Key | Action |
| --- | --- |
| `t` | Toggle between Stereo VU Meter and 16-Band Spectrum Analyzer |
| `Space` | Toggle Play / Pause on the currently synced player |
| `←` / `→` | Previous / Next track on the currently synced player |
| `s` | Open interactive popup to manually select sync target |
| `a` | Toggle AutoSync automation on/off |
| `q` / `Ctrl+C` | Quit |

> **Note on Playback Controls**: Playback control commands (`Space`, `←`, `→`) are sent directly to the physical player (or sync group master) that `slimvu` is currently synced with, allowing you to control the active room directly from the terminal.

### CLI Options

```
Usage of slimvu:
  -server string
        LMS server host or IP (leave empty for UDP auto-discovery)
  -port int
        SlimProto port (default 3483 / auto-discovered)
  -rpc int
        JSON-RPC port (default 9000 / auto-discovered)
  -name string
        Squeezebox virtual player name (default "SlimVU")
  -mac string
        Player MAC address (default "auto")
  -sync
        Automatically sync to active player (default true)
  -cover
        Display album cover art thumbnail (auto-detected by default)
  -cell-aspect float
        Terminal character cell aspect ratio Height/Width (0.0 for auto-detect)
  -fps int
        UI refresh rate in FPS (default 60)
  -hold int
        Peak hold time in milliseconds (default 250)
  -decay float
        Peak decay rate in blocks/sec (default 20)
  -min-db float
        Minimum decibel level for scale (default -60)
  -max-db float
        Maximum decibel level for scale (default 0)
  -log string
        File path to write debug/info logs (disabled by default)
```

## Multi-Room Synchronization & Sync Groups

### Sync Master Resolution
When synchronizing to a player in Logitech Media Server:
- If the selected player is standalone, `go-slimvu` synchronizes directly to that player.
- If the selected player is part of an active LMS **sync group** (synchronized with other players), `go-slimvu` automatically resolves and synchronizes to the **sync master** (`sync_master`) of the group. This ensures that the virtual player reliably joins the group's common SlimProto broadcast stream and stays in perfect lockstep with all synchronized rooms.

### Playback Command Forwarding
All playback control methods (`Play()`, `TogglePause()`, `StopPlayback()`, `Next()`, `Previous()`) dynamically resolve the active sync target. When `go-slimvu` is synced to a zone, calling these methods sends JSON-RPC transport commands directly to the synced-to player or group master, allowing remote control of the physical audio playback.

## LMS Group Players Plugin

If you are using the LMS **Group Players** plugin (`LMS-Groups` by philippe44) to create virtual group players, external/virtual players like SlimVU can synchronize to the group master:
- In LMS Web UI, navigate to **Plugins -> Group Players**.
- Enable the option **"Synchronize to Group Players"**.
- With this enabled, SlimVU can slave directly to the Group Player entity and receive synced audio streams when the group is playing.

## Library SDK Guide

The `go-slimvu` package exposes a clean, high-level API designed for applications, LED controllers, displays, and audio monitors.

### Quick Start

```go
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/jtl5770/go-slimvu"
)

func main() {
	// Configure the provider. Leave Server empty for automatic UDP discovery.
	cfg := slimvu.Config{
		Server:     "",         // Empty string triggers UDP auto-discovery
		PlayerName: "VU Meter", // Display name in LMS
		PlayerMAC:  "auto",     // Automatically generates/derives a virtual MAC
		AutoSync:   true,       // Automatically sync to active playing zones
	}

	provider, err := slimvu.NewProvider(cfg)
	if err != nil {
		panic(err)
	}

	// Start() connects to LMS, begins SlimProto streaming, and initiates discovery
	if err := provider.Start(); err != nil {
		panic(err)
	}
	defer provider.Stop()

	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	// Pre-allocated destination buffer for 16-band spectrum data (0 allocations in loop)
	var spectrum [slimvu.SpectrumBandsCount]float32
	bars := []rune{' ', ' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	for range ticker.C {
		// 1. Read instantaneous stereo RMS decibel levels
		leftDB, rightDB, isPlaying := provider.GetLevels()
		if !isPlaying {
			continue
		}

		// 2. Read 16-band frequency spectrum (dBFS, lock-free, zero-allocation)
		numBands := provider.GetSpectrum(spectrum[:])

		prefix := ""
		if track, ok := provider.GetTrackInfo(); ok {
			prefix = fmt.Sprintf("[%s - %s]: ", track.Artist, track.Title)
		}

		// Format VU levels
		vuStr := fmt.Sprintf("L:%5.1f dB | R:%5.1f dB", leftDB, rightDB)

		// Format simple visualizer for the 16 frequency bands (-60 dBFS to 0 dBFS)
		var specBar strings.Builder
		for i := 0; i < numBands; i++ {
			val := spectrum[i]
			idx := int((val + 60.0) / 60.0 * float32(len(bars)-1))
			if idx < 0 {
				idx = 0
			} else if idx >= len(bars) {
				idx = len(bars) - 1
			}
			specBar.WriteRune(bars[idx])
		}

		fmt.Printf("%s%s | Spectrum: %s\n", prefix, vuStr, specBar.String())
	}
}
```

### Configuration Options

```go
type Config struct {
    Server         string        // LMS hostname or IP (empty = UDP auto-discovery)
    SlimProtoPort  int           // SlimProto port (0 = auto-discover / default 3483)
    JSONRPCPort    int           // JSON-RPC port (0 = auto-discover / default 9000)
    PlayerName     string        // Name reported to LMS (default: "SlimVU")
    PlayerMAC      string        // MAC address string, or "auto"
    AutoSync       bool          // Automatically slave to active playing rooms
    IgnoredPlayers []string      // Names/MACs to exclude from AutoSync targeting
    PollInterval   time.Duration // LMS status poll interval (default: 500ms)
}
```

### Core Interfaces & Types

- **`slimvu.AudioProvider`**: Composite interface uniting `slimvu.LevelsProvider`, `slimvu.SpectrumProvider`, and lifecycle control (`Start()`, `Stop()`).
- **`slimvu.LevelsProvider`**: Exposes `GetLevels() (leftDB, rightDB float64, playing bool)`.
- **`slimvu.SpectrumProvider`**: Exposes `GetSpectrum(dst []float32) int`.
- **`slimvu.SpectrumBandsCount`**: Constant defining the 16 logarithmic frequency bands (`20 Hz` to `20 kHz`).
- **`slimvu.AtomicLevels`**: Packed 64-bit atomic integer container guaranteeing zero-allocation, lock-free level reads and writes.
- **`slimvu.AtomicSpectrum`**: Atomic 32-bit float array container guaranteeing zero-allocation, lock-free 16-band spectrum reads and writes.

### Full API Reference

#### Core Lifecycle & Metering
- **`provider.Start() error`**  
  Starts background workers, connects to LMS over SlimProto, and performs the initial player discovery. *Must be called prior to querying levels or player state.*
- **`provider.Stop() error`**  
  Gracefully unsyncs from any active sync group, closes the SlimProto audio connection, and stops all background workers.
- **`provider.GetLevels() (leftDB, rightDB float64, playing bool)`**  
  Lock-free, zero-allocation read of instantaneous stereo audio levels (in dBFS, e.g. `-100.0 dB` silence up to `0.0 dB` full-scale).
- **`provider.GetSpectrum(dst []float32) int`**  
  Lock-free, zero-allocation read of instantaneous 16-band frequency spectrum levels (in dBFS, e.g. `-100.0 dBFS` silence up to `0.0 dBFS` full-scale). Copies up to 16 bands into `dst` and returns the number of bands copied.

#### Player Discovery & Status
- **`provider.GetAllPlayers() []control.PlayerStatus`**  
  Returns a snapshot of all external physical and group players currently connected to LMS (virtual SlimVU instances are automatically filtered). Automatically updates in real time when players disconnect or power down.
- **`provider.GetOurPlayer() control.PlayerStatus`**  
  Returns the current status of the local virtual player.
- **`provider.SyncedWith() (mac, name string)`**  
  Returns the MAC address and friendly name of the master player SlimVU is currently slaved to (or `("", "")` if standalone).
- **`provider.GetTrackInfo() (control.TrackInfo, bool)`**  
  Returns metadata for the currently playing track (`Title`, `Artist`, `Album`, `Duration`, `Elapsed`, `CoverID`, `ArtworkURL`, etc.).

#### Multi-Room Zone Synchronization
- **`provider.SyncTo(target string)`**  
  Manually syncs the virtual player to a specific target player (by friendly name or MAC address). If the target player belongs to an LMS sync group, `go-slimvu` automatically synchronizes to the group's `sync_master`.
- **`provider.Unsync()`**  
  Detaches SlimVU from its current sync group.
- **`provider.SetAutoSync(enabled bool)`** / **`provider.GetAutoSync() bool`**  
  Dynamically enables or disables automatic zone following.

#### Playback Controls & Media Artwork
- **`provider.Play(ctx context.Context) error`**  
  Sends the play command to the currently synced target player / sync master.
- **`provider.TogglePause(ctx context.Context) error`**  
  Toggles play / pause state on the currently synced target player / sync master.
- **`provider.StopPlayback(ctx context.Context) error`**  
  Stops playback on the currently synced target player / sync master.
- **`provider.Next(ctx context.Context) error`**  
  Skips to the next track on the currently synced target player / sync master.
- **`provider.Previous(ctx context.Context) error`**  
  Restarts the current track or skips to the previous track on the currently synced target player / sync master.
- **`provider.GetArtwork(ctx context.Context, artworkURL, coverID string) ([]byte, error)`**  
  Fetches raw JPEG/PNG cover artwork image bytes directly from LMS.
- **`provider.GetServerInfo() (host string, slimProtoPort, jsonRPCPort int)`**  
  Returns the resolved server host and network ports.

## Running Tests

```bash
go test -v -race ./...
```

## Acknowledgments

- Special thanks to the [**Squeezelite**](https://github.com/ralph-irving/squeezelite) project (by Adrian Smith and Ralph Irving). The SlimProto network state machine, sample pacing calculations, and protocol implementation details in this project were inspired by and modeled after their pioneering C codebase.
- Built for the [Logitech Media Server / Lyrion Music Server](https://lyrion.org/) ecosystem.
- Terminal UI powered by [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss).
- Audio decoding powered by `mewkiz/flac`, `hajimehoshi/go-mp3`, `skrashevich/go-aac`, `jfreymuth/oggvorbis`, and `pion/opus`.

## License

LGPL-3.0 License. See [COPYING.LESSER](COPYING.LESSER) and [COPYING](COPYING) for details.
