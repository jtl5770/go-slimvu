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

	"github.com/jtl5770/go-slimvu"
	"github.com/jtl5770/go-slimvu/control"
)

func TestSyncPopup_SortingByName(t *testing.T) {
	popup := newSyncPopup()

	players := []slimvu.PlayerStatus{
		{PlayerID: "01", Name: "Zebra Room"},
		{PlayerID: "02", Name: "Apple Living Room"},
		{PlayerID: "03", Name: "Kitchen"},
	}

	popup.Open(players)

	if len(popup.players) != 3 {
		t.Fatalf("expected 3 players, got %d", len(popup.players))
	}
	if popup.players[0].Name != "Apple Living Room" {
		t.Errorf("expected 1st player 'Apple Living Room', got %q", popup.players[0].Name)
	}
	if popup.players[1].Name != "Kitchen" {
		t.Errorf("expected 2nd player 'Kitchen', got %q", popup.players[1].Name)
	}
	if popup.players[2].Name != "Zebra Room" {
		t.Errorf("expected 3rd player 'Zebra Room', got %q", popup.players[2].Name)
	}
}

func TestSyncPopup_NavigationAndViewport(t *testing.T) {
	popup := newSyncPopup()

	players := []slimvu.PlayerStatus{
		{PlayerID: "00:04:20:00:00:01", Name: "Player 1", Mode: "play"},
		{PlayerID: "00:04:20:00:00:02", Name: "Player 2", Mode: "pause"},
		{PlayerID: "00:04:20:00:00:03", Name: "Player 3", Mode: "stop"},
		{PlayerID: "00:04:20:00:00:04", Name: "Player 4", Mode: "play"},
		{PlayerID: "00:04:20:00:00:05", Name: "Player 5", Mode: "play"},
		{PlayerID: "00:04:20:00:00:06", Name: "Player 6", Mode: "pause"},
	}

	popup.Open(players)
	if !popup.IsVisible() {
		t.Fatal("expected popup to be visible")
	}

	if p, ok := popup.SelectedPlayer(); !ok || p.PlayerID != "00:04:20:00:00:01" {
		t.Fatalf("expected first player selected, got %+v", p)
	}

	// Move down step by step to test viewport scrolling (capacity 4)
	popup.MoveDown()
	popup.MoveDown()
	popup.MoveDown()
	popup.MoveDown() // Index 4 (Player 5)

	if popup.cursorIndex != 4 {
		t.Fatalf("expected cursorIndex 4, got %d", popup.cursorIndex)
	}
	if popup.viewportOffset != 1 {
		t.Fatalf("expected viewportOffset 1, got %d", popup.viewportOffset)
	}

	// Move back up
	popup.MoveUp()
	popup.MoveUp()
	popup.MoveUp()
	popup.MoveUp() // Index 0 (Player 1)

	if popup.cursorIndex != 0 {
		t.Fatalf("expected cursorIndex 0, got %d", popup.cursorIndex)
	}
	if popup.viewportOffset != 0 {
		t.Fatalf("expected viewportOffset 0, got %d", popup.viewportOffset)
	}
}

func TestSyncPopup_SelectabilityRules(t *testing.T) {
	playingPlayer := slimvu.PlayerStatus{PlayerID: "01", Mode: "play"}
	pausedPlayer := slimvu.PlayerStatus{PlayerID: "02", Mode: "pause"}
	stoppedPlayer := slimvu.PlayerStatus{PlayerID: "03", Mode: "stop"}

	// When AutoSync is true: Only playing players are selectable
	if !isPlayerSelectable(playingPlayer, true) {
		t.Error("playing player should be selectable when autoSync=true")
	}
	if isPlayerSelectable(pausedPlayer, true) {
		t.Error("paused player should NOT be selectable when autoSync=true")
	}
	if isPlayerSelectable(stoppedPlayer, true) {
		t.Error("stopped player should NOT be selectable when autoSync=true")
	}

	// When AutoSync is false: Playing and paused players are selectable
	if !isPlayerSelectable(playingPlayer, false) {
		t.Error("playing player should be selectable when autoSync=false")
	}
	if !isPlayerSelectable(pausedPlayer, false) {
		t.Error("paused player should be selectable when autoSync=false")
	}
	if isPlayerSelectable(stoppedPlayer, false) {
		t.Error("stopped player should NOT be selectable when autoSync=false")
	}
}

func TestSyncPopup_RenderAndOverlay(t *testing.T) {
	popup := newSyncPopup()
	players := []slimvu.PlayerStatus{
		{
			PlayerID: "00:04:20:00:00:01",
			Name:     "Living Room",
			Mode:     "play",
			PlaylistLoop: []control.PlaylistTrack{
				{
					Artist: "Daft Punk",
					Title:  "Get Lucky",
				},
			},
		},
		{
			PlayerID: "00:04:20:00:00:02",
			Name:     "Kitchen",
			Mode:     "pause",
		},
	}

	popup.Open(players)
	bgView := "Header Line\n\nTrack Line\n\nVU Left\nVU Right\nScale\n\nFooter Line\n"
	output := popup.Overlay(bgView, 80, 24, true, "00:04:20:00:00:01", "Living Room", 0)

	if !strings.Contains(output, "Select Sync Target") {
		t.Error("rendered popup missing title")
	}
	if !strings.Contains(output, "Living Room") {
		t.Error("rendered popup missing player 1 name")
	}
	if !strings.Contains(output, "SYNCED ") {
		t.Error("rendered popup missing SYNCED status for active master")
	}
	if !strings.Contains(output, "Not selectable") {
		t.Error("rendered popup missing 'Not selectable' for paused player under autoSync=true")
	}
	if !strings.Contains(output, "Header Line") {
		t.Error("overlay missing background header line")
	}
}
