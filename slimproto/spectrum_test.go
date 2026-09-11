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

func TestAtomicSpectrum_InitialSilence(t *testing.T) {
	as := NewAtomicSpectrum()
	var bufL, bufR [SpectrumBandsCount]float32
	n := as.CopyTo(bufL[:], bufR[:])
	if n != SpectrumBandsCount {
		t.Fatalf("Expected %d bands copied, got %d", SpectrumBandsCount, n)
	}

	for i := 0; i < SpectrumBandsCount; i++ {
		if bufL[i] != -100.0 {
			t.Errorf("Left Band %d: expected -100.0, got %.2f", i, bufL[i])
		}
		if bufR[i] != -100.0 {
			t.Errorf("Right Band %d: expected -100.0, got %.2f", i, bufR[i])
		}
	}
}

func TestAtomicSpectrum_SetAndCopy(t *testing.T) {
	as := NewAtomicSpectrum()

	var testLevelsL, testLevelsR [SpectrumBandsCount]float32
	for i := range testLevelsL {
		testLevelsL[i] = float32(-10.0 + float32(i))
		testLevelsR[i] = float32(-20.0 - float32(i))
	}

	as.Set(&testLevelsL, &testLevelsR)

	var dstL, dstR [SpectrumBandsCount]float32
	n := as.CopyTo(dstL[:], dstR[:])
	if n != SpectrumBandsCount {
		t.Fatalf("Expected %d bands copied, got %d", SpectrumBandsCount, n)
	}

	for i := range testLevelsL {
		if dstL[i] != testLevelsL[i] {
			t.Errorf("Left Band %d: expected %.2f, got %.2f", i, testLevelsL[i], dstL[i])
		}
		if dstR[i] != testLevelsR[i] {
			t.Errorf("Right Band %d: expected %.2f, got %.2f", i, testLevelsR[i], dstR[i])
		}
	}

	// Partial copy
	var partialL, partialR [4]float32
	n = as.CopyTo(partialL[:], partialR[:])
	if n != 4 {
		t.Fatalf("Expected 4 bands copied, got %d", n)
	}
	for i := 0; i < 4; i++ {
		if partialL[i] != testLevelsL[i] {
			t.Errorf("Partial Left band %d: expected %.2f, got %.2f", i, testLevelsL[i], partialL[i])
		}
		if partialR[i] != testLevelsR[i] {
			t.Errorf("Partial Right band %d: expected %.2f, got %.2f", i, testLevelsR[i], partialR[i])
		}
	}
}

func TestAtomicSpectrum_ZeroAllocations(t *testing.T) {
	as := NewAtomicSpectrum()
	var testLevelsL, testLevelsR [SpectrumBandsCount]float32
	var dstL, dstR [SpectrumBandsCount]float32

	// Verify Set has 0 heap allocations
	setAllocs := testing.AllocsPerRun(100, func() {
		as.Set(&testLevelsL, &testLevelsR)
	})
	if setAllocs != 0 {
		t.Errorf("Expected 0 allocations in AtomicSpectrum.Set, got %.2f", setAllocs)
	}

	// Verify CopyTo has 0 heap allocations
	copyAllocs := testing.AllocsPerRun(100, func() {
		as.CopyTo(dstL[:], dstR[:])
	})
	if copyAllocs != 0 {
		t.Errorf("Expected 0 allocations in AtomicSpectrum.CopyTo, got %.2f", copyAllocs)
	}
}

func TestAtomicSpectrum_ConcurrentAccess(t *testing.T) {
	as := NewAtomicSpectrum()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		var levelsL, levelsR [SpectrumBandsCount]float32
		var counter float32
		for {
			select {
			case <-stop:
				return
			default:
				counter += 0.1
				for i := range levelsL {
					levelsL[i] = counter + float32(i)
					levelsR[i] = counter - float32(i)
				}
				as.Set(&levelsL, &levelsR)
			}
		}
	}()

	// Reader goroutines
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var bufL, bufR [SpectrumBandsCount]float32
			for {
				select {
				case <-stop:
					return
				default:
					n := as.CopyTo(bufL[:], bufR[:])
					if n != SpectrumBandsCount {
						t.Errorf("Concurrent read expected %d bands, got %d", SpectrumBandsCount, n)
						return
					}
				}
			}
		}()
	}

	// Run concurrently for a brief period
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
