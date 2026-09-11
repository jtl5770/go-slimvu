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
	"math"
	"sync/atomic"

	"github.com/jtl5770/go-slimvu/dsp"
)

// SpectrumBandsCount defines the standard 16 logarithmic frequency bands.
const SpectrumBandsCount = dsp.SpectrumBandsCount

// AtomicSpectrum stores real-time 16-band stereo audio spectrum levels using atomic 32-bit floats.
// Provides 100% lock-free, zero-allocation, thread-safe access on both read and write paths.
type AtomicSpectrum struct {
	bandsLeft  [SpectrumBandsCount]atomic.Uint32
	bandsRight [SpectrumBandsCount]atomic.Uint32
}

// NewAtomicSpectrum creates an initialized AtomicSpectrum with silence (-100 dBFS) across all stereo bands.
func NewAtomicSpectrum() *AtomicSpectrum {
	as := &AtomicSpectrum{}
	silenceBits := math.Float32bits(float32(dsp.SilenceFloorDB))
	for b := 0; b < SpectrumBandsCount; b++ {
		as.bandsLeft[b].Store(silenceBits)
		as.bandsRight[b].Store(silenceBits)
	}
	return as
}

// Set stores all 16 Left and Right frequency band levels atomically with zero heap allocations.
func (a *AtomicSpectrum) Set(levelsLeft, levelsRight *[SpectrumBandsCount]float32) {
	for b := 0; b < SpectrumBandsCount; b++ {
		a.bandsLeft[b].Store(math.Float32bits(levelsLeft[b]))
		a.bandsRight[b].Store(math.Float32bits(levelsRight[b]))
	}
}

// CopyTo copies the current stereo spectrum levels into dstLeft and dstRight and returns the number of bands copied per channel.
// Operates lock-free, thread-safe, and with 0 heap allocations.
func (a *AtomicSpectrum) CopyTo(dstLeft, dstRight []float32) int {
	nL := len(dstLeft)
	if nL > SpectrumBandsCount {
		nL = SpectrumBandsCount
	}
	for b := 0; b < nL; b++ {
		dstLeft[b] = math.Float32frombits(a.bandsLeft[b].Load())
	}

	nR := len(dstRight)
	if nR > SpectrumBandsCount {
		nR = SpectrumBandsCount
	}
	for b := 0; b < nR; b++ {
		dstRight[b] = math.Float32frombits(a.bandsRight[b].Load())
	}

	if nL < nR {
		return nL
	}
	return nR
}
