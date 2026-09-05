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
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
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
}

var meterColorLUT [256]meterColorEntry

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
	styleAutoSyncActive = lipgloss.NewStyle().Foreground(lipgloss.Color("#00E676")).Bold(true)
	styleVuMargin       = lipgloss.NewStyle().MarginTop(1)
	styleContainer      = lipgloss.NewStyle().MarginLeft(1).MarginTop(1)
	styleSep            = styleHelp.Render(" • ")

	renderedLabelL        = styleBarLabel.Render("L")
	renderedLabelR        = styleBarLabel.Render("R")
	renderedValInf        = styleBarVal.Render(" -inf  dB")
	renderedStatusPlaying = styleStatusPlaying.Render("● PLAYING")
	renderedStatusIdle    = styleStatusIdle.Render("■ IDLE")

	renderedFooterAutoSyncOn  = buildStaticFooter(true)
	renderedFooterAutoSyncOff = buildStaticFooter(false)
)

func buildStaticFooter(autoSync bool) string {
	var iconStr string
	if autoSync {
		iconStr = styleAutoSyncActive.Render("⇆")
	} else {
		iconStr = styleHelp.Render("⇆")
	}
	autoSyncItem := fmt.Sprintf("%s %s", styleHelp.Render("[a] Auto sync"), iconStr)

	return fmt.Sprintf("%s%s%s%s%s%s%s%s%s",
		styleHelp.Render("[Space] Play/Pause"),
		styleSep,
		styleHelp.Render("[←/→] Prev/Next"),
		styleSep,
		styleHelp.Render("[s] Sync to..."),
		styleSep,
		autoSyncItem,
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
					if isPlayerSelectable(player, m.autoSync) {
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
			m.popup.Open(m.provider.GetAllPlayers())
			return m, nil
		case " ", "space":
			return m, sendPlayerCommand(m.provider.TogglePause)
		case "right":
			return m, sendPlayerCommand(m.provider.Next)
		case "left":
			return m, sendPlayerCommand(m.provider.Previous)
		case "a":
			enabled := !m.provider.GetAutoSync()
			m.provider.SetAutoSync(enabled)
			m.autoSync = enabled
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
		if m.showCover && m.hasTrack {
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
		} else if !m.hasTrack {
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

	var sb strings.Builder
	sb.Grow(barLen*24 + 48)

	if label == "L" {
		sb.WriteString(renderedLabelL)
	} else {
		sb.WriteString(renderedLabelR)
	}
	sb.WriteString("  ")

	for i := 0; i < barLen; i++ {
		t := float64(i) / float64(barLen-1)
		col := getMeterColorEntry(t)
		isPeak := (i == peakCell)

		if float64(i+1) <= barPos {
			// Fully lit cell
			if isPeak {
				sb.WriteString(peak.boldStr)
				sb.WriteString("\u2588")
				sb.WriteString(ansiReset)
			} else {
				sb.WriteString(col.str)
				sb.WriteString("\u2588")
				sb.WriteString(ansiReset)
			}
		} else if float64(i) < barPos {
			// Fractional sub-pixel tip of the active bar
			frac := barPos - float64(i)
			subIdx := int(math.Round(frac * 8.0))
			if subIdx > 8 {
				subIdx = 8
			}

			if isPeak {
				sb.WriteString(peak.boldStr)
				if subIdx <= 0 {
					sb.WriteString("\u258f")
				} else {
					sb.WriteString(subBlocks[subIdx])
				}
				sb.WriteString(ansiReset)
			} else if subIdx <= 0 {
				sb.WriteString(ansiOff)
				sb.WriteString("\u2591")
				sb.WriteString(ansiReset)
			} else if subIdx == 8 {
				sb.WriteString(col.str)
				sb.WriteString("\u2588")
				sb.WriteString(ansiReset)
			} else {
				sb.WriteString(col.str)
				sb.WriteString(subBlocks[subIdx])
				sb.WriteString(ansiReset)
			}
		} else {
			// Dark / off region beyond bar
			if isPeak {
				// Floating peak needle positioned at sub-pixel accuracy
				frac := peak.position - float64(i)
				sb.WriteString(peak.boldStr)
				if frac < 0.25 {
					sb.WriteString("\u258f")
				} else if frac < 0.50 {
					sb.WriteString("\u258e")
				} else if frac < 0.75 {
					sb.WriteString("\u258c")
				} else {
					sb.WriteString("\u2595")
				}
				sb.WriteString(ansiReset)
			} else {
				sb.WriteString(ansiOff)
				sb.WriteString("\u2591")
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
	if !m.hasTrack {
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

	icon := "♫ "
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

	if m.coverBlock != "" {
		return m.coverBlock
	}

	lines := m.coverLines
	if len(lines) == 0 {
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

	var header string
	if m.syncedName != "" {
		syncedStr := styleSynced.Render(m.syncedName)
		header = styleHeaderTitle.Render(fmt.Sprintf("Squeezebox Stereo VU Meter — %s  •  Synced to: %s", statusStr, syncedStr))
	} else if m.syncedMAC != "" {
		syncedStr := styleSynced.Render(m.syncedMAC)
		header = styleHeaderTitle.Render(fmt.Sprintf("Squeezebox Stereo VU Meter — %s  •  Synced to: %s", statusStr, syncedStr))
	} else {
		header = styleHeaderTitle.Render(fmt.Sprintf("Squeezebox Stereo VU Meter — %s", statusStr))
	}

	trackLine := m.renderTrackInfo(totalWidth)

	leftBar := m.renderBar("L", m.leftDB, m.peakLeft, barLen)
	rightBar := m.renderBar("R", m.rightDB, m.peakRight, barLen)
	scale := renderScale(barLen, m.minDB, m.maxDB)

	var footer string
	if m.autoSync {
		footer = renderedFooterAutoSyncOn
	} else {
		footer = renderedFooterAutoSyncOff
	}

	// The track info line slot is unconditionally rendered to prevent any vertical layout jumping
	vuContent := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n%s\n\n%s", header, trackLine, leftBar, rightBar, scale, footer)

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

func main() {
	defaultCover := isTerminalGraphicsSupported()

	server := flag.String("server", "", "LMS server host or IP (leave empty for UDP auto-discovery)")
	slimPort := flag.Int("port", 0, "SlimProto port (default 3483 / auto-discovered)")
	rpcPort := flag.Int("rpc", 0, "JSON-RPC port (default 9000 / auto-discovered)")
	name := flag.String("name", "SlimVU", "Squeezebox virtual player name")
	mac := flag.String("mac", "auto", "Player MAC address (or 'auto')")
	autoSync := flag.Bool("sync", true, "Automatically sync to active player")
	minDB := flag.Float64("min-db", -60.0, "Minimum decibel level for scale")
	maxDB := flag.Float64("max-db", 0.0, "Maximum decibel level for scale")
	fps := flag.Int("fps", 30, "UI refresh rate (FPS)")
	holdMS := flag.Int("hold", 250, "Peak hold time in milliseconds")
	decay := flag.Float64("decay", 20.0, "Peak decay rate (blocks/sec)")
	cover := flag.Bool("cover", defaultCover, "Display album cover art thumbnail (auto-detected by default)")
	cellAspect := flag.Float64("cell-aspect", 0.0, "Terminal character cell aspect ratio Height/Width (0.0 for auto-detect)")
	logPath := flag.String("log", "", "File path to write debug/info logs (disabled by default)")

	flag.Parse()

	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			defer f.Close()
			slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})))
		}
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	cfg := slimvu.Config{
		Server:        *server,
		SlimProtoPort: *slimPort,
		JSONRPCPort:   *rpcPort,
		PlayerName:    *name,
		PlayerMAC:     *mac,
		AutoSync:      *autoSync,
	}

	provider, err := slimvu.NewProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing provider: %v\n", err)
		os.Exit(1)
	}

	if err := provider.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting provider: %v\n", err)
		os.Exit(1)
	}
	defer provider.Stop()

	m := initialModel(provider, *minDB, *maxDB, *fps, time.Duration(*holdMS)*time.Millisecond, *decay, *cover, *cellAspect)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}
}
