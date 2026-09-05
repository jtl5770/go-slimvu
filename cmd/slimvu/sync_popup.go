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
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/jtl5770/go-slimvu"
)

const (
	popupVisibleCount = 4
	popupBoxWidth     = 56
)

type syncPopup struct {
	visible        bool
	players        []slimvu.PlayerStatus
	selectedMAC    string
	cursorIndex    int
	viewportOffset int
}

func newSyncPopup() syncPopup {
	return syncPopup{
		visible: false,
	}
}

func (p *syncPopup) Open(players []slimvu.PlayerStatus) {
	p.visible = true
	p.SetPlayers(players)
}

func (p *syncPopup) Close() {
	p.visible = false
}

func (p *syncPopup) IsVisible() bool {
	return p.visible
}

func (p *syncPopup) SetPlayers(players []slimvu.PlayerStatus) {
	if len(players) == 0 {
		p.players = nil
		p.cursorIndex = 0
		p.viewportOffset = 0
		p.selectedMAC = ""
		return
	}

	// Make a sorted copy by player Name (case-insensitive)
	sorted := make([]slimvu.PlayerStatus, len(players))
	copy(sorted, players)
	sort.SliceStable(sorted, func(i, j int) bool {
		nameI := strings.ToLower(sorted[i].Name)
		if nameI == "" {
			nameI = strings.ToLower(sorted[i].PlayerID)
		}
		nameJ := strings.ToLower(sorted[j].Name)
		if nameJ == "" {
			nameJ = strings.ToLower(sorted[j].PlayerID)
		}
		return nameI < nameJ
	})
	p.players = sorted

	found := false
	for i, player := range p.players {
		if player.PlayerID == p.selectedMAC {
			p.cursorIndex = i
			found = true
			break
		}
	}

	if !found {
		if p.cursorIndex >= len(p.players) {
			p.cursorIndex = len(p.players) - 1
		}
		if p.cursorIndex < 0 {
			p.cursorIndex = 0
		}
		p.selectedMAC = p.players[p.cursorIndex].PlayerID
	}

	p.clampViewport()
}

func (p *syncPopup) clampViewport() {
	if p.viewportOffset > p.cursorIndex {
		p.viewportOffset = p.cursorIndex
	} else if p.cursorIndex >= p.viewportOffset+popupVisibleCount {
		p.viewportOffset = p.cursorIndex - popupVisibleCount + 1
	}
	if p.viewportOffset < 0 {
		p.viewportOffset = 0
	}
}

func (p *syncPopup) MoveUp() {
	if p.cursorIndex > 0 {
		p.cursorIndex--
		p.clampViewport()
		if p.cursorIndex < len(p.players) {
			p.selectedMAC = p.players[p.cursorIndex].PlayerID
		}
	}
}

func (p *syncPopup) MoveDown() {
	if p.cursorIndex < len(p.players)-1 {
		p.cursorIndex++
		p.clampViewport()
		if p.cursorIndex < len(p.players) {
			p.selectedMAC = p.players[p.cursorIndex].PlayerID
		}
	}
}

func (p *syncPopup) SelectedPlayer() (slimvu.PlayerStatus, bool) {
	if p.cursorIndex >= 0 && p.cursorIndex < len(p.players) {
		return p.players[p.cursorIndex], true
	}
	return slimvu.PlayerStatus{}, false
}

func isPlayerSelectable(p slimvu.PlayerStatus, autoSync bool) bool {
	if autoSync {
		return p.IsPlaying()
	}
	return p.IsPlaying() || p.IsPaused()
}

