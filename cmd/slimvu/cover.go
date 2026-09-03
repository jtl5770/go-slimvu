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
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// quadrantRunes maps a 4-bit mask (TL, TR, BL, BR) to the corresponding Unicode block rune.
var quadrantRunes = [16]rune{
	0:  ' ', // 0000: all background
	1:  '▗', // 0001: bottom-right
	2:  '▖', // 0010: bottom-left
	3:  '▄', // 0011: bottom half
	4:  '▝', // 0100: top-right
	5:  '▐', // 0101: right half
	6:  '▞', // 0110: diagonal TR + BL
	7:  '▟', // 0111: TR + BL + BR
	8:  '▘', // 1000: top-left
	9:  '▚', // 1001: diagonal TL + BR
	10: '▌', // 1010: left half
	11: '▙', // 1011: TL + BL + BR
	12: '▀', // 1100: top half
	13: '▜', // 1101: TL + TR + BR
	14: '▛', // 1110: TL + TR + BL
	15: '█', // 1111: all foreground
}

func isTerminalGraphicsSupported() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(strings.ToLower(os.Getenv("TERM")), "kitty") ||
		os.Getenv("GHOSTTY_RESOURCES_DIR") != "" ||
		os.Getenv("WEZTERM_PANE") != "" ||
		os.Getenv("COLORTERM") == "truecolor" ||
		os.Getenv("COLORTERM") == "24bit" {
		return true
	}
	return false
}

type rgb struct {
	r, g, b float64
}

func (c rgb) distSq(o rgb) float64 {
	dr := c.r - o.r
	dg := c.g - o.g
	db := c.b - o.b
	return dr*dr + dg*dg + db*db
}

func colorFromRGBA(c color.Color) rgb {
	r, g, b, _ := c.RGBA()
	return rgb{
		r: float64(r >> 8),
		g: float64(g >> 8),
		b: float64(b >> 8),
	}
}

// renderCoverQuadrant renders an image using 2x2 Unicode quadrant sub-pixels with 24-bit TrueColor.
func renderCoverQuadrant(imgData []byte, cols, rows int) ([]string, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return nil, fmt.Errorf("empty image bounds")
	}

	pixelW := cols * 2
	pixelH := rows * 2

	var lines []string
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		for c := 0; c < cols; c++ {
			coords := [4][2]int{
				{c * 2, r * 2},
				{c*2 + 1, r * 2},
				{c * 2, r*2 + 1},
				{c*2 + 1, r*2 + 1},
			}

			var p [4]rgb
			for i, xy := range coords {
				sx := bounds.Min.X + (xy[0]*srcW)/pixelW
				sy := bounds.Min.Y + (xy[1]*srcH)/pixelH
				p[i] = colorFromRGBA(img.At(sx, sy))
			}

			maxDist := -1.0
			c1Idx, c2Idx := 0, 1
			for i := 0; i < 4; i++ {
				for j := i + 1; j < 4; j++ {
					d := p[i].distSq(p[j])
					if d > maxDist {
						maxDist = d
						c1Idx, c2Idx = i, j
					}
				}
			}

			fgCenter := p[c1Idx]
			bgCenter := p[c2Idx]

			mask := 0
			var fgSum, bgSum rgb
			fgCount, bgCount := 0, 0

			for i := 0; i < 4; i++ {
				dFG := p[i].distSq(fgCenter)
				dBG := p[i].distSq(bgCenter)
				if dFG <= dBG {
					mask |= (1 << (3 - i))
					fgSum.r += p[i].r
					fgSum.g += p[i].g
					fgSum.b += p[i].b
					fgCount++
				} else {
					bgSum.r += p[i].r
					bgSum.g += p[i].g
					bgSum.b += p[i].b
					bgCount++
				}
			}

			fg := fgCenter
			if fgCount > 0 {
				fg = rgb{fgSum.r / float64(fgCount), fgSum.g / float64(fgCount), fgSum.b / float64(fgCount)}
			}

			bg := bgCenter
			if bgCount > 0 {
				bg = rgb{bgSum.r / float64(bgCount), bgSum.g / float64(bgCount), bgSum.b / float64(bgCount)}
			}

			ch := quadrantRunes[mask]
			sb.WriteString(fmt.Sprintf("\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm%c",
				byte(bg.r), byte(bg.g), byte(bg.b),
				byte(fg.r), byte(fg.g), byte(fg.b),
				ch,
			))
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}

	return lines, nil
}

// renderPlaceholderCover generates a clean fixed-size 18x9 placeholder with centered "NO COVER".
func renderPlaceholderCover() []string {
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A")).Bold(true)

	return []string{
		"                  ",
		"                  ",
		"                  ",
		"                  ",
		labelStyle.Render("     NO COVER     "),
		"                  ",
		"                  ",
		"                  ",
		"                  ",
	}
}
