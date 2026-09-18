package main

// The lobby and team-picking screens.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	selStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

// lobbyView lists open invitations.
func (m model) lobbyView() string {
	var sb strings.Builder
	sb.WriteString("\n  " + titleStyle.Render("Battle lobby") +
		dimStyle.Render(fmt.Sprintf("  ·  you are %s", m.trainer)) + "\n\n")

	if m.lobby == nil {
		sb.WriteString(dimStyle.Render("  loading…\n"))
	} else if len(m.lobby.Waiting) == 0 {
		sb.WriteString(dimStyle.Render("  nobody is waiting.\n  press n to open a battle and let someone join you.\n"))
	} else {
		for i, w := range m.lobby.Waiting {
			marker := "  "
			line := fmt.Sprintf("%-8s %-12s %s", w.BattleId, w.Trainer, strings.Join(w.Team, ", "))
			if i == m.lobbyIdx {
				marker = selStyle.Render("▸ ")
				line = selStyle.Render(line)
			}
			sb.WriteString("  " + marker + line + "\n")
		}
	}

	sb.WriteString("\n" + dimStyle.Render("  ↑/↓ choose · enter join · n new battle · r refresh · esc back · q quit"))
	return sb.String()
}

// teamView reuses the browse list to pick three Pokemon, rather than
// inventing a second picker for the same data.
func (m model) teamView() string {
	head := "  " + titleStyle.Render("Choose three Pokemon")
	if m.joining != "" {
		head += dimStyle.Render(fmt.Sprintf("  ·  joining %s", m.joining))
	} else {
		head += dimStyle.Render("  ·  opening a new battle")
	}

	// An empty slot shows a die, not an ellipsis, and says so in the
	// hint below. The server fills whatever is left out, so starting
	// with one pick - or none - is a real option, and an ellipsis reads
	// as "incomplete, keep going" rather than "this will be random".
	// The web picker uses the same die for the same reason.
	picked := "  "
	for i := 0; i < 3; i++ {
		if i < len(m.team) {
			picked += selStyle.Render(fmt.Sprintf("%d. %-14s", i+1, m.team[i]))
			continue
		}
		// Padded to 13, not 14: the die is two columns wide and
		// %-14s counts it as one, so the columns step right without it.
		picked += dimStyle.Render(fmt.Sprintf("%d. \U0001F3B2 %-13s", i+1, "random"))
	}

	left := listStyle.Width(listWidth).Render(m.list.View())
	right := detailStyle.Render(m.detail())

	return head + "\n" + picked + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, left, right) + "\n" +
		dimStyle.Render("  enter pick · backspace undo · / filter · tab start with what you have · esc back")
}
