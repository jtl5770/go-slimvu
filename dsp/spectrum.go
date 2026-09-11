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
	// invMaxPCM16 converts a 16-bit signed PCM sample [-32768, 32767] to [-1.0, 1.0].
	invMaxPCM16 = float32(1.0 / 32768.0)
	// DefaultDecayRateDBPerSec is the ballistic decay speed in dB per second (100 dB/s).
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

// SpectrumAnalyzer computes real-time 16-band stereo spectrum measurements from continuous stereo PCM.
// It leverages a single complex radix-2 FFT to compute both Left and Right spectrums simultaneously
// via complex conjugate decomposition, achieving full stereo analysis at virtually zero extra CPU cost.
// All working buffers are pre-allocated upfront, ensuring 100% zero heap allocations in the hot path.
type bandBinRange struct {
	kStart int
	kEnd   int
}

type SpectrumAnalyzer struct {
	ringBufL       [MaxRingBufferSize]float32
	ringBufR       [MaxRingBufferSize]float32
	ringPos        int
	realBuf        [MaxRingBufferSize]float32
	imagBuf        [MaxRingBufferSize]float32
	levelsLeft     [SpectrumBandsCount]float32
	levelsRight    [SpectrumBandsCount]float32
	decayRate      float32
	lastSampleRate uint32
	lastN          int
	bandRanges     [SpectrumBandsCount]bandBinRange
}

