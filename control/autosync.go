// Copyright (C) 2026 Jens Lautenbacher <jtl@gmx.com>
//
// This file is part of go-slimvu.
//
// go-slimvu is free software: you can redistribute it/r modify
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
	"strings"
	"sync"
	"time"
)

// MagicMACPrefix is the OUI / prefix used for auto-generated virtual SlimVU clients.
const MagicMACPrefix = "00:04:20:ee"

// IsVirtualPlayerMAC reports whether the given MAC address matches the SlimVU virtual client prefix.
func IsVirtualPlayerMAC(mac string) bool {
	clean := strings.ToLower(strings.ReplaceAll(mac, "-", ":"))
	return strings.HasPrefix(clean, MagicMACPrefix)
}

// MatchMAC compares two MAC addresses case-insensitively and regardless of delimiter (- vs :).
func MatchMAC(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	cleanA := strings.ToLower(strings.ReplaceAll(a, "-", ":"))
	cleanB := strings.ToLower(strings.ReplaceAll(b, "-", ":"))
	return cleanA == cleanB
}

// Config holds configuration parameters for the PlayerManager.
type Config struct {
	OurMAC         string        // MAC address of our SlimVU client
	OurName        string        // Player name of our SlimVU client
	AutoSync       bool          // Whether to automatically slave to active playing players
	IgnoredPlayers []string      // List of player names or MACs to ignore during AutoSync
	PollInterval   time.Duration // Polling interval (defaults to 1000ms)
}

// AutoSyncConfig is an alias for Config for backward compatibility.
type AutoSyncConfig = Config

// LMSClientInterface defines the subset of LMSClient methods used by PlayerManager.
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

// PlayerManager continuously monitors LMS players, maintains state snapshots,
// and optionally manages auto-synchronization.
type PlayerManager struct {
	client LMSClientInterface
	cfg    Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu              sync.RWMutex
	ourPlayer       PlayerStatus
	externalPlayers []PlayerStatus
}

// AutoSyncManager is an alias for PlayerManager for backward compatibility.
type AutoSyncManager = PlayerManager

// NewPlayerManager creates a new PlayerManager and performs an initial synchronous discovery.
func NewPlayerManager(client LMSClientInterface, cfg Config) *PlayerManager {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1000 * time.Millisecond
	}

	m := &PlayerManager{
		client: client,
		cfg:    cfg,
	}

	// Perform initial discovery so caller has immediate state access.
	initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Second)
	m.poll(initCtx)
	initCancel()

	return m
}

// NewAutoSyncManager is an alias for NewPlayerManager for backward compatibility.
func NewAutoSyncManager(client LMSClientInterface, cfg AutoSyncConfig) *PlayerManager {
	return NewPlayerManager(client, cfg)
}

// Start begins the background polling goroutine.
func (m *PlayerManager) Start() {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.wg.Add(1)
	go m.pollLoop()
}

// Stop stops the background poll loop and cleanly unsyncs our player if AutoSync was active.
func (m *PlayerManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	if m.cfg.AutoSync {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)
	}

	m.mu.Lock()
	m.ourPlayer = PlayerStatus{}
	m.externalPlayers = nil
	m.mu.Unlock()

	slog.Info("PlayerManager stopped cleanly")
}

// GetAllPlayers returns a thread-safe snapshot copy of all connected external physical players.
func (m *PlayerManager) GetAllPlayers() []PlayerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.externalPlayers) == 0 {
		return nil
	}
	snapshot := make([]PlayerStatus, len(m.externalPlayers))
	copy(snapshot, m.externalPlayers)
	return snapshot
}

// GetOurPlayer returns a copy of our own SlimVU player's status.
func (m *PlayerManager) GetOurPlayer() PlayerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ourPlayer
}

// SyncedWith returns the MAC address and name of the master player we are currently synced with.
// If we are standalone / unsynced, it returns empty strings ("", "").
func (m *PlayerManager) SyncedWith() (mac, name string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	masterMAC := m.ourPlayer.SyncMaster
	if masterMAC == "" || MatchMAC(masterMAC, m.cfg.OurMAC) {
		return "", ""
	}

	for _, p := range m.externalPlayers {
		if MatchMAC(p.PlayerID, masterMAC) {
			return masterMAC, p.Name
		}
	}
	return masterMAC, ""
}

// SyncedTrack returns the TrackInfo of our player (which matches our sync group when slaved).
func (m *PlayerManager) SyncedTrack() (TrackInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	track := m.ourPlayer.GetTrackInfo()
	hasTrack := track.Title != "" || track.Artist != ""
	return track, hasTrack
}

