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

package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAutoSyncManager_SelfIgnoreAndSync(t *testing.T) {
	var mu sync.Mutex
	syncCalled := false
	unsyncCalled := false
	ourMAC := "00:04:20:ee:12:34"
	targetMAC := "00:04:20:99:88:77"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{
						"playerid": ourMAC,
						"name":     "GoLEDs VU",
					},
					{
						"playerid": targetMAC,
						"name":     "Living Room",
					},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			mode := "stop"
			if player == targetMAC {
				mode = "play"
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": player,
				"mode":        mode,
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 {
					arg := cmds[1].(string)
					if arg == ourMAC && player == targetMAC {
						syncCalled = true
					}
					if arg == "-" && player == ourMAC {
						unsyncCalled = true
					}
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:         ourMAC,
		OurName:        "GoLEDs VU",
		AutoSync:       true,
		IgnoredPlayers: []string{},
		PollInterval:   20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if !syncCalled {
		t.Errorf("Expected SyncPlayer to be called on targetMAC with ourMAC")
	}
	mu.Unlock()

	mgr.Stop()

	mu.Lock()
	if !unsyncCalled {
		t.Errorf("Expected UnsyncPlayer to be called on Stop()")
	}
	mu.Unlock()
}

func TestAutoSyncManager_SwitchFromPausedToNewPlayer(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	player1MAC := "00:04:20:11:11:11"
	player2MAC := "00:04:20:22:22:22"

	player1Mode := "play"
	player2Mode := "stop"
	ourMaster := ""

	syncedTargets := make([]string, 0)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": ourMAC, "name": "GoLEDs VU"},
					{"playerid": player1MAC, "name": "Living Room"},
					{"playerid": player2MAC, "name": "Kitchen"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "GoLEDs VU",
					"mode":        "play",
					"sync_master": ourMaster,
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
				return
			}

			mode := "stop"
			if player == player1MAC {
				mode = player1Mode
			} else if player == player2MAC {
				mode = player2Mode
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": player,
				"mode":        mode,
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 {
					arg := cmds[1].(string)
					if arg == "-" && player == ourMAC {
						ourMaster = ""
					} else if arg == ourMAC {
						syncedTargets = append(syncedTargets, player)
						ourMaster = player
					}
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:         ourMAC,
		OurName:        "GoLEDs VU",
		AutoSync:       true,
		IgnoredPlayers: []string{},
		PollInterval:   20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	time.Sleep(60 * time.Millisecond)

	// Step 1: Verify it synced to Player 1 initially
	mac, _ := mgr.SyncedWith()
	if mac != player1MAC {
		t.Fatalf("Expected initial sync with Player 1 (%s), got: %s", player1MAC, mac)
	}

	// Step 2: Pause Player 1, start Player 2
	mu.Lock()
	player1Mode = "pause"
	player2Mode = "play"
	mu.Unlock()

	time.Sleep(60 * time.Millisecond)

	// Step 3: Verify it switched to Player 2
	mac, _ = mgr.SyncedWith()
	if mac != player2MAC {
		t.Fatalf("Expected AutoSync to switch to Player 2 (%s), got: %s", player2MAC, mac)
	}

	mgr.Stop()
}

func TestAutoSyncManager_PausedMaster_DoesNotUnsyncWhenNoOtherPlayer(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	player1MAC := "00:04:20:11:11:11"

	player1Mode := "play"
	ourMaster := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": ourMAC, "name": "GoLEDs VU"},
					{"playerid": player1MAC, "name": "Living Room"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "GoLEDs VU",
					"mode":        "play",
					"sync_master": ourMaster,
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
				return
			}

			mode := "stop"
			if player == player1MAC {
				mode = player1Mode
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": player,
				"mode":        mode,
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 {
					arg := cmds[1].(string)
					if arg == "-" && player == ourMAC {
						ourMaster = ""
					} else if arg == ourMAC {
						ourMaster = player
					}
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:         ourMAC,
		OurName:        "GoLEDs VU",
		AutoSync:       true,
		IgnoredPlayers: []string{},
		PollInterval:   20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	time.Sleep(60 * time.Millisecond)

	// Step 1: Verify it synced to Player 1 initially
	mac, _ := mgr.SyncedWith()
	if mac != player1MAC {
		t.Fatalf("Expected initial sync with Player 1 (%s), got: %s", player1MAC, mac)
	}

	// Step 2: Pause Player 1 (no other playing players available)
	mu.Lock()
	player1Mode = "pause"
	mu.Unlock()

	time.Sleep(60 * time.Millisecond)

	// Step 3: Verify it remains slaved to Player 1 (does not unsync)
	mac, _ = mgr.SyncedWith()
	if mac != player1MAC {
		t.Fatalf("Expected to stay slaved to paused Player 1 (%s), got: %s", player1MAC, mac)
	}

	mgr.Stop()
}

func TestPlayerManager_AutoSyncFalse_MaintainsManualState(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	player1MAC := "00:04:20:11:11:11"

	syncCalled := false
	unsyncCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": ourMAC, "name": "GoLEDs VU"},
					{"playerid": player1MAC, "name": "Living Room"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			resp := map[string]interface{}{
				"playerid":    player1MAC,
				"player_name": "Living Room",
				"mode":        "play",
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 {
					arg := cmds[1].(string)
					if arg == "-" {
						unsyncCalled = true
					} else if arg == ourMAC {
						syncCalled = true
					}
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:         ourMAC,
		OurName:        "GoLEDs VU",
		AutoSync:       false, // AutoSync is OFF
		IgnoredPlayers: []string{},
		PollInterval:   20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if syncCalled {
		t.Errorf("Expected AutoSync to NOT trigger when AutoSync=false")
	}
	mu.Unlock()

	mgr.Stop()

	mu.Lock()
	if unsyncCalled {
		t.Errorf("Expected Stop() NOT to issue UnsyncPlayer when AutoSync=false")
	}
	mu.Unlock()
}

func TestPlayerManager_SelfMasterGuard(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	slaveMAC := "00:04:20:99:99:99"
	unsyncCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": slaveMAC, "name": "Bedroom"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				// SlimVU is erroneously marked as sync master with slaveMAC slaved to it
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "SlimVU",
					"mode":        "play",
					"sync_slaves": slaveMAC, // We are master!
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
				return
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": "Bedroom",
				"mode":        "play",
				"sync_master": ourMAC,
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if player == ourMAC && len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 && cmds[1] == "-" {
					unsyncCalled = true
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       ourMAC,
		OurName:      "SlimVU",
		AutoSync:     false,
		PollInterval: 20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	if !unsyncCalled {
		t.Errorf("Expected self-master guard to issue unsync ('sync -') for our player")
	}
	mu.Unlock()

	mgr.Stop()
}

func TestPlayerManager_SlaveWithSyncSlaves_DoesNotSelfUnsync(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	masterMAC := "00:04:20:88:88:88"
	unsyncCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": masterMAC, "name": "Kitchen"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				// Slave player, but LMS includes sync_slaves containing group members
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "SlimVU",
					"mode":        "play",
					"sync_master": masterMAC,
					"sync_slaves": ourMAC,
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
				return
			}
			resp := map[string]interface{}{
				"playerid":    masterMAC,
				"player_name": "Kitchen",
				"mode":        "play",
				"sync_slaves": ourMAC,
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if player == ourMAC && len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 && cmds[1] == "-" {
					unsyncCalled = true
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       ourMAC,
		OurName:      "SlimVU",
		AutoSync:     true,
		PollInterval: 20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	if unsyncCalled {
		t.Errorf("Expected self-master guard NOT to trigger when our player has a valid sync_master")
	}
	mu.Unlock()

	mgr.Stop()
}

func TestPlayerManager_IgnoresVirtualAndAvoidListPlayers(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	virtualOtherMAC := "00:04:20:ee:99:99"
	ignoredMAC := "00:04:20:55:55:55"
	validMAC := "00:04:20:66:66:66"

	syncedTo := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": ourMAC, "name": "SlimVU 1"},
					{"playerid": virtualOtherMAC, "name": "SlimVU 2"},
					{"playerid": ignoredMAC, "name": "Ignored Room"},
					{"playerid": validMAC, "name": "Valid Room"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": player,
				"mode":        "play",
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 && cmds[1] == ourMAC {
					syncedTo = player
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:         ourMAC,
		OurName:        "SlimVU 1",
		AutoSync:       true,
		IgnoredPlayers: []string{ignoredMAC},
		PollInterval:   20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()
	time.Sleep(60 * time.Millisecond)

	// GetAllPlayers should NOT contain virtualOtherMAC, but SHOULD contain ignoredMAC and validMAC
	all := mgr.GetAllPlayers()
	for _, p := range all {
		if p.PlayerID == virtualOtherMAC {
			t.Errorf("GetAllPlayers should have filtered out virtual MAC %s", virtualOtherMAC)
		}
	}
	if len(all) != 2 {
		t.Fatalf("Expected 2 external players (ignored + valid), got: %d", len(all))
	}

	// AutoSync should have synced to validMAC, skipping virtualOtherMAC and ignoredMAC
	mu.Lock()
	if syncedTo != validMAC {
		t.Fatalf("Expected AutoSync to sync to %s, got: %s", validMAC, syncedTo)
	}
	mu.Unlock()

	mgr.Stop()
}

func TestPlayerManager_FilterUserSuppliedCustomMAC(t *testing.T) {
	// User supplies a custom hardware MAC not matching 00:04:20:ee
	customOurMAC := "b8:27:eb:11:22:33"
	physicalOtherMAC := "00:04:20:77:88:99"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

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
					{"playerid": customOurMAC, "name": "Custom SlimVU"},
					{"playerid": physicalOtherMAC, "name": "Living Room"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			player := ""
			if len(req.Params) > 0 {
				if p, ok := req.Params[0].(string); ok {
					player = p
				}
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": player,
				"mode":        "stop",
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       "B8-27-EB-11-22-33", // hyphenated and uppercase
		OurName:      "Custom SlimVU",
		AutoSync:     false,
		PollInterval: 20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()
	defer mgr.Stop()

	all := mgr.GetAllPlayers()

	if len(all) != 1 {
		t.Fatalf("Expected exactly 1 external player, got %d: %v", len(all), all)
	}
	if all[0].PlayerID != physicalOtherMAC {
		t.Fatalf("Expected external player %s, got: %s", physicalOtherMAC, all[0].PlayerID)
	}
}

func TestPlayerManager_ManualSyncAndUnsyncIntent(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	kitchenMAC := "00:04:20:11:22:33"
	bedroomMAC := "00:04:20:44:55:66"

	syncTarget := ""
	unsyncCalled := false
	ourMaster := ""

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": kitchenMAC, "name": "Kitchen"},
					{"playerid": bedroomMAC, "name": "Bedroom"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			if player == ourMAC {
				resp := map[string]interface{}{
					"playerid":    ourMAC,
					"player_name": "SlimVU",
					"mode":        "play",
					"sync_master": ourMaster,
				}
				data, _ := json.Marshal(resp)
				_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
				return
			}
			name := "Bedroom"
			if player == kitchenMAC {
				name = "Kitchen"
			}
			resp := map[string]interface{}{
				"playerid":    player,
				"player_name": name,
				"mode":        "stop",
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			if len(req.Params) > 1 {
				cmds := req.Params[1].([]interface{})
				if len(cmds) > 1 {
					arg := cmds[1].(string)
					if arg == "-" && player == ourMAC {
						unsyncCalled = true
						ourMaster = ""
					} else if arg == ourMAC {
						syncTarget = player
						ourMaster = player
					}
				}
			}
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       ourMAC,
		OurName:      "SlimVU",
		AutoSync:     false,
		PollInterval: 20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	// 1. Trigger manual sync intent by Name
	mgr.SyncTo("Kitchen")
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if syncTarget != kitchenMAC {
		t.Fatalf("Expected manual SyncTo to sync to Kitchen (%s), got: %s", kitchenMAC, syncTarget)
	}
	mu.Unlock()

	mac, name := mgr.SyncedWith()
	if mac != kitchenMAC || name != "Kitchen" {
		t.Fatalf("Expected SyncedWith to return (%s, Kitchen), got (%s, %s)", kitchenMAC, mac, name)
	}

	// 2. Trigger manual Unsync intent
	mgr.Unsync()
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if !unsyncCalled {
		t.Fatalf("Expected Unsync to call LMS unsync")
	}
	mu.Unlock()

	mac, name = mgr.SyncedWith()
	if mac != "" || name != "" {
		t.Fatalf("Expected SyncedWith to return empty strings after Unsync, got (%s, %s)", mac, name)
	}

	mgr.Stop()
}

func TestPlayerManager_ManualSync_NonPlayingRejectedWhenAutoSyncTrue(t *testing.T) {
	var mu sync.Mutex
	ourMAC := "00:04:20:ee:12:34"
	kitchenMAC := "00:04:20:11:22:33"
	syncCalled := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		defer mu.Unlock()

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
					{"playerid": kitchenMAC, "name": "Kitchen"},
				},
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "status":
			resp := map[string]interface{}{
				"playerid":    kitchenMAC,
				"player_name": "Kitchen",
				"mode":        "pause", // NOT playing!
			}
			data, _ := json.Marshal(resp)
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})

		case "sync":
			syncCalled = true
			_ = json.NewEncoder(w).Encode(JSONRPCResponse{})
		}
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       ourMAC,
		OurName:      "SlimVU",
		AutoSync:     true, // AutoSync is ACTIVE
		PollInterval: 20 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	mgr.Start()

	// Try to manually sync to paused Kitchen when AutoSync is true
	mgr.SyncTo("Kitchen")
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	if syncCalled {
		t.Errorf("Expected manual sync to non-playing room to be rejected when AutoSync is true")
	}
	mu.Unlock()

	mgr.Stop()
}

func TestPlayerManager_DynamicSetAutoSync(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"players_loop": []map[string]interface{}{},
		}
		data, _ := json.Marshal(resp)
		_ = json.NewEncoder(w).Encode(JSONRPCResponse{Result: data})
	}))
	defer ts.Close()

	client := &LMSClient{
		endpoint:   ts.URL,
		httpClient: ts.Client(),
	}

	cfg := Config{
		OurMAC:       "00:04:20:ee:12:34",
		OurName:      "SlimVU",
		AutoSync:     true,
		PollInterval: 100 * time.Millisecond,
	}

	mgr := NewPlayerManager(client, cfg)
	if !mgr.GetAutoSync() {
		t.Errorf("Expected GetAutoSync to return true initially")
	}

	mgr.SetAutoSync(false)
	if mgr.GetAutoSync() {
		t.Errorf("Expected GetAutoSync to return false after SetAutoSync(false)")
	}

	mgr.SetAutoSync(true)
	if !mgr.GetAutoSync() {
		t.Errorf("Expected GetAutoSync to return true after SetAutoSync(true)")
	}
}
