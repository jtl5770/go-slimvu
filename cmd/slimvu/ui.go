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

var (
	scaleTickDBs = [...]float64{-60, -48, -36, -24, -18, -12, -6, -3, 0}
	subBlocks    = [9]string{"", "\u258f", "\u258e", "\u258d", "\u258c", "\u258b", "\u258a", "\u2589", "\u2588"}
	scaleCache   [128]string
)

type colorRGB struct {
	r, g, b uint8
}

func (c colorRGB) String() string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
}

func (c colorRGB) BoldString() string {
	return fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm", c.r, c.g, c.b)
}

type meterColorEntry struct {
	color   colorRGB
	str     string
	boldStr string
	revStr  string
}

var meterColorLUT [256]meterColorEntry

var spectrumFreqLabelsCompact = [16]string{
	"25", "  ", "63", "  ", " ·", "  ", " ·", "  ",
	"1k", "  ", "2k", "  ", "6k", "  ", "16", "  ",
}

var spectrumFreqLabels = [16]string{
	"25", "40", "63", "100", "160", "250", "400", "630",
	"1k", "1.6", "2.5", "4k", "6.3", "10k", "16k", "20k",
}

var vertBlocks = [9]string{
	" ",
	"\u2581", // lower 1/8
	"\u2582", // lower 1/4
	"\u2583", // lower 3/8
	"\u2584", // lower 1/2
	"\u2585", // lower 5/8
	"\u2586", // lower 3/4
	"\u2587", // lower 7/8
	"\u2588", // full block
}

type spectrumCell struct {
	charStr  string
	colorStr string
}

type spectrumLUTEntry struct {
	top    spectrumCell
	bottom spectrumCell
}

var spectrumLUT [17]spectrumLUTEntry
var spectrumScaleCache [128]string

type spectrumBuffers struct {
	top strings.Builder
	bot strings.Builder
}

type vuBarBuffers struct {
	l strings.Builder
	r strings.Builder
}

var (
	styleBarLabel       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ECEFF4"))
	styleBarVal         = lipgloss.NewStyle().Foreground(lipgloss.Color("#D8DEE9"))
	styleScale          = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	styleTrackIcon      = lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0"))
	styleTrackTitle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ECEFF4")).Bold(true)
	styleTrackBadge     = lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1"))
	styleCoverBorder    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4C566A")).MarginRight(2)
	styleCoverLabel     = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A")).Bold(true)
	styleHeaderTitle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#88C0D0"))
	styleStatusPlaying  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00E676"))
	styleStatusIdle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4C566A"))
	styleSynced         = lipgloss.NewStyle().Foreground(lipgloss.Color("#EBCB8B")).Bold(true)
	styleHelp           = lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	styleDisabled       = lipgloss.NewStyle().Foreground(lipgloss.Color("#3B4252"))
	styleAutoSyncActive = lipgloss.NewStyle().Foreground(lipgloss.Color("#00E676")).Bold(true)
	styleVuMargin       = lipgloss.NewStyle().MarginTop(1)
	styleContainer      = lipgloss.NewStyle().MarginLeft(1).MarginTop(1)
	styleSep            = styleHelp.Render(" • ")

	renderedLabelL        = styleBarLabel.Render("L")
	renderedLabelR        = styleBarLabel.Render("R")
	renderedValInf        = styleBarVal.Render(" -inf  dB")
	renderedStatusPlaying = styleStatusPlaying.Render("● PLAYING")
	renderedStatusIdle    = styleStatusIdle.Render("■ IDLE")

	renderedFooterAutoSyncOnSynced    = buildStaticFooter(true, true)
	renderedFooterAutoSyncOnUnsynced  = buildStaticFooter(true, false)
	renderedFooterAutoSyncOffSynced   = buildStaticFooter(false, true)
	renderedFooterAutoSyncOffUnsynced = buildStaticFooter(false, false)
)

func buildStaticFooter(autoSync, isSynced bool) string {
	var iconStr string
	if autoSync {
		iconStr = styleAutoSyncActive.Render("⇆")
	} else {
		iconStr = styleHelp.Render("⇆")
	}
	autoSyncItem := fmt.Sprintf("%s %s", styleHelp.Render("[a] Auto sync"), iconStr)

	playPauseStyle := styleHelp
	prevNextStyle := styleHelp
	if !isSynced {
		playPauseStyle = styleDisabled
		prevNextStyle = styleDisabled
	}

	return fmt.Sprintf("%s%s%s%s%s%s%s%s%s%s%s",
		playPauseStyle.Render("[Space] Play/Pause"),
		styleSep,
		prevNextStyle.Render("[←/→] Prev/Next"),
		styleSep,
		styleHelp.Render("[s] Sync"),
		styleSep,
		autoSyncItem,
		styleSep,
		styleHelp.Render("[t] Effect"),
		styleSep,
		styleHelp.Render("[q] Quit"),
	)
}

