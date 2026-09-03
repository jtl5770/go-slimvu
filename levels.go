package slimvu

import (
	"github.com/jtl5770/go-slimvu/slimproto"
)

// AudioProvider provides thread-safe, lock-free audio level measurements.
type AudioProvider interface {
	// GetLevels returns the latest left and right dB levels and whether audio is playing.
	GetLevels() (leftDB, rightDB float64, playing bool)
	// Start starts the audio provider background worker.
	Start() error
	// Stop stops the audio provider and gracefully cleans up resources.
	Stop() error
}

// LevelsSnapshot captures an immutable stereo audio measurement.
type LevelsSnapshot = slimproto.LevelsSnapshot

// AtomicLevels stores instantaneous stereo audio levels using atomic pointer snapshots.
type AtomicLevels = slimproto.AtomicLevels

// NewAtomicLevels creates an initialized AtomicLevels instance with silence (-100 dB).
func NewAtomicLevels() *AtomicLevels {
	return slimproto.NewAtomicLevels()
}
