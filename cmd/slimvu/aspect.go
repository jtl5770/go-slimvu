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
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// detectCellAspect detects the terminal character cell aspect ratio (Height / Width).
// It queries the terminal in the following order:
// 1. TIOCGWINSZ ioctl pixel dimensions
// 2. CSI 16t (Cell pixel size query) or CSI 14t (Window pixel size query)
// 3. Fallback default ~1.6 (standard for modern monospace fonts)
func detectCellAspect() float64 {
	// 1. Try ioctl TIOCGWINSZ
	if aspect, ok := getIoctlAspect(); ok && aspect >= 1.0 && aspect <= 3.0 {
		return aspect
	}

	// 2. Try terminal escape queries (CSI 16t / 14t)
	if aspect, ok := queryTerminalAspect(); ok && aspect >= 1.0 && aspect <= 3.0 {
		return aspect
	}

	// 3. Fallback default
	return 1.6
}

func getIoctlAspect() (float64, bool) {
	for _, f := range []*os.File{os.Stdout, os.Stdin, os.Stderr} {
		if f == nil {
			continue
		}
		ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
		if err == nil && ws.Col > 0 && ws.Row > 0 && ws.Xpixel > 0 && ws.Ypixel > 0 {
			cellW := float64(ws.Xpixel) / float64(ws.Col)
			cellH := float64(ws.Ypixel) / float64(ws.Row)
			if cellW > 0 {
				return cellH / cellW, true
			}
		}
	}
	return 0, false
}

func queryTerminalAspect() (float64, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 0, false
	}
	defer tty.Close()

	fd := int(tty.Fd())
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return 0, false
	}

	raw := *termios
	raw.Lflag &^= syscall.ECHO | syscall.ICANON
	raw.Cc[syscall.VMIN] = 0
	raw.Cc[syscall.VTIME] = 1 // 100ms timeout

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return 0, false
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, termios)

	// Send CSI 16t (cell size) and CSI 14t (window size)
	_, _ = tty.WriteString("\x1b[16t\x1b[14t")

	buf := make([]byte, 128)
	n := 0
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) && n < len(buf) {
		readBytes, err := tty.Read(buf[n:])
		if readBytes > 0 {
			n += readBytes
			if bytes.Contains(buf[:n], []byte("t")) {
				break
			}
		}
		if err != nil {
			break
		}
	}

	resp := string(buf[:n])

	// Parse CSI 6;<h>;<w>t (cell size)
	for _, part := range strings.Split(resp, "\x1b[") {
		if strings.HasPrefix(part, "6;") && strings.HasSuffix(part, "t") {
			body := strings.TrimSuffix(strings.TrimPrefix(part, "6;"), "t")
			dims := strings.Split(body, ";")
			if len(dims) == 2 {
				h, errH := strconv.Atoi(dims[0])
				w, errW := strconv.Atoi(dims[1])
				if errH == nil && errW == nil && w > 0 && h > 0 {
					return float64(h) / float64(w), true
				}
			}
		}
	}

	// Parse CSI 4;<h>;<w>t (window size in pixels)
	for _, part := range strings.Split(resp, "\x1b[") {
		if strings.HasPrefix(part, "4;") && strings.HasSuffix(part, "t") {
			body := strings.TrimSuffix(strings.TrimPrefix(part, "4;"), "t")
			dims := strings.Split(body, ";")
			if len(dims) == 2 {
				winH, errH := strconv.Atoi(dims[0])
				winW, errW := strconv.Atoi(dims[1])
				if errH == nil && errW == nil && winW > 0 && winH > 0 {
					ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
					if err == nil && ws.Col > 0 && ws.Row > 0 {
						cellW := float64(winW) / float64(ws.Col)
						cellH := float64(winH) / float64(ws.Row)
						if cellW > 0 {
							return cellH / cellW, true
						}
					}
				}
			}
		}
	}

	return 0, false
}
