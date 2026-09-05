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
	"context"
	"log/slog"
	"time"

	"github.com/jtl5770/go-slimvu/dsp"
)

// ConsumerCallbacks provides access to player state and telemetry for the PacedConsumer.
type ConsumerCallbacks interface {
	GetState() PlaybackState
	SetState(s PlaybackState)
	GetSampleRate() uint32
	GetStartAt() uint32
	GetPauseFrames() int64
	DeductPauseFrames(frames int64)
	AddFramesPlayed(frames uint64)
	IsDecoderDone() bool
	SendStat(event StatEvent) error
}

// PacedConsumerConfig defines configuration for PacedConsumer.
type PacedConsumerConfig struct {
	TickInterval time.Duration
	RingBuffer   *AudioRingBuffer
	Levels       *AtomicLevels
	Clock        Clock
	Callbacks    ConsumerCallbacks
}

// PacedConsumer drains PCM audio samples from the AudioRingBuffer in real-time,
// synchronizes jiffies timestamps, deducts micro-pause frames, detects underruns,
// and feeds RMS dB audio level measurements into AtomicLevels.
type PacedConsumer struct {
	tickInterval time.Duration
	ringBuffer   *AudioRingBuffer
	levels       *AtomicLevels
	clock        Clock
	callbacks    ConsumerCallbacks

	chunkBuf         []byte
	frameAccumulator float64
}

// NewPacedConsumer creates an initialized PacedConsumer.
func NewPacedConsumer(cfg PacedConsumerConfig) *PacedConsumer {
	interval := cfg.TickInterval
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	clock := cfg.Clock
	if clock == nil {
		clock = NewSystemClock()
	}
	return &PacedConsumer{
		tickInterval: interval,
		ringBuffer:   cfg.RingBuffer,
		levels:       cfg.Levels,
		clock:        clock,
		callbacks:    cfg.Callbacks,
		chunkBuf:     make([]byte, 65536),
	}
}

// Run executes the continuous audio consumption loop until ctx is canceled.
func (p *PacedConsumer) Run(ctx context.Context) {
	ticker := time.NewTicker(p.tickInterval)
	defer ticker.Stop()

	lastTime := p.clock.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			dt := now.Sub(lastTime)
			lastTime = now
			p.Step(dt)
		}
	}
}

// Step processes a single time delta of audio consumption.
// Can be called directly by deterministic unit tests with a MockClock.
func (p *PacedConsumer) Step(dt time.Duration) {
	if p.callbacks == nil || p.ringBuffer == nil || p.levels == nil {
		return
	}

	state := p.callbacks.GetState()
	sr := p.callbacks.GetSampleRate()
	if sr == 0 {
		sr = 44100
	}

	switch state {
	case StateStartAt:
		nowMs := p.clock.NowMonotonicMs()
		startAt := p.callbacks.GetStartAt()
		if nowMs >= startAt || (startAt > nowMs && (startAt-nowMs) > 10000) {
			p.callbacks.SetState(StateRunning)
			_ = p.callbacks.SendStat(StatEventPlaybackStarted)
		}
		p.levels.Set(-100, -100, false)
		p.frameAccumulator = 0

	case StateRunning:
		pauseFrames := p.callbacks.GetPauseFrames()
		if pauseFrames > 0 {
			p.levels.Set(-100, -100, false)
			framesDeducted := int64(float64(sr) * dt.Seconds())
			p.callbacks.DeductPauseFrames(framesDeducted)
			return
		}

		p.frameAccumulator += float64(sr) * dt.Seconds()
		framesToConsume := int(p.frameAccumulator)
		if framesToConsume <= 0 {
			return
		}
		p.frameAccumulator -= float64(framesToConsume)

		bytesToConsume := framesToConsume * 4 // 16-bit stereo = 4 bytes per frame

		if len(p.chunkBuf) < bytesToConsume {
			p.chunkBuf = make([]byte, bytesToConsume)
		}

		n, _ := p.ringBuffer.Read(p.chunkBuf[:bytesToConsume])
		if n > 0 {
			p.callbacks.AddFramesPlayed(uint64(n / 4))
			leftDB, rightDB := dsp.CalculateLevels(p.chunkBuf[:n])
			p.levels.Set(leftDB, rightDB, true)
		} else {
			// Buffer underrun
			if p.callbacks.IsDecoderDone() {
				slog.Info("SlimProto stream playback finished (underrun at EOF)")
				p.callbacks.SetState(StateStopped)
				_ = p.callbacks.SendStat(StatEventOutputUnderrun)
			}
			p.levels.Set(-100, -100, false)
		}

	case StateStopped, StateBuffering, StateWaitingStart, StatePaused:
		p.levels.Set(-100, -100, false)
		p.frameAccumulator = 0
	}
}