func init() {
	for i := 0; i < 256; i++ {
		t := float64(i) / 255.0
		c := computeMeterColor(t)
		meterColorLUT[i] = meterColorEntry{
			color:   c,
			str:     c.String(),
			boldStr: c.BoldString(),
			revStr:  c.String() + "\x1b[7m",
		}
	}

	for s := 0; s <= 16; s++ {
		t := float64(s) / 16.0
		colStr := getMeterColorEntry(t).str

		// Bottom cell
		if s == 0 {
			spectrumLUT[s].bottom = spectrumCell{charStr: " ", colorStr: ""}
		} else if s < 8 {
			spectrumLUT[s].bottom = spectrumCell{
				charStr:  vertBlocks[s],
				colorStr: colStr,
			}
		} else {
			spectrumLUT[s].bottom = spectrumCell{
				charStr:  vertBlocks[8],
				colorStr: colStr,
			}
		}

		// Top cell
		if s <= 8 {
			spectrumLUT[s].top = spectrumCell{charStr: " ", colorStr: ""}
		} else if s < 16 {
			spectrumLUT[s].top = spectrumCell{
				charStr:  vertBlocks[s-8],
				colorStr: colStr,
			}
		} else {
			spectrumLUT[s].top = spectrumCell{
				charStr:  vertBlocks[8],
				colorStr: colStr,
			}
		}
	}
}

type peakInfo struct {
	position  float64
	holdUntil time.Time
	boldStr   string
}

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

	leftDB     float64
	rightDB    float64
	playing    bool
	syncedMAC  string
	syncedName string
	autoSync   bool
	track      slimvu.TrackInfo
	hasTrack   bool

	cachedTrackKey   string
	cachedRawTitle   string
	cachedTitleRunes []rune

	artworkKey string
	coverLines []string
	coverBlock string

	popup syncPopup

	showSpectrum  bool
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

func computeMeterColor(t float64) colorRGB {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}

	var r, g, b float64
	// Green:  #00E676 -> (0, 230, 118)
	// Yellow: #FFD600 -> (255, 214, 0)
	// Red:    #FF1744 -> (255, 23, 68)
	// Transition 1 centered at 0.60 (from 0.45 to 0.75)
	// Transition 2 centered at 0.85 (from 0.75 to 0.95)
	if t < 0.45 {
		r, g, b = 0, 230, 118
	} else if t < 0.75 {
		f := (t - 0.45) / (0.75 - 0.45)
		r = 0 + f*(255-0)
		g = 230 + f*(214-230)
		b = 118 + f*(0-118)
	} else if t < 0.95 {
		f := (t - 0.75) / (0.95 - 0.75)
		r = 255
		g = 214 + f*(23-214)
		b = 0 + f*(68-0)
	} else {
		r, g, b = 255, 23, 68
	}

	return colorRGB{
		r: uint8(math.Round(r)),
		g: uint8(math.Round(g)),
		b: uint8(math.Round(b)),
	}
}

func getMeterColorEntry(t float64) meterColorEntry {
	if t <= 0 {
		return meterColorLUT[0]
	}
	if t >= 1 {
		return meterColorLUT[255]
	}
	idx := int(t * 255.0)
	if idx > 255 {
		idx = 255
	}
	return meterColorLUT[idx]
}

