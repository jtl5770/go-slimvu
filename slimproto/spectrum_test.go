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
	var buf [SpectrumBandsCount]float32
	n := as.CopyTo(buf[:])
	if n != SpectrumBandsCount {
		t.Fatalf("Expected %d bands copied, got %d", SpectrumBandsCount, n)
	}

	for i, v := range buf {
		if v != -100.0 {
			t.Errorf("Band %d: expected -100.0, got %.2f", i, v)
		}
	}
}

func TestAtomicSpectrum_SetAndCopy(t *testing.T) {
	as := NewAtomicSpectrum()

	var testLevels [SpectrumBandsCount]float32
	for i := range testLevels {
		testLevels[i] = float32(-10.0 + float32(i))
	}

	as.Set(&testLevels)

	var dst [SpectrumBandsCount]float32
	n := as.CopyTo(dst[:])
	if n != SpectrumBandsCount {
		t.Fatalf("Expected %d bands copied, got %d", SpectrumBandsCount, n)
	}

	for i := range testLevels {
		if dst[i] != testLevels[i] {
			t.Errorf("Band %d: expected %.2f, got %.2f", i, testLevels[i], dst[i])
		}
	}

	// Partial copy
	var partial [4]float32
	n = as.CopyTo(partial[:])
	if n != 4 {
		t.Fatalf("Expected 4 bands copied, got %d", n)
	}
	for i := 0; i < 4; i++ {
		if partial[i] != testLevels[i] {
			t.Errorf("Partial band %d: expected %.2f, got %.2f", i, testLevels[i], partial[i])
		}
	}
}

func TestAtomicSpectrum_ZeroAllocations(t *testing.T) {
	as := NewAtomicSpectrum()
	var testLevels [SpectrumBandsCount]float32
	var dst [SpectrumBandsCount]float32

	// Verify Set has 0 heap allocations
	setAllocs := testing.AllocsPerRun(100, func() {
		as.Set(&testLevels)
	})
	if setAllocs != 0 {
		t.Errorf("Expected 0 allocations in AtomicSpectrum.Set, got %.2f", setAllocs)
	}

	// Verify CopyTo has 0 heap allocations
	copyAllocs := testing.AllocsPerRun(100, func() {
		as.CopyTo(dst[:])
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
		var levels [SpectrumBandsCount]float32
		var counter float32
		for {
			select {
			case <-stop:
				return
			default:
				counter += 0.1
				for i := range levels {
					levels[i] = counter + float32(i)
				}
				as.Set(&levels)
			}
		}
	}()

	// Reader goroutines
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf [SpectrumBandsCount]float32
			for {
				select {
				case <-stop:
					return
				default:
					n := as.CopyTo(buf[:])
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