// GetArtwork fetches track cover artwork from LMS.
func (m *PlayerManager) GetArtwork(ctx context.Context, artworkURL, coverID string) ([]byte, error) {
	return m.client.GetArtwork(ctx, artworkURL, coverID, m.cfg.OurMAC)
}

// Next skips to the next track.
func (m *PlayerManager) Next(ctx context.Context) error {
	return m.client.Next(ctx, m.cfg.OurMAC)
}

// Previous skips to the previous track.
func (m *PlayerManager) Previous(ctx context.Context) error {
	return m.client.Previous(ctx, m.cfg.OurMAC)
}

// TogglePause toggles play/pause on our player.
func (m *PlayerManager) TogglePause(ctx context.Context) error {
	return m.client.TogglePause(ctx, m.cfg.OurMAC)
}

// Play starts playback on our player.
func (m *PlayerManager) Play(ctx context.Context) error {
	return m.client.Play(ctx, m.cfg.OurMAC)
}

// StopPlayback stops playback on our player.
func (m *PlayerManager) StopPlayback(ctx context.Context) error {
	return m.client.Stop(ctx, m.cfg.OurMAC)
}

func (m *PlayerManager) isOurPlayer(playerID, name string) bool {
	if m.cfg.OurMAC != "" && MatchMAC(playerID, m.cfg.OurMAC) {
		return true
	}
	if m.cfg.OurName != "" && strings.EqualFold(name, m.cfg.OurName) {
		return true
	}
	return false
}

func (m *PlayerManager) pollLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(m.cfg.PollInterval)
	defer ticker.Stop()

	// Configure player sync buffer preferences once at startup
	initCtx, initCancel := context.WithTimeout(m.ctx, 3*time.Second)
	_ = m.client.SetPlayerPref(initCtx, m.cfg.OurMAC, "maintainSync", "0")
	_ = m.client.SetPlayerPref(initCtx, m.cfg.OurMAC, "minSyncAdjust", "5000")
	initCancel()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			pollCtx, pollCancel := context.WithTimeout(m.ctx, 5*time.Second)
			m.poll(pollCtx)
			pollCancel()
		}
	}
}

// poll executes one full cycle: query all players, guard against self-master role,
// update registry, and run AutoSync decision logic if enabled.
func (m *PlayerManager) poll(ctx context.Context) {
	playersList, err := m.client.GetPlayers(ctx)
	if err != nil {
		slog.Debug("PlayerManager: failed to fetch players list", "error", err)
		return
	}

	// 1. Query status for every discovered player
	var allStatuses []PlayerStatus
	var ourStatus *PlayerStatus

	for _, p := range playersList {
		status, err := m.client.GetPlayerStatus(ctx, p.PlayerID)
		if err != nil || status == nil {
			continue
		}
		if status.Name == "" && p.Name != "" {
			status.Name = p.Name
		}
		allStatuses = append(allStatuses, *status)
		if m.isOurPlayer(p.PlayerID, p.Name) {
			s := *status
			ourStatus = &s
		}
	}

	// If our player was not in the players list, query it directly
	if ourStatus == nil && m.cfg.OurMAC != "" {
		if status, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC); err == nil && status != nil {
			ourStatus = status
			allStatuses = append(allStatuses, *status)
		}
	}

	// 2. Self-Master Guard: ensure SlimVU never remains a sync master.
	// In LMS, a player is a master ONLY if it has non-empty slaves and is NOT slaved to another master (SyncMaster is empty or self).
	cleanSlaves := ""
	if ourStatus != nil {
		cleanSlaves = strings.TrimSpace(ourStatus.SyncSlaves)
	}
	isMaster := ourStatus != nil && cleanSlaves != "" && cleanSlaves != "-" && (ourStatus.SyncMaster == "" || MatchMAC(ourStatus.SyncMaster, m.cfg.OurMAC))

	if isMaster {
		slog.Warn("PlayerManager: detected our player is sync master, unsyncing immediately", "slaves", ourStatus.SyncSlaves)
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)

		// Re-query our status and slave statuses after self-unsync
		if status, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC); err == nil && status != nil {
			ourStatus = status
		}
		for i, st := range allStatuses {
			if m.isOurPlayer(st.PlayerID, st.Name) {
				if ourStatus != nil {
					allStatuses[i] = *ourStatus
				}
			} else if strings.Contains(cleanSlaves, st.PlayerID) {
				if updated, err := m.client.GetPlayerStatus(ctx, st.PlayerID); err == nil && updated != nil {
					allStatuses[i] = *updated
				}
			}
		}
	}

	// 3. Filter external physical players and update registry
	var external []PlayerStatus
	for _, st := range allStatuses {
		// Filter out our own player (by user-supplied MAC, generated MAC, or name)
		if m.isOurPlayer(st.PlayerID, st.Name) {
			continue
		}
		// Filter out any virtual SlimVU clients by magic MAC prefix
		if IsVirtualPlayerMAC(st.PlayerID) {
			continue
		}
		external = append(external, st)
	}

	m.mu.Lock()
	if ourStatus != nil {
		m.ourPlayer = *ourStatus
	}
	m.externalPlayers = external
	m.mu.Unlock()

	// 4. If AutoSync is not enabled, we are done
	if !m.cfg.AutoSync {
		return
	}

	// 5. AutoSync Decision Logic
	m.evaluateAutoSync(ctx, ourStatus, external)
}

