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

package slimproto

import (
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPStreamer_FetchSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	_, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Read request
		reqBuf := make([]byte, 1024)
		_, _ = conn.Read(reqBuf)

		// Send standard response headers + payload
		resp := "HTTP/1.0 200 OK\r\nServer: Logitech Media Server\r\nContent-Type: audio/flac\r\n\r\nAUDIO_DATA_PAYLOAD"
		_, _ = conn.Write([]byte(resp))
	}()

	streamer := NewHTTPStreamer(1 * time.Second)
	counter := &atomic.Uint64{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	meta, err := streamer.Fetch(ctx, net.ParseIP("127.0.0.1"), uint16(port), "127.0.0.1", "GET /stream.mp3 HTTP/1.0\r\n\r\n", counter)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	defer meta.Conn.Close()

	if !strings.Contains(meta.Headers, "HTTP/1.0 200 OK") {
		t.Errorf("Expected 200 OK in headers, got %q", meta.Headers)
	}

	body, err := io.ReadAll(meta.BodyReader)
	if err != nil {
		t.Fatalf("Failed to read body: %v", err)
	}

	if string(body) != "AUDIO_DATA_PAYLOAD" {
		t.Errorf("Expected AUDIO_DATA_PAYLOAD, got %q", string(body))
	}

	if counter.Load() != uint64(len("AUDIO_DATA_PAYLOAD")) {
		t.Errorf("Expected byte counter %d, got %d", len("AUDIO_DATA_PAYLOAD"), counter.Load())
	}
}

func TestHTTPStreamer_DialFailure(t *testing.T) {
	streamer := NewHTTPStreamer(100 * time.Millisecond)
	ctx := context.Background()

	// Dial an unusable local port
	_, err := streamer.Fetch(ctx, net.ParseIP("127.0.0.1"), 59999, "127.0.0.1", "GET / HTTP/1.0\r\n\r\n", nil)
	if err == nil {
		t.Errorf("Expected dial error, got nil")
	}
}
