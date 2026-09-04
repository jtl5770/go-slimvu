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

// renderCoverQuadrant renders an image using 2x2 Unicode quadrant sub-pixels with 24-bit TrueColor,
// dynamically compensating for the aspect ratio of monospace terminal font cells (Height / Width).
func renderCoverQuadrant(imgData []byte, cols, rows int, cellAspect float64) ([]string, error) {
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

	if cellAspect <= 0 {
		cellAspect = 1.6
	}

	dispW := float64(cols)
	dispH := float64(rows) * cellAspect

	srcAspect := float64(srcW) / float64(srcH)
	dispAspect := dispW / dispH

	var scaleX, scaleY, offX, offY float64
	if srcAspect > dispAspect {
		scaleX = 1.0
		scaleY = dispAspect / srcAspect
		offX = 0.0
		offY = (1.0 - scaleY) / 2.0
	} else {
		scaleY = 1.0
		scaleX = srcAspect / dispAspect
		offY = 0.0
		offX = (1.0 - scaleX) / 2.0
	}

	pixelW := cols * 2
	pixelH := rows * 2

	samplePixel := func(px, py int) rgb {
		u := (float64(px) + 0.5) / float64(pixelW)
		v := (float64(py) + 0.5) / float64(pixelH)

		imgU := (u - offX) / scaleX
		imgV := (v - offY) / scaleY

		if imgU < 0.0 || imgU >= 1.0 || imgV < 0.0 || imgV >= 1.0 {
			return rgb{r: 20, g: 24, b: 30} // subtle dark border background
		}

		sx := bounds.Min.X + int(imgU*float64(srcW))
		sy := bounds.Min.Y + int(imgV*float64(srcH))
		if sx >= bounds.Max.X {
			sx = bounds.Max.X - 1
		}
		if sy >= bounds.Max.Y {
			sy = bounds.Max.Y - 1
		}
		return colorFromRGBA(img.At(sx, sy))
	}

	var lines []string
	for r := 0; r < rows; r++ {
		var sb strings.Builder
		for c := 0; c < cols; c++ {
			// Sample 4 quadrant sub-pixels
			p := [4]rgb{
				samplePixel(c*2, r*2),
				samplePixel(c*2+1, r*2),
				samplePixel(c*2, r*2+1),
				samplePixel(c*2+1, r*2+1),
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
