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
	"sync/atomic"
)

// LevelsSnapshot captures an immutable stereo audio measurement.
type LevelsSnapshot struct {
	LeftDB  float64
	RightDB float64
	Playing bool
}

// AtomicLevels stores instantaneous stereo audio levels using atomic pointer snapshots,
// guaranteeing a 100% atomic read snapshot with zero allocations on the read path.
type AtomicLevels struct {
	snapshot atomic.Pointer[LevelsSnapshot]
}

// NewAtomicLevels creates an initialized AtomicLevels instance with silence (-100 dB).
func NewAtomicLevels() *AtomicLevels {
	al := &AtomicLevels{}
	al.snapshot.Store(&LevelsSnapshot{
		LeftDB:  -100,
		RightDB: -100,
		Playing: false,
	})
	return al
}

// Set stores the dB levels and playing state in an atomic transaction.
func (a *AtomicLevels) Set(leftDB, rightDB float64, playing bool) {
	a.snapshot.Store(&LevelsSnapshot{
		LeftDB:  leftDB,
		RightDB: rightDB,
		Playing: playing,
	})
}

// Get loads the current dB levels and playing state atomically with zero allocations.
func (a *AtomicLevels) Get() (leftDB, rightDB float64, playing bool) {
	s := a.snapshot.Load()
	if s == nil {
		return -100, -100, false
	}
	return s.LeftDB, s.RightDB, s.Playing
}
