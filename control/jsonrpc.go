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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// JSONRPCRequest represents a request to the LMS JSON-RPC API.
type JSONRPCRequest struct {
	ID     int           `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// JSONRPCResponse represents a generic response from LMS JSON-RPC.
type JSONRPCResponse struct {
	ID     int             `json:"id"`
	Method string          `json:"method"`
	Params []interface{}   `json:"params"`
	Result json.RawMessage `json:"result"`
}

// PlayerInfo holds basic info about a discovered player on LMS.
type PlayerInfo struct {
	PlayerID  string `json:"playerid"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	IsPlayer  int    `json:"isplayer"`
	Connected int    `json:"connected"`
	IP        string `json:"ip"`
}

// PlaylistTrack represents a single track item in playlist_loop.
type PlaylistTrack struct {
	Title      string      `json:"title"`
	Artist     string      `json:"artist"`
	Album      string      `json:"album"`
	TrackNum   interface{} `json:"tracknum"`
	Duration   interface{} `json:"duration"`
	ArtworkURL string      `json:"artwork_url"`
	CoverID    string      `json:"coverid"`
}

// TrackInfo holds normalized metadata for the currently active track.
type TrackInfo struct {
	Title         string  `json:"title"`
	Artist        string  `json:"artist"`
	Album         string  `json:"album"`
	TrackNum      int     `json:"track_num"`      // Track number from album tags (if available)
	PlaylistIndex int     `json:"playlist_index"` // 1-based index in the active playlist
	PlaylistTotal int     `json:"playlist_total"` // Total count of tracks in the active playlist
	TotalTracks   int     `json:"total_tracks"`   // Backwards-compatible alias for PlaylistTotal
	Elapsed       float64 `json:"elapsed"`
	Duration      float64 `json:"duration"`
	IsLiveStream  bool    `json:"is_live_stream"`
	ArtworkURL    string  `json:"artwork_url"`
	CoverID       string  `json:"coverid"`
}

// PlayerStatus holds the playback state and metadata of a player.
type PlayerStatus struct {
	PlayerID         string          `json:"playerid"`
	Name             string          `json:"player_name"`
	Mode             string          `json:"mode"`        // "play", "pause", "stop"
	Power            int             `json:"power"`       // 1 = on, 0 = off
	Connected        int             `json:"player_connected"`
	SyncMaster       string          `json:"sync_master"` // Master MAC if synced as slave
	SyncSlaves       string          `json:"sync_slaves"` // Comma-separated slave MACs if master
	Time             interface{}     `json:"time"`        // Current track playback position in seconds
	Duration         interface{}     `json:"duration"`    // Track duration in seconds
	PlaylistCurIndex interface{}     `json:"playlist_cur_index"`
	PlaylistTracks   interface{}     `json:"playlist_tracks"`
	RemoteTitle      string          `json:"remote_title"`
	CurrentTitle     string          `json:"current_title"`
	ArtworkURL       string          `json:"artwork_url"`
	CoverID          string          `json:"coverid"`
	PlaylistLoop     []PlaylistTrack `json:"playlist_loop"`
}

func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f
	case json.Number:
		f, _ := val.Float64()
		return f
	}
	return 0
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		var i int
		fmt.Sscanf(val, "%d", &i)
		return i
	case json.Number:
		i, _ := val.Int64()
		return int(i)
	}
	return 0
}