// NewSpectrumAnalyzer creates an initialized SpectrumAnalyzer with silence (-100 dBFS) on both channels.
func NewSpectrumAnalyzer() *SpectrumAnalyzer {
	sa := &SpectrumAnalyzer{
		decayRate: DefaultDecayRateDBPerSec,
	}
	for i := range sa.levelsLeft {
		sa.levelsLeft[i] = float32(SilenceFloorDB)
		sa.levelsRight[i] = float32(SilenceFloorDB)
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

// Process pushes incoming 16-bit stereo LittleEndian PCM audio, executes the windowed stereo FFT,
// applies 16-band logarithmic aggregation, spectral tilt, and ballistics smoothing for both Left and Right channels,
// and copies the resulting dB levels into dstLeft and dstRight (if non-nil).
// Operates with zero heap allocations.
func (s *SpectrumAnalyzer) Process(pcm []byte, sampleRate uint32, dtSeconds float32, dstLeft, dstRight *[SpectrumBandsCount]float32) {
	frames := len(pcm) / Stereo16BitFrameBytes
	if frames == 0 {
		s.DecaySilence(dtSeconds, dstLeft, dstRight)
		return
	}

	if sampleRate == 0 {
		sampleRate = 44100
	}

	// 1. Ingest stereo PCM samples into Left and Right ring buffers
	s.ingestStereoPCM(pcm)

	// 2. Select precomputed FFT plan
	n := SelectFFTSize(sampleRate)
	plan := getFFTPlan(n)
	if plan == nil {
		s.DecaySilence(dtSeconds, dstLeft, dstRight)
		return
	}

	// 3. Extract the last N samples, apply Hann window, and pack Left into real and Right into imag
	s.applyWindow(n, plan)

	// 4. Compute in-place complex FFT (both channels transformed simultaneously)
	plan.computeRadix2FFT(s.realBuf[:n], s.imagBuf[:n])

	// 5. Decompose complex spectrum into discrete Left & Right bands with ballistics
	decay := s.calcDecay(dtSeconds)
	s.aggregateBands(n, sampleRate, plan, decay)

	// 6. Copy output
	if dstLeft != nil {
		*dstLeft = s.levelsLeft
	}
	if dstRight != nil {
		*dstRight = s.levelsRight
	}
}

// ingestStereoPCM separates stereo 16-bit PCM frames into Left and Right float32 channels in [-1.0, 1.0]
// using single 32-bit loads and bitwise ring buffer wrapping.
func (s *SpectrumAnalyzer) ingestStereoPCM(pcm []byte) {
	pos := s.ringPos
	for offset := 0; offset <= len(pcm)-Stereo16BitFrameBytes; offset += Stereo16BitFrameBytes {
		frame := binary.LittleEndian.Uint32(pcm[offset : offset+4])
		sL := int32(int16(frame))
		sR := int32(int16(frame >> 16))
		s.ringBufL[pos] = float32(sL) * invMaxPCM16
		s.ringBufR[pos] = float32(sR) * invMaxPCM16
		pos = (pos + 1) & ringMask
	}
	s.ringPos = pos
}

// applyWindow copies the last n samples from the Left ring buffer into realBuf and Right into imagBuf with Hann windowing.
func (s *SpectrumAnalyzer) applyWindow(n int, plan *fftPlan) {
	start := (s.ringPos - n + MaxRingBufferSize) & ringMask
	for k := 0; k < n; k++ {
		idx := (start + k) & ringMask
		w := plan.hannWindow[k]
		s.realBuf[k] = s.ringBufL[idx] * w
		s.imagBuf[k] = s.ringBufR[idx] * w
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

// aggregateBands separates Left & Right frequency bins via Hermitian symmetry decomposition,
// computes fractional-octave integrated energy per band, converts to dBFS, and applies attack/decay ballistics.
func (s *SpectrumAnalyzer) aggregateBands(n int, sampleRate uint32, plan *fftPlan, decay float32) {
	if sampleRate != s.lastSampleRate || n != s.lastN {
		s.updateBandRanges(n, sampleRate)
	}

	for b := 0; b < SpectrumBandsCount; b++ {
		br := s.bandRanges[b]

		var sumMagSqL, sumMagSqR float32
		for k := br.kStart; k <= br.kEnd; k++ {
			rNk := s.realBuf[n-k]
			iNk := s.imagBuf[n-k]
			rk := s.realBuf[k]
			ik := s.imagBuf[k]

			// Left channel: X_L[k] = 0.5 * (Z[k] + conj(Z[n-k]))
			reL := (rk + rNk) * 0.5
			imL := (ik - iNk) * 0.5
			sumMagSqL += reL*reL + imL*imL

			// Right channel: X_R[k] = -0.5j * (Z[k] - conj(Z[n-k]))
			reR := (ik + iNk) * 0.5
			imR := (rNk - rk) * 0.5
			sumMagSqR += reR*reR + imR*imR
		}

		// Left channel dBFS calculation
		valL := float64(sumMagSqL)
		if valL < minMagSqClamp {
			valL = minMagSqClamp
		}
		bandDBL := float32(10.0*math.Log10(valL)) + plan.normDB + bandTiltDB[b]
		if bandDBL < float32(SilenceFloorDB) {
			bandDBL = float32(SilenceFloorDB)
		} else if bandDBL > 0.0 {
			bandDBL = 0.0
		}

		// Right channel dBFS calculation
		valR := float64(sumMagSqR)
		if valR < minMagSqClamp {
			valR = minMagSqClamp
		}
		bandDBR := float32(10.0*math.Log10(valR)) + plan.normDB + bandTiltDB[b]
		if bandDBR < float32(SilenceFloorDB) {
			bandDBR = float32(SilenceFloorDB)
		} else if bandDBR > 0.0 {
			bandDBR = 0.0
		}

		// Left ballistics: instant rise, smooth decay
		if bandDBL >= s.levelsLeft[b] {
			s.levelsLeft[b] = bandDBL
		} else {
			s.levelsLeft[b] -= decay
			if s.levelsLeft[b] < bandDBL {
				s.levelsLeft[b] = bandDBL
			}
			if s.levelsLeft[b] < float32(SilenceFloorDB) {
				s.levelsLeft[b] = float32(SilenceFloorDB)
			}
		}

		// Right ballistics: instant rise, smooth decay
		if bandDBR >= s.levelsRight[b] {
			s.levelsRight[b] = bandDBR
		} else {
			s.levelsRight[b] -= decay
			if s.levelsRight[b] < bandDBR {
				s.levelsRight[b] = bandDBR
			}
			if s.levelsRight[b] < float32(SilenceFloorDB) {
				s.levelsRight[b] = float32(SilenceFloorDB)
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

// DecaySilence decays all Left and Right frequency bands toward the silence floor (-100 dBFS).
func (s *SpectrumAnalyzer) DecaySilence(dtSeconds float32, dstLeft, dstRight *[SpectrumBandsCount]float32) {
	decay := s.calcDecay(dtSeconds)
	for b := 0; b < SpectrumBandsCount; b++ {
		s.levelsLeft[b] -= decay
		if s.levelsLeft[b] < float32(SilenceFloorDB) {
			s.levelsLeft[b] = float32(SilenceFloorDB)
		}
		s.levelsRight[b] -= decay
		if s.levelsRight[b] < float32(SilenceFloorDB) {
			s.levelsRight[b] = float32(SilenceFloorDB)
		}
	}
	if dstLeft != nil {
		*dstLeft = s.levelsLeft
	}
	if dstRight != nil {
		*dstRight = s.levelsRight
	}
}

// Reset clears the internal ring buffers and resets all levels to silence.
func (s *SpectrumAnalyzer) Reset() {
	for i := range s.ringBufL {
		s.ringBufL[i] = 0
		s.ringBufR[i] = 0
	}
	s.ringPos = 0
	for i := range s.levelsLeft {
		s.levelsLeft[i] = float32(SilenceFloorDB)
		s.levelsRight[i] = float32(SilenceFloorDB)
	}
}