func (p syncPopup) RenderBox(termWidth int, autoSync bool, syncedMAC, syncedName string, tickCount int) string {
	boxWidth := popupBoxWidth
	if termWidth > 10 && termWidth-6 < boxWidth {
		boxWidth = termWidth - 6
	}
	if boxWidth < 36 {
		boxWidth = 36
	}
	innerW := boxWidth - 4 // Border (2) + Padding (2)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#88C0D0"))
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4C566A"))

	var content strings.Builder
	content.WriteString(titleStyle.Render("Select Sync Target"))
	content.WriteString("\n")

	if len(p.players) == 0 {
		content.WriteString(helpStyle.Render("No external players discovered on LMS"))
		content.WriteString("\n")
	} else {
		start := p.viewportOffset
		if start < 0 {
			start = 0
		}
		end := start + popupVisibleCount
		if end > len(p.players) {
			end = len(p.players)
		}

		for idx := start; idx < end; idx++ {
			player := p.players[idx]
			isFocused := (idx == p.cursorIndex)
			selectable := isPlayerSelectable(player, autoSync)

			isSynced := (syncedMAC != "" && player.Matches(syncedMAC)) ||
				(syncedName != "" && player.Matches(syncedName))

			var statusIcon string
			var statusText string
			var statusStyle lipgloss.Style

			if player.IsPlaying() {
				statusIcon = "▶"
				if isSynced {
					statusText = "SYNCED "
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0")).Bold(true)
				} else if !selectable {
					statusText = "PLAYING"
					statusStyle = helpStyle
				} else {
					statusText = "PLAYING"
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00E676")).Bold(true)
				}
			} else if player.IsPaused() {
				statusIcon = "‖"
				if isSynced {
					statusText = "SYNCED "
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0")).Bold(true)
				} else if !selectable {
					statusText = "PAUSED "
					statusStyle = helpStyle
				} else {
					statusText = "PAUSED "
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EBCB8B")).Bold(true)
				}
			} else {
				statusIcon = "■"
				if isSynced {
					statusText = "SYNCED "
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0")).Bold(true)
				} else {
					statusText = "STOPPED"
					statusStyle = helpStyle
				}
			}

			caret := "  "
			if isFocused {
				caret = "> "
			}

			pName := player.Name
			if pName == "" {
				pName = player.PlayerID
			}

			nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#D8DEE9"))
			if isFocused {
				nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ECEFF4")).Bold(true)
			} else if !selectable {
				nameStyle = helpStyle
			}

			badgeRendered := fmt.Sprintf("%s %s", statusIcon, statusText)
			badgeLen := lipgloss.Width(badgeRendered)

			maxNameW := innerW - 2 - badgeLen - 2
			if maxNameW < 8 {
				maxNameW = 8
			}

			displayName := pName
			if len([]rune(displayName)) > maxNameW {
				displayName = string([]rune(displayName)[:maxNameW-1]) + "…"
			}

			nameLen := lipgloss.Width(displayName)
			spacesNeeded := innerW - (2 + nameLen + badgeLen)
			if spacesNeeded < 1 {
				spacesNeeded = 1
			}

			var line1 string
			var line2 string

			if isFocused {
				bgCol := lipgloss.Color("#262B35")
				caretStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#88C0D0")).Bold(true).Background(bgCol)
				nameStyle = nameStyle.Background(bgCol)
				statusStyle = statusStyle.Background(bgCol)
				spacesStyle := lipgloss.NewStyle().Background(bgCol)

				line1 = fmt.Sprintf("%s%s%s%s",
					caretStyle.Render(caret),
					nameStyle.Render(displayName),
					spacesStyle.Render(strings.Repeat(" ", spacesNeeded)),
					statusStyle.Render(badgeRendered),
				)

				availTrackW := innerW - 2
				if availTrackW < 10 {
					availTrackW = 10
				}

				leadStyle := lipgloss.NewStyle().Background(bgCol)
				trailStyle := lipgloss.NewStyle().Background(bgCol)

				if !selectable {
					hintStyle := helpStyle.Background(bgCol)
					trailSpaces := innerW - 2 - lipgloss.Width("Not selectable")
					if trailSpaces < 0 {
						trailSpaces = 0
					}
					line2 = fmt.Sprintf("%s%s%s",
						leadStyle.Render("  "),
						hintStyle.Render("Not selectable"),
						trailStyle.Render(strings.Repeat(" ", trailSpaces)),
					)
				} else {
					track := player.GetTrackInfo()
					var parts []string
					if track.Artist != "" {
						parts = append(parts, track.Artist)
					}
					if track.Album != "" {
						parts = append(parts, track.Album)
					}
					if track.Title != "" {
						parts = append(parts, track.Title)
					}
					rawTrack := strings.Join(parts, " · ")
					if rawTrack == "" {
						rawTrack = "No track info"
					}

					displayTrack := rawTrack
					trackRunes := []rune(rawTrack)
					if len(trackRunes) > availTrackW {
						sep := "   •••   "
						fullRunes := append(trackRunes, []rune(sep)...)
						scrollOffset := (tickCount / 12) % len(fullRunes)

						var looped []rune
						for i := 0; i < availTrackW; i++ {
							idx := (scrollOffset + i) % len(fullRunes)
							looped = append(looped, fullRunes[idx])
						}
						displayTrack = string(looped)
					}

					trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#81A1C1")).Background(bgCol)
					trailSpaces := innerW - 2 - lipgloss.Width(displayTrack)
					if trailSpaces < 0 {
						trailSpaces = 0
					}
					line2 = fmt.Sprintf("%s%s%s",
						leadStyle.Render("  "),
						trackStyle.Render(displayTrack),
						trailStyle.Render(strings.Repeat(" ", trailSpaces)),
					)
				}
			} else {
				caretStyle := lipgloss.NewStyle()
				line1 = fmt.Sprintf("%s%s%s%s",
					caretStyle.Render(caret),
					nameStyle.Render(displayName),
					strings.Repeat(" ", spacesNeeded),
					statusStyle.Render(badgeRendered),
				)

				availTrackW := innerW - 2
				if availTrackW < 10 {
					availTrackW = 10
				}

				if !selectable {
					line2 = "  " + helpStyle.Render("Not selectable")
				} else {
					track := player.GetTrackInfo()
					var parts []string
					if track.Artist != "" {
						parts = append(parts, track.Artist)
					}
					if track.Album != "" {
						parts = append(parts, track.Album)
					}
					if track.Title != "" {
						parts = append(parts, track.Title)
					}
					rawTrack := strings.Join(parts, " · ")
					if rawTrack == "" {
						rawTrack = "No track info"
					}

					displayTrack := rawTrack
					trackRunes := []rune(rawTrack)
					if len(trackRunes) > availTrackW {
						displayTrack = string(trackRunes[:availTrackW-1]) + "…"
					}

					trackStyle := helpStyle
					line2 = "  " + trackStyle.Render(displayTrack)
				}
			}

			content.WriteString(line1)
			content.WriteString("\n")
			content.WriteString(line2)
			content.WriteString("\n")
		}
	}

	content.WriteString("\n")
	content.WriteString(helpStyle.Render("[↑/↓] Navigate • [Enter] Select • [Esc] Cancel"))

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#88C0D0")).
		Padding(0, 1)

	return borderStyle.Render(content.String())
}

