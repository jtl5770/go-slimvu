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
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jtl5770/go-slimvu"
)

func TestFormatFixedDuration(t *testing.T) {
	tests := []struct {
		sec      float64
		hasHours bool
		expected string
	}{
		{sec: 0, hasHours: false, expected: "00:00"},
		{sec: -10, hasHours: false, expected: "00:00"},
		{sec: 65, hasHours: false, expected: "01:05"},
		{sec: 3599, hasHours: false, expected: "59:59"},
		{sec: 3600, hasHours: true, expected: "1:00:00"},
		{sec: 3665, hasHours: true, expected: "1:01:05"},
		{sec: 7325, hasHours: true, expected: "2:02:05"},
	}

	for _, tt := range tests {
		got := formatFixedDuration(tt.sec, tt.hasHours)
		if got != tt.expected {
			t.Errorf("formatFixedDuration(%v, %v) = %q, expected %q", tt.sec, tt.hasHours, got, tt.expected)
		}
	}
}

func TestComputeMeterColorAndLUT(t *testing.T) {
	// Test boundary clamps
	c0 := computeMeterColor(-1.0)
	if c0.r != 0 || c0.g != 230 || c0.b != 118 {
		t.Errorf("expected green color at t<0, got %+v", c0)
	}

	c1 := computeMeterColor(2.0)
	if c1.r != 255 || c1.g != 23 || c1.b != 68 {
		t.Errorf("expected red color at t>1, got %+v", c1)
	}

	// Test LUT lookup
	entry0 := getMeterColorEntry(0.0)
	if !strings.Contains(entry0.str, "38;2;") {
		t.Errorf("expected ANSI escape sequence in entry.str, got %q", entry0.str)
	}

	entryHigh := getMeterColorEntry(1.0)
	if !strings.Contains(entryHigh.boldStr, "1;38;2;") {
		t.Errorf("expected bold ANSI escape sequence in entry.boldStr, got %q", entryHigh.boldStr)
	}
}

func TestRenderScale(t *testing.T) {
	scale30 := renderScale(30, -60, 0)
	if !strings.Contains(scale30, "|") {
		t.Errorf("expected scale ticks '|' in rendered scale, got %q", scale30)
	}

	// Scale should be cached
	scale30Cached := renderScale(30, -60, 0)
	if scale30 != scale30Cached {
		t.Errorf("expected cached scale result to match")
	}
}

func TestPeakPhysics(t *testing.T) {
	m := initialModel(nil, -60, 0, 30, 200*time.Millisecond, 20.0, false, 1.6)
	m.playing = true

	var peak peakInfo
	barLen := 40
	now := time.Now()

	// Initial jump to max level (0 dB -> 100% -> barLen)
	m.updatePeak(&peak, 0.0, barLen, 0.033, now)
	if peak.position != float64(barLen) {
		t.Errorf("expected peak position %v, got %v", barLen, peak.position)
	}
	if peak.holdUntil.Before(now) {
		t.Errorf("expected holdUntil to be in the future")
	}

	// During hold duration, peak should not drop despite level dropping to -60 dB
	duringHold := now.Add(100 * time.Millisecond)
	m.updatePeak(&peak, -60.0, barLen, 0.033, duringHold)
	if peak.position != float64(barLen) {
		t.Errorf("expected peak to hold during hold duration, got %v", peak.position)
	}

	// After hold duration expires, peak should decay
	afterHold := now.Add(300 * time.Millisecond)
	m.updatePeak(&peak, -60.0, barLen, 0.1, afterHold)
	if peak.position >= float64(barLen) {
		t.Errorf("expected peak to decay after hold expires, got %v", peak.position)
	}

	// When playing is false, peak resets to 0
	m.playing = false
	m.updatePeak(&peak, 0.0, barLen, 0.033, time.Now())
	if peak.position != 0 {
		t.Errorf("expected peak position 0 when not playing, got %v", peak.position)
	}
}

