package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

var (
	listStyle = lipgloss.NewStyle()

	moveNameStyle  = lipgloss.NewStyle().Width(18)
	movePowerStyle = lipgloss.NewStyle().PaddingLeft(2)

	labelStyle  = lipgloss.NewStyle().Faint(true)
	nameStyle   = lipgloss.NewStyle().Bold(true)
	detailStyle = lipgloss.NewStyle().Padding(1, 2)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Padding(1, 2)
)

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

	fmt.Fprintf(&b, "%s %.1f m\n", labelStyle.Render("height"), float64(p.Height)/10)
	fmt.Fprintf(&b, "%s %.1f kg\n\n", labelStyle.Render("weight"), float64(p.Weight)/10)

	if len(p.Moves) > 0 {
		b.WriteString(labelStyle.Render("moves") + "\n")
		for _, mv := range p.Moves {
			power := "—"
			if mv.Power > 0 {
				power = fmt.Sprintf("%d", mv.Power)
			}
			b.WriteString("  " +
				moveNameStyle.Render(mv.Name) +
				typeBadgePadded(mv.Type, 12) +
				movePowerStyle.Render(power) + "\n")
		}
	}

	return detailStyle.Render(b.String())
}

// browseView is the original two-pane Pokedex.
func (m model) browseView() string {
	var body string
	switch {
	case m.err != nil:
		body = errStyle.Render(fmt.Sprintf("could not reach the API\n\n%v", m.err))
	case m.loading:
		body = detailStyle.Render(labelStyle.Render("loading…"))
	default:
		body = m.detail()
	}

	left := listStyle.Width(listWidth).Render(m.list.View())
	joined := lipgloss.JoinHorizontal(lipgloss.Top, left, body)

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
		if m.lastBattle != "" {
			hint = fmt.Sprintf("  enter pick · g back to battle · ctrl+r battle as %s · b lobby · / filter · q quit", m.trainer)
		}
	}
	return joined + "\n  team " + strings.Join(slots, " ") + "\n" + labelStyle.Render(hint)
}
