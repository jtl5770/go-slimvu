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
		t.Fatalf("Failed to create provider: %v", err)
	}

	leftDB, rightDB, playing := provider.GetLevels()
	if playing {
		t.Errorf("Expected initial playing to be false")
	}
	if leftDB != -100 || rightDB != -100 {
		t.Errorf("Expected initial levels to be -100, got %f, %f", leftDB, rightDB)
	}
}

func TestSqueezeboxAudioProvider_ExplicitHost_DefaultPorts(t *testing.T) {
	cfg := Config{
		Server:     "127.0.0.1",
		PlayerMAC:  "00:04:20:11:22:33",
		PlayerName: "Test VU",
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	if provider.proto == nil {
		t.Fatalf("Expected proto client to be initialized")
	}
}

func TestSqueezeboxAudioProvider_StartStopLifecycle(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	tcpAddr := ln.Addr().(*net.TCPAddr)

	cfg := Config{
		Server:        "127.0.0.1",
		SlimProtoPort: tcpAddr.Port,
		JSONRPCPort:   9000,
		PlayerMAC:     "00:04:20:ee:11:22",
		PlayerName:    "Test Lifecycle",
		AutoSync:      false,
		PollInterval:  50 * time.Millisecond,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			buf := make([]byte, 256)
			_, _ = conn.Read(buf)
		}
	}()

	if err := provider.Start(); err != nil {
		t.Fatalf("Failed to start provider: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	if err := provider.Stop(); err != nil {
		t.Fatalf("Failed to stop provider: %v", err)
	}
}

func TestSqueezeboxAudioProvider_PlayerDiscoveryAndMetadata(t *testing.T) {
	ourMAC := "00:04:20:ee:88:99"
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
		Server:       u.Hostname(),
		JSONRPCPort:  port,
		PlayerMAC:    ourMAC,
		PlayerName:   "SlimVU",
		AutoSync:     false,
		PollInterval: 20 * time.Millisecond,
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	players := provider.GetAllPlayers()
	if len(players) != 1 || players[0].PlayerID != physicalMAC {
		t.Fatalf("Expected 1 player (%s), got: %v", physicalMAC, players)
	}
	if players[0].Name != "Living Room" {
		t.Fatalf("Expected Living Room, got: %s", players[0].Name)
	}
}
