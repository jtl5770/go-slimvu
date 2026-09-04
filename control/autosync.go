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
	"context"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

// AutoSyncConfig holds configuration parameters for the AutoSyncManager.
type AutoSyncConfig struct {
	OurMAC         string        // MAC address of our GoLEDs Squeezebox client
	OurName        string        // Player name of our GoLEDs client
	IgnoredPlayers []string      // List of player names or MACs to ignore
	PollInterval   time.Duration // Polling interval (e.g. 1000ms)
}

// AutoSyncManager continuously discovers active LMS players and synchronizes our client.
type AutoSyncManager struct {
	client LMSClientInterface
	cfg    AutoSyncConfig

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	syncedWith string    // Currently synced player MAC
	syncedName string    // Currently synced player Name
	track      TrackInfo // Currently playing track info
	hasTrack   bool
}

// LMSClientInterface defines the subset of LMSClient methods used by AutoSyncManager.
type LMSClientInterface interface {
	GetPlayers(ctx context.Context) ([]PlayerInfo, error)
	GetPlayerStatus(ctx context.Context, playerMAC string) (*PlayerStatus, error)
	SyncPlayer(ctx context.Context, ourMAC, targetMAC string) error
	UnsyncPlayer(ctx context.Context, ourMAC string) error
	SetPlayerPref(ctx context.Context, playerMAC, pref, value string) error
	GetArtwork(ctx context.Context, artworkURL, coverID, playerMAC string) ([]byte, error)
	Next(ctx context.Context, playerMAC string) error
	Previous(ctx context.Context, playerMAC string) error
	TogglePause(ctx context.Context, playerMAC string) error
	Play(ctx context.Context, playerMAC string) error
	Stop(ctx context.Context, playerMAC string) error
}

// NewAutoSyncManager creates a new AutoSyncManager.
func NewAutoSyncManager(client LMSClientInterface, cfg AutoSyncConfig) *AutoSyncManager {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1000 * time.Millisecond
	}
	return &AutoSyncManager{
		client: client,
		cfg:    cfg,
	}
}

// Start begins the background monitoring goroutine.
func (m *AutoSyncManager) Start() {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.monitorLoop()
}

// Stop stops the background monitor and unsyncs our player from any active group.
func (m *AutoSyncManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	// Unsync on shutdown with dedicated short timeout (500ms)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)

	m.mu.Lock()
	m.syncedWith = ""
	m.syncedName = ""
	m.track = TrackInfo{}
	m.hasTrack = false
	m.mu.Unlock()

	slog.Info("AutoSyncManager stopped and cleanly unsynced from LMS")
}

// SyncedWith returns the MAC and Name of the player currently synced with.
func (m *AutoSyncManager) SyncedWith() (mac, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.syncedWith, m.syncedName
}

// SyncedTrack returns the TrackInfo of the active synchronized player.
func (m *AutoSyncManager) SyncedTrack() (TrackInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.track, m.hasTrack
}

// GetArtwork fetches the artwork image bytes from LMS.
func (m *AutoSyncManager) GetArtwork(ctx context.Context, artworkURL, coverID string) ([]byte, error) {
	m.mu.Lock()
	synced := m.syncedWith
	m.mu.Unlock()
	return m.client.GetArtwork(ctx, artworkURL, coverID, synced)
}

func (m *AutoSyncManager) getTargetPlayer() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.syncedWith != "" {
		return m.syncedWith
	}
	return m.cfg.OurMAC
}

// Next skips to the next track on the synchronized master player.
func (m *AutoSyncManager) Next(ctx context.Context) error {
	return m.client.Next(ctx, m.getTargetPlayer())
}

// Previous skips to the previous track on the synchronized master player.
func (m *AutoSyncManager) Previous(ctx context.Context) error {
	return m.client.Previous(ctx, m.getTargetPlayer())
}

// TogglePause toggles play/pause on the synchronized master player.
func (m *AutoSyncManager) TogglePause(ctx context.Context) error {
	return m.client.TogglePause(ctx, m.getTargetPlayer())
}

// Play starts playback on the synchronized master player.
func (m *AutoSyncManager) Play(ctx context.Context) error {
	return m.client.Play(ctx, m.getTargetPlayer())
}

// Stop stops playback on the synchronized master player.
func (m *AutoSyncManager) StopPlayback(ctx context.Context) error {
	return m.client.Stop(ctx, m.getTargetPlayer())
}

