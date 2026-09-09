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

package dsp

import (
	"encoding/binary"
	"math"
)

const (
	// SpectrumBandsCount is the standard number of logarithmic frequency bands (16).
	SpectrumBandsCount = 16
	// MaxRingBufferSize is the maximum FFT window supported (covers up to 384 kHz).
	MaxRingBufferSize = 8192
	// ringMask enables single-cycle bitwise masking instead of signed modulo division.
	ringMask = MaxRingBufferSize - 1
	// invMaxPCM32 converts sum of two 16-bit signed PCM channels [-65536, 65534] to [-1.0, 1.0].
	invMaxPCM32 = float32(1.0 / 65536.0)
	// DefaultDecayRateDBPerSec is the ballistic decay speed in dB per second (60 dB/s).
	DefaultDecayRateDBPerSec = 100.0
	// minMagSqClamp clamps squared magnitude before log10 to prevent evaluation below -100 dBFS ((1e-5)^2 = 1e-10).
	minMagSqClamp = 1e-10
)

// Logarithmic cutoffs in Hz defining the 16 frequency bands from 20 Hz to 20,000 Hz.
var bandCutoffsHz = [17]float64{
	20.0, 31.0, 48.0, 75.0, 117.0, 182.0, 284.0, 442.0,
	689.0, 1074.0, 1673.0, 2608.0, 4064.0, 6334.0, 9871.0, 15383.0,
	20000.0,
}

// Tilt boosts higher frequencies relative to ~1 kHz (+0.5 dB per band) to compensate for 1/f pink noise rolloff.
var bandTiltDB = [16]float32{
	-4.0, -3.5, -3.0, -2.5, -2.0, -1.5, -1.0, -0.5,
	0.0, 0.5, 1.0, 1.5, 2.0, 2.5, 3.0, 3.5,
}

// SpectrumAnalyzer computes real-time 16-band spectrum measurements from continuous stereo PCM.
// All working buffers are pre-allocated upfront, ensuring 100% zero heap allocations in the hot path.
type bandBinRange struct {
	kStart int
	kEnd   int
}

type SpectrumAnalyzer struct {
	ringBuf        [MaxRingBufferSize]float32
	ringPos        int
	realBuf        [MaxRingBufferSize]float32
	imagBuf        [MaxRingBufferSize]float32
	levels         [SpectrumBandsCount]float32
	decayRate      float32
	lastSampleRate uint32
	lastN          int
	bandRanges     [SpectrumBandsCount]bandBinRange
}

// NewSpectrumAnalyzer creates an initialized SpectrumAnalyzer with silence (-100 dBFS).
func NewSpectrumAnalyzer() *SpectrumAnalyzer {
	sa := &SpectrumAnalyzer{
		decayRate: DefaultDecayRateDBPerSec,
	}
	for i := range sa.levels {
		sa.levels[i] = float32(SilenceFloorDB)
	}
	return sa
}

// SetDecayRate configures the ballistic decay speed in dB per second.
func (s *SpectrumAnalyzer) SetDecayRate(rate float32) {
	if rate < 0 {
		rate = 0
	}
	s.decayRate = rate
}

// SelectFFTSize maps the audio sample rate to the appropriate power-of-two FFT window.
// Uses N=2048 for <=48 kHz to ensure adequate sub-bass resolution between 20 Hz and 50 Hz.
func SelectFFTSize(sampleRate uint32) int {
	switch {
	case sampleRate <= 48000:
		return FFTSize2048
	case sampleRate <= 96000:
		return FFTSize4096
	default:
		return FFTSize8192
	}
}

// Process pushes incoming 16-bit stereo LittleEndian PCM audio, executes the windowed FFT,
// applies 16-band logarithmic aggregation, spectral tilt, and ballistics smoothing,
// and copies the resulting dB levels into dst.
// Operates with zero heap allocations.
func (s *SpectrumAnalyzer) Process(pcm []byte, sampleRate uint32, dtSeconds float32, dst *[SpectrumBandsCount]float32) {
	frames := len(pcm) / Stereo16BitFrameBytes
	if frames == 0 {
		s.DecaySilence(dtSeconds, dst)
		return
	}

	if sampleRate == 0 {
		sampleRate = 44100
	}

	// 1. Ingest PCM samples into ring buffer
	s.ingestStereoPCM(pcm)

	// 2. Select precomputed FFT plan
	n := SelectFFTSize(sampleRate)
	plan := getFFTPlan(n)
	if plan == nil {
		s.DecaySilence(dtSeconds, dst)
		return
	}

	// 3. Extract the last N samples and apply Hann window
	s.applyWindow(n, plan)

	// 4. Compute in-place FFT
	plan.computeRadix2FFT(s.realBuf[:n], s.imagBuf[:n])

	// 5. Aggregate into 16 logarithmic frequency bands with ballistics
	decay := s.calcDecay(dtSeconds)
	s.aggregateBands(n, sampleRate, plan, decay)

	// 6. Copy output
	if dst != nil {
		*dst = s.levels
	}
}

