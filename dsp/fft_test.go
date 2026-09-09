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
	"testing"
)

func TestFFT_SineWavePeak(t *testing.T) {
	sizes := []int{FFTSize1024, FFTSize2048, FFTSize4096, FFTSize8192}

	for _, n := range sizes {
		plan := getFFTPlan(n)
		if plan == nil {
			t.Fatalf("Plan for size %d returned nil", n)
		}

		realBuf := make([]float32, n)
		imagBuf := make([]float32, n)

		// Target bin: e.g. bin 20
		targetBin := 20
		for i := 0; i < n; i++ {
			realBuf[i] = float32(math.Cos(2.0 * math.Pi * float64(targetBin) * float64(i) / float64(n)))
			imagBuf[i] = 0
		}

		plan.computeRadix2FFT(realBuf, imagBuf)

		// Find peak magnitude
		peakBin := 0
		var maxMag float32
		for i := 0; i < n/2; i++ {
			mag := float32(math.Sqrt(float64(realBuf[i]*realBuf[i] + imagBuf[i]*imagBuf[i])))
			if mag > maxMag {
				maxMag = mag
				peakBin = i
			}
		}

		if peakBin != targetBin {
			t.Errorf("FFT size %d: expected peak at bin %d, got %d (mag %.2f)", n, targetBin, peakBin, maxMag)
		}
	}
}

func TestFFT_ZeroAllocations(t *testing.T) {
	plan := getFFTPlan(FFTSize1024)
	realBuf := make([]float32, FFTSize1024)
	imagBuf := make([]float32, FFTSize1024)

	allocs := testing.AllocsPerRun(100, func() {
		plan.computeRadix2FFT(realBuf, imagBuf)
	})

	if allocs != 0 {
		t.Errorf("Expected 0 allocations in computeRadix2FFT, got %.2f", allocs)
	}
}
