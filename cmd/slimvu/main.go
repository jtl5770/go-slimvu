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

type peakInfo struct {
	position  float64
	holdUntil time.Time
	color     lipgloss.Color
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
	track      slimvu.TrackInfo
	hasTrack   bool

	artworkKey string
	coverLines []string

	tickCount int

	colorGreen  lipgloss.Color
	colorYellow lipgloss.Color
	colorRed    lipgloss.Color
	colorOff    lipgloss.Color
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

func initialModel(provider *slimvu.SqueezeboxAudioProvider, minDB, maxDB float64, fps int, holdTime time.Duration, decayRate float64, showCover bool) model {
	return model{
		provider:   provider,
		minDB:      minDB,
		maxDB:      maxDB,
		fps:        fps,
		holdTime:   holdTime,
		decayRate:  decayRate,
		showCover:  showCover,
		lastUpdate: time.Now(),
		leftDB:     minDB,
		rightDB:    minDB,
		termWidth:  80,
		termHeight: 24,

		colorGreen:  lipgloss.Color("#00E676"),
		colorYellow: lipgloss.Color("#FFD600"),
		colorRed:    lipgloss.Color("#FF1744"),
		colorOff:    lipgloss.Color("#2E3440"),
	}
}

func (m model) Init() tea.Cmd {
	return tickCmd(m.fps)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case " ", "space":
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = m.provider.TogglePause(ctx)
			}()
			return m, nil
		case "n", ">", "right":
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = m.provider.Next(ctx)
			}()
			return m, nil
		case "p", "<", "left":
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = m.provider.Previous(ctx)
			}()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case artworkLoadedMsg:
		if msg.key == m.artworkKey {
			if len(msg.data) > 0 {
				lines, err := renderCoverQuadrant(msg.data, 18, 9)
				if err == nil {
					m.coverLines = lines
				} else {
					m.coverLines = renderPlaceholderCover()
				}
			} else {
				m.coverLines = renderPlaceholderCover()
			}
		}
		return m, nil

	case tickMsg:
		m.tickCount++
		now := time.Time(msg)
		dt := now.Sub(m.lastUpdate).Seconds()
		if dt <= 0 || dt > 1.0 {
			dt = 1.0 / float64(m.fps)
		}
		m.lastUpdate = now

		m.leftDB, m.rightDB, m.playing = m.provider.GetLevels()
		m.syncedMAC, m.syncedName = m.provider.SyncedWith()
		m.track, m.hasTrack = m.provider.GetTrackInfo()

		var artworkCmd tea.Cmd
		if m.showCover && m.hasTrack {
			newKey := fmt.Sprintf("%s:%s:%s", m.track.ArtworkURL, m.track.CoverID, m.track.Title)
			if newKey != m.artworkKey {
				m.artworkKey = newKey
				if m.track.ArtworkURL != "" || m.track.CoverID != "" {
					artworkCmd = fetchArtworkCmd(m.provider, m.track.ArtworkURL, m.track.CoverID, newKey)
				} else {
					m.coverLines = renderPlaceholderCover()
				}
			}
		} else if !m.hasTrack {
			m.artworkKey = ""
			m.coverLines = nil
		}

		barLen := m.getBarLength()
		m.updatePeak(&m.peakLeft, m.leftDB, barLen, dt, now)
		m.updatePeak(&m.peakRight, m.rightDB, barLen, dt, now)

		if artworkCmd != nil {
			return m, tea.Batch(tickCmd(m.fps), artworkCmd)
		}
		return m, tickCmd(m.fps)
	}

	return m, nil
}

