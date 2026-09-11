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
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/jtl5770/go-slimvu"
)

type artworkLoadedMsg struct {
	key  string
	data []byte
}

type model struct {
	provider   *slimvu.SqueezeboxAudioProvider
	minDB      float64
	maxDB      float64
	fps        int
	holdTime   time.Duration
	decayRate  float64
	showCover  bool
	cellAspect float64
	lastUpdate time.Time

	termWidth  int
	termHeight int

	peakLeft  peakInfo
	peakRight peakInfo

	leftDB        float64
	rightDB       float64
	smoothLeftDB  float64
	smoothRightDB float64
	playing       bool
	syncedMAC     string
	syncedName    string
	autoSync      bool
	track         slimvu.TrackInfo
	hasTrack      bool

	cachedTrackKey   string
	cachedRawTitle   string
	cachedTitleRunes []rune

	artworkKey string
	coverLines []string
	coverBlock string

	popup syncPopup

	showSpectrum  bool
	specLeft      [16]float32
	specRight     [16]float32
	spectrumBands [16]float32
	specBuf       *spectrumBuffers
	vuBuf         *vuBarBuffers

	tickCount int
}

type tickMsg time.Time

func tickCmd(fps int) tea.Cmd {
	d := time.Second / time.Duration(fps)
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchArtworkCmd(provider *slimvu.SqueezeboxAudioProvider, artworkURL, coverID, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		data, err := provider.GetArtwork(ctx, artworkURL, coverID)
		if err != nil || len(data) == 0 {
			return artworkLoadedMsg{key: key, data: nil}
		}
		return artworkLoadedMsg{key: key, data: data}
	}
}

func sendPlayerCommand(cmd func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = cmd(ctx)
		return nil
	}
}

func initialModel(provider *slimvu.SqueezeboxAudioProvider, minDB, maxDB float64, fps int, holdTime time.Duration, decayRate float64, showCover bool, cellAspect float64) model {
	if cellAspect <= 0 {
		cellAspect = detectCellAspect()
	}

	autoSync := false
	if provider != nil {
		autoSync = provider.GetAutoSync()
		provider.SetSpectrumEnabled(false)
	}

	return model{
		provider:      provider,
		minDB:         minDB,
		maxDB:         maxDB,
		fps:           fps,
		holdTime:      holdTime,
		decayRate:     decayRate,
		showCover:     showCover,
		cellAspect:    cellAspect,
		lastUpdate:    time.Now(),
		leftDB:        minDB,
		rightDB:       minDB,
		smoothLeftDB:  minDB,
		smoothRightDB: minDB,
		autoSync:      autoSync,
		popup:         newSyncPopup(),
		specBuf:       &spectrumBuffers{},
		vuBuf:         &vuBarBuffers{},
		termWidth:     80,
		termHeight:    24,
	}
}

func (m model) currentFPS() int {
	if !m.playing && !m.popup.IsVisible() {
		if m.fps < 10 {
			return m.fps
		}
		return 10
	}
	return m.fps
}

func (m model) isSynced() bool {
	return m.syncedMAC != "" || m.syncedName != ""
}

func (m model) Init() tea.Cmd {
	return tickCmd(m.currentFPS())
}

func (m model) getCoverCols() int {
	cols := int(math.Round(9.0 * m.cellAspect))
	if cols < 8 {
		cols = 8
	}
	return cols
}

