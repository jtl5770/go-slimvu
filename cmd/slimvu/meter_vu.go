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

var (
	scaleTickDBs = [...]float64{-60, -48, -36, -24, -18, -12, -6, -3, 0}
	subBlocks    = [9]string{"", "\u258f", "\u258e", "\u258d", "\u258c", "\u258b", "\u258a", "\u2589", "\u2588"}
	scaleCache   [128]string
)

type vuBarBuffers struct {
	l strings.Builder
	r strings.Builder
}

func (m model) renderBar(label string, db float64, peak peakInfo, barLen int) string {
	clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
	level := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
	barPos := level * float64(barLen)

	peakCell := -1
	if peak.position >= 0.125 {
		peakCell = int(peak.position)
		if peakCell >= barLen {
			peakCell = barLen - 1
		}
	}

	isL := (label == "L")

	var sb *strings.Builder
	if m.vuBuf != nil {
		if isL {
			sb = &m.vuBuf.l
		} else {
			sb = &m.vuBuf.r
		}
		sb.Reset()
	} else {
		sb = &strings.Builder{}
	}
	sb.Grow(barLen*24 + 48)

	if isL {
		sb.WriteString(renderedLabelL)
	} else {
		sb.WriteString(renderedLabelR)
	}
	sb.WriteString("  ")

	for i := 0; i < barLen; i++ {
		t := float64(i) / float64(barLen-1)
		col := getMeterColorEntry(t)
		isPeak := (i == peakCell)

		litCells := int(math.Round(barPos))
		if i < litCells {
			// Lit cell (uses upper 7/8 for L and lower 7/8 for R to maintain gap)
			if isPeak {
				sb.WriteString(peak.boldStr)
				if isL {
					sb.WriteString(ansiRev)
					sb.WriteString("\u2581")
				} else {
					sb.WriteString("\u2587")
				}
				sb.WriteString(ansiReset)
			} else {
				if isL {
					sb.WriteString(col.revStr)
					sb.WriteString("\u2581")
				} else {
					sb.WriteString(col.str)
					sb.WriteString("\u2587")
				}
				sb.WriteString(ansiReset)
			}

		} else {
			// Dark / off region beyond bar
			if isPeak {
				// Floating peak needle positioned at sub-pixel accuracy
				frac := peak.position - float64(i)
				sb.WriteString(peak.boldStr)
				if frac < 0.25 {
					sb.WriteString("\u258f")
				} else if frac < 0.50 {
					sb.WriteString("\u258e")
				} else if frac < 0.75 {
					sb.WriteString("\u258c")
				} else {
					sb.WriteString("\u2595")
				}
				sb.WriteString(ansiReset)
			} else {
				sb.WriteString(ansiOff)
				sb.WriteString("\u2591")
				sb.WriteString(ansiReset)
			}
		}
	}

	sb.WriteString(" ")
	if !m.playing || db <= m.minDB {
		sb.WriteString(renderedValInf)
	} else {
		sb.WriteString(styleBarVal.Render(fmt.Sprintf("%6.1f dB", db)))
	}

	return sb.String()
}

func renderScale(barLen int, minDB, maxDB float64) string {
	if barLen >= 0 && barLen < len(scaleCache) && scaleCache[barLen] != "" {
		return scaleCache[barLen]
	}

	scaleLine := make([]byte, barLen)
	for i := range scaleLine {
		scaleLine[i] = ' '
	}

	for _, db := range scaleTickDBs {
		if db < minDB || db > maxDB {
			continue
		}
		pos := int(math.Round(((db - minDB) / (maxDB - minDB)) * float64(barLen-1)))
		if pos >= 0 && pos < barLen {
			scaleLine[pos] = '|'
		}
	}

	indent := strings.Repeat(" ", 3) // "L  " = 3
	res := indent + styleScale.Render(string(scaleLine))
	if barLen >= 0 && barLen < len(scaleCache) {
		scaleCache[barLen] = res
	}
	return res
}