// GetTrackInfo extracts and normalizes the current track metadata from PlayerStatus.
func (s *PlayerStatus) GetTrackInfo() TrackInfo {
	info := TrackInfo{
		Elapsed:       toFloat(s.Time),
		Duration:      toFloat(s.Duration),
		PlaylistTotal: toInt(s.PlaylistTracks),
		TotalTracks:   toInt(s.PlaylistTracks),
		ArtworkURL:    s.ArtworkURL,
		CoverID:       s.CoverID,
	}

	if s.PlaylistCurIndex != nil {
		info.PlaylistIndex = toInt(s.PlaylistCurIndex) + 1
		info.TrackNum = info.PlaylistIndex
	}

	if len(s.PlaylistLoop) > 0 {
		track := s.PlaylistLoop[0]
		info.Title = track.Title
		info.Artist = track.Artist
		info.Album = track.Album
		if info.Duration <= 0 {
			info.Duration = toFloat(track.Duration)
		}
		if track.TrackNum != nil {
			info.TrackNum = toInt(track.TrackNum)
		}
		if info.ArtworkURL == "" {
			info.ArtworkURL = track.ArtworkURL
		}
		if info.CoverID == "" {
			info.CoverID = track.CoverID
		}
	}

	if info.Title == "" {
		if s.CurrentTitle != "" {
			info.Title = s.CurrentTitle
		} else if s.RemoteTitle != "" {
			info.Title = s.RemoteTitle
		}
	}

	if info.Duration <= 0 && (s.RemoteTitle != "" || s.CurrentTitle != "") {
		info.IsLiveStream = true
	}

	return info
}

// IsPlaying reports whether this player is actively playing audio.
func (s *PlayerStatus) IsPlaying() bool {
	return s != nil && s.Mode == "play"
}

// IsPaused reports whether this player is currently paused.
func (s *PlayerStatus) IsPaused() bool {
	return s != nil && s.Mode == "pause"
}

// IsStopped reports whether this player is stopped.
func (s *PlayerStatus) IsStopped() bool {
	return s == nil || s.Mode == "stop" || s.Mode == ""
}

// IsSyncMaster reports whether this player is leading a sync group
// (has non-empty sync_slaves and is not slaved to another player).
func (s *PlayerStatus) IsSyncMaster() bool {
	if s == nil {
		return false
	}
	cleanSlaves := strings.TrimSpace(s.SyncSlaves)
	if cleanSlaves == "" || cleanSlaves == "-" {
		return false
	}
	return s.SyncMaster == "" || MatchMAC(s.SyncMaster, s.PlayerID)
}

// IsSlaved reports whether this player is actively slaved to a master player.
func (s *PlayerStatus) IsSlaved() bool {
	if s == nil {
		return false
	}
	return s.SyncMaster != "" && !MatchMAC(s.SyncMaster, s.PlayerID)
}

// IsStandalone reports whether this player is independent (neither master nor slave).
func (s *PlayerStatus) IsStandalone() bool {
	if s == nil {
		return true
	}
	return !s.IsSyncMaster() && !s.IsSlaved()
}

// Matches reports whether this player matches the given identifier by MAC or Name.
func (s *PlayerStatus) Matches(identifier string) bool {
	if s == nil || identifier == "" {
		return false
	}
	return MatchMAC(s.PlayerID, identifier) || strings.EqualFold(s.Name, identifier)
}

// LMSClient provides methods to query and control LMS via JSON-RPC.
type LMSClient struct {
	host       string
	port       int
	endpoint   string
	httpClient *http.Client
}