func (m *PlayerManager) isIgnored(p PlayerStatus) bool {
	for _, ign := range m.cfg.IgnoredPlayers {
		if MatchMAC(p.PlayerID, ign) || strings.EqualFold(p.Name, ign) {
			return true
		}
	}
	return false
}

// evaluateAutoSync checks if current sync master is playing, and if not, slaves to an active player.
func (m *PlayerManager) evaluateAutoSync(ctx context.Context, ourStatus *PlayerStatus, external []PlayerStatus) {
	currentMaster := ""
	m.mu.RLock()
	if m.ourPlayer.SyncMaster != "" && !MatchMAC(m.ourPlayer.SyncMaster, m.cfg.OurMAC) {
		currentMaster = m.ourPlayer.SyncMaster
	}
	m.mu.RUnlock()

	// If currently synced to a master, verify it is still actively playing
	if currentMaster != "" {
		masterPlaying := false
		for _, p := range external {
			if MatchMAC(p.PlayerID, currentMaster) {
				if p.Mode == "play" {
					masterPlaying = true
				}
				break
			}
		}
		if masterPlaying {
			// Master is still playing, stay synced
			return
		}

		// Current master is no longer playing -> unsync
		slog.Info("PlayerManager: Current master is not playing, unsyncing", "master", currentMaster)
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)
		if updatedOur, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC); err == nil && updatedOur != nil {
			m.mu.Lock()
			m.ourPlayer = *updatedOur
			m.mu.Unlock()
		} else {
			m.mu.Lock()
			m.ourPlayer.SyncMaster = ""
			m.mu.Unlock()
		}
		currentMaster = ""
	}

	// Find active candidates from external physical players
	type candidate struct {
		targetMasterMAC  string
		targetMasterName string
	}
	var candidates []candidate

	for _, p := range external {
		if m.isIgnored(p) {
			continue
		}
		if p.Mode != "play" {
			continue
		}

		// Determine the master of this player's sync group
		targetMaster := p.PlayerID
		targetName := p.Name
		if p.SyncMaster != "" && !MatchMAC(p.SyncMaster, p.PlayerID) {
			targetMaster = p.SyncMaster
			// Look up master's name if available
			for _, ext := range external {
				if MatchMAC(ext.PlayerID, targetMaster) {
					targetName = ext.Name
					break
				}
			}
		}

		// Safety check: Never sync to a virtual SlimVU client or to ourselves
		if IsVirtualPlayerMAC(targetMaster) || MatchMAC(targetMaster, m.cfg.OurMAC) {
			slog.Debug("PlayerManager: skipping group with virtual master or self", "master", targetMaster)
			continue
		}

		candidates = append(candidates, candidate{
			targetMasterMAC:  targetMaster,
			targetMasterName: targetName,
		})
	}

	if len(candidates) == 0 {
		return
	}

	// Pick first active candidate
	selected := candidates[0]
	if MatchMAC(currentMaster, selected.targetMasterMAC) {
		return
	}

	slog.Info("PlayerManager: Automatically syncing to active player",
		"targetName", selected.targetMasterName,
		"targetMAC", selected.targetMasterMAC)

	// Always unsync first before syncing to new master
	_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)

	if err := m.client.SyncPlayer(ctx, m.cfg.OurMAC, selected.targetMasterMAC); err != nil {
		slog.Warn("PlayerManager: Failed to sync player", "target", selected.targetMasterMAC, "error", err)
		return
	}

	// Immediately refresh our state after sync
	if updatedOur, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC); err == nil && updatedOur != nil {
		m.mu.Lock()
		if updatedOur.SyncMaster == "" {
			updatedOur.SyncMaster = selected.targetMasterMAC
		}
		m.ourPlayer = *updatedOur
		m.mu.Unlock()
	} else {
		m.mu.Lock()
		m.ourPlayer.SyncMaster = selected.targetMasterMAC
		m.mu.Unlock()
	}
}
