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
	"math"
)

// Supported FFT window sizes.
const (
	FFTSize1024 = 1024
	FFTSize2048 = 2048
	FFTSize4096 = 4096
	FFTSize8192 = 8192
)

// fftPlan stores pre-computed twiddle factors, bit-reversal lookup tables,
// and Hann window coefficients for a fixed power-of-two FFT size.
type fftPlan struct {
	size       int
	bitReverse []int
	cosTable   []float32 // cos(-2*pi*k / size) for k in [0, size/2)
	sinTable   []float32 // sin(-2*pi*k / size) for k in [0, size/2)
	hannWindow []float32 // Hann window pre-computed for [0, size)
	windowNorm float32   // 2.0 / sum(hannWindow) for amplitude normalization
	normDB     float32   // 20 * log10(windowNorm) precomputed to eliminate runtime sqrt in dB calculation
}

// Precomputed plans allocated once at initialization.
var (
	plan1024 = newFFTPlan(FFTSize1024)
	plan2048 = newFFTPlan(FFTSize2048)
	plan4096 = newFFTPlan(FFTSize4096)
	plan8192 = newFFTPlan(FFTSize8192)
)

// newFFTPlan initializes precomputed tables for an FFT of length n.
func newFFTPlan(n int) *fftPlan {
	plan := &fftPlan{
		size:       n,
		bitReverse: make([]int, n),
		cosTable:   make([]float32, n/2),
		sinTable:   make([]float32, n/2),
		hannWindow: make([]float32, n),
	}

	// Bit reversal permutation table
	bits := 0
	for (1 << bits) < n {
		bits++
	}
	for i := 0; i < n; i++ {
		rev := 0
		for b := 0; b < bits; b++ {
			if (i & (1 << b)) != 0 {
				rev |= 1 << (bits - 1 - b)
			}
		}
		plan.bitReverse[i] = rev
	}

	// Twiddle factor tables for e^(-2*pi*i * k / n)
	for k := 0; k < n/2; k++ {
		angle := -2.0 * math.Pi * float64(k) / float64(n)
		plan.cosTable[k] = float32(math.Cos(angle))
		plan.sinTable[k] = float32(math.Sin(angle))
	}

	// Hann window: w[k] = 0.5 * (1 - cos(2*pi*k / n))
	var winSum float64
	for k := 0; k < n; k++ {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(k)/float64(n)))
		plan.hannWindow[k] = float32(w)
		winSum += w
	}
	if winSum > 0 {
		plan.windowNorm = float32(2.0 / winSum)
		plan.normDB = float32(20.0 * math.Log10(float64(plan.windowNorm)))
	} else {
		plan.windowNorm = 1.0
		plan.normDB = 0.0
	}

	return plan
}

// getFFTPlan returns the precomputed plan for the given size, or nil if unsupported.
// Uses a direct switch to avoid hash table lookups in the hot path.
func getFFTPlan(n int) *fftPlan {
	switch n {
	case FFTSize1024:
		return plan1024
	case FFTSize2048:
		return plan2048
	case FFTSize4096:
		return plan4096
	case FFTSize8192:
		return plan8192
	default:
		return nil
	}
}

// computeRadix2FFT executes an in-place decimation-in-time radix-2 FFT on realBuf and imagBuf.
// Requires len(realBuf) == len(imagBuf) == plan.size.
// Operates with 0 heap allocations.
func (p *fftPlan) computeRadix2FFT(realBuf, imagBuf []float32) {
	n := p.size

	// 1. Bit-reversal permutation
	for i := 0; i < n; i++ {
		j := p.bitReverse[i]
		if i < j {
			realBuf[i], realBuf[j] = realBuf[j], realBuf[i]
			imagBuf[i], imagBuf[j] = imagBuf[j], imagBuf[i]
		}
	}

	// 2. Cooley-Tukey butterfly stages
	for m := 2; m <= n; m <<= 1 {
		halfM := m >> 1
		step := n / m

		for j := 0; j < halfM; j++ {
			idx := j * step
			c := p.cosTable[idx]
			s := p.sinTable[idx]

			for i := j; i < n; i += m {
				k := i + halfM
				tReal := realBuf[k]*c - imagBuf[k]*s
				tImag := realBuf[k]*s + imagBuf[k]*c

				realBuf[k] = realBuf[i] - tReal
				imagBuf[k] = imagBuf[i] - tImag
				realBuf[i] += tReal
				imagBuf[i] += tImag
			}
		}
	}
}