// NewLMSClient creates an LMS JSON-RPC client.
func NewLMSClient(host string, port int) *LMSClient {
	if port <= 0 {
		port = 9000
	}
	return &LMSClient{
		host:     host,
		port:     port,
		endpoint: fmt.Sprintf("http://%s:%d/jsonrpc.js", host, port),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *LMSClient) call(ctx context.Context, playerID string, command []interface{}, target interface{}) error {
	reqBody := JSONRPCRequest{
		ID:     1,
		Method: "slim.request",
		Params: []interface{}{playerID, command},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal json-rpc: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create http req: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do json-rpc req: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("json-rpc error HTTP %d: %s", resp.StatusCode, string(body))
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return fmt.Errorf("decode json-rpc resp: %w", err)
	}

	if target != nil && len(rpcResp.Result) > 0 {
		if err := json.Unmarshal(rpcResp.Result, target); err != nil {
			return fmt.Errorf("unmarshal result into target: %w", err)
		}
	}

	return nil
}

// SetPlayerPref sets a player-specific preference on LMS.
func (c *LMSClient) SetPlayerPref(ctx context.Context, playerMAC, pref, value string) error {
	cmd := []interface{}{"playerpref", pref, value}
	return c.call(ctx, playerMAC, cmd, nil)
}

// GetPlayers retrieves the list of all connected players on LMS.
func (c *LMSClient) GetPlayers(ctx context.Context) ([]PlayerInfo, error) {
	var result struct {
		PlayersLoop []PlayerInfo `json:"players_loop"`
	}

	cmd := []interface{}{"players", 0, 100}
	if err := c.call(ctx, "", cmd, &result); err != nil {
		return nil, err
	}
	return result.PlayersLoop, nil
}

// GetPlayerStatus gets the current status ("play", "pause", "stop") and track info of a player by MAC.
func (c *LMSClient) GetPlayerStatus(ctx context.Context, playerMAC string) (*PlayerStatus, error) {
	var result PlayerStatus
	// tags: a (artist), c (cover), d (duration), t (tracknum), u (url/stream), y (year), l (album), J (artwork_url)
	cmd := []interface{}{"status", "-", 1, "tags:acdtuylJ"}
	if err := c.call(ctx, playerMAC, cmd, &result); err != nil {
		return nil, err
	}
	if result.PlayerID == "" {
		result.PlayerID = playerMAC
	}
	return &result, nil
}

// Next skips to the next track in the playlist on the specified player.
func (c *LMSClient) Next(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"playlist", "index", "+1"}
	return c.call(ctx, playerMAC, cmd, nil)
}

// Previous skips to the previous track in the playlist on the specified player.
func (c *LMSClient) Previous(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"playlist", "index", "-1"}
	return c.call(ctx, playerMAC, cmd, nil)
}

// TogglePause toggles the play/pause state of the specified player.
func (c *LMSClient) TogglePause(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"pause"}
	return c.call(ctx, playerMAC, cmd, nil)
}

// Play starts playback on the specified player.
func (c *LMSClient) Play(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"play"}
	return c.call(ctx, playerMAC, cmd, nil)
}

// Stop stops playback on the specified player.
func (c *LMSClient) Stop(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"stop"}
	return c.call(ctx, playerMAC, cmd, nil)
}

// GetArtwork fetches the image bytes for a cover or artwork URL from LMS.
func (c *LMSClient) GetArtwork(ctx context.Context, artworkURL, coverID, playerMAC string) ([]byte, error) {
	targetURL := ""
	if strings.HasPrefix(artworkURL, "http://") || strings.HasPrefix(artworkURL, "https://") {
		targetURL = artworkURL
	} else if strings.HasPrefix(artworkURL, "/") {
		targetURL = fmt.Sprintf("http://%s:%d%s", c.host, c.port, artworkURL)
	} else if coverID != "" {
		targetURL = fmt.Sprintf("http://%s:%d/music/%s/cover.jpg", c.host, c.port, coverID)
	} else if playerMAC != "" {
		targetURL = fmt.Sprintf("http://%s:%d/music/current/cover.jpg?player=%s", c.host, c.port, playerMAC)
	} else {
		return nil, fmt.Errorf("no artwork or cover id provided")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artwork fetch HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// SyncPlayer synchronizes our player (ourMAC, the slave) with a master player (targetMAC).
func (c *LMSClient) SyncPlayer(ctx context.Context, slaveMAC, masterMAC string) error {
	cmd := []interface{}{"sync", slaveMAC}
	return c.call(ctx, masterMAC, cmd, nil)
}

// UnsyncPlayer removes a player from any sync group.
func (c *LMSClient) UnsyncPlayer(ctx context.Context, playerMAC string) error {
	cmd := []interface{}{"sync", "-"}
	return c.call(ctx, playerMAC, cmd, nil)
}
