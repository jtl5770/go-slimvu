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

	"github.com/charmbracelet/lipgloss"
)

const (
	ansiReset = "\x1b[0m"
	ansiOff   = "\x1b[38;2;46;52;64m" // #2E3440
	ansiRev   = "\x1b[7m"
)

type colorRGB struct {
	r, g, b uint8
}

func (c colorRGB) String() string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
}

func (c colorRGB) BoldString() string {
	return fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm", c.r, c.g, c.b)
}

type meterColorEntry struct {
	color   colorRGB
	str     string
	boldStr string
	revStr  string
}

var meterColorLUT [256]meterColorEntry

var (
	styleBarLabel       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ECEFF4"))
	styleBarVal         = lipgloss.NewStyle().Foreground(lipgloss.Color("#D8DEE9"))
	styleScale          = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	styleTrackIcon      = lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0"))
	styleTrackTitle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ECEFF4")).Bold(true)
	styleTrackBadge     = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1"))
	styleCoverBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4C566A")).MarginRight(2)
	styleCoverLabel     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A")).Bold(true)
	styleHeaderTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#88C0D0"))
	styleStatusPlaying  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00E676"))
	styleStatusIdle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4C566A"))
	styleSynced         = lipgloss.NewStyle().Foreground(lipgloss.Color("#EBCB8B")).Bold(true)
	styleHelp           = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	styleDisabled       = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4252"))
	styleAutoSyncActive = lipgloss.NewStyle().Foreground(lipgloss.Color("#00E676")).Bold(true)
	styleVuMargin       = lipgloss.NewStyle().MarginTop(1)
	styleContainer      = lipgloss.NewStyle().MarginLeft(1).MarginTop(1)
	styleSep            = styleHelp.Render(" • ")

	renderedLabelL        = styleBarLabel.Render("L")
	renderedLabelR        = styleBarLabel.Render("R")
	renderedValInf        = styleBarVal.Render(" -inf  dB")
	renderedStatusPlaying = styleStatusPlaying.Render("● PLAYING")
	renderedStatusIdle    = styleStatusIdle.Render("■ IDLE")

	renderedFooterAutoSyncOnSynced    = buildStaticFooter(true, true)
	renderedFooterAutoSyncOnUnsynced  = buildStaticFooter(true, false)
	renderedFooterAutoSyncOffSynced   = buildStaticFooter(false, true)
	renderedFooterAutoSyncOffUnsynced = buildStaticFooter(false, false)
)

func buildStaticFooter(autoSync, isSynced bool) string {
	var iconStr string
	if autoSync {
		iconStr = styleAutoSyncActive.Render("⇆")
	} else {
		iconStr = styleHelp.Render("⇆")
	}
	autoSyncItem := fmt.Sprintf("%s %s", styleHelp.Render("[a] Auto sync"), iconStr)

	playPauseStyle := styleHelp
	prevNextStyle := styleHelp
	if !isSynced {
		playPauseStyle = styleDisabled
		prevNextStyle = styleDisabled
	}

	return fmt.Sprintf("%s%s%s%s%s%s%s%s%s%s%s",
		playPauseStyle.Render("[Space] Play/Pause"),
		styleSep,
		prevNextStyle.Render("[←/→] Prev/Next"),
		styleSep,
		styleHelp.Render("[s] Sync"),
		styleSep,
		autoSyncItem,
		styleSep,
		styleHelp.Render("[t] Effect"),
		styleSep,
		styleHelp.Render("[q] Quit"),
	)
}

func computeMeterColor(t float64) colorRGB {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}

	var r, g, b float64
	// Green:  #00E676 -> (0, 230, 118)
	// Yellow: #FFD600 -> (255, 214, 0)
	// Red:    #FF1744 -> (255, 23, 68)
	// Transition 1 centered at 0.60 (from 0.45 to 0.75)
	// Transition 2 centered at 0.85 (from 0.75 to 0.95)
	if t < 0.45 {
		r, g, b = 0, 230, 118
	} else if t < 0.75 {
		f := (t - 0.45) / (0.75 - 0.45)
		r = 0 + f*(255-0)
		g = 230 + f*(214-230)
		b = 118 + f*(0-118)
	} else if t < 0.95 {
		f := (t - 0.75) / (0.95 - 0.75)
		r = 255
		g = 214 + f*(23-214)
		b = 0 + f*(68-0)
	} else {
		r, g, b = 255, 23, 68
	}

	return colorRGB{
		r: uint8(math.Round(r)),
		g: uint8(math.Round(g)),
		b: uint8(math.Round(b)),
	}
}

func getMeterColorEntry(t float64) meterColorEntry {
	if t <= 0 {
		return meterColorLUT[0]
	}
	if t >= 1 {
		return meterColorLUT[255]
	}
	idx := int(t * 255.0)
	if idx > 255 {
		idx = 255
	}
	return meterColorLUT[idx]
}

func init() {
	for i := 0; i < 256; i++ {
		t := float64(i) / 255.0
		c := computeMeterColor(t)
		meterColorLUT[i] = meterColorEntry{
			color:   c,
			str:     c.String(),
			boldStr: c.BoldString(),
			revStr:  c.String() + "\x1b[7m",
		}
	}
}
