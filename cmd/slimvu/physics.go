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
package main

import (
	"math"
	"time"
)

type peakInfo struct {
	position  float64
	holdUntil time.Time
	boldStr   string
}

func (m *model) updatePeak(peak *peakInfo, db float64, barLen int, dt float64, now time.Time) {
	if !m.playing {
		peak.position = 0
		return
	}

	clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
	level := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
	targetPeak := level * float64(barLen)

	if targetPeak >= peak.position {
		peak.position = targetPeak
		peak.holdUntil = now.Add(m.holdTime)

		t := targetPeak / float64(barLen)
		peak.boldStr = getMeterColorEntry(t).boldStr
	} else {
		if now.After(peak.holdUntil) && dt > 0 {
			peak.position -= m.decayRate * dt
			if peak.position < targetPeak {
				peak.position = targetPeak
			}
		}
	}

	if peak.position < 0 {
		peak.position = 0
	} else if peak.position > float64(barLen) {
		peak.position = float64(barLen)
	}
}
