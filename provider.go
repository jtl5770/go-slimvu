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

package slimvu

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/jtl5770/go-slimvu/control"
	"github.com/jtl5770/go-slimvu/discovery"
	"github.com/jtl5770/go-slimvu/slimproto"
)

// SqueezeboxAudioProvider implements AudioProvider using SlimProto and LMS JSON-RPC.
type SqueezeboxAudioProvider struct {
	levels   *AtomicLevels
	proto    *slimproto.Client
	autoSync *control.AutoSyncManager
}

// Provider is an alias for SqueezeboxAudioProvider.
type Provider = SqueezeboxAudioProvider

// Config holds configuration parameters for the Squeezebox audio provider.
type Config struct {
	Server         string        `yaml:"Server"`
	SlimProtoPort  int           `yaml:"SlimProtoPort"`
	JSONRPCPort    int           `yaml:"JSONRPCPort"`
	PlayerMAC      string        `yaml:"PlayerMAC"`
	PlayerName     string        `yaml:"PlayerName"`
	IgnoredPlayers []string      `yaml:"IgnoredPlayers"`
	AutoSync       bool          `yaml:"AutoSync"`
	PollInterval   time.Duration `yaml:"PollInterval"`
}

// GeneratePlayerMAC derives a Squeezebox MAC address in the format 00:04:20:ee:Y:Z
// where Y and Z are derived from the host's primary network interface hardware address.
func GeneratePlayerMAC() net.HardwareAddr {
	var hostY, hostZ byte = 0x12, 0x34

	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			// Find first active, non-loopback interface with a valid MAC
			if iface.Flags&net.FlagLoopback == 0 && iface.Flags&net.FlagUp != 0 && len(iface.HardwareAddr) >= 6 {
				hostY = iface.HardwareAddr[len(iface.HardwareAddr)-2]
				hostZ = iface.HardwareAddr[len(iface.HardwareAddr)-1]
				break
			}
		}
	} else if hostname, err := os.Hostname(); err == nil {
		hash := sha256.Sum256([]byte(hostname))
		hostY = hash[0]
		hostZ = hash[1]
	}

	return net.HardwareAddr{0x00, 0x04, 0x20, 0xee, hostY, hostZ}
}

// NewProvider creates an initialized SqueezeboxAudioProvider.
// If cfg.Server is empty, it uses modern UDP auto-discovery to locate LMS, ignoring any configured ports.
// If cfg.Server is specified, it connects to that host and uses the given ports or defaults.
func NewProvider(cfg Config) (*SqueezeboxAudioProvider, error) {
	var serverHost string
	var slimProtoPort int
	var jsonrpcPort int

	cleanServer := strings.TrimSpace(cfg.Server)
	if cleanServer == "" {
		slog.Info("Squeezebox Server not configured, initiating UDP auto-discovery...")
		discCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		discovered, err := discovery.DiscoverServer(discCtx, 3*time.Second)
		if err != nil {
			return nil, fmt.Errorf("squeezebox auto-discovery: %w", err)
		}
		serverHost = discovered.Host
		slimProtoPort = discovered.SlimProtoPort
		jsonrpcPort = discovered.JSONRPCPort
		slog.Info("Using auto-discovered LMS server",
			"name", discovered.Name,
			"host", serverHost,
			"slimproto_port", slimProtoPort,
			"jsonrpc_port", jsonrpcPort,
			"version", discovered.Version)
	} else {
		serverHost = cleanServer
		slimProtoPort = cfg.SlimProtoPort
		if slimProtoPort <= 0 {
			slimProtoPort = discovery.DefaultPort
		}
		jsonrpcPort = cfg.JSONRPCPort
		if jsonrpcPort <= 0 {
			jsonrpcPort = discovery.DefaultJSONRPCPort
		}
		slog.Info("Using statically configured LMS server",
			"host", serverHost,
			"slimproto_port", slimProtoPort,
			"jsonrpc_port", jsonrpcPort)
	}

	if cfg.PlayerName == "" {
		cfg.PlayerName = "SlimVU"
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = 1000 * time.Millisecond
	}

	var mac net.HardwareAddr
	cleanMAC := strings.TrimSpace(cfg.PlayerMAC)
	if cleanMAC == "" || strings.EqualFold(cleanMAC, "auto") {
		mac = GeneratePlayerMAC()
	} else {
		var err error
		mac, err = net.ParseMAC(cleanMAC)
		if err != nil {
			mac = GeneratePlayerMAC()
		}
	}

	levels := NewAtomicLevels()
	serverSlim := fmt.Sprintf("%s:%d", serverHost, slimProtoPort)
	helo := slimproto.HeloConfig{
		MAC:        mac,
		DeviceID:   12, // SqueezePlay / SqueezeSlave
		Revision:   1,
		PlayerName: cfg.PlayerName,
	}

	protoClient := slimproto.NewClient(serverSlim, helo, levels)

	var autoSyncMgr *control.AutoSyncManager
	if cfg.AutoSync {
		lmsClient := control.NewLMSClient(serverHost, jsonrpcPort)
		syncConfig := control.AutoSyncConfig{
			OurMAC:         mac.String(),
			OurName:        cfg.PlayerName,
			IgnoredPlayers: cfg.IgnoredPlayers,
			PollInterval:   pollInterval,
		}
		autoSyncMgr = control.NewAutoSyncManager(lmsClient, syncConfig)
	}

	return &SqueezeboxAudioProvider{
		levels:   levels,
		proto:    protoClient,
		autoSync: autoSyncMgr,
	}, nil
}

