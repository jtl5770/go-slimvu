// Copyright (C) 2026 Jens Lautenbacher <jtl@gmx.com>
//
// This file is part of go-slimvu.
//
// go-slimvu is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-slimvu is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with go-slimvu.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jtl5770/go-slimvu"
)

func main() {
	defaultCover := isTerminalGraphicsSupported()

	server := flag.String("server", "", "LMS server host or IP (leave empty for UDP auto-discovery)")
	slimPort := flag.Int("port", 0, "SlimProto port (default 3483 / auto-discovered)")
	rpcPort := flag.Int("rpc", 0, "JSON-RPC port (default 9000 / auto-discovered)")
	name := flag.String("name", "SlimVU", "Squeezebox virtual player name")
	mac := flag.String("mac", "auto", "Player MAC address (or 'auto')")
	autoSync := flag.Bool("sync", true, "Automatically sync to active player")
	minDB := flag.Float64("min-db", -60.0, "Minimum decibel level for scale")
	maxDB := flag.Float64("max-db", 0.0, "Maximum decibel level for scale")
	fps := flag.Int("fps", 30, "UI refresh rate (FPS)")
	holdMS := flag.Int("hold", 250, "Peak hold time in milliseconds")
	decay := flag.Float64("decay", 20.0, "Peak decay rate (blocks/sec)")
	cover := flag.Bool("cover", defaultCover, "Display album cover art thumbnail (auto-detected by default)")
	cellAspect := flag.Float64("cell-aspect", 0.0, "Terminal character cell aspect ratio Height/Width (0.0 for auto-detect)")
	logPath := flag.String("log", "", "File path to write debug/info logs (disabled by default)")

	flag.Parse()

	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			defer f.Close()
			slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})))
		}
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	cfg := slimvu.Config{
		Server:        *server,
		SlimProtoPort: *slimPort,
		JSONRPCPort:   *rpcPort,
		PlayerName:    *name,
		PlayerMAC:     *mac,
		AutoSync:      *autoSync,
	}

	provider, err := slimvu.NewProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing provider: %v\n", err)
		os.Exit(1)
	}

	if err := provider.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting provider: %v\n", err)
		os.Exit(1)
	}
	defer provider.Stop()

	m := initialModel(provider, *minDB, *maxDB, *fps, time.Duration(*holdMS)*time.Millisecond, *decay, *cover, *cellAspect)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}
}
