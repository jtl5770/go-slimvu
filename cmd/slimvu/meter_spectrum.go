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
	"math"
	"strings"
)

var spectrumFreqLabelsCompact = [16]string{
	"25", "  ", "63", "  ", " ·", "  ", " ·", "  ",
	"1k", "  ", "2k", "  ", "6k", "  ", "16", "  ",
}

var spectrumFreqLabels = [16]string{
	"25", "40", "63", "100", "160", "250", "400", "630",
	"1k", "1.6", "2.5", "4k", "6.3", "10k", "16k", "20k",
}

var vertBlocks = [9]string{
	" ",
	"\u2581", // lower 1/8
	"\u2582", // lower 1/4
	"\u2583", // lower 3/8
	"\u2584", // lower 1/2
	"\u2585", // lower 5/8
	"\u2586", // lower 3/4
	"\u2587", // lower 7/8
	"\u2588", // full block
}

type spectrumCell struct {
	charStr  string
	colorStr string
}

type spectrumLUTEntry struct {
	top    spectrumCell
	bottom spectrumCell
}

var spectrumLUT [17]spectrumLUTEntry
var spectrumScaleCache [128]string

type spectrumBuffers struct {
	top strings.Builder
	bot strings.Builder
}

const spaces128 = "                                                                                                                                "

func writeSpaces(sb *strings.Builder, n int) {
	for n > len(spaces128) {
		sb.WriteString(spaces128)
		n -= len(spaces128)
	}
	if n > 0 {
		sb.WriteString(spaces128[:n])
	}
}

func init() {
	for s := 0; s <= 16; s++ {
		t := float64(s) / 16.0
		colStr := getMeterColorEntry(t).str

		// Bottom cell
		if s == 0 {
			spectrumLUT[s].bottom = spectrumCell{charStr: " ", colorStr: ""}
		} else if s < 8 {
			spectrumLUT[s].bottom = spectrumCell{
				charStr:  vertBlocks[s],
				colorStr: colStr,
			}
		} else {
			spectrumLUT[s].bottom = spectrumCell{
				charStr:  vertBlocks[8],
				colorStr: colStr,
			}
		}

		// Top cell
		if s <= 8 {
			spectrumLUT[s].top = spectrumCell{charStr: " ", colorStr: ""}
		} else if s < 16 {
			spectrumLUT[s].top = spectrumCell{
				charStr:  vertBlocks[s-8],
				colorStr: colStr,
			}
		} else {
			spectrumLUT[s].top = spectrumCell{
				charStr:  vertBlocks[8],
				colorStr: colStr,
			}
		}
	}
}

func renderSpectrumScale(barLen int) string {
	if barLen >= 0 && barLen < len(spectrumScaleCache) && spectrumScaleCache[barLen] != "" {
		return spectrumScaleCache[barLen]
	}

	binWidth := barLen / 16
	if binWidth < 1 {
		binWidth = 1
	}
	contentW := binWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	sepW := binWidth - contentW

	totalBandsW := 16 * binWidth
	trailingPad := barLen - totalBandsW
	if trailingPad < 0 {
		trailingPad = 0
	}

	var sb strings.Builder
	sb.Grow(barLen + 32)

	for i := 0; i < 16; i++ {
		var lbl string
		if contentW >= 3 {
			lbl = spectrumFreqLabels[i]
		} else if contentW == 2 {
			lbl = spectrumFreqLabelsCompact[i]
		} else {
			lbl = "·"
		}

		runes := []rune(lbl)
		if len(runes) <= contentW {
			padL := (contentW - len(runes)) / 2
			padR := contentW - len(runes) - padL
			writeSpaces(&sb, padL)
			sb.WriteString(string(runes))
			writeSpaces(&sb, padR)
		} else {
			sb.WriteString(string(runes[:contentW]))
		}
		writeSpaces(&sb, sepW)
	}
	if trailingPad > 0 {
		writeSpaces(&sb, trailingPad)
	}

	indent := "   " // "   " matching "L  " = 3
	res := indent + styleScale.Render(sb.String())
	if barLen >= 0 && barLen < len(spectrumScaleCache) {
		spectrumScaleCache[barLen] = res
	}
	return res
}

func (m model) renderSpectrum(barLen int) (string, string, string) {
	if m.specBuf == nil {
		m.specBuf = &spectrumBuffers{}
	}

	binWidth := barLen / 16
	if binWidth < 1 {
		binWidth = 1
	}
	contentW := binWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	sepW := binWidth - contentW

	totalBandsW := 16 * binWidth
	trailingPad := barLen - totalBandsW
	if trailingPad < 0 {
		trailingPad = 0
	}

	m.specBuf.top.Reset()
	m.specBuf.bot.Reset()
	m.specBuf.top.Grow(barLen*24 + 48)
	m.specBuf.bot.Grow(barLen*24 + 48)

	indent := "   "
	m.specBuf.top.WriteString(indent)
	m.specBuf.bot.WriteString(indent)

	for i := 0; i < 16; i++ {
		db := float64(m.spectrumBands[i])
		clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
		norm := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
		step := int(math.Round(norm * 16.0))
		if step < 0 {
			step = 0
		} else if step > 16 {
			step = 16
		}

		entry := spectrumLUT[step]

		// Top line
		if entry.top.colorStr != "" {
			m.specBuf.top.WriteString(entry.top.colorStr)
			for k := 0; k < contentW; k++ {
				m.specBuf.top.WriteString(entry.top.charStr)
			}
			m.specBuf.top.WriteString(ansiReset)
		} else {
			for k := 0; k < contentW; k++ {
				m.specBuf.top.WriteByte(' ')
			}
		}
		for k := 0; k < sepW; k++ {
			m.specBuf.top.WriteByte(' ')
		}

		// Bottom line
		if entry.bottom.colorStr != "" {
			m.specBuf.bot.WriteString(entry.bottom.colorStr)
			for k := 0; k < contentW; k++ {
				m.specBuf.bot.WriteString(entry.bottom.charStr)
			}
			m.specBuf.bot.WriteString(ansiReset)
		} else {
			for k := 0; k < contentW; k++ {
				m.specBuf.bot.WriteByte(' ')
			}
		}
		for k := 0; k < sepW; k++ {
			m.specBuf.bot.WriteByte(' ')
		}
	}

	if trailingPad > 0 {
		writeSpaces(&m.specBuf.top, trailingPad)
		writeSpaces(&m.specBuf.bot, trailingPad)
	}

	// Suffix spacing to maintain totalWidth = barLen + 13
	writeSpaces(&m.specBuf.top, 10)
	writeSpaces(&m.specBuf.bot, 10)

	topLine := m.specBuf.top.String()
	botLine := m.specBuf.bot.String()
	scaleLine := renderSpectrumScale(barLen)

	return topLine, botLine, scaleLine
}