// NewSqueezeboxAudioProvider is an alias for NewProvider for backward compatibility.
func NewSqueezeboxAudioProvider(cfg Config) (*SqueezeboxAudioProvider, error) {
	return NewProvider(cfg)
}

// GetLevels returns the latest left/right dB levels atomically (0 allocs).
func (s *SqueezeboxAudioProvider) GetLevels() (leftDB, rightDB float64, playing bool) {
	return s.levels.Get()
}

// Start starts the SlimProto client and AutoSyncManager.
func (s *SqueezeboxAudioProvider) Start() error {
	if err := s.proto.Start(); err != nil {
		return fmt.Errorf("start slimproto client: %w", err)
	}
	if s.autoSync != nil {
		s.autoSync.Start()
	}
	return nil
}

// Stop stops the AutoSyncManager and closes SlimProto connection.
func (s *SqueezeboxAudioProvider) Stop() error {
	if s.autoSync != nil {
		s.autoSync.Stop()
	}
	return s.proto.Stop()
}

// SyncedWith returns the MAC address and name of the active LMS player currently synced with.
func (s *SqueezeboxAudioProvider) SyncedWith() (mac, name string) {
	if s.autoSync == nil {
		return "", ""
	}
	return s.autoSync.SyncedWith()
}

// TrackInfo is an alias for control.TrackInfo.
type TrackInfo = control.TrackInfo

// GetTrackInfo returns the current TrackInfo of the active synchronized player, if available.
func (s *SqueezeboxAudioProvider) GetTrackInfo() (TrackInfo, bool) {
	if s.autoSync == nil {
		return TrackInfo{}, false
	}
	return s.autoSync.SyncedTrack()
}

// GetArtwork fetches the image bytes for a track artwork URL or current player cover.
func (s *SqueezeboxAudioProvider) GetArtwork(ctx context.Context, artworkURL, coverID string) ([]byte, error) {
	if s.autoSync == nil {
		return nil, fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.GetArtwork(ctx, artworkURL, coverID)
}

// Next skips to the next track on the synchronized master player.
func (s *SqueezeboxAudioProvider) Next(ctx context.Context) error {
	if s.autoSync == nil {
		return fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.Next(ctx)
}

// Previous skips to the previous track on the synchronized master player.
func (s *SqueezeboxAudioProvider) Previous(ctx context.Context) error {
	if s.autoSync == nil {
		return fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.Previous(ctx)
}

// TogglePause toggles playback/pause on the synchronized master player.
func (s *SqueezeboxAudioProvider) TogglePause(ctx context.Context) error {
	if s.autoSync == nil {
		return fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.TogglePause(ctx)
}

// Play starts playback on the synchronized master player.
func (s *SqueezeboxAudioProvider) Play(ctx context.Context) error {
	if s.autoSync == nil {
		return fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.Play(ctx)
}

// StopPlayback stops playback on the synchronized master player.
func (s *SqueezeboxAudioProvider) StopPlayback(ctx context.Context) error {
	if s.autoSync == nil {
		return fmt.Errorf("autosync not enabled")
	}
	return s.autoSync.StopPlayback(ctx)
}
