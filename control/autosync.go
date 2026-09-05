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
	"strings"
	"sync"
	"sync/atomic"
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

type syncIntentKind int

const (
	intentNone syncIntentKind = iota
	intentSyncTo
	intentUnsync
)

type syncIntent struct {
	kind   syncIntentKind
	target string
}

// Config holds configuration parameters for the PlayerManager.
type Config struct {
	OurMAC         string        // MAC address of our SlimVU client
	OurName        string        // Player name of our SlimVU client
	AutoSync       bool          // Whether to automatically slave to active playing players
	IgnoredPlayers []string      // List of player names or MACs to ignore during AutoSync
	PollInterval   time.Duration // Polling interval (defaults to 1000ms)
}

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

	autoSync        atomic.Bool
	mu              sync.RWMutex
	ourPlayer       PlayerStatus
	externalPlayers []PlayerStatus
	pendingIntent   syncIntent
}

// NewPlayerManager creates a new PlayerManager and performs an initial synchronous discovery.
func NewPlayerManager(client LMSClientInterface, cfg Config) *PlayerManager {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1000 * time.Millisecond
	}

	m := &PlayerManager{
		client: client,
		cfg:    cfg,
	}
	m.autoSync.Store(cfg.AutoSync)

	// Perform initial discovery so caller has immediate state access.
	initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Second)
	m.poll(initCtx)
	initCancel()

	return m
}

// SetAutoSync dynamically enables or disables automatic synchronization to playing players.
func (m *PlayerManager) SetAutoSync(enabled bool) {
	m.autoSync.Store(enabled)
}

// GetAutoSync reports whether automatic synchronization is currently enabled.
func (m *PlayerManager) GetAutoSync() bool {
	return m.autoSync.Load()
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

	if m.autoSync.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)
	}

	m.mu.Lock()
	m.ourPlayer = PlayerStatus{}
	m.externalPlayers = nil
	m.pendingIntent = syncIntent{}
	m.mu.Unlock()

	slog.Info("PlayerManager stopped cleanly")
}

// SyncTo registers an intent to manually sync SlimVU to the target player (by Name or MAC).
// The command executes at the next iteration of the polling loop.
func (m *PlayerManager) SyncTo(target string) {
	m.mu.Lock()
	m.pendingIntent = syncIntent{
		kind:   intentSyncTo,
		target: target,
	}
	m.mu.Unlock()
}

