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

func generateSinePCMStereo(freqL, freqR float64, sampleRate uint32, durationSec float64, ampL, ampR float64) []byte {
	frames := int(float64(sampleRate) * durationSec)
	buf := make([]byte, frames*Stereo16BitFrameBytes)

	for i := 0; i < frames; i++ {
		var sampleL, sampleR int16
		if ampL > 0 {
			valL := math.Sin(2.0 * math.Pi * freqL * float64(i) / float64(sampleRate))
			sampleL = int16(valL * ampL * 32767.0)
		}
		if ampR > 0 {
			valR := math.Sin(2.0 * math.Pi * freqR * float64(i) / float64(sampleRate))
			sampleR = int16(valR * ampR * 32767.0)
		}

		offset := i * Stereo16BitFrameBytes
		binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(sampleL))
		binary.LittleEndian.PutUint16(buf[offset+2:offset+4], uint16(sampleR))
	}
	return buf
}

func TestSpectrumAnalyzer_SineDetection(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)

	// Generate 1000 Hz sine wave for 50 ms (5 chunks of 10 ms)
	pcmChunk := generateSinePCM(1000.0, sr, 0.010, 0.8)

	var bandsL, bandsR [SpectrumBandsCount]float32
	for i := 0; i < 5; i++ {
		sa.Process(pcmChunk, sr, 0.010, &bandsL, &bandsR)
	}

	// 1000 Hz should fall into band 8 (cutoffs: 689 Hz to 1074 Hz)
	for _, bands := range [][SpectrumBandsCount]float32{bandsL, bandsR} {
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
}

func TestSpectrumAnalyzer_StereoSeparation(t *testing.T) {
	sr := uint32(44100)

	// 1. Left channel only (Right channel silence)
	saL := NewSpectrumAnalyzer()
	pcmL := generateSinePCMStereo(1000.0, 0, sr, 0.010, 0.8, 0)
	var bandsL, bandsR [SpectrumBandsCount]float32
	for i := 0; i < 5; i++ {
		saL.Process(pcmL, sr, 0.010, &bandsL, &bandsR)
	}

	if bandsL[8] < -10.0 {
		t.Errorf("Expected active Left band 8 > -10 dBFS, got %.2f", bandsL[8])
	}
	for b := 0; b < SpectrumBandsCount; b++ {
		if bandsR[b] > float32(SilenceFloorDB) {
			t.Errorf("Right band %d expected silence floor, got %.2f", b, bandsR[b])
		}
	}

	// 2. Right channel only (Left channel silence)
	saR := NewSpectrumAnalyzer()
	pcmR := generateSinePCMStereo(0, 1000.0, sr, 0.010, 0, 0.8)
	for i := 0; i < 5; i++ {
		saR.Process(pcmR, sr, 0.010, &bandsL, &bandsR)
	}

	if bandsR[8] < -10.0 {
		t.Errorf("Expected active Right band 8 > -10 dBFS, got %.2f", bandsR[8])
	}
	for b := 0; b < SpectrumBandsCount; b++ {
		if bandsL[b] > float32(SilenceFloorDB) {
			t.Errorf("Left band %d expected silence floor, got %.2f", b, bandsL[b])
		}
	}
}

func TestSpectrumAnalyzer_ZeroAllocations(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)
	pcmChunk := generateSinePCM(1000.0, sr, 0.010, 0.5)

	var bandsL, bandsR [SpectrumBandsCount]float32

	// Warm up
	for i := 0; i < 10; i++ {
		sa.Process(pcmChunk, sr, 0.010, &bandsL, &bandsR)
	}

	allocs := testing.AllocsPerRun(100, func() {
		sa.Process(pcmChunk, sr, 0.010, &bandsL, &bandsR)
	})

	if allocs != 0 {
		t.Errorf("Expected 0 allocations in SpectrumAnalyzer.Process, got %.2f", allocs)
	}
}

func TestSpectrumAnalyzer_DecaySilence(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	var bandsL, bandsR [SpectrumBandsCount]float32

	// Start with high levels
	for i := range sa.levelsLeft {
		sa.levelsLeft[i] = -10.0
		sa.levelsRight[i] = -10.0
	}

	// Decay over 0.5 second (decay rate is 100 dB/s)
	sa.DecaySilence(0.5, &bandsL, &bandsR)

	for b := 0; b < SpectrumBandsCount; b++ {
		expected := float32(-60.0) // -10 - (100 * 0.5)
		if math.Abs(float64(bandsL[b]-expected)) > 0.1 {
			t.Errorf("Left Band %d: expected %.2f dB, got %.2f dB", b, expected, bandsL[b])
		}
		if math.Abs(float64(bandsR[b]-expected)) > 0.1 {
			t.Errorf("Right Band %d: expected %.2f dB, got %.2f dB", b, expected, bandsR[b])
		}
	}
}

func TestSpectrumAnalyzer_SampleRateTransitions(t *testing.T) {
	sa := NewSpectrumAnalyzer()
	rates := []uint32{44100, 48000, 96000, 192000, 384000, 44100}

	var bandsL, bandsR [SpectrumBandsCount]float32
	for _, sr := range rates {
		pcm := generateSinePCM(440.0, sr, 0.010, 0.5)
		sa.Process(pcm, sr, 0.010, &bandsL, &bandsR)
		// Band 7 is 411 Hz to 633 Hz, 440 Hz should be active
		if bandsL[7] <= float32(SilenceFloorDB) || bandsR[7] <= float32(SilenceFloorDB) {
			t.Errorf("At sample rate %d, expected band 7 active, got L=%.2f R=%.2f", sr, bandsL[7], bandsR[7])
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

	var bandsL, bandsR [SpectrumBandsCount]float32
	sa.Process(buf, sr, 0.050, &bandsL, &bandsR)

	// Band 14 (9.8 kHz - 15.4 kHz) and Band 15 (15.4 kHz - 20 kHz) should have robust levels > -30 dBFS
	if bandsL[14] < -30.0 || bandsR[14] < -30.0 {
		t.Errorf("Expected band 14 level > -30 dBFS, got L=%.2f R=%.2f", bandsL[14], bandsR[14])
	}
	if bandsL[15] < -30.0 || bandsR[15] < -30.0 {
		t.Errorf("Expected band 15 level > -30 dBFS, got L=%.2f R=%.2f", bandsL[15], bandsR[15])
	}
}

func BenchmarkSpectrumAnalyzer_Process_44k(b *testing.B) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(44100)
	pcm := generateSinePCM(1000.0, sr, 0.010, 0.5) // 10ms chunk
	var bandsL, bandsR [SpectrumBandsCount]float32

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sa.Process(pcm, sr, 0.010, &bandsL, &bandsR)
	}
}

func BenchmarkSpectrumAnalyzer_Process_96k(b *testing.B) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(96000)
	pcm := generateSinePCM(1000.0, sr, 0.010, 0.5) // 10ms chunk
	var bandsL, bandsR [SpectrumBandsCount]float32

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sa.Process(pcm, sr, 0.010, &bandsL, &bandsR)
	}
}

func BenchmarkSpectrumAnalyzer_Process_192k(b *testing.B) {
	sa := NewSpectrumAnalyzer()
	sr := uint32(192000)
	pcm := generateSinePCM(1000.0, sr, 0.010, 0.5) // 10ms chunk
	var bandsL, bandsR [SpectrumBandsCount]float32

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sa.Process(pcm, sr, 0.010, &bandsL, &bandsR)
	}
}
