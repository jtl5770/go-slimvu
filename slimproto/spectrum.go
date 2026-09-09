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

// AtomicSpectrum stores real-time 16-band audio spectrum levels using atomic 32-bit floats.
// Provides 100% lock-free, zero-allocation, thread-safe access on both read and write paths.
type AtomicSpectrum struct {
	bands [SpectrumBandsCount]atomic.Uint32
}

// NewAtomicSpectrum creates an initialized AtomicSpectrum with silence (-100 dBFS) across all bands.
func NewAtomicSpectrum() *AtomicSpectrum {
	as := &AtomicSpectrum{}
	silenceBits := math.Float32bits(float32(dsp.SilenceFloorDB))
	for b := 0; b < SpectrumBandsCount; b++ {
		as.bands[b].Store(silenceBits)
	}
	return as
}

// Set stores all 16 frequency band levels atomically with zero heap allocations.
func (a *AtomicSpectrum) Set(levels *[SpectrumBandsCount]float32) {
	for b := 0; b < SpectrumBandsCount; b++ {
		a.bands[b].Store(math.Float32bits(levels[b]))
	}
}

// CopyTo copies the current spectrum levels into dst and returns the number of bands copied.
// Operates lock-free, thread-safe, and with 0 heap allocations.
func (a *AtomicSpectrum) CopyTo(dst []float32) int {
	n := len(dst)
	if n > SpectrumBandsCount {
		n = SpectrumBandsCount
	}
	for b := 0; b < n; b++ {
		dst[b] = math.Float32frombits(a.bands[b].Load())
	}
	return n
}
