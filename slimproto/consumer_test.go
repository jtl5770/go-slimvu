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

package slimproto

import (
	"sync"
	"testing"
	"time"
)

type mockConsumerCallbacks struct {
	mu           sync.Mutex
	state        PlaybackState
	sampleRate   uint32
	startAt      uint32
	pauseFrames  int64
	framesPlayed uint64
	decoderDone  bool
	sentStats    [][4]byte
}

func (m *mockConsumerCallbacks) GetState() PlaybackState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *mockConsumerCallbacks) SetState(s PlaybackState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
}

func (m *mockConsumerCallbacks) GetSampleRate() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sampleRate
}

func (m *mockConsumerCallbacks) GetStartAt() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startAt
}

func (m *mockConsumerCallbacks) GetPauseFrames() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pauseFrames
}

func (m *mockConsumerCallbacks) DeductPauseFrames(frames int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseFrames -= frames
	if m.pauseFrames < 0 {
		m.pauseFrames = 0
	}
}

func (m *mockConsumerCallbacks) AddFramesPlayed(frames uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.framesPlayed += frames
}

func (m *mockConsumerCallbacks) IsDecoderDone() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.decoderDone
}

func (m *mockConsumerCallbacks) SendStat(event [4]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentStats = append(m.sentStats, event)
	return nil
}

func TestPacedConsumer_AudioPlaybackPacing(t *testing.T) {
	rb := NewAudioRingBuffer(65536)
	levels := NewAtomicLevels()
	cb := &mockConsumerCallbacks{
		state:      StateRunning,
		sampleRate: 44100,
	}

	pc := NewPacedConsumer(PacedConsumerConfig{
		RingBuffer: rb,
		Levels:     levels,
		Callbacks:  cb,
	})

	// 100ms of synthetic 16-bit audio @ 44.1kHz = 4410 frames = 17640 bytes
	audioData := make([]byte, 17640)
	for i := range audioData {
		audioData[i] = 0x20
	}
	_, err := rb.Write(audioData)
	if err != nil {
		t.Fatalf("Failed to write to ring buffer: %v", err)
	}

	// Step forward by 10ms (should consume 441 frames = 1764 bytes)
	pc.Step(10 * time.Millisecond)

	if cb.framesPlayed != 441 {
		t.Errorf("Expected 441 frames played after 10ms, got %d", cb.framesPlayed)
	}

	leftDB, rightDB, active := levels.Get()
	if !active {
		t.Errorf("Expected active playback levels, got inactive")
	}
	if leftDB <= -100 || rightDB <= -100 {
		t.Errorf("Expected non-silence levels, got left=%.2f right=%.2f", leftDB, rightDB)
	}

	// Step forward by 25ms (44.1 * 25 = 1102.5 frames -> accumulator handles fractional frames)
	pc.Step(25 * time.Millisecond)
	if cb.framesPlayed != 441+1102 {
		t.Errorf("Expected 1543 total frames played, got %d", cb.framesPlayed)
	}

	// Step forward by another 25ms (accumulated 0.5 + 0.5 = 1 extra frame -> 1103 frames)
	pc.Step(25 * time.Millisecond)
	if cb.framesPlayed != 441+1102+1103 {
		t.Errorf("Expected 2646 total frames played, got %d", cb.framesPlayed)
	}
}

func TestPacedConsumer_PauseFramesCountdown(t *testing.T) {
	rb := NewAudioRingBuffer(65536)
	levels := NewAtomicLevels()
	cb := &mockConsumerCallbacks{
		state:       StateRunning,
		sampleRate:  44100,
		pauseFrames: 4410, // 100ms of pause frames
	}

	pc := NewPacedConsumer(PacedConsumerConfig{
		RingBuffer: rb,
		Levels:     levels,
		Callbacks:  cb,
	})

	// Fill buffer with audio
	audioData := make([]byte, 17640)
	_, _ = rb.Write(audioData)

	// Step by 50ms (should deduct 2205 pause frames, without consuming ring buffer audio)
	pc.Step(50 * time.Millisecond)

	if cb.pauseFrames != 2205 {
		t.Errorf("Expected 2205 pause frames remaining, got %d", cb.pauseFrames)
	}
	if cb.framesPlayed != 0 {
		t.Errorf("Expected 0 frames played while in pauseFrames, got %d", cb.framesPlayed)
	}
	left, right, active := levels.Get()
	if active || left != -100 || right != -100 {
		t.Errorf("Expected silence while skipping pause frames, got %f/%f %v", left, right, active)
	}

	// Step by another 50ms (should exhaust pause frames)
	pc.Step(50 * time.Millisecond)
	if cb.pauseFrames != 0 {
		t.Errorf("Expected 0 pause frames remaining, got %d", cb.pauseFrames)
	}

	// Next step consumes audio normally
	pc.Step(10 * time.Millisecond)
	if cb.framesPlayed != 441 {
		t.Errorf("Expected 441 frames played after pause frames finished, got %d", cb.framesPlayed)
	}
}

