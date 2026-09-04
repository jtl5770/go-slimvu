# go-slimvu

High-performance, pure Go virtual Squeezebox / Logitech Media Server (LMS) audio level provider and VU meter engine.

![slimvu TUI](assets/screencast.gif)

`go-slimvu` emulates a hardware Squeezebox player over the **SlimProto** protocol, decodes incoming audio streams in real time with high-precision sample pacing, and exposes lock-free, zero-allocation left/right stereo RMS decibel levels for LED visualizers, displays, terminal visualizers, and audio monitors.

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
- **Zero-Allocation Level Metering**: Lock-free atomic snapshots (`AtomicLevels`) for real-time reads at 30–60+ FPS without garbage collection pressure.
- **LMS UDP Auto-Discovery**: Automatically locates Logitech Media Server instances on the local network (IPv4 UDP broadcast `e/E` probe).
- **Intelligent AutoSync**: Automatically queries LMS via JSON-RPC to slave the virtual VU player to any currently playing physical player in the house, following playlist changes and room migrations dynamically.
- **Rich Terminal UI (`slimvu`)**:
  - Real-time 60 FPS stereo RMS decibel meter with smooth peak-hold decay.
  - Full-color album cover art thumbnail rendered via 2×2 Unicode quadrant sub-pixel clustering with terminal cell aspect ratio compensation.
  - Live metadata tracking (`Artist · Album · Title`, elapsed/total duration, track number) with marquee scrolling.

## Used By

- [**GoLEDS**](https://github.com/jtl5770/goleds) — A flexible concurrent lighting system and reactive LED strip controller that uses `go-slimvu` to drive live stereo RMS decibel visualizers and multi-room audio sync.

## Installation

```bash
go get github.com/jtl5770/go-slimvu
```

To install the `slimvu` TUI binary directly:

```bash
go install github.com/jtl5770/go-slimvu/cmd/slimvu@latest
```

## Running the Terminal UI (`slimvu`)

Launch `slimvu` to automatically discover your LMS server, synchronize to the currently playing room, and display the live stereo VU meter with album artwork:

```bash
slimvu
```

### CLI Options

```
Usage of slimvu:
  -server string
        LMS server host or IP (leave empty for UDP auto-discovery)
  -sync
        Automatically sync to active player (default true)
  -cover
        Display album cover art thumbnail (default true)
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
```

## Library Quick Start

```go
package main

import (
	"fmt"
	"time"

	"github.com/jtl5770/go-slimvu"
)

func main() {
	// Configure the provider. Leave Server empty for automatic UDP discovery.
	cfg := slimvu.Config{
		Server:     "",         // Empty string triggers auto-discovery
		PlayerName: "VU Meter", // Display name in LMS
		PlayerMAC:  "auto",     // Automatically derives hardware MAC address
		AutoSync:   true,       // Automatically sync to active playing zones
	}

	provider, err := slimvu.NewProvider(cfg)
	if err != nil {
		panic(err)
	}

	if err := provider.Start(); err != nil {
		panic(err)
	}
	defer provider.Stop()

	ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
	defer ticker.Stop()

	for range ticker.C {
		leftDB, rightDB, isPlaying := provider.GetLevels()
		if isPlaying {
			fmt.Printf("L: %6.1f dB | R: %6.1f dB\n", leftDB, rightDB)
		}
	}
}
```

## Running Tests

```bash
go-task test
# or
go test -v -race ./...
```

## Acknowledgments

Special thanks to the [**Squeezelite**](https://github.com/ralph-irving/squeezelite) project (by Adrian Smith and Ralph Irving). The SlimProto network state machine, sample pacing calculations, and protocol implementation details in this project were inspired by and modeled after their pioneering C codebase.

## License

LGPL-3.0 License. See [COPYING.LESSER](COPYING.LESSER) and [COPYING](COPYING) for details.
