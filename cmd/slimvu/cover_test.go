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
	"image"
	"image/color"
	"image/png"
	"testing"
)

func createTestPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 36, 18))
	for y := 0; y < 18; y++ {
		for x := 0; x < 36; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func TestRenderCoverQuadrant(t *testing.T) {
	pngData := createTestPNG()
	lines, err := renderCoverQuadrant(pngData, 18, 9, 1.6)
	if err != nil {
		t.Fatalf("renderCoverQuadrant error: %v", err)
	}
	if len(lines) != 9 {
		t.Errorf("expected 9 lines, got %d", len(lines))
	}
}

func TestRenderPlaceholderCover(t *testing.T) {
	lines := renderPlaceholderCover()
	if len(lines) != 9 {
		t.Errorf("expected 9 placeholder lines, got %d", len(lines))
	}
}

func TestDetectCellAspect(t *testing.T) {
	aspect := detectCellAspect()
	if aspect < 1.0 || aspect > 3.0 {
		t.Errorf("expected reasonable cell aspect ratio between 1.0 and 3.0, got %f", aspect)
	}
}