func initialModel(provider *slimvu.SqueezeboxAudioProvider, minDB, maxDB float64, fps int, holdTime time.Duration, decayRate float64, showCover bool, cellAspect float64) model {
	if cellAspect <= 0 {
		cellAspect = detectCellAspect()
	}

	autoSync := false
	if provider != nil {
		autoSync = provider.GetAutoSync()
	}

	return model{
		provider:   provider,
		minDB:      minDB,
		maxDB:      maxDB,
		fps:        fps,
		holdTime:   holdTime,
		decayRate:  decayRate,
		showCover:  showCover,
		cellAspect: cellAspect,
		lastUpdate: time.Now(),
		leftDB:     minDB,
		rightDB:    minDB,
		autoSync:   autoSync,
		popup:      newSyncPopup(),
		specBuf:    &spectrumBuffers{},
		vuBuf:      &vuBarBuffers{},
		termWidth:  80,
		termHeight: 24,
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

		m.leftDB, m.rightDB, m.playing = m.provider.GetLevels()
		if m.provider != nil {
			m.provider.GetSpectrum(m.spectrumBands[:])
		}
		m.syncedMAC, m.syncedName = m.provider.SyncedWith()
		m.autoSync = m.provider.GetAutoSync()
		m.track, m.hasTrack = m.provider.GetTrackInfo()

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
				m.cachedRawTitle = strings.Join(titleParts, " · ")
				m.cachedTitleRunes = []rune(m.cachedRawTitle)
			}
		} else {
			m.cachedTrackKey = ""
			m.cachedRawTitle = ""
			m.cachedTitleRunes = nil
		}

		if m.popup.IsVisible() {
			m.popup.SetPlayers(m.provider.GetAllPlayers())
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

func (m *model) updatePeak(peak *peakInfo, db float64, barLen int, dt float64, now time.Time) {
	if !m.playing {
		peak.position = 0
		return
	}

	clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
	level := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
	targetPeak := level * float64(barLen)

	if targetPeak >= peak.position {
		peak.position = targetPeak
		peak.holdUntil = now.Add(m.holdTime)

		t := targetPeak / float64(barLen)
		peak.boldStr = getMeterColorEntry(t).boldStr
	} else {
		if now.After(peak.holdUntil) && dt > 0 {
			peak.position -= m.decayRate * dt
			if peak.position < targetPeak {
				peak.position = targetPeak
			}
		}
	}

	if peak.position < 0 {
		peak.position = 0
	} else if peak.position > float64(barLen) {
		peak.position = float64(barLen)
	}
}

const (
	ansiReset = "\x1b[0m"
	ansiOff   = "\x1b[38;2;46;52;64m" // #2E3440
	ansiRev   = "\x1b[7m"
)

func (m model) renderBar(label string, db float64, peak peakInfo, barLen int) string {
	clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
	level := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
	barPos := level * float64(barLen)

	peakCell := -1
	if peak.position >= 0.125 {
		peakCell = int(peak.position)
		if peakCell >= barLen {
			peakCell = barLen - 1
		}
	}

	isL := (label == "L")

	var sb *strings.Builder
	if m.vuBuf != nil {
		if isL {
			sb = &m.vuBuf.l
		} else {
			sb = &m.vuBuf.r
		}
		sb.Reset()
	} else {
		sb = &strings.Builder{}
	}
	sb.Grow(barLen*24 + 48)

	if isL {
		sb.WriteString(renderedLabelL)
	} else {
		sb.WriteString(renderedLabelR)
	}
	sb.WriteString("  ")

	for i := 0; i < barLen; i++ {
		t := float64(i) / float64(barLen-1)
		col := getMeterColorEntry(t)
		isPeak := (i == peakCell)

		litCells := int(math.Round(barPos))
		if i < litCells {
			// Lit cell (uses upper 7/8 for L and lower 7/8 for R to maintain gap)
			if isPeak {
				sb.WriteString(peak.boldStr)
				if isL {
					sb.WriteString(ansiRev)
					sb.WriteString("\u2581")
				} else {
					sb.WriteString("\u2587")
				}
				sb.WriteString(ansiReset)
			} else {
				if isL {
					sb.WriteString(col.revStr)
					sb.WriteString("\u2581")
				} else {
					sb.WriteString(col.str)
					sb.WriteString("\u2587")
				}
				sb.WriteString(ansiReset)
			}

		} else {
			// Dark / off region beyond bar
			if isPeak {
				// Floating peak needle positioned at sub-pixel accuracy
				frac := peak.position - float64(i)
				sb.WriteString(peak.boldStr)
				if frac < 0.25 {
					sb.WriteString("▏")
				} else if frac < 0.50 {
					sb.WriteString("▎")
				} else if frac < 0.75 {
					sb.WriteString("▌")
				} else {
					sb.WriteString("▕")
				}
				sb.WriteString(ansiReset)
			} else {
				sb.WriteString(ansiOff)
				sb.WriteString("░")
				sb.WriteString(ansiReset)
			}
		}
	}

	sb.WriteString(" ")
	if !m.playing || db <= m.minDB {
		sb.WriteString(renderedValInf)
	} else {
		sb.WriteString(styleBarVal.Render(fmt.Sprintf("%6.1f dB", db)))
	}

	return sb.String()
}

func renderSpectrumScale(barLen int) string {
	if barLen >= 0 && barLen < len(spectrumScaleCache) && spectrumScaleCache[barLen] != "" {
		return spectrumScaleCache[barLen]
	}

	binWidth := barLen / 16
	if binWidth < 1 {
		binWidth = 1
	}
	contentW := binWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	sepW := binWidth - contentW

	totalBandsW := 16 * binWidth
	trailingPad := barLen - totalBandsW
	if trailingPad < 0 {
		trailingPad = 0
	}

	var sb strings.Builder
	sb.Grow(barLen + 32)

	for i := 0; i < 16; i++ {
		var lbl string
		if contentW >= 3 {
			lbl = spectrumFreqLabels[i]
		} else if contentW == 2 {
			lbl = spectrumFreqLabelsCompact[i]
		} else {
			lbl = "·"
		}

		runes := []rune(lbl)
		if len(runes) <= contentW {
			padL := (contentW - len(runes)) / 2
			padR := contentW - len(runes) - padL
			sb.WriteString(strings.Repeat(" ", padL))
			sb.WriteString(string(runes))
			sb.WriteString(strings.Repeat(" ", padR))
		} else {
			sb.WriteString(string(runes[:contentW]))
		}
		sb.WriteString(strings.Repeat(" ", sepW))
	}
	if trailingPad > 0 {
		sb.WriteString(strings.Repeat(" ", trailingPad))
	}

	indent := strings.Repeat(" ", 3) // "   " matching "L  " = 3
	res := indent + styleScale.Render(sb.String())
	if barLen >= 0 && barLen < len(spectrumScaleCache) {
		spectrumScaleCache[barLen] = res
	}
	return res
}

func (m model) renderSpectrum(barLen int) (string, string, string) {
	if m.specBuf == nil {
		m.specBuf = &spectrumBuffers{}
	}

	binWidth := barLen / 16
	if binWidth < 1 {
		binWidth = 1
	}
	contentW := binWidth - 1
	if contentW < 1 {
		contentW = 1
	}
	sepW := binWidth - contentW

	totalBandsW := 16 * binWidth
	trailingPad := barLen - totalBandsW
	if trailingPad < 0 {
		trailingPad = 0
	}

	m.specBuf.top.Reset()
	m.specBuf.bot.Reset()
	m.specBuf.top.Grow(barLen*24 + 48)
	m.specBuf.bot.Grow(barLen*24 + 48)

	indent := "   "
	m.specBuf.top.WriteString(indent)
	m.specBuf.bot.WriteString(indent)

	for i := 0; i < 16; i++ {
		db := float64(m.spectrumBands[i])
		clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
		norm := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
		step := int(math.Round(norm * 16.0))
		if step < 0 {
			step = 0
		} else if step > 16 {
			step = 16
		}

		entry := spectrumLUT[step]

		// Top line
		if entry.top.colorStr != "" {
			m.specBuf.top.WriteString(entry.top.colorStr)
			for k := 0; k < contentW; k++ {
				m.specBuf.top.WriteString(entry.top.charStr)
			}
			m.specBuf.top.WriteString(ansiReset)
		} else {
			for k := 0; k < contentW; k++ {
				m.specBuf.top.WriteByte(' ')
			}
		}
		for k := 0; k < sepW; k++ {
			m.specBuf.top.WriteByte(' ')
		}

		// Bottom line
		if entry.bottom.colorStr != "" {
			m.specBuf.bot.WriteString(entry.bottom.colorStr)
			for k := 0; k < contentW; k++ {
				m.specBuf.bot.WriteString(entry.bottom.charStr)
			}
			m.specBuf.bot.WriteString(ansiReset)
		} else {
			for k := 0; k < contentW; k++ {
				m.specBuf.bot.WriteByte(' ')
			}
		}
		for k := 0; k < sepW; k++ {
			m.specBuf.bot.WriteByte(' ')
		}
	}

	if trailingPad > 0 {
		padStr := strings.Repeat(" ", trailingPad)
		m.specBuf.top.WriteString(padStr)
		m.specBuf.bot.WriteString(padStr)
	}

	// Suffix spacing to maintain totalWidth = barLen + 13
	m.specBuf.top.WriteString(strings.Repeat(" ", 10))
	m.specBuf.bot.WriteString(strings.Repeat(" ", 10))

	topLine := m.specBuf.top.String()
	botLine := m.specBuf.bot.String()
	scaleLine := renderSpectrumScale(barLen)

	return topLine, botLine, scaleLine
}

func renderScale(barLen int, minDB, maxDB float64) string {
	if barLen >= 0 && barLen < len(scaleCache) && scaleCache[barLen] != "" {
		return scaleCache[barLen]
	}

	scaleLine := make([]byte, barLen)
	for i := range scaleLine {
		scaleLine[i] = ' '
	}

	for _, db := range scaleTickDBs {
		if db < minDB || db > maxDB {
			continue
		}
		pos := int(math.Round(((db - minDB) / (maxDB - minDB)) * float64(barLen-1)))
		if pos >= 0 && pos < barLen {
			scaleLine[pos] = '|'
		}
	}

	indent := strings.Repeat(" ", 3) // "L  " = 3
	res := indent + styleScale.Render(string(scaleLine))
	if barLen >= 0 && barLen < len(scaleCache) {
		scaleCache[barLen] = res
	}
	return res
}

func formatFixedDuration(sec float64, hasHours bool) string {
	if sec < 0 {
		sec = 0
	}
	totalSec := int(math.Round(sec))
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	secs := totalSec % 60
	if hasHours {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}

func (m model) renderTrackInfo(totalWidth int) string {
	if !m.isSynced() || !m.hasTrack {
		// Return blank line matching width to freeze vertical UI layout
		return strings.Repeat(" ", totalWidth)
	}

	hasHours := m.track.Duration >= 3600 || m.track.Elapsed >= 3600
	var timeParts string
	if m.track.Duration > 0 {
		timeParts = fmt.Sprintf("%s / %s",
			formatFixedDuration(m.track.Elapsed, hasHours),
			formatFixedDuration(m.track.Duration, hasHours),
		)
	} else if m.track.Elapsed > 0 {
		timeParts = formatFixedDuration(m.track.Elapsed, hasHours)
	}

	var trackParts string
	curIdx := m.track.PlaylistIndex
	if curIdx <= 0 {
		curIdx = m.track.TrackNum
	}
	total := m.track.PlaylistTotal
	if total <= 0 {
		total = m.track.TotalTracks
	}

	if total > 0 && curIdx > 0 {
		digits := len(fmt.Sprintf("%d", total))
		trackParts = fmt.Sprintf("[%*d/%d]", digits, curIdx, total)
	} else if curIdx > 0 {
		trackParts = fmt.Sprintf("[#%d]", curIdx)
	}

	rightBadge := strings.TrimSpace(trackParts + "  " + timeParts)
	rightBadgeLen := len([]rune(rightBadge))

	icon := "♬ "
	iconLen := 2
	availWidth := totalWidth - rightBadgeLen - iconLen - 2
	if availWidth < 10 {
		availWidth = 10
	}

	runes := m.cachedTitleRunes
	var displayTitle string
	if len(runes) > availWidth {
		sep := "   •••   "
		fullRunes := append(runes, []rune(sep)...)
		scrollOffset := (m.tickCount / 6) % len(fullRunes)

		var looped []rune
		for i := 0; i < availWidth; i++ {
			idx := (scrollOffset + i) % len(fullRunes)
			looped = append(looped, fullRunes[idx])
		}
		displayTitle = string(looped)
	} else {
		displayTitle = m.cachedRawTitle
	}

	displayLen := len([]rune(displayTitle))
	spacing := totalWidth - (iconLen + displayLen + rightBadgeLen)
	if spacing < 1 {
		spacing = 1
	}

	return fmt.Sprintf("%s%s%s%s",
		styleTrackIcon.Render(icon),
		styleTrackTitle.Render(displayTitle),
		strings.Repeat(" ", spacing),
		styleTrackBadge.Render(rightBadge),
	)
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

func (m model) View() string {
	barLen := m.getBarLength()
	totalWidth := barLen + 13

	var statusStr string
	if m.playing {
		statusStr = renderedStatusPlaying
	} else {
		statusStr = renderedStatusIdle
	}

	titlePrefix := "Squeezebox Stereo VU Meter"
	if m.showSpectrum {
		titlePrefix = "Squeezebox 16-Band Spectrum"
	}

	var header string
	if m.syncedName != "" {
		syncedStr := styleSynced.Render(m.syncedName)
		header = styleHeaderTitle.Render(fmt.Sprintf("%s — %s  •  Synced to: %s", titlePrefix, statusStr, syncedStr))
	} else if m.syncedMAC != "" {
		syncedStr := styleSynced.Render(m.syncedMAC)
		header = styleHeaderTitle.Render(fmt.Sprintf("%s — %s  •  Synced to: %s", titlePrefix, statusStr, syncedStr))
	} else {
		header = styleHeaderTitle.Render(fmt.Sprintf("%s — %s", titlePrefix, statusStr))
	}

	trackLine := m.renderTrackInfo(totalWidth)

	var line1 string
	var line2 string
	var scale string

	if m.showSpectrum {
		line1, line2, scale = m.renderSpectrum(barLen)
	} else {
		line1 = m.renderBar("L", m.leftDB, m.peakLeft, barLen)
		line2 = m.renderBar("R", m.rightDB, m.peakRight, barLen)
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
