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

package slimvu

import (
	"sync"
	"testing"
)

func TestAtomicLevels_InitialState(t *testing.T) {
	al := NewAtomicLevels()
	left, right, playing := al.Get()
	if left != -100 || right != -100 || playing {
		t.Fatalf("expected initial state (-100, -100, false), got (%f, %f, %v)", left, right, playing)
	}
}

func TestAtomicLevels_SetAndGet(t *testing.T) {
	al := NewAtomicLevels()
	al.Set(-12.5, -14.2, true)
	left, right, playing := al.Get()
	if left != -12.5 || right != -14.2 || !playing {
		t.Fatalf("expected (-12.5, -14.2, true), got (%f, %f, %v)", left, right, playing)
	}
}

func TestAtomicLevels_Concurrency(t *testing.T) {
	al := NewAtomicLevels()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(val float64) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				al.Set(val, val, true)
			}
		}(float64(i))
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				al.Get()
			}
		}()
	}

	wg.Wait()
}
