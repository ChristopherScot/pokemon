package main

// The browse screen's rendering: the two-pane Pokedex list and detail.
//
// Here rather than in model.go because model.go is state and Update,
// and the other two screens' views already live beside each other -
// lobbyView and teamView in views.go, the battle in battle_view.go.
// The browse view was the one left in the state file, which is the
// only place a maintainer could not guess from the filename.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	// listStyle frames the left pane. Its frame is subtracted from the
	// pane width before the list is sized, via GetFrameSize.
	listStyle = lipgloss.NewStyle()

	// Fixed-width columns for the moves table. Set on the style rather
	// than with fmt verbs, because these strings carry ANSI escapes and
	// only lipgloss measures their DISPLAY width.
	moveNameStyle  = lipgloss.NewStyle().Width(18)
	movePowerStyle = lipgloss.NewStyle().PaddingLeft(2)

	labelStyle  = lipgloss.NewStyle().Faint(true)
	nameStyle   = lipgloss.NewStyle().Bold(true)
	detailStyle = lipgloss.NewStyle().Padding(1, 2)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1, 2)
)

// typeColors are the conventional Pokemon type colours, so a fire type
// reads as fire at a glance rather than as one more line of text.
var typeColors = map[string]string{
	"normal": "249", "fire": "202", "water": "39", "electric": "220",
	"grass": "78", "ice": "87", "fighting": "124", "poison": "127",
	"ground": "179", "flying": "147", "psychic": "205", "bug": "112",
	"rock": "137", "ghost": "97", "dragon": "63", "dark": "240",
	"steel": "145", "fairy": "218",
}

func typeBadge(t string) string {
	c, ok := typeColors[t]
	if !ok {
		c = "245"
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("232")).
		Background(lipgloss.Color(c)).
		Padding(0, 1).
		Render(t)
}

// typeBadgePadded is typeBadge in a fixed-width column, for tables where
// the next column has to line up. The PADDING is added after rendering,
// as plain spaces, so the badge's background stops where the word does.
func typeBadgePadded(t string, width int) string {
	b := typeBadge(t)
	if pad := width - lipgloss.Width(b); pad > 0 {
		return b + strings.Repeat(" ", pad)
	}
	return b
}

// detail renders the right-hand pane for the highlighted Pokemon.
func (m model) detail() string {
	sel, ok := m.list.SelectedItem().(item)
	if !ok {
		return detailStyle.Render(labelStyle.Render("no selection"))
	}
	p := sel.p

	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s\n\n", nameStyle.Render(p.Name), labelStyle.Render(fmt.Sprintf("#%03d", p.ID)))

	badges := make([]string, len(p.Types))
	for i, t := range p.Types {
		badges[i] = typeBadge(t)
	}
	fmt.Fprintf(&b, "%s\n\n", strings.Join(badges, " "))

	// Decimetres and hectograms are what the upstream Pokedex reports;
	// convert, because nobody thinks in hectograms.
	fmt.Fprintf(&b, "%s %.1f m\n", labelStyle.Render("height"), float64(p.Height)/10)
	fmt.Fprintf(&b, "%s %.1f kg\n\n", labelStyle.Render("weight"), float64(p.Weight)/10)

	if len(p.Moves) > 0 {
		b.WriteString(labelStyle.Render("moves") + "\n")
		for _, mv := range p.Moves {
			power := "—"
			if mv.Power > 0 {
				power = fmt.Sprintf("%d", mv.Power)
			}
			// lipgloss Width, not fmt's %-10s: a styled badge carries ANSI
			// escapes, and fmt pads to BYTE count, so every badge pushed
			// the column a different distance and the rows came out
			// ragged. lipgloss measures what is actually on screen.
			// The badge is padded by ITS OWN style, not wrapped in a
			// second one: a background colour set on an outer Width()
			// paints the padding too, which is what made these columns
			// start at different places rather than just end at them.
			b.WriteString("  " +
				moveNameStyle.Render(mv.Name) +
				typeBadgePadded(mv.Type, 12) +
				movePowerStyle.Render(power) + "\n")
		}
	}

	// Bounded to the pane, so a long move name cannot spill into the
	// list beside it - lipgloss truncates rather than wrapping into the
	// neighbouring column.
	return detailStyle.Render(b.String())
}

// browseView is the original two-pane Pokedex.
func (m model) browseView() string {
	var body string
	switch {
	case m.err != nil:
		// The list is still drawn beside it: an API that is down should
		// not make the whole interface disappear.
		body = errStyle.Render(fmt.Sprintf("could not reach the API\n\n%v", m.err))
	case m.loading:
		body = detailStyle.Render(labelStyle.Render("loading…"))
	default:
		body = m.detail()
	}

	// Width, so the two panes sit at fixed columns: the list renders to
	// whatever its longest row needs, which is narrower than listWidth,
	// and without this the detail pane would slide left and right as the
	// selection changed.
	left := listStyle.Width(listWidth).Render(m.list.View())
	joined := lipgloss.JoinHorizontal(lipgloss.Top, left, body)

	// The team, shown as three slots above the help line. A die for a
	// slot nobody picked, because the server fills those at random and
	// that should look like a choice rather than a gap.
	slots := make([]string, 3)
	for i := range slots {
		if i < len(m.team) {
			slots[i] = selStyle.Render(fmt.Sprintf("[%s]", m.team[i]))
		} else {
			slots[i] = labelStyle.Render("[🎲 random]")
		}
	}

	hint := "  enter pick · backspace undo · / filter · q quit"
	if m.bc != nil {
		hint = fmt.Sprintf("  enter pick · ctrl+r battle as %s · b lobby · / filter · q quit", m.trainer)
		// Only offered when there is somewhere to go back TO, so the
		// hint never advertises a key that answers "no battle".
		if m.lastBattle != "" {
			hint = fmt.Sprintf("  enter pick · g back to battle · ctrl+r battle as %s · b lobby · / filter · q quit", m.trainer)
		}
	}
	return joined + "\n  team " + strings.Join(slots, " ") + "\n" + labelStyle.Render(hint)
}
