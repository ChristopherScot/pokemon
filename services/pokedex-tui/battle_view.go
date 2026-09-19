package main

// Rendering the battle screen.

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"github.com/christopherscot/pokemon/services/pokedex/battletext"
)

var (
	// Stat changes read as a buff; a condition reads as a warning.
	stageStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	disabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Strikethrough(true)
	condStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Italic(true)

	// Fixed-width columns. Set on the style rather than with fmt verbs,
	// because these strings carry ANSI escapes and only lipgloss
	// measures their DISPLAY width.
	nameCol = lipgloss.NewStyle().Width(24)
	hpCol   = lipgloss.NewStyle().Width(9)

	hpGood     = lipgloss.NewStyle().Foreground(lipgloss.Color("78"))
	hpWarn     = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	hpDanger   = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	faintStyle = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	pickStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle   = lipgloss.NewStyle().Faint(true)
	bannerYou  = lipgloss.NewStyle().Foreground(lipgloss.Color("78")).Bold(true)
	bannerThem = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	superStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	weakStyle  = lipgloss.NewStyle().Faint(true)
)

// impactStyle is the colour a bar flashes when it is hit, by how much
// the type chart mattered.
func impactStyle(effect float64) lipgloss.Style {
	switch {
	case effect >= 2:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	case effect == 0:
		return dimStyle
	case effect < 1:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	}
}

// hpStyleFor colours a bar by how much is left, so a player reads danger
// without doing arithmetic.
func hpStyleFor(hp, max int) lipgloss.Style {
	if max <= 0 {
		return dimStyle
	}
	switch frac := float64(hp) / float64(max); {
	case frac <= 0.2:
		return hpDanger
	case frac <= 0.5:
		return hpWarn
	default:
		return hpGood
	}
}

// hpBar draws the animated value, not the real one: `shown` lags `hp`
// while a drain plays out.
func hpBar(shown, max, width int) string {
	if max <= 0 {
		return strings.Repeat("░", width)
	}
	filled := shown * width / max
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	if shown > 0 && filled == 0 {
		// Alive should never look empty.
		filled = 1
	}
	return hpStyleFor(shown, max).Render(strings.Repeat("█", filled)) +
		dimStyle.Render(strings.Repeat("░", width-filled))
}

// monLine is one Pokemon: name, types, bar, and any damage floating off
// it this frame.
func (bs *battleState) monLine(si, pi int, p api.BattlePokemon, selected bool) string {
	k := slot{si, pi}
	shown := bs.shown[k]

	// Padded by a lipgloss Width, not a %-24s: a styled name carries
	// ANSI escapes and fmt pads to BYTE count, so a fainted or selected
	// Pokemon pushed its whole row left.
	name := "  " + p.Name
	if selected {
		name = "▸ " + p.Name
	}
	nameStyle := nameCol
	switch {
	case p.Fainted:
		nameStyle = nameCol.Faint(true).Strikethrough(true)
	case selected:
		nameStyle = nameCol.Foreground(lipgloss.Color("212")).Bold(true)
	}
	name = nameStyle.Render(name)

	badges := make([]string, 0, len(p.Types))
	for _, t := range p.Types {
		badges = append(badges, typeBadge(t))
	}

	bar := hpBar(shown, p.MaxHp, 14)

	// A hit shakes its row and flashes the bar. The shake is a leading
	// space that comes and goes, which is the only "motion" a terminal
	// row has - and it reads as a jolt rather than a redraw because it
	// lasts a handful of frames.
	lead := ""
	if im, ok := bs.impacts[k]; ok {
		if im.life%4 < 2 {
			lead = " "
		}
		bar = impactStyle(im.effect).Render(strings.Repeat("━", 14))
	}

	line := lead + fmt.Sprintf("%s %s %s %s",
		name,
		bar,
		hpCol.Render(fmt.Sprintf("%d/%d", shown, p.MaxHp)),
		strings.Join(badges, " "))

	// Stat changes and conditions, after the types. Only when there is
	// something to say, so an ordinary battle draws no extra clutter.
	if st := battletext.StageLabel(p); st != "" {
		line += "  " + stageStyle.Render(st)
	}
	for _, cond := range battletext.Conditions(p) {
		line += "  " + condStyle.Render(cond)
	}

	// The damage number rides alongside the bar while it is alive,
	// fading as it goes.
	for _, f := range bs.floats {
		if f.slot != k {
			continue
		}
		line += "  " + renderFloat(f)
	}
	return line
}

