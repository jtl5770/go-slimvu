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
	"testing"
)

func TestPlayerStatus_GetTrackInfo_Standard(t *testing.T) {
	status := PlayerStatus{
		Mode:             "play",
		Time:             125.4,
		Duration:         300.0,
		PlaylistCurIndex: 2,
		PlaylistTracks:   12,
		PlaylistLoop: []PlaylistTrack{
			{
				Title:    "Money for Nothing",
				Artist:   "Dire Straits",
				Album:    "Brothers in Arms",
				Duration: 300.0,
				TrackNum: 3,
			},
		},
	}

	info := status.GetTrackInfo()
	if info.Title != "Money for Nothing" {
		t.Errorf("expected title 'Money for Nothing', got '%s'", info.Title)
	}
	if info.Artist != "Dire Straits" {
		t.Errorf("expected artist 'Dire Straits', got '%s'", info.Artist)
	}
	if info.Album != "Brothers in Arms" {
		t.Errorf("expected album 'Brothers in Arms', got '%s'", info.Album)
	}
	if info.TrackNum != 3 {
		t.Errorf("expected track 3, got %d", info.TrackNum)
	}
	if info.TotalTracks != 12 {
		t.Errorf("expected total 12, got %d", info.TotalTracks)
	}
	if info.Elapsed != 125.4 {
		t.Errorf("expected elapsed 125.4, got %f", info.Elapsed)
	}
	if info.Duration != 300.0 {
		t.Errorf("expected duration 300.0, got %f", info.Duration)
	}
	if info.IsLiveStream {
		t.Errorf("expected not to be live stream")
	}
}

func TestPlayerStatus_GetTrackInfo_RadioStream(t *testing.T) {
	status := PlayerStatus{
		Mode:         "play",
		Time:         45.0,
		Duration:     0,
		RemoteTitle:  "Radio Paradise",
		CurrentTitle: "Pink Floyd - Comfortably Numb",
	}

	info := status.GetTrackInfo()
	if info.Title != "Pink Floyd - Comfortably Numb" {
		t.Errorf("expected title 'Pink Floyd - Comfortably Numb', got '%s'", info.Title)
	}
	if !info.IsLiveStream {
		t.Errorf("expected live stream to be true")
	}
}

func TestPlayerStatus_GetTrackInfo_StringTypes(t *testing.T) {
	status := PlayerStatus{
		Time:             "65.5",
		Duration:         "210",
		PlaylistCurIndex: "4",
		PlaylistTracks:   "10",
		PlaylistLoop: []PlaylistTrack{
			{
				Title:    "Test Title",
				Artist:   "Test Artist",
				TrackNum: "5",
			},
		},
	}

	info := status.GetTrackInfo()
	if info.TrackNum != 5 {
		t.Errorf("expected track 5, got %d", info.TrackNum)
	}
	if info.TotalTracks != 10 {
		t.Errorf("expected total 10, got %d", info.TotalTracks)
	}
	if info.Elapsed != 65.5 {
		t.Errorf("expected elapsed 65.5, got %f", info.Elapsed)
	}
	if info.Duration != 210.0 {
		t.Errorf("expected duration 210, got %f", info.Duration)
	}
}

func TestPlayerStatus_PlaybackPredicates(t *testing.T) {
	playStatus := &PlayerStatus{Mode: "play"}
	if !playStatus.IsPlaying() || playStatus.IsPaused() || playStatus.IsStopped() {
		t.Errorf("expected IsPlaying=true, IsPaused=false, IsStopped=false")
	}

	pauseStatus := &PlayerStatus{Mode: "pause"}
	if pauseStatus.IsPlaying() || !pauseStatus.IsPaused() || pauseStatus.IsStopped() {
		t.Errorf("expected IsPlaying=false, IsPaused=true, IsStopped=false")
	}

	stopStatus := &PlayerStatus{Mode: "stop"}
	if stopStatus.IsPlaying() || stopStatus.IsPaused() || !stopStatus.IsStopped() {
		t.Errorf("expected IsPlaying=false, IsPaused=false, IsStopped=true")
	}

	var nilStatus *PlayerStatus
	if nilStatus.IsPlaying() || nilStatus.IsPaused() || !nilStatus.IsStopped() {
		t.Errorf("expected nil status IsPlaying=false, IsPaused=false, IsStopped=true")
	}
}

func TestPlayerStatus_SyncPredicates(t *testing.T) {
	master := &PlayerStatus{PlayerID: "00:04:20:11:11:11", SyncSlaves: "00:04:20:22:22:22"}
	if !master.IsSyncMaster() || master.IsSlaved() || master.IsStandalone() {
		t.Errorf("master player should be IsSyncMaster=true, IsSlaved=false, IsStandalone=false")
	}

	slave := &PlayerStatus{PlayerID: "00:04:20:22:22:22", SyncMaster: "00:04:20:11:11:11"}
	if slave.IsSyncMaster() || !slave.IsSlaved() || slave.IsStandalone() {
		t.Errorf("slave player should be IsSyncMaster=false, IsSlaved=true, IsStandalone=false")
	}

	standalone := &PlayerStatus{PlayerID: "00:04:20:33:33:33"}
	if standalone.IsSyncMaster() || standalone.IsSlaved() || !standalone.IsStandalone() {
		t.Errorf("standalone player should be IsSyncMaster=false, IsSlaved=false, IsStandalone=true")
	}
}
