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

package main

import (
	"fmt"
	"math"
	"strings"
)

// scrollRunes scrolls the given runes slice with marquee wrap-around at a given tick divider.
func scrollRunes(runes []rune, maxW int, tickCount int, speedDiv int) string {
	if len(runes) <= maxW {
		return string(runes)
	}
	sep := "   •••   "
	fullRunes := append(runes, []rune(sep)...)
	if speedDiv <= 0 {
		speedDiv = 1
	}
	scrollOffset := (tickCount / speedDiv) % len(fullRunes)

	var looped []rune
	for i := 0; i < maxW; i++ {
		idx := (scrollOffset + i) % len(fullRunes)
		looped = append(looped, fullRunes[idx])
	}
	return string(looped)
}

func formatFixedDuration(sec float64, hasHours bool) string {
	if sec < 0 {
		sec = 0
	}
	totalSec := int(math.Round(sec))
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	secs := totalSec % 60
	if hasHours {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func (m model) renderTrackInfo(totalWidth int) string {
	if !m.isSynced() || !m.hasTrack {
		// Return blank line matching width to freeze vertical UI layout
		return strings.Repeat(" ", totalWidth)
	}

	hasHours := m.track.Duration >= 3600 || m.track.Elapsed >= 3600
	var timeParts string
	if m.track.Duration > 0 {
		timeParts = fmt.Sprintf("%s / %s",
			formatFixedDuration(m.track.Elapsed, hasHours),
			formatFixedDuration(m.track.Duration, hasHours),
		)
	} else if m.track.Elapsed > 0 {
		timeParts = formatFixedDuration(m.track.Elapsed, hasHours)
	}

	var trackParts string
	curIdx := m.track.PlaylistIndex
	if curIdx <= 0 {
		curIdx = m.track.TrackNum
	}
	total := m.track.PlaylistTotal
	if total <= 0 {
		total = m.track.TotalTracks
	}

	if total > 0 && curIdx > 0 {
		digits := len(fmt.Sprintf("%d", total))
		trackParts = fmt.Sprintf("[%*d/%d]", digits, curIdx, total)
	} else if curIdx > 0 {
		trackParts = fmt.Sprintf("[#%d]", curIdx)
	}

	rightBadge := strings.TrimSpace(trackParts + "  " + timeParts)
	rightBadgeLen := len([]rune(rightBadge))

	icon := "♫ "
	iconLen := 2
	availWidth := totalWidth - rightBadgeLen - iconLen - 2
	if availWidth < 10 {
		availWidth = 10
	}

	displayTitle := scrollRunes(m.cachedTitleRunes, availWidth, m.tickCount, 6)

	displayLen := len([]rune(displayTitle))
	spacing := totalWidth - (iconLen + displayLen + rightBadgeLen)
	if spacing < 1 {
		spacing = 1
	}

	return fmt.Sprintf("%s%s%s%s",
		styleTrackIcon.Render(icon),
		styleTrackTitle.Render(displayTitle),
		strings.Repeat(" ", spacing),
		styleTrackBadge.Render(rightBadge),
	)
}