// renderFloat styles a damage number by effectiveness and fades it as it
// expires, which is what makes a big hit feel different from a chip.
func renderFloat(f damageFloat) string {
	// An immune hit deals 0, so "-0" in damage red reads as a hit that
	// landed. logView already treats 0 as its own case; this switch did
	// not, so the same event was styled two ways on one screen.
	text := fmt.Sprintf("-%d", f.amount)
	switch {
	case f.effect == 0:
		text = "no effect"
	case f.effect >= 2:
		text += " !!"
	case f.effect < 1:
		text += " ..."
	}

	style := hpDanger
	switch {
	case f.effect == 0:
		style = dimStyle
	case f.effect >= 2:
		style = superStyle
	case f.effect < 1:
		style = weakStyle
	}
	// Past the halfway point the number dims, so it reads as leaving
	// rather than blinking out.
	if f.life < floatLife/2 {
		style = style.Faint(true)
	}
	return style.Render(text)
}

// view renders the whole battle screen.
func (bs *battleState) view(width, height int) string {
	if bs.err != nil {
		return errStyle.Render(fmt.Sprintf("battle error\n\n%v\n\npress esc to go back", bs.err))
	}
	b := bs.battle
	if b == nil {
		return dimStyle.Render("  loading battle…")
	}

	var sb strings.Builder

	mine, theirs, ok := bs.client.SideFor(b)
	if !ok {
		// Still waiting for an opponent: show the id to share and who is
		// already in.
		sb.WriteString(fmt.Sprintf("  battle %s — waiting for an opponent\n\n", b.ID))
		if len(b.Sides) > 0 {
			for pi, p := range b.Sides[0].Team {
				sb.WriteString(bs.monLine(0, pi, p, false) + "\n")
			}
		}
		sb.WriteString(dimStyle.Render("\n  share the id above · esc to leave"))
		return sb.String()
	}

	mineIdx, theirsIdx := 0, 1
	if b.Sides[1].Trainer == bs.client.Name {
		mineIdx, theirsIdx = 1, 0
	}

	sb.WriteString("  " + bs.banner(b) + "\n\n")

	sb.WriteString(dimStyle.Render("  "+theirs.Trainer) + "\n")
	for pi, p := range theirs.Team {
		sel := bs.focus == focusTarget && pi == bs.pickTarget
		sb.WriteString(bs.monLine(theirsIdx, pi, p, sel) + "\n")
	}

	sb.WriteString("\n" + dimStyle.Render("  you") + "\n")
	for pi, p := range mine.Team {
		sel := bs.focus == focusAttacker && pi == bs.pickAttacker
		sb.WriteString(bs.monLine(mineIdx, pi, p, sel) + "\n")
	}

	sb.WriteString("\n" + bs.moveRow(mine) + "\n")
	sb.WriteString("\n" + bs.logView(height) + "\n")
	sb.WriteString(bs.help(b))
	return sb.String()
}

// banner says whose turn it is, or who won.
func (bs *battleState) banner(b *api.Battle) string {
	switch b.Status {
	case api.BattleStatusFinished:
		if w, ok := b.Winner.Get(); ok {
			if w == bs.client.Name {
				return bannerYou.Render("★  you win!  ★")
			}
			return bannerThem.Render(fmt.Sprintf("%s wins", w))
		}
		return dimStyle.Render("a draw")
	case api.BattleStatusActive:
		if bs.client.MyTurn(b) {
			// Pulsing for the first few frames after the turn arrives,
			// so a player who looked away sees it change rather than
			// having to notice a static line.
			if bs.bannerPulse > 0 && bs.bannerPulse%6 < 3 {
				return bannerYou.Reverse(true).Render(" your turn ")
			}
			return bannerYou.Render("your turn")
		}
		return dimStyle.Render(fmt.Sprintf("waiting on %s…", b.Turn.Value))
	default:
		return dimStyle.Render("waiting for an opponent…")
	}
}