func TestPacedConsumer_StartAtSynchronization(t *testing.T) {
	rb := NewAudioRingBuffer(65536)
	levels := NewAtomicLevels()
	cb := &mockConsumerCallbacks{
		state:      StateStartAt,
		sampleRate: 44100,
		startAt:    5000,
	}
	mockClock := NewMockClock(4000, time.Now())

	pc := NewPacedConsumer(PacedConsumerConfig{
		RingBuffer: rb,
		Levels:     levels,
		Clock:      mockClock,
		Callbacks:  cb,
	})

	// Step while nowMs < startAt
	pc.Step(10 * time.Millisecond)
	if cb.GetState() != StateStartAt {
		t.Errorf("Expected state to remain StateStartAt, got %v", cb.GetState())
	}

	// Advance clock past startAt
	mockClock.Advance(1001 * time.Millisecond)
	pc.Step(10 * time.Millisecond)

	if cb.GetState() != StateRunning {
		t.Errorf("Expected transition to StateRunning, got %v", cb.GetState())
	}
	if len(cb.sentStats) == 0 || cb.sentStats[0] != [4]byte{'S', 'T', 'M', 's'} {
		t.Errorf("Expected STMs stat event sent on start")
	}
}

func TestPacedConsumer_UnderrunAtEOF(t *testing.T) {
	rb := NewAudioRingBuffer(65536)
	levels := NewAtomicLevels()
	cb := &mockConsumerCallbacks{
		state:       StateRunning,
		sampleRate:  44100,
		decoderDone: true, // EOF reached on input stream
	}

	pc := NewPacedConsumer(PacedConsumerConfig{
		RingBuffer: rb,
		Levels:     levels,
		Callbacks:  cb,
	})

	// Step on empty buffer
	pc.Step(10 * time.Millisecond)

	if cb.GetState() != StateStopped {
		t.Errorf("Expected transition to StateStopped on EOF underrun, got %v", cb.GetState())
	}
	if len(cb.sentStats) == 0 || cb.sentStats[0] != [4]byte{'S', 'T', 'M', 'u'} {
		t.Errorf("Expected STMu stat event sent on EOF underrun")
	}
}

func TestPacedConsumer_SpectrumProcessingAndReset(t *testing.T) {
	rb := NewAudioRingBuffer(65536)
	levels := NewAtomicLevels()
	spectrum := NewAtomicSpectrum()
	cb := &mockConsumerCallbacks{
		state:      StateRunning,
		sampleRate: 44100,
	}
	mockClock := NewMockClock(1000, time.Now())

	pc := NewPacedConsumer(PacedConsumerConfig{
		RingBuffer:      rb,
		Levels:          levels,
		Spectrum:        spectrum,
		SpectrumEnabled: false,
		Clock:           mockClock,
		Callbacks:       cb,
	})

	if pc.IsSpectrumEnabled() {
		t.Fatal("Expected spectrum to be disabled by default")
	}

	// 100ms of synthetic 16-bit audio
	audioData := make([]byte, 17640)
	for i := range audioData {
		audioData[i] = 0x40
	}
	_, _ = rb.Write(audioData)

	// Step by 25ms with spectrum disabled: levels update, but spectrum remains silent
	pc.Step(25 * time.Millisecond)
	var specBandsL, specBandsR [SpectrumBandsCount]float32
	spectrum.CopyTo(specBandsL[:], specBandsR[:])
	for i := 0; i < SpectrumBandsCount; i++ {
		if specBandsL[i] != -100.0 {
			t.Fatalf("Left Band %d: expected -100.0 with spectrum disabled, got %.2f", i, specBandsL[i])
		}
		if specBandsR[i] != -100.0 {
			t.Fatalf("Right Band %d: expected -100.0 with spectrum disabled, got %.2f", i, specBandsR[i])
		}
	}

	// Enable spectrum computation
	pc.SetSpectrumEnabled(true)
	if !pc.IsSpectrumEnabled() {
		t.Fatal("Expected spectrum to be enabled")
	}

	// Step by 25ms: spectrum should now be computed from active audio
	pc.Step(25 * time.Millisecond)
	n := spectrum.CopyTo(specBandsL[:], specBandsR[:])
	if n != SpectrumBandsCount {
		t.Fatalf("Expected %d bands copied, got %d", SpectrumBandsCount, n)
	}

	// Levels and spectrum should be active
	left, right, active := levels.Get()
	if !active || left <= -100 || right <= -100 {
		t.Errorf("Expected active levels, got left=%.2f right=%.2f active=%v", left, right, active)
	}

	// Disable spectrum computation: bands should immediately return to silence
	pc.SetSpectrumEnabled(false)
	if pc.IsSpectrumEnabled() {
		t.Fatal("Expected spectrum to be disabled")
	}
	spectrum.CopyTo(specBandsL[:], specBandsR[:])
	for i := 0; i < SpectrumBandsCount; i++ {
		if specBandsL[i] != -100.0 {
			t.Errorf("Left Band %d: expected -100.0 after disabling spectrum, got %.2f", i, specBandsL[i])
		}
		if specBandsR[i] != -100.0 {
			t.Errorf("Right Band %d: expected -100.0 after disabling spectrum, got %.2f", i, specBandsR[i])
		}
	}

	// Test Reset()
	pc.Reset()
	n = spectrum.CopyTo(specBandsL[:], specBandsR[:])
	for i := 0; i < SpectrumBandsCount; i++ {
		if specBandsL[i] != -100.0 {
			t.Errorf("Left Band %d: expected -100.0 after Reset(), got %.2f", i, specBandsL[i])
		}
		if specBandsR[i] != -100.0 {
			t.Errorf("Right Band %d: expected -100.0 after Reset(), got %.2f", i, specBandsR[i])
		}
	}
	l, r, act := levels.Get()
	if act || l != -100 || r != -100 {
		t.Errorf("Expected silence levels after Reset, got %f/%f %v", l, r, act)
	}
}
