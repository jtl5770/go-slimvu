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

func TestRenderCoverToANSI(t *testing.T) {
	// Create synthetic 10x10 PNG
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode error: %v", err)
	}

	lines, err := renderCoverToANSI(buf.Bytes(), 14, 7)
	if err != nil {
		t.Fatalf("renderCoverToANSI error: %v", err)
	}

	if len(lines) != 7 {
		t.Errorf("expected 7 lines, got %d", len(lines))
	}
}

func TestRenderCoverToANSI_InvalidData(t *testing.T) {
	_, err := renderCoverToANSI([]byte("not an image"), 14, 7)
	if err == nil {
		t.Errorf("expected error on invalid image data")
	}
}
