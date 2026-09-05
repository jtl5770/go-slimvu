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
	"github.com/jtl5770/go-slimvu/slimproto"
)

// AudioProvider provides thread-safe, lock-free audio level measurements.
type AudioProvider interface {
	// GetLevels returns the latest left and right dB levels and whether audio is playing.
	GetLevels() (leftDB, rightDB float64, playing bool)
	// Start starts the audio provider background worker and performs initial server discovery.
	// Start must be called before querying levels or player state.
	Start() error
	// Stop stops the audio provider and gracefully cleans up resources.
	Stop() error
}

// LevelsSnapshot captures an immutable stereo audio measurement.
type LevelsSnapshot = slimproto.LevelsSnapshot

// AtomicLevels stores instantaneous stereo audio levels using a packed atomic uint64,
// guaranteeing 100% lock-free, zero-allocation operations on both read and write paths.
type AtomicLevels = slimproto.AtomicLevels

// NewAtomicLevels creates an initialized AtomicLevels instance with silence (-100 dB).
func NewAtomicLevels() *AtomicLevels {
	return slimproto.NewAtomicLevels()
}
