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
// along with go-slimvu.  If not, see <https://www.gnu.org/licenses/>.\n
package slimproto

import (
	"math"
	"sync/atomic"
)

// LevelsSnapshot captures an immutable stereo audio measurement.
type LevelsSnapshot struct {
	LeftDB  float64
	RightDB float64
	Playing bool
}

// AtomicLevels stores instantaneous stereo audio levels using a packed 64-bit atomic integer,
// guaranteeing 100% lock-free, zero-allocation operations on both read and write paths.
//
// Bit packing layout:
//   - Bits [0..23]:  Left dB level stored as signed 24-bit integer centibels (dB * 100)
//   - Bits [24..47]: Right dB level stored as signed 24-bit integer centibels (dB * 100)
//   - Bit  [48]:     Playing boolean flag (1 = true, 0 = false)
//   - Bits [49..63]: Reserved
type AtomicLevels struct {
	state atomic.Uint64
}

// NewAtomicLevels creates an initialized AtomicLevels instance with silence (-100 dB).
func NewAtomicLevels() *AtomicLevels {
	al := &AtomicLevels{}
	al.Set(-100, -100, false)
	return al
}

// Set stores the dB levels and playing state in an atomic transaction without any heap allocations.
func (a *AtomicLevels) Set(leftDB, rightDB float64, playing bool) {
	l := int32(math.Round(leftDB * 100.0))
	r := int32(math.Round(rightDB * 100.0))

	var p uint64
	if playing {
		p = 1
	}

	packed := (uint64(uint32(l)) & 0xFFFFFF) |
		((uint64(uint32(r)) & 0xFFFFFF) << 24) |
		(p << 48)

	a.state.Store(packed)
}

// Get loads the current dB levels and playing state atomically with zero allocations.
func (a *AtomicLevels) Get() (leftDB, rightDB float64, playing bool) {
	packed := a.state.Load()

	lRaw := int32(uint32(packed & 0xFFFFFF))
	if lRaw&0x800000 != 0 {
		lRaw |= ^0xFFFFFF
	}

	rRaw := int32(uint32((packed >> 24) & 0xFFFFFF))
	if rRaw&0x800000 != 0 {
		rRaw |= ^0xFFFFFF
	}

	playing = ((packed >> 48) & 1) == 1
	return float64(lRaw) / 100.0, float64(rRaw) / 100.0, playing
}