// Unsync registers an intent to detach SlimVU from its current sync group.
// The command executes at the next iteration of the polling loop.
func (m *PlayerManager) Unsync() {
	m.mu.Lock()
	m.pendingIntent = syncIntent{
		kind: intentUnsync,
	}
	m.mu.Unlock()
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

	if !m.ourPlayer.IsSlaved() {
		return "", ""
	}

	masterMAC := m.ourPlayer.SyncMaster
	if p := findExternal(m.externalPlayers, masterMAC); p != nil {
		return masterMAC, p.Name
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

func (m *PlayerManager) isOurPlayer(p PlayerStatus) bool {
	if m.cfg.OurMAC != "" && p.Matches(m.cfg.OurMAC) {
		return true
	}
	if m.cfg.OurName != "" && p.Matches(m.cfg.OurName) {
		return true
	}
	return false
}

// isValidSyncTarget reports whether targetMaster is valid (non-empty, not a virtual SlimVU client, and not our player).
func (m *PlayerManager) isValidSyncTarget(targetMaster string) bool {
	if targetMaster == "" {
		return false
	}
	if IsVirtualPlayerMAC(targetMaster) || MatchMAC(targetMaster, m.cfg.OurMAC) {
		return false
	}
	return true
}

func findExternal(players []PlayerStatus, identifier string) *PlayerStatus {
	for i := range players {
		if players[i].Matches(identifier) {
			return &players[i]
		}
	}
	return nil
}

// syncToPlayer cleanly unsyncs our player and attaches it as a slave to the target player.
// Returns true if the sync command was successfully issued.
func (m *PlayerManager) syncToPlayer(ctx context.Context, target PlayerStatus) bool {
	if !m.isValidSyncTarget(target.PlayerID) {
		slog.Warn("PlayerManager: invalid sync target master", "master", target.PlayerID, "name", target.Name)
		return false
	}

	slog.Info("PlayerManager: syncing to player",
		"targetName", target.Name, "targetMAC", target.PlayerID)

	_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)
	if err := m.client.SyncPlayer(ctx, m.cfg.OurMAC, target.PlayerID); err != nil {
		slog.Warn("PlayerManager: failed to sync player", "target", target.PlayerID, "error", err)
		return false
	}

	ourStatus, _, _ := m.refreshState(ctx)
	if ourStatus != nil && ourStatus.SyncMaster == "" {
		m.mu.Lock()
		m.ourPlayer.SyncMaster = target.PlayerID
		m.mu.Unlock()
	}
	return true
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

// refreshState queries LMS for all connected players, runs the Self-Master guard,
// and updates internal snapshots under a mutex lock.
func (m *PlayerManager) refreshState(ctx context.Context) (*PlayerStatus, []PlayerStatus, error) {
	playersList, err := m.client.GetPlayers(ctx)
	if err != nil {
		slog.Debug("PlayerManager: failed to fetch players list", "error", err)
		return nil, nil, err
	}

	var allStatuses []PlayerStatus
	var ourStatus *PlayerStatus

	for _, p := range playersList {
		status, err := m.client.GetPlayerStatus(ctx, p.PlayerID)
		if err != nil || status == nil {
			continue
		}
		if p.Name != "" && (status.Name == "" || status.Matches(p.PlayerID)) {
			status.Name = p.Name
		}
		allStatuses = append(allStatuses, *status)
		if m.isOurPlayer(*status) {
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

	// Self-Master Guard: ensure SlimVU never remains a sync master.
	if ourStatus != nil && ourStatus.IsSyncMaster() {
		slog.Warn("PlayerManager: detected our player is sync master, unsyncing immediately", "slaves", ourStatus.SyncSlaves)
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)

		if status, err := m.client.GetPlayerStatus(ctx, m.cfg.OurMAC); err == nil && status != nil {
			ourStatus = status
		}
		for i, st := range allStatuses {
			if m.isOurPlayer(st) {
				if ourStatus != nil {
					allStatuses[i] = *ourStatus
				}
			} else if strings.Contains(ourStatus.SyncSlaves, st.PlayerID) {
				if updated, err := m.client.GetPlayerStatus(ctx, st.PlayerID); err == nil && updated != nil {
					allStatuses[i] = *updated
				}
			}
		}
	}

	var external []PlayerStatus
	for _, st := range allStatuses {
		if m.isOurPlayer(st) || IsVirtualPlayerMAC(st.PlayerID) {
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

	return ourStatus, external, nil
}

// poll executes one full cycle: queries state, processes any pending manual sync intent,
// and runs AutoSync decision logic if enabled.
func (m *PlayerManager) poll(ctx context.Context) {
	ourStatus, external, err := m.refreshState(ctx)
	if err != nil {
		return
	}

	// Consume pending manual sync/unsync intent
	m.mu.Lock()
	intent := m.pendingIntent
	m.pendingIntent = syncIntent{}
	m.mu.Unlock()

	intentHandled := false
	autoSyncEnabled := m.autoSync.Load()

	switch intent.kind {
	case intentUnsync:
		slog.Info("PlayerManager: executing manual unsync intent")
		_ = m.client.UnsyncPlayer(ctx, m.cfg.OurMAC)
		ourStatus, external, _ = m.refreshState(ctx)
		intentHandled = true

	case intentSyncTo:
		targetPlayer := findExternal(external, intent.target)
		if targetPlayer == nil {
			slog.Warn("PlayerManager: manual sync target not found in external players", "target", intent.target)
		} else if autoSyncEnabled && !targetPlayer.IsPlaying() {
			slog.Warn("PlayerManager: cannot manually sync to non-playing player while AutoSync is active",
				"target", targetPlayer.Name, "mode", targetPlayer.Mode)
		} else {
			target := *targetPlayer
			if targetPlayer.IsSlaved() {
				if master := findExternal(external, targetPlayer.SyncMaster); master != nil {
					target = *master
				}
			}
			intentHandled = m.syncToPlayer(ctx, target)
		}
	}

	// AutoSync automation (if enabled and no manual intent was just executed)
	if autoSyncEnabled && !intentHandled {
		m.evaluateAutoSync(ctx, ourStatus, external)
	}
}

func (m *PlayerManager) isIgnored(p PlayerStatus) bool {
	for _, ign := range m.cfg.IgnoredPlayers {
		if p.Matches(ign) {
			return true
		}
	}
	return false
}

// evaluateAutoSync checks if the current sync master is playing, and if not, slaves to an active playing player.
func (m *PlayerManager) evaluateAutoSync(ctx context.Context, ourStatus *PlayerStatus, external []PlayerStatus) {
	// If currently slaved to a master that is actively playing, stay slaved.
	if ourStatus != nil && ourStatus.IsSlaved() {
		if currentMaster := findExternal(external, ourStatus.SyncMaster); currentMaster != nil && currentMaster.IsPlaying() {
			return
		}
	}

	// Find the first external physical player that is actively playing
	for _, p := range external {
		if m.isIgnored(p) || !p.IsPlaying() {
			continue
		}

		target := p
		if p.IsSlaved() {
			if master := findExternal(external, p.SyncMaster); master != nil {
				target = *master
			}
		}

		if m.syncToPlayer(ctx, target) {
			return
		}
	}
}