func (m *AutoSyncManager) isIgnored(p PlayerInfo) bool {
	// Always ignore ourselves
	if strings.EqualFold(p.PlayerID, m.cfg.OurMAC) || strings.EqualFold(p.Name, m.cfg.OurName) {
		return true
	}
	for _, ign := range m.cfg.IgnoredPlayers {
		if strings.EqualFold(p.PlayerID, ign) || strings.EqualFold(p.Name, ign) {
			return true
		}
	}
	return false
}

func (m *AutoSyncManager) monitorLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	initCtx, initCancel := context.WithTimeout(m.ctx, 3*time.Second)
	_ = m.client.SetPlayerPref(initCtx, m.cfg.OurMAC, "maintainSync", "0")
	_ = m.client.SetPlayerPref(initCtx, m.cfg.OurMAC, "minSyncAdjust", "5000")
	initCancel()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.evaluatePlayers()
		}
	}
}

func (m *AutoSyncManager) evaluatePlayers() {
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	defer cancel()

	// 1. Check our own player's status on LMS to see if we are already synced to a group
	currentMaster := ""
	ourStatus, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC)
	if err == nil && ourStatus != nil && ourStatus.SyncMaster != "" {
		currentMaster = ourStatus.SyncMaster
	}
	if currentMaster == "" {
		m.mu.Lock()
		currentMaster = m.syncedWith
		m.mu.Unlock()
	}

	// 2. If currently synced to a player, check if it is actively playing
	if currentMaster != "" && !strings.EqualFold(currentMaster, m.cfg.OurMAC) {
		status, err := m.client.GetPlayerStatus(ctx, currentMaster)
		if err == nil && status != nil {
			if status.Mode == "play" {
				m.mu.Lock()
				m.syncedWith = currentMaster
				if status.Name != "" {
					m.syncedName = status.Name
				}
				m.track = status.GetTrackInfo()
				m.hasTrack = (m.track.Title != "" || m.track.Artist != "")
				m.mu.Unlock()
				return
			}
			slog.Debug("AutoSyncManager: Synced master is not actively playing", "master", currentMaster, "mode", status.Mode)
		}
	}

	// 3. Discover available players that are actively in "play" mode
	players, err := m.client.GetPlayers(ctx)
	if err != nil {
		slog.Debug("AutoSyncManager: failed to fetch players", "error", err)
		return
	}

	type activeCandidate struct {
		player    PlayerInfo
		masterMAC string
		status    *PlayerStatus
	}
	var activeCandidates []activeCandidate

	for _, p := range players {
		if m.isIgnored(p) {
			continue
		}
		status, err := m.client.GetPlayerStatus(ctx, p.PlayerID)
		if err != nil || status == nil {
			continue
		}
		if status.Mode == "play" {
			targetMaster := p.PlayerID
			if status.SyncMaster != "" {
				targetMaster = status.SyncMaster
			}
			activeCandidates = append(activeCandidates, activeCandidate{
				player:    p,
				masterMAC: targetMaster,
				status:    status,
			})
		}
	}

	if len(activeCandidates) == 0 {
		if currentMaster != "" {
			m.mu.Lock()
			m.syncedWith = ""
			m.syncedName = ""
			m.track = TrackInfo{}
			m.hasTrack = false
			m.mu.Unlock()
		}
		return
	}

	// 4. Pick an active candidate
	selected := activeCandidates[rand.IntN(len(activeCandidates))]
	targetMAC := selected.masterMAC
	targetName := selected.player.Name

	if strings.EqualFold(currentMaster, targetMAC) {
		m.mu.Lock()
		m.syncedWith = targetMAC
		m.syncedName = targetName
		m.track = selected.status.GetTrackInfo()
		m.hasTrack = (m.track.Title != "" || m.track.Artist != "")
		m.mu.Unlock()
		return
	}

	slog.Info("AutoSyncManager: Automatically syncing to active player", "targetName", targetName, "targetMAC", targetMAC)

	if err := m.client.SyncPlayer(ctx, m.cfg.OurMAC, targetMAC); err != nil {
		slog.Warn("AutoSyncManager: Failed to sync player", "target", targetMAC, "error", err)
		return
	}

	_ = m.client.SetPlayerPref(ctx, m.cfg.OurMAC, "maintainSync", "0")
	_ = m.client.SetPlayerPref(ctx, m.cfg.OurMAC, "minSyncAdjust", "5000")

	m.mu.Lock()
	m.syncedWith = targetMAC
	m.syncedName = targetName
	m.track = selected.status.GetTrackInfo()
	m.hasTrack = (m.track.Title != "" || m.track.Artist != "")
	m.mu.Unlock()
}