// ingestStereoPCM downmixes stereo 16-bit PCM frames to mono float32 in [-1.0, 1.0]
// using single 32-bit loads and bitwise ring buffer wrapping.
func (s *SpectrumAnalyzer) ingestStereoPCM(pcm []byte) {
	pos := s.ringPos
	for offset := 0; offset <= len(pcm)-Stereo16BitFrameBytes; offset += Stereo16BitFrameBytes {
		frame := binary.LittleEndian.Uint32(pcm[offset : offset+4])
		sL := int32(int16(frame))
		sR := int32(int16(frame >> 16))
		s.ringBuf[pos] = float32(sL+sR) * invMaxPCM32
		pos = (pos + 1) & ringMask
	}
	s.ringPos = pos
}

// applyWindow copies the last n mono samples from the ring buffer into realBuf with Hann windowing.
func (s *SpectrumAnalyzer) applyWindow(n int, plan *fftPlan) {
	start := (s.ringPos - n + MaxRingBufferSize) & ringMask
	for k := 0; k < n; k++ {
		idx := (start + k) & ringMask
		s.realBuf[k] = s.ringBuf[idx] * plan.hannWindow[k]
		s.imagBuf[k] = 0.0
	}
}

// updateBandRanges precalculates FFT bin boundaries for all 16 bands whenever sample rate or FFT size changes.
func (s *SpectrumAnalyzer) updateBandRanges(n int, sampleRate uint32) {
	deltaF := float64(sampleRate) / float64(n)
	halfN := n / 2

	for b := 0; b < SpectrumBandsCount; b++ {
		kStart := int(math.Floor(bandCutoffsHz[b] / deltaF))
		if kStart < 1 {
			kStart = 1
		}
		kEnd := int(math.Floor(bandCutoffsHz[b+1] / deltaF))
		if kEnd < kStart {
			kEnd = kStart
		}
		if kEnd > halfN {
			kEnd = halfN
		}
		s.bandRanges[b] = bandBinRange{kStart: kStart, kEnd: kEnd}
	}
	s.lastSampleRate = sampleRate
	s.lastN = n
}

// aggregateBands computes peak energy per band, converts to dBFS without runtime sqrt,
// and applies attack and decay ballistics.
func (s *SpectrumAnalyzer) aggregateBands(n int, sampleRate uint32, plan *fftPlan, decay float32) {
	if sampleRate != s.lastSampleRate || n != s.lastN {
		s.updateBandRanges(n, sampleRate)
	}

	for b := 0; b < SpectrumBandsCount; b++ {
		br := s.bandRanges[b]

		// Sum power across all bins in this band (ANSI fractional-octave integrated energy)
		var sumMagSq float32
		for k := br.kStart; k <= br.kEnd; k++ {
			sumMagSq += s.realBuf[k]*s.realBuf[k] + s.imagBuf[k]*s.imagBuf[k]
		}

		// Fast dBFS calculation: 20 * log10(sqrt(P) * W) = 10 * log10(P) + 20 * log10(W)
		val := float64(sumMagSq)
		if val < minMagSqClamp {
			val = minMagSqClamp
		}
		bandDB := float32(10.0*math.Log10(val)) + plan.normDB + bandTiltDB[b]

		if bandDB < float32(SilenceFloorDB) {
			bandDB = float32(SilenceFloorDB)
		} else if bandDB > 0.0 {
			bandDB = 0.0
		}

		// Ballistics: instant rise, smooth exponential decay
		if bandDB >= s.levels[b] {
			s.levels[b] = bandDB
		} else {
			s.levels[b] -= decay
			if s.levels[b] < bandDB {
				s.levels[b] = bandDB
			}
			if s.levels[b] < float32(SilenceFloorDB) {
				s.levels[b] = float32(SilenceFloorDB)
			}
		}
	}
}

func (s *SpectrumAnalyzer) calcDecay(dtSeconds float32) float32 {
	decay := s.decayRate * dtSeconds
	if decay < 0 {
		return 0
	}
	return decay
}

// DecaySilence decays all frequency bands toward the silence floor (-100 dBFS).
// Used during pause, stop, or buffer underruns.
func (s *SpectrumAnalyzer) DecaySilence(dtSeconds float32, dst *[SpectrumBandsCount]float32) {
	decay := s.calcDecay(dtSeconds)
	for b := 0; b < SpectrumBandsCount; b++ {
		s.levels[b] -= decay
		if s.levels[b] < float32(SilenceFloorDB) {
			s.levels[b] = float32(SilenceFloorDB)
		}
	}
	if dst != nil {
		*dst = s.levels
	}
}

// Reset clears the internal ring buffer and resets all levels to silence.
func (s *SpectrumAnalyzer) Reset() {
	for i := range s.ringBuf {
		s.ringBuf[i] = 0
	}
	s.ringPos = 0
	for i := range s.levels {
		s.levels[i] = float32(SilenceFloorDB)
	}
}