func TestModelKeyHandling(t *testing.T) {
	m := initialModel(nil, -60, 0, 30, 250*time.Millisecond, 20.0, false, 1.6)

	// Quit keys
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd == nil {
			t.Errorf("expected quit cmd for key %q", key)
		}
		_ = newModel
	}

	// Playback keys should NO-OP when unsynced
	for _, key := range []string{" ", "space", "right", "left"} {
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		if cmd != nil {
			t.Errorf("expected nil cmd for playback key %q when unsynced", key)
		}
	}

	// Window resize
	updatedModel, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = updatedModel.(model)
	if m.termWidth != 100 || m.termHeight != 40 {
		t.Errorf("expected term dimensions (100, 40), got (%d, %d)", m.termWidth, m.termHeight)
	}
}

func TestModelViewRendering_UnsyncedSuppressesTrackInfoAndCover(t *testing.T) {
	m := initialModel(nil, -60, 0, 30, 250*time.Millisecond, 20.0, true, 1.6)
	m.termWidth = 80
	m.termHeight = 24

	// When unsynced (syncedMAC == "" && syncedName == ""), even if LMS returns trackinfo for slimvu orphan playlist:
	m.track = slimvu.TrackInfo{
		Title:    "SlimVU Orphan Title",
		Artist:   "SlimVU Orphan Artist",
		Album:    "SlimVU Orphan Album",
		Duration: 240,
		Elapsed:  60,
	}
	m.hasTrack = true
	m.cachedRawTitle = "SlimVU Orphan Artist · SlimVU Orphan Album · SlimVU Orphan Title"
	m.cachedTitleRunes = []rune(m.cachedRawTitle)

	view := m.View()

	// 1. Should NOT contain track info in view
	if strings.Contains(view, "SlimVU Orphan") {
		t.Errorf("expected track info to be suppressed when unsynced, got %s", view)
	}

	// 2. Should render placeholder cover art ("NO COVER")
	if !strings.Contains(view, "NO COVER") {
		t.Errorf("expected placeholder cover art 'NO COVER' when unsynced, got %s", view)
	}

	// 3. Status should be IDLE
	if !strings.Contains(view, "IDLE") {
		t.Errorf("expected IDLE status in view, got %s", view)
	}
}

func TestModelViewRendering_Synced(t *testing.T) {
	m := initialModel(nil, -60, 0, 30, 250*time.Millisecond, 20.0, false, 1.6)
	m.termWidth = 80
	m.termHeight = 24

	// PLAYING view with synced player
	m.syncedMAC = "00:04:20:11:22:33"
	m.syncedName = "Living Room"
	m.playing = true
	m.leftDB = -6.0
	m.rightDB = -12.0
	m.track = slimvu.TrackInfo{
		Title:    "Test Song",
		Artist:   "Test Artist",
		Album:    "Test Album",
		Duration: 240,
		Elapsed:  60,
	}
	m.hasTrack = true
	m.cachedRawTitle = "Test Artist · Test Album · Test Song"
	m.cachedTitleRunes = []rune(m.cachedRawTitle)

	viewPlaying := m.View()
	if !strings.Contains(viewPlaying, "PLAYING") {
		t.Errorf("expected PLAYING status in view, got %s", viewPlaying)
	}
	if !strings.Contains(viewPlaying, "Synced to: Living Room") {
		t.Errorf("expected 'Synced to: Living Room' in header, got %s", viewPlaying)
	}
	if !strings.Contains(viewPlaying, "Test Artist") {
		t.Errorf("expected track info in view when synced, got %s", viewPlaying)
	}
	if !strings.Contains(viewPlaying, "01:00 / 04:00") {
		t.Errorf("expected duration in view, got %s", viewPlaying)
	}
}

func TestModelCoverCols(t *testing.T) {
	m := initialModel(nil, -60, 0, 30, 250*time.Millisecond, 20.0, true, 2.0)
	cols := m.getCoverCols()
	// 9.0 * 2.0 = 18 cols
	if cols != 18 {
		t.Errorf("expected 18 cover cols for aspect 2.0, got %d", cols)
	}

	mAspectSmall := initialModel(nil, -60, 0, 30, 250*time.Millisecond, 20.0, true, 0.5)
	colsSmall := mAspectSmall.getCoverCols()
	// Min cols is 8
	if colsSmall < 8 {
		t.Errorf("expected minimum 8 cover cols, got %d", colsSmall)
	}
}
