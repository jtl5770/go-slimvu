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
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// isKittySupported checks if the host terminal natively supports the Kitty Graphics Protocol.
func isKittySupported() bool {
	return os.Getenv("KITTY_WINDOW_ID") != "" ||
		strings.Contains(strings.ToLower(os.Getenv("TERM")), "kitty") ||
		os.Getenv("GHOSTTY_RESOURCES_DIR") != "" ||
		os.Getenv("WEZTERM_PANE") != ""
}

// isTerminalGraphicsSupported detects whether the current terminal supports Kitty graphics or TrueColor thumbnails.
func isTerminalGraphicsSupported() bool {
	if isKittySupported() ||
		os.Getenv("COLORTERM") == "truecolor" ||
		os.Getenv("COLORTERM") == "24bit" {
		return true
	}
	return false
}

// renderKittyCover transmits the full-resolution image using the native Kitty Graphics Protocol.
func renderKittyCover(imgData []byte, cols, rows int) ([]string, error) {
	img, _, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, fmt.Errorf("encode png: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	var kittyEsc strings.Builder
	chunkSize := 4096
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		more := 1
		if end >= len(b64) {
			end = len(b64)
			more = 0
		}
		chunk := b64[i:end]
		if i == 0 {
			// Transmit & display immediately at current cursor position, sized to (cols x rows)
			kittyEsc.WriteString(fmt.Sprintf("\x1b_Ga=T,f=100,t=d,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, chunk))
		} else {
			kittyEsc.WriteString(fmt.Sprintf("\x1b_Gm=%d;%s\x1b\\", more, chunk))
		}
	}

	lines := make([]string, rows)
	lines[0] = kittyEsc.String() + strings.Repeat(" ", cols)
	for r := 1; r < rows; r++ {
		lines[r] = strings.Repeat(" ", cols)
	}
	return lines, nil
}

// renderCoverToANSI renders an image into a slice of ANSI truecolor half-block strings.
// cols: character width (e.g. 16)
// rows: character height (e.g. 8, which equals 16 vertical pixel rows)
func renderCoverToANSI(imgData []byte, cols, rows int) ([]string, error) {
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

	pixelW := cols
	pixelH := rows * 2

	var lines []string
	for r := 0; r < rows; r++ {
		topPixelY := r * 2
		botPixelY := r*2 + 1

		var sb strings.Builder
		for c := 0; c < cols; c++ {
			// Sample top pixel (maps to cell background)
			srcTopX := bounds.Min.X + (c*srcW)/pixelW
			srcTopY := bounds.Min.Y + (topPixelY*srcH)/pixelH
			tr, tg, tb, _ := img.At(srcTopX, srcTopY).RGBA()

			// Sample bottom pixel (maps to cell foreground with '▄')
			srcBotX := bounds.Min.X + (c*srcW)/pixelW
			srcBotY := bounds.Min.Y + (botPixelY*srcH)/pixelH
			br, bg, bb, _ := img.At(srcBotX, srcBotY).RGBA()

			sb.WriteString(fmt.Sprintf("\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm▄",
				byte(tr>>8), byte(tg>>8), byte(tb>>8),
				byte(br>>8), byte(bg>>8), byte(bb>>8),
			))
		}
		sb.WriteString("\x1b[0m")
		lines = append(lines, sb.String())
	}

	return lines, nil
}

// renderCover renders an artwork image using native Kitty Graphics if supported, or TrueColor half-blocks otherwise.
func renderCover(imgData []byte, cols, rows int) ([]string, error) {
	if isKittySupported() {
		return renderKittyCover(imgData, cols, rows)
	}
	return renderCoverToANSI(imgData, cols, rows)
}

// renderPlaceholderCover generates a fixed-size 16x8 stylized vinyl disc placeholder.
func renderPlaceholderCover() []string {
	discStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	noteStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4252"))

	return []string{
		"                ",
		discStyle.Render("    .------.    "),
		discStyle.Render("   /   ") + noteStyle.Render("♫") + discStyle.Render("    \\   "),
		discStyle.Render("  |    ") + noteStyle.Render("◉") + discStyle.Render("     |  "),
		discStyle.Render("   \\        /   "),
		discStyle.Render("    '------'    "),
		labelStyle.Render("    NO COVER    "),
		"                ",
	}
}