// moveRow shows the selected Pokemon's moves, with the chosen one lit
// and its power beside it.
func (bs *battleState) moveRow(mine api.Side) string {
	if bs.pickAttacker >= len(mine.Team) {
		return ""
	}
	attacker := mine.Team[bs.pickAttacker]
	moves := attacker.Moves
	parts := make([]string, 0, len(moves))
	for i, mv := range moves {
		// The power is composed as PLAIN text and styled once with the
		// label around it. It used to be pre-rendered by powerLabel and
		// then wrapped again by the cases below, and a rendered string
		// nested inside another Render loses the inner sequence's escape
		// byte: the terminal showed "mega-punch [2m(80)[m", the codes
		// printed as literal characters on whichever move was selected.
		//
		// One Render per label, never a Render of a Render.
		label := fmt.Sprintf("%s %s", mv.Name, powerText(mv.Power))
		switch {
		case !battleclient.MoveUsable(attacker, i):
			// Selecting a disabled move is a 409, so it must not look
			// available - a failed turn is worse than a greyed label.
			label = disabledStyle.Render(label + " (disabled)")
		case bs.focus == focusMove && i == bs.pickMove:
			label = pickStyle.Render("▸" + label)
		case i == bs.pickMove:
			label = lipgloss.NewStyle().Underline(true).Render(label)
		default:
			label = dimStyle.Render(label)
		}
		parts = append(parts, label)
	}
	return "  " + strings.Join(parts, dimStyle.Render(" · "))
}

// powerText is the power in parentheses, UNSTYLED.
//
// Styling happens once, at the call site, around the whole label. A
// helper that returns pre-styled text cannot be safely composed into a
// larger styled string, which is how escape codes ended up on screen.
func powerText(p int) string {
	if p == 0 {
		return "(status)"
	}
	return fmt.Sprintf("(%d)", p)
}

// logView shows the tail of the battle log, newest last.
func (bs *battleState) logView(height int) string {
	const lines = 5
	log := bs.battle.Log
	from := len(log) - lines
	if from < 0 {
		from = 0
	}
	var sb strings.Builder
	for _, ev := range log[from:] {
		text := ev.Text
		switch e := ev.Effectiveness.Or(1); {
		case e >= 2:
			text = superStyle.Render(text)
		case e > 0 && e < 1:
			text = weakStyle.Render(text)
		case e == 0:
			text = dimStyle.Render(text)
		}
		// Glyph as well as colour. Colour alone cannot say "fainted" or
		// "that was a status move", and it is the first thing lost to a
		// screenshot, a pipe, or a colourblind reader.
		if icon := battletext.EventIcon(ev); icon != "" {
			sb.WriteString(" " + icon + " " + text + "\n")
			continue
		}
		sb.WriteString("    " + text + "\n")
	}
	return sb.String()
}

func (bs *battleState) help(b *api.Battle) string {
	if b.Status == api.BattleStatusFinished {
		return dimStyle.Render("  esc back to the lobby · q quit")
	}
	if !bs.client.MyTurn(b) {
		return dimStyle.Render("  waiting · esc back · q quit")
	}
	switch bs.focus {
	case focusAttacker:
		return dimStyle.Render("  ↑/↓ pick your pokemon · tab moves on · enter attack · esc back")
	case focusMove:
		return dimStyle.Render("  ←/→ pick a move · tab target · shift+tab back · enter attack")
	default:
		return dimStyle.Render("  ↑/↓ pick a target · shift+tab back · enter attack")
	}
}
