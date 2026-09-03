# go-slimvu

High-performance, pure Go virtual Squeezebox / Logitech Media Server (LMS) audio level provider and VU meter engine.

`go-slimvu` emulates a hardware Squeezebox player over the **SlimProto** protocol, decodes incoming audio streams in real time with high-precision sample pacing, and exposes lock-free, zero-allocation left/right stereo RMS decibel levels for LED visualizers, displays, and audio monitors.

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

## Installation

```bash
go get github.com/jtl5770/go-slimvu
```

## Quick Start

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

	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
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

## License

GPL-3.0 License.
