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
	"sync"
	"testing"
	"time"
)

// MockClock provides deterministic time control for unit tests.
type MockClock struct {
	mu          sync.Mutex
	monotonicMs uint32
	currentTime time.Time
}

// NewMockClock creates an initialized MockClock with a base time.
func NewMockClock(startMs uint32, startTime time.Time) *MockClock {
	return &MockClock{
		monotonicMs: startMs,
		currentTime: startTime,
	}
}

func (m *MockClock) NowMonotonicMs() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.monotonicMs
}

func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTime
}

// Advance shifts monotonic and wall clock time forward by d.
func (m *MockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.monotonicMs += uint32(d.Milliseconds())
	m.currentTime = m.currentTime.Add(d)
}

func TestSystemClock(t *testing.T) {
	c := NewSystemClock()
	ms1 := c.NowMonotonicMs()
	time.Sleep(10 * time.Millisecond)
	ms2 := c.NowMonotonicMs()

	if ms2 < ms1 {
		t.Errorf("SystemClock monotonic time went backwards: ms1=%d, ms2=%d", ms1, ms2)
	}

	wall := c.Now()
	if time.Since(wall) > 1*time.Second {
		t.Errorf("SystemClock wall clock deviated excessively: %v", wall)
	}
}

func TestMockClock_Advance(t *testing.T) {
	base := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	mc := NewMockClock(1000, base)

	if mc.NowMonotonicMs() != 1000 {
		t.Fatalf("Expected monotonic ms 1000, got %d", mc.NowMonotonicMs())
	}
	if !mc.Now().Equal(base) {
		t.Fatalf("Expected wall time %v, got %v", base, mc.Now())
	}

	mc.Advance(250 * time.Millisecond)

	if mc.NowMonotonicMs() != 1250 {
		t.Errorf("Expected monotonic ms 1250 after advance, got %d", mc.NowMonotonicMs())
	}
	expectedTime := base.Add(250 * time.Millisecond)
	if !mc.Now().Equal(expectedTime) {
		t.Errorf("Expected wall time %v, got %v", expectedTime, mc.Now())
	}
}