func (m *model) updateCoverBlock() {
	if len(m.coverLines) == 0 {
		m.coverBlock = ""
		return
	}
	m.coverBlock = styleCoverBorder.Render(strings.Join(m.coverLines, "\n"))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.popup.IsVisible() {
			switch msg.String() {
			case "esc", "q", "ctrl+c", "s":
				m.popup.Close()
				return m, nil
			case "up", "k":
				m.popup.MoveUp()
				return m, nil
			case "down", "j":
				m.popup.MoveDown()
				return m, nil
			case "enter":
				if player, ok := m.popup.SelectedPlayer(); ok {
					if isPlayerSelectable(player, m.autoSync, m.popup.AnyPlayerPlaying()) {
						m.provider.SyncTo(player.PlayerID)
						m.popup.Close()
					}
				}
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "s":
			if m.provider != nil {
				m.popup.Open(m.provider.GetAllPlayers())
			}
			return m, nil
		case " ", "space":
			if !m.isSynced() || m.provider == nil {
				return m, nil
			}
			return m, sendPlayerCommand(m.provider.TogglePause)
		case "right":
			if !m.isSynced() || m.provider == nil {
				return m, nil
			}
			return m, sendPlayerCommand(m.provider.Next)
		case "left":
			if !m.isSynced() || m.provider == nil {
				return m, nil
			}
			return m, sendPlayerCommand(m.provider.Previous)
		case "a":
			if m.provider != nil {
				enabled := !m.provider.GetAutoSync()
				m.provider.SetAutoSync(enabled)
				m.autoSync = enabled
			}
			return m, nil
		case "t":
			m.showSpectrum = !m.showSpectrum
			if m.provider != nil {
				m.provider.SetSpectrumEnabled(m.showSpectrum)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case artworkLoadedMsg:
		if msg.key == m.artworkKey {
			cols := m.getCoverCols()
			if len(msg.data) > 0 {
				lines, err := renderCoverQuadrant(msg.data, cols, 9, m.cellAspect)
				if err == nil {
					m.coverLines = lines
				} else {
					m.coverLines = renderPlaceholderCover(cols)
				}
			} else {
				m.coverLines = renderPlaceholderCover(cols)
			}
			m.updateCoverBlock()
		}
		return m, nil

	case tickMsg:
		m.tickCount++
		now := time.Time(msg)
		dt := now.Sub(m.lastUpdate).Seconds()
		if dt <= 0 || dt > 1.0 {
			dt = 1.0 / float64(m.currentFPS())
		}
		m.lastUpdate = now

		if m.provider != nil {
			m.leftDB, m.rightDB, m.playing = m.provider.GetLevels()
			if m.showSpectrum {
				m.provider.GetSpectrum(m.specLeft[:], m.specRight[:])
				for i := 0; i < len(m.spectrumBands); i++ {
					m.spectrumBands[i] = (m.specLeft[i] + m.specRight[i]) / 2.0
				}
			}
			m.syncedMAC, m.syncedName = m.provider.SyncedWith()
			m.autoSync = m.provider.GetAutoSync()
			m.track, m.hasTrack = m.provider.GetTrackInfo()
		}

		// Smooth dB readouts for human readability (~300ms time constant)
		if !m.playing {
			m.smoothLeftDB = m.minDB
			m.smoothRightDB = m.minDB
		} else {
			alpha := 1.0 - math.Exp(-dt/0.3)
			if alpha > 1.0 {
				alpha = 1.0
			}
			if m.smoothLeftDB <= m.minDB {
				m.smoothLeftDB = m.leftDB
			} else {
				m.smoothLeftDB += alpha * (m.leftDB - m.smoothLeftDB)
			}
			if m.smoothRightDB <= m.minDB {
				m.smoothRightDB = m.rightDB
			} else {
				m.smoothRightDB += alpha * (m.rightDB - m.smoothRightDB)
			}
		}

		if m.popup.IsVisible() && m.provider != nil {
			m.popup.SetPlayers(m.provider.GetAllPlayers())
		}

		if m.hasTrack {
			trackKey := fmt.Sprintf("%s|%s|%s", m.track.Artist, m.track.Album, m.track.Title)
			if trackKey != m.cachedTrackKey || m.cachedTitleRunes == nil {
				m.cachedTrackKey = trackKey
				var titleParts []string
				if m.track.Artist != "" {
					titleParts = append(titleParts, m.track.Artist)
				}
				if m.track.Album != "" {
					titleParts = append(titleParts, m.track.Album)
				}
				if m.track.Title != "" {
					titleParts = append(titleParts, m.track.Title)
				}
				m.cachedRawTitle = strings.Join(titleParts, " • ")
				m.cachedTitleRunes = []rune(m.cachedRawTitle)
			}
		} else {
			m.cachedTrackKey = ""
			m.cachedRawTitle = ""
			m.cachedTitleRunes = nil
		}

		var artworkCmd tea.Cmd
		if m.showCover && m.hasTrack && m.isSynced() {
			newKey := fmt.Sprintf("%s:%s:%s", m.track.ArtworkURL, m.track.CoverID, m.track.Title)
			if newKey != m.artworkKey {
				m.artworkKey = newKey
				if m.track.ArtworkURL != "" || m.track.CoverID != "" {
					artworkCmd = fetchArtworkCmd(m.provider, m.track.ArtworkURL, m.track.CoverID, newKey)
				} else {
					m.coverLines = renderPlaceholderCover(m.getCoverCols())
					m.updateCoverBlock()
				}
			}
		} else if !m.hasTrack || !m.isSynced() {
			m.artworkKey = ""
			m.coverLines = nil
			m.coverBlock = ""
		}

		barLen := m.getBarLength()
		m.updatePeak(&m.peakLeft, m.leftDB, barLen, dt, now)
		m.updatePeak(&m.peakRight, m.rightDB, barLen, dt, now)

		nextTick := tickCmd(m.currentFPS())
		if artworkCmd != nil {
			return m, tea.Batch(nextTick, artworkCmd)
		}
		return m, nextTick
	}

	return m, nil
}

func (m *model) getBarLength() int {
	offset := 20
	if m.showCover {
		// Border (2 cols) + MarginRight (2 cols) + cover box width
		offset += m.getCoverCols() + 4
	}

	w := m.termWidth - offset
	if w < 20 {
		w = 20
	} else if w > 92 {
		w = 92
	}
	return w
}

func (m model) renderCoverArt() string {
	if !m.showCover {
		return ""
	}

	if m.isSynced() && m.coverBlock != "" {
		return m.coverBlock
	}

	lines := m.coverLines
	if !m.isSynced() || len(lines) == 0 {
		lines = renderPlaceholderCover(m.getCoverCols())
	}

	return styleCoverBorder.Render(strings.Join(lines, "\n"))
}

func (m model) renderHeader(totalWidth int) string {
	titlePrefix := "Squeezebox Stereo VU Meter"
	if m.showSpectrum {
		titlePrefix = "Squeezebox 16-Band Spectrum"
	}
	leftStr := styleHeaderTitle.Render(titlePrefix)
	leftLen := len([]rune(titlePrefix))

	var statusStr string
	var statusVisibleLen int
	if m.playing {
		statusStr = renderedStatusPlaying
		statusVisibleLen = len([]rune("● PLAYING"))
	} else {
		statusStr = renderedStatusIdle
		statusVisibleLen = len([]rune("■ IDLE"))
	}

	target := m.syncedName
	if target == "" {
		target = m.syncedMAC
	}

	var rightStr string
	var rightLen int
	if target != "" {
		syncedTargetStr := styleSynced.Render(target)
		rightStr = fmt.Sprintf("%s%s%s%s", statusStr, styleSep, styleHelp.Render("Synced to: "), syncedTargetStr)
		rightLen = statusVisibleLen + len([]rune(" • Synced to: ")) + len([]rune(target))
	} else {
		rightStr = statusStr
		rightLen = statusVisibleLen
	}

	spacing := totalWidth - (leftLen + rightLen)
	if spacing < 1 {
		spacing = 1
	}

	return fmt.Sprintf("%s%s%s", leftStr, strings.Repeat(" ", spacing), rightStr)
}

func (m model) View() string {
	barLen := m.getBarLength()
	totalWidth := barLen + 14

	header := m.renderHeader(totalWidth)
	trackLine := m.renderTrackInfo(totalWidth)

	var line1 string
	var line2 string
	var scale string

	if m.showSpectrum {
		line1, line2, scale = m.renderSpectrum(barLen)
	} else {
		line1 = m.renderBar("L", m.leftDB, m.smoothLeftDB, m.peakLeft, barLen)
		line2 = m.renderBar("R", m.rightDB, m.smoothRightDB, m.peakRight, barLen)
		scale = renderScale(barLen, m.minDB, m.maxDB)
	}

	var footer string
	if m.autoSync {
		if m.isSynced() {
			footer = renderedFooterAutoSyncOnSynced
		} else {
			footer = renderedFooterAutoSyncOnUnsynced
		}
	} else {
		if m.isSynced() {
			footer = renderedFooterAutoSyncOffSynced
		} else {
			footer = renderedFooterAutoSyncOffUnsynced
		}
	}

	// The track info line slot is unconditionally rendered to prevent any vertical layout jumping
	vuContent := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n%s\n\n%s", header, trackLine, line1, line2, scale, footer)

	var finalView string
	if m.showCover {
		coverBlock := m.renderCoverArt()
		vuStyled := styleVuMargin.Render(vuContent)
		finalView = lipgloss.JoinHorizontal(lipgloss.Top, coverBlock, vuStyled)
	} else {
		finalView = vuContent
	}

	rendered := styleContainer.Render(finalView) + "\n"

	if m.popup.IsVisible() {
		return m.popup.Overlay(rendered, m.termWidth, m.termHeight, m.autoSync, m.syncedMAC, m.syncedName, m.tickCount)
	}

	return rendered
}
