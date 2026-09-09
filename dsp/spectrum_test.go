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

package dsp

import (
	"encoding/binary"
	"math"
	"testing"
)

func generateSinePCM(freq float64, sampleRate uint32, durationSec float64, amplitude float64) []byte {
	frames := int(float64(sampleRate) * durationSec)
	buf := make([]byte, frames*Stereo16BitFrameBytes)

	for i := 0; i < frames; i++ {
		val := math.Sin(2.0 * math.Pi * freq * float64(i) / float64(sampleRate))
		sample := int16(val * amplitude * 32767.0)

		offset := i * Stereo16BitFrameBytes
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(sample))
		binary.LittleEndian.PutUint16(buf[offset+2:offset+4], uint16(sample))
	}
	return buf
}

func TestSpectrumAnalyzer_SineDetection(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)

	// Generate 1000 Hz sine wave for 50 ms (5 chunks of 10 ms)
	pcmChunk := generateSinePCM(1000.0, sr, 0.010, 0.8)

	var bands [SpectrumBandsCount]float32
	for i := 0; i < 5; i++ {
		sa.Process(pcmChunk, sr, 0.010, &bands)
	}

	// 1000 Hz should fall into band 8 (cutoffs: 689 Hz to 1074 Hz)
	// Band 9 should have the maximum level
	maxBand := 0
	maxLevel := float32(-200.0)
	for b := 0; b < SpectrumBandsCount; b++ {
		if bands[b] > maxLevel {
			maxLevel = bands[b]
			maxBand = b
		}
	}

	if maxBand != 8 {
		t.Errorf("Expected peak at band 8 (1000 Hz), got band %d with level %.2f dBFS", maxBand, maxLevel)
	}

	if maxLevel < -10.0 {
		t.Errorf("Expected high level near full scale for 0.8 sine, got %.2f dBFS", maxLevel)
	}
}

func TestSpectrumAnalyzer_ZeroAllocations(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)
	pcmChunk := generateSinePCM(1000.0, sr, 0.010, 0.5)

	var bands [SpectrumBandsCount]float32

	// Warm up
	for i := 0; i < 10; i++ {
		sa.Process(pcmChunk, sr, 0.010, &bands)
	}

	allocs := testing.AllocsPerRun(100, func() {
		sa.Process(pcmChunk, sr, 0.010, &bands)
	})

	if allocs != 0 {
		t.Errorf("Expected 0 allocations in SpectrumAnalyzer.Process, got %.2f", allocs)
	}
}

func TestSpectrumAnalyzer_DecaySilence(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	var bands [SpectrumBandsCount]float32

	// Start with high levels
	for i := range sa.levels {
		sa.levels[i] = -10.0
	}

	// Decay over 0.5 second (decay rate is 100 dB/s)
	sa.DecaySilence(0.5, &bands)

	for b := 0; b < SpectrumBandsCount; b++ {
		expected := float32(-60.0) // -10 - (100 * 0.5)
		if math.Abs(float64(bands[b]-expected)) > 0.1 {
			t.Errorf("Band %d: expected %.2f dB, got %.2f dB", b, expected, bands[b])
		}
	}
}

func TestSpectrumAnalyzer_SampleRateTransitions(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	rates := []uint32{44100, 48000, 96000, 192000, 384000, 44100}

	var bands [SpectrumBandsCount]float32
	for _, sr := range rates {
		pcm := generateSinePCM(440.0, sr, 0.010, 0.5)
		sa.Process(pcm, sr, 0.010, &bands)
		// Band 7 is 411 Hz to 633 Hz, 440 Hz should be active
		if bands[7] <= float32(SilenceFloorDB) {
			t.Errorf("At sample rate %d, expected band 7 active, got %.2f", sr, bands[7])
		}
	}
}

func TestSpectrumAnalyzer_HighFrequencyDistributedEnergy(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)

	// Generate multi-tone broadband high frequency signal (12k, 14k, 16k, 18k)
	frames := int(float64(sr) * 0.050)
	buf := make([]byte, frames*Stereo16BitFrameBytes)
	freqs := []float64{12000.0, 14000.0, 16000.0, 18000.0}
	amp := 0.25

	for i := 0; i < frames; i++ {
		var val float64
		for _, f := range freqs {
			val += math.Sin(2.0 * math.Pi * f * float64(i) / float64(sr))
		}
		sample := int16(val * amp * 32767.0)
		offset := i * Stereo16BitFrameBytes
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(sample))
		binary.LittleEndian.PutUint16(buf[offset+2:offset+4], uint16(sample))
	}

	var bands [SpectrumBandsCount]float32
	sa.Process(buf, sr, 0.050, &bands)

	// Band 14 (9.8 kHz - 15.4 kHz) and Band 15 (15.4 kHz - 20 kHz) should have robust levels > -30 dBFS
	if bands[14] < -30.0 {
		t.Errorf("Expected band 14 level > -30 dBFS, got %.2f dBFS", bands[14])
	}
	if bands[15] < -30.0 {
		t.Errorf("Expected band 15 level > -30 dBFS, got %.2f dBFS", bands[15])
	}
}