func splitAnsiTokens(s string) []string {
	var tokens []string
	runes := []rune(s)
	n := len(runes)
	i := 0
	for i < n {
		if runes[i] == '\x1b' && i+1 < n && runes[i+1] == '[' {
			j := i + 2
			for j < n && (runes[j] < 0x40 || runes[j] > 0x7E) {
				j++
			}
			if j < n {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		} else {
			tokens = append(tokens, string(runes[i]))
			i++
		}
	}
	return tokens
}

func takeVisualCols(s string, maxCols int) (string, int) {
	if maxCols <= 0 {
		return "", 0
	}
	tokens := splitAnsiTokens(s)
	var sb strings.Builder
	curCol := 0
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "\x1b[") {
			sb.WriteString(tok)
		} else {
			w := lipgloss.Width(tok)
			if curCol+w > maxCols {
				break
			}
			sb.WriteString(tok)
			curCol += w
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String(), curCol
}

func dropVisualCols(s string, skipCols int) string {
	tokens := splitAnsiTokens(s)
	var sb strings.Builder
	curCol := 0
	started := false
	var lastAnsi string

	for _, tok := range tokens {
		if strings.HasPrefix(tok, "\x1b[") {
			if tok == "\x1b[0m" || tok == "\x1b[m" {
				lastAnsi = ""
			} else {
				lastAnsi = tok
			}
			if started {
				sb.WriteString(tok)
			}
		} else {
			w := lipgloss.Width(tok)
			if curCol >= skipCols {
				if !started {
					started = true
					if lastAnsi != "" {
						sb.WriteString(lastAnsi)
					}
				}
				sb.WriteString(tok)
			}
			curCol += w
		}
	}
	return sb.String()
}

func overlayLine(bg string, fg string, startX int) string {
	leftPart, leftWidth := takeVisualCols(bg, startX)
	if leftWidth < startX {
		leftPart += strings.Repeat(" ", startX-leftWidth)
	}
	fgWidth := lipgloss.Width(fg)
	rightPart := dropVisualCols(bg, startX+fgWidth)
	return leftPart + fg + rightPart
}

// Overlay layers the popup box over the running VU meter background view.
func (p syncPopup) Overlay(bgView string, termWidth, termHeight int, autoSync bool, syncedMAC, syncedName string, tickCount int) string {
	modalBox := p.RenderBox(termWidth, autoSync, syncedMAC, syncedName, tickCount)

	bgLines := strings.Split(bgView, "\n")
	modalLines := strings.Split(modalBox, "\n")

	for len(bgLines) > 0 && bgLines[len(bgLines)-1] == "" {
		bgLines = bgLines[:len(bgLines)-1]
	}
	for len(modalLines) > 0 && modalLines[len(modalLines)-1] == "" {
		modalLines = modalLines[:len(modalLines)-1]
	}

	modalHeight := len(modalLines)
	modalWidth := lipgloss.Width(modalLines[0])

	startY := 2 // Start just below header and track info
	if len(bgLines) < startY+modalHeight+1 {
		for len(bgLines) < startY+modalHeight+1 {
			bgLines = append(bgLines, "")
		}
	}

	startX := (termWidth - modalWidth) / 2
	if startX < 0 {
		startX = 0
	}

	for i, mLine := range modalLines {
		y := startY + i
		if y < len(bgLines) {
			bgLines[y] = overlayLine(bgLines[y], mLine, startX)
		}
	}

	return strings.Join(bgLines, "\n") + "\n"
}
