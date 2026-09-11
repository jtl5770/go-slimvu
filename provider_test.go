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
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/jtl5770/go-slimvu/control"
)

func TestGeneratePlayerMAC(t *testing.T) {
	mac := GeneratePlayerMAC()
	if len(mac) != 6 {
		t.Fatalf("Expected 6 bytes MAC, got %d", len(mac))
	}
	if mac[0] != 0x00 || mac[1] != 0x04 || mac[2] != 0x20 || mac[3] != 0xee {
		t.Errorf("Expected prefix 00:04:20:ee, got %s", mac.String())
	}
}

func TestSqueezeboxAudioProvider_ExplicitHost(t *testing.T) {
	cfg := Config{
		Server:        "127.0.0.1",
		SlimProtoPort: 3483,
		JSONRPCPort:   9000,
		PlayerMAC:     "auto",
		PlayerName:    "Test VU",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Expected successful creation with explicit host, got: %v", err)
	}
	if provider == nil {
		t.Fatal("Expected provider not nil")
	}

	left, right, playing := provider.GetLevels()
	if left != -100 || right != -100 || playing {
		t.Errorf("Expected initial levels -100/-100 false, got %f/%f %v", left, right, playing)
	}

	var bandsL, bandsR [SpectrumBandsCount]float32
	n := provider.GetSpectrum(bandsL[:], bandsR[:])
	if n != SpectrumBandsCount {
		t.Errorf("Expected %d spectrum bands copied, got %d", SpectrumBandsCount, n)
	}
	for b := 0; b < SpectrumBandsCount; b++ {
		if bandsL[b] != -100.0 {
			t.Errorf("Left Band %d: expected -100.0 initial silence, got %.2f", b, bandsL[b])
		}
		if bandsR[b] != -100.0 {
			t.Errorf("Right Band %d: expected -100.0 initial silence, got %.2f", b, bandsR[b])
		}
	}

	// Test spectrum enable/disable
	if provider.IsSpectrumEnabled() {
		t.Error("Expected spectrum to be disabled initially")
	}
	provider.SetSpectrumEnabled(true)
	if !provider.IsSpectrumEnabled() {
		t.Error("Expected spectrum to be enabled after SetSpectrumEnabled(true)")
	}
	provider.SetSpectrumEnabled(false)
	if provider.IsSpectrumEnabled() {
		t.Error("Expected spectrum to be disabled after SetSpectrumEnabled(false)")
	}
}

func TestSqueezeboxAudioProvider_ExplicitHost_DefaultPorts(t *testing.T) {
	cfg := Config{
		Server: "192.168.1.100",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Expected success with default ports, got: %v", err)
	}
	if provider == nil {
		t.Fatal("Expected provider not nil")
	}
}

func TestSqueezeboxAudioProvider_StartStopLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind mock SlimProto: %v", err)
	}
	defer ln.Close()

	tcpAddr := ln.Addr().(*net.TCPAddr)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := control.JSONRPCResponse{
			Result: []byte(`{"version": "8.3.0"}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())

	cfg := Config{
		Server:        u.Hostname(),
		SlimProtoPort: tcpAddr.Port,
		JSONRPCPort:   port,
		PlayerMAC:     "00:04:20:aa:bb:cc",
		PlayerName:    "LifecyclePlayer",
		AutoSync:      true,
		PollInterval:  50 * time.Millisecond,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("NewProvider failed: %v", err)
	}

	if err := provider.Start(); err != nil {
		t.Fatalf("Provider Start failed: %v", err)
	}

	if !provider.GetAutoSync() {
		t.Error("Expected AutoSync to be true")
	}
	provider.SetAutoSync(false)
	if provider.GetAutoSync() {
		t.Error("Expected AutoSync to be false")
	}

	provider.SyncTo("Kitchen")
	provider.Unsync()

	if err := provider.Stop(); err != nil {
		t.Fatalf("Provider Stop failed: %v", err)
	}
}

func TestSqueezeboxAudioProvider_PlayerDiscoveryAndMetadata(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to bind mock SlimProto: %v", err)
	}
	defer ln.Close()

	tcpAddr := ln.Addr().(*net.TCPAddr)
	ourMAC := "00:04:20:99:99:99"
	physicalMAC := "00:04:20:77:77:77"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req control.JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		player := ""
		if len(req.Params) > 0 {
			if p, ok := req.Params[0].(string); ok {
				player = p
			}
		}

		var cmd string
		if len(req.Params) > 1 {
			if cmds, ok := req.Params[1].([]interface{}); ok && len(cmds) > 0 {
				cmd = cmds[0].(string)
			}
		}

		switch cmd {
		case "players":
			resp := map[string]interface{}{
				"players_loop": []map[string]interface{}{
					{"playerid": ourMAC, "name": "SlimVU"},
					{"playerid": physicalMAC, "name": "Living Room"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(control.JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "SlimVU",
					"mode":        "stop",
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(control.JSONRPCResponse{Result: data})
				return
			}
			resp := map[string]interface{}{
				"playerid":      physicalMAC,
				"player_name":   "Living Room",
				"mode":          "play",
				"current_title": "Jazz Song",
				"playlist_loop": []map[string]interface{}{
					{"title": "Jazz Song", "artist": "Miles Davis", "album": "Kind of Blue"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(control.JSONRPCResponse{Result: data})
		}
	}))
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	port, _ := strconv.Atoi(u.Port())

	cfg := Config{
		Server:        u.Hostname(),
		SlimProtoPort: tcpAddr.Port,
		JSONRPCPort:   port,
		PlayerMAC:     ourMAC,
		PlayerName:    "SlimVU",
		AutoSync:      false,
		PollInterval:  20 * time.Millisecond,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	if err := provider.Start(); err != nil {
		t.Fatalf("Failed to start provider: %v", err)
	}
	defer provider.Stop()

	players := provider.GetAllPlayers()
	if len(players) != 1 || players[0].PlayerID != physicalMAC {
		t.Fatalf("Expected 1 player (%s), got: %v", physicalMAC, players)
	}
	if players[0].Name != "Living Room" {
		t.Fatalf("Expected Living Room, got: %s", players[0].Name)
	}
}

func BenchmarkSqueezeboxAudioProvider_GetSpectrum(b *testing.B) {
	cfg := Config{
		Server:        "127.0.0.1",
		SlimProtoPort: 3483,
		JSONRPCPort:   9000,
	}
	provider, err := NewProvider(cfg)
	if err != nil {
		b.Fatalf("Failed to create provider: %v", err)
	}

	var bufL, bufR [SpectrumBandsCount]float32

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = provider.GetSpectrum(bufL[:], bufR[:])
	}
}