func (m *model) getBarLength() int {
	offset := 22
	if m.showCover {
		offset += 22 // Fixed width reservation for 18-column cover box
	}

	w := m.termWidth - offset
	if w < 20 {
		w = 20
	} else if w > 90 {
		w = 90
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
	targetPeak := math.Ceil(level * float64(barLen))

	greenEnd := int(float64(barLen) * 0.60)
	yellowEnd := int(float64(barLen) * 0.85)

	if targetPeak >= peak.position {
		peak.position = targetPeak
		peak.holdUntil = now.Add(m.holdTime)

		idx := int(math.Round(targetPeak)) - 1
		if idx < greenEnd {
			peak.color = m.colorGreen
		} else if idx < yellowEnd {
			peak.color = m.colorYellow
		} else {
			peak.color = m.colorRed
		}
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

func (m model) renderBar(label string, db float64, peak peakInfo, barLen int) string {
	clampedDB := math.Min(m.maxDB, math.Max(m.minDB, db))
	level := (clampedDB - m.minDB) / (m.maxDB - m.minDB)
	activeBlocks := int(math.Ceil(level * float64(barLen)))

	greenEnd := int(float64(barLen) * 0.60)
	yellowEnd := int(float64(barLen) * 0.85)

	peakIdx := -1
	if peak.position >= 1.0 {
		peakIdx = int(math.Round(peak.position)) - 1
		if peakIdx >= barLen {
			peakIdx = barLen - 1
		}
	}

	var sb strings.Builder
	for i := 0; i < barLen; i++ {
		if i == peakIdx && i >= activeBlocks {
			peakStyle := lipgloss.NewStyle().Foreground(peak.color).Bold(true)
			sb.WriteString(peakStyle.Render("█"))
			continue
		}

		if i < activeBlocks {
			var col lipgloss.Color
			if i < greenEnd {
				col = m.colorGreen
			} else if i < yellowEnd {
				col = m.colorYellow
			} else {
				col = m.colorRed
			}
			blockStyle := lipgloss.NewStyle().Foreground(col)
			sb.WriteString(blockStyle.Render("█"))
		} else {
			offStyle := lipgloss.NewStyle().Foreground(m.colorOff)
			sb.WriteString(offStyle.Render("░"))
		}
	}

	dbStr := ""
	if !m.playing || db <= m.minDB {
		dbStr = " -inf  dB"
	} else {
		dbStr = fmt.Sprintf("%6.1f dB", db)
	}

	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ECEFF4"))
	bracketStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	valStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D8DEE9"))

	return fmt.Sprintf("%s  %s%s%s %s",
		labelStyle.Render(label),
		bracketStyle.Render("["),
		sb.String(),
		bracketStyle.Render("]"),
		valStyle.Render(dbStr),
	)
}

func (m model) renderScale(barLen int) string {
	dBs := []float64{-60, -48, -36, -24, -18, -12, -6, -3, 0}
	scaleLine := make([]byte, barLen)
	for i := range scaleLine {
		scaleLine[i] = ' '
	}

	for _, db := range dBs {
		if db < m.minDB || db > m.maxDB {
			continue
		}
		pos := int(math.Round(((db - m.minDB) / (m.maxDB - m.minDB)) * float64(barLen-1)))
		if pos >= 0 && pos < barLen {
			scaleLine[pos] = '|'
		}
	}

	scaleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	indent := strings.Repeat(" ", 4) // "L  [" = 4
	return indent + scaleStyle.Render(string(scaleLine))
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
	rawTitle := strings.Join(titleParts, " · ")

	icon := "♫ "
	iconLen := 2
	availWidth := totalWidth - rightBadgeLen - iconLen - 2
	if availWidth < 10 {
		availWidth = 10
	}

	runes := []rune(rawTitle)
	displayTitle := rawTitle
	if len(runes) > availWidth {
		sep := "   •••   "
		fullRunes := append(runes, []rune(sep)...)
		scrollOffset := (m.tickCount / 12) % len(fullRunes)

		var looped []rune
		for i := 0; i < availWidth; i++ {
			idx := (scrollOffset + i) % len(fullRunes)
			looped = append(looped, fullRunes[idx])
		}
		displayTitle = string(looped)
	}

	displayLen := len([]rune(displayTitle))
	spacing := totalWidth - (iconLen + displayLen + rightBadgeLen)
	if spacing < 1 {
		spacing = 1
	}

	iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0"))
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ECEFF4")).Bold(true)
	badgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1"))

	return fmt.Sprintf("%s%s%s%s",
		iconStyle.Render(icon),
		titleStyle.Render(displayTitle),
		strings.Repeat(" ", spacing),
		badgeStyle.Render(rightBadge),
	)
}

func (m model) renderCoverArt() string {
	if !m.showCover {
		return ""
	}

	lines := m.coverLines
	if len(lines) == 0 {
		lines = renderPlaceholderCover()
	}

	var sb strings.Builder
	for i, line := range lines {
		sb.WriteString(line)
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4C566A")).
		MarginRight(2)

	return borderStyle.Render(sb.String())
}

func (m model) View() string {
	barLen := m.getBarLength()
	totalWidth := barLen + 15

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#88C0D0"))

	statusStyle := lipgloss.NewStyle().Bold(true)
	var statusStr string
	if m.playing {
		statusStr = statusStyle.Foreground(m.colorGreen).Render("● PLAYING")
	} else {
		statusStr = statusStyle.Foreground(lipgloss.Color("#4C566A")).Render("■ IDLE")
	}

	syncedInfo := ""
	if m.syncedName != "" {
		syncedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EBCB8B")).Bold(true)
		syncedInfo = fmt.Sprintf("  •  Synced to: %s", syncedStyle.Render(m.syncedName))
	} else if m.syncedMAC != "" {
		syncedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#EBCB8B")).Bold(true)
		syncedInfo = fmt.Sprintf("  •  Synced to: %s", syncedStyle.Render(m.syncedMAC))
	}

	header := titleStyle.Render(fmt.Sprintf("Squeezebox Stereo VU Meter — %s%s", statusStr, syncedInfo))
	trackLine := m.renderTrackInfo(totalWidth)

	leftBar := m.renderBar("L", m.leftDB, m.peakLeft, barLen)
	rightBar := m.renderBar("R", m.rightDB, m.peakRight, barLen)
	scale := m.renderScale(barLen)

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))
	footer := helpStyle.Render("[Space] Play/Pause • [n/p or ←/→] Prev/Next • [q] Quit")

	// The track info line slot is unconditionally rendered to prevent any vertical layout jumping
	vuContent := fmt.Sprintf("%s\n\n%s\n\n%s\n%s\n%s\n\n%s", header, trackLine, leftBar, rightBar, scale, footer)

	var finalView string
	if m.showCover {
		coverBlock := m.renderCoverArt()
		vuStyled := lipgloss.NewStyle().MarginTop(1).Render(vuContent)
		finalView = lipgloss.JoinHorizontal(lipgloss.Top, coverBlock, vuStyled)
	} else {
		finalView = vuContent
	}

	containerStyle := lipgloss.NewStyle().MarginLeft(1).MarginTop(1)
	return containerStyle.Render(finalView) + "\n"
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
	fps := flag.Int("fps", 60, "UI refresh rate (FPS)")
	holdMS := flag.Int("hold", 250, "Peak hold time in milliseconds")
	decay := flag.Float64("decay", 20.0, "Peak decay rate (blocks/sec)")
	cover := flag.Bool("cover", defaultCover, "Display album cover art thumbnail (auto-detected by default)")
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

	m := initialModel(provider, *minDB, *maxDB, *fps, time.Duration(*holdMS)*time.Millisecond, *decay, *cover)
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running UI: %v\n", err)
		os.Exit(1)
	}
}
