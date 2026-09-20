package main

// The four screens, plus the nav bar that moves between them.
//
// Every tappable thing is at least tapTarget high. That is Android's
// 48dp accessibility floor, and on the battle screen a missed tap costs
// a turn - the terminal can afford a one-line row, a thumb cannot.

import (
	"image"
	"image/color"

	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// tapBtn is a button sized for a thumb.
func tapBtn(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string) layout.Dimensions {
	return m3Button(gtx, th, c, label, btnFilled, true)
}

// tonalBtn is a secondary action: prominent, but not the one thing
// the screen wants you to do.
func tonalBtn(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string) layout.Dimensions {
	return m3Button(gtx, th, c, label, btnTonal, true)
}

// rigid wraps a flex child so it sizes to its CONTENT.
//
// A vertical Flex hands each child the full remaining height as a
// minimum, and anything built from another Flex honours that minimum -
// so the first thing in the column claims the whole screen and
// everything after it draws off the bottom. Clearing Min.Y is what
// makes "as tall as it needs to be" mean that.
func rigid(w layout.Widget) layout.FlexChild {
	return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = 0
		return w(gtx)
	})
}

func spacer(h int) layout.FlexChild {
	return layout.Rigid(layout.Spacer{Height: unit.Dp(h)}.Layout)
}

// --- register -------------------------------------------------------

// A phone has no `pokedex-cli register`, so the app has to be able to
// create a trainer itself. This screen is the one thing the terminal
// clients do not need.
func (a *ui) registerScreen(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if a.regBtn.Clicked(gtx) {
		if name := strings.TrimSpace(a.nameInput.Text()); name != "" {
			a.register(name)
		}
	}
	// Enter submits too - a phone keyboard shows a Go key and people
	// press it rather than dismissing the keyboard to find a button.
	for {
		ev, ok := a.nameInput.Update(gtx)
		if !ok {
			break
		}
		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			if name := strings.TrimSpace(a.nameInput.Text()); name != "" {
				a.register(name)
			}
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(material.Body1(th, "Pick a trainer name to battle with.").Layout),
		spacer(16),
		rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
			return material.Editor(th, &a.nameInput, "trainer name").Layout(gtx)
		}),
		spacer(16),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return tapBtn(gtx, th, &a.regBtn, "Register")
		}),
	)
}

// --- browse ---------------------------------------------------------

// The Pokedex, and where a team is picked. The terminal has a separate
// team screen because a list and a cursor cannot show both; here a row
// shows its own pick order, so one screen does both jobs.
func (a *ui) browseScreen(gtx layout.Context, th *material.Theme) layout.Dimensions {
	rows := a.filteredDex()

	for i := range rows {
		if i < len(a.dexClicks) && a.dexClicks[i].Clicked(gtx) {
			a.team = toggleTeam(a.team, rows[i].Name)
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return searchField(gtx, th, &a.filter, "Search the Pokedex")
		}),
		rigid(layout.Spacer{Height: gapM}.Layout),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.teamChip(gtx, th)
		}),
		rigid(layout.Spacer{Height: gapM}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &a.dexList).Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return a.dexRow(gtx, th, rows[i], i)
			})
		}),
	)
}

// searchField is an M3 filled text field: a rounded surfaceVariant
// container, not a bare line of text.
func searchField(gtx layout.Context, th *material.Theme, e *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{
		Left: gapL, Right: gapL, Top: gapM, Bottom: gapM,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, e, hint)
		ed.Color = m3.onSurface
		ed.HintColor = m3.onSurfaceVariant
		return ed.Layout(gtx)
	})
	call := macro.Stop()
	fillRRect(gtx, m3.surfaceVariant, cornerFull, dims.Size)
	call.Add(gtx.Ops)
	return dims
}

// teamChip shows the picked team as an M3 assist chip, coloured once
// the team is complete so "ready" is visible without reading.
func (a *ui) teamChip(gtx layout.Context, th *material.Theme) layout.Dimensions {
	txt := "No team picked — tap three"
	bg, fg := m3.surfaceVariant, m3.onSurfaceVariant
	if len(a.team) > 0 {
		names := make([]string, len(a.team))
		for i, n := range a.team {
			names[i] = title(n)
		}
		txt = strings.Join(names, " · ")
	}
	if teamReady(a.team) {
		bg, fg = m3.primaryContainer, m3.onPrimaryContainer
		txt = "Ready: " + txt
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{
		Left: gapM, Right: gapM, Top: gapS, Bottom: gapS,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(14), txt)
		l.Color = fg
		l.MaxLines = 1
		return l.Layout(gtx)
	})
	call := macro.Stop()
	fillRRect(gtx, bg, cornerSmall, dims.Size)
	call.Add(gtx.Ops)
	return dims
}

func (a *ui) dexRow(gtx layout.Context, th *material.Theme, p api.Pokemon, i int) layout.Dimensions {
	if i >= len(a.dexClicks) {
		return layout.Dimensions{}
	}
	pos := teamPosition(a.team, p.Name)
	// The leading slot carries the pick order when picked and the
	// dex number otherwise, so a glance down the list reads as a team
	// sheet rather than as a column of identical buttons.
	leading := "#" + itoa(p.ID)
	trailing := ""
	if pos > 0 {
		leading = itoa(pos)
		trailing = "on team"
	}
	return layout.Inset{Bottom: gapXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return listItem(gtx, th, &a.dexClicks[i], leading,
			title(p.Name), strings.Join(p.Types, " · "), trailing, pos > 0)
	})
}

// title capitalises a name for display. The API returns lowercase
// ids; a list of lowercase names reads as data, not as a Pokedex.
func title(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- team -----------------------------------------------------------

// Confirms the picked team and starts or joins. Separate from browse
// so the Start button is never off the bottom of a long list.
func (a *ui) teamScreen(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if a.startBtn.Clicked(gtx) && teamReady(a.team) {
		if a.joining != "" {
			a.joinBattle(a.joining, a.team)
		} else {
			a.createBattle(a.team)
		}
	}
	if a.teamBack.Clicked(gtx) {
		a.screen = screenBrowse
	}

	title := "Start a battle"
	if a.joining != "" {
		title = "Join battle"
	}

	children := []layout.FlexChild{
		rigid(material.H6(th, title).Layout),
		spacer(12),
	}
	for i, n := range a.team {
		n := n
		i := i
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return material.Body1(th, itoa(i+1)+".  "+n).Layout(gtx)
		}), spacer(4))
	}
	if !teamReady(a.team) {
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(th, "Pick "+itoa(teamSize-len(a.team))+" more on the Pokedex tab.")
			l.Color = dim
			return l.Layout(gtx)
		}))
	}
	children = append(children,
		spacer(16),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if !teamReady(a.team) {
				gtx = gtx.Disabled()
			}
			lbl := "Start battle"
			if a.joining != "" {
				lbl = "Join battle"
			}
			return tapBtn(gtx, th, &a.startBtn, lbl)
		}),
		spacer(8),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return tonalBtn(gtx, th, &a.teamBack, "Back to Pokedex")
		}),
	)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// --- lobby ----------------------------------------------------------

func (a *ui) lobbyScreen(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if a.refreshBtn.Clicked(gtx) {
		a.loadLobby()
	}
	if a.newBattle.Clicked(gtx) {
		a.joining = ""
		a.screen = screenTeam
	}
	var waiting []waitingItem
	if a.lobby != nil {
		for _, b := range a.lobby.Waiting {
			waiting = append(waiting, waitingItem{ID: b.BattleId, Trainer: b.Trainer, Team: b.Team})
		}
	}
	for i := range waiting {
		if i < len(a.lobbyBtns) && a.lobbyBtns[i].Clicked(gtx) {
			a.joining = waiting[i].ID
			a.screen = screenTeam
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return tapBtn(gtx, th, &a.newBattle, "Open a new battle")
		}),
		spacer(8),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return tonalBtn(gtx, th, &a.refreshBtn, "Refresh")
		}),
		spacer(12),
		rigid(func(gtx layout.Context) layout.Dimensions {
			t := "Waiting for an opponent"
			if len(waiting) == 0 {
				t = "Nobody is waiting. Open one and someone can join."
			}
			l := material.Body2(th, t)
			l.Color = dim
			return l.Layout(gtx)
		}),
		spacer(8),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &a.lobbyList).Layout(gtx, len(waiting), func(gtx layout.Context, i int) layout.Dimensions {
				if i >= len(a.lobbyBtns) {
					return layout.Dimensions{}
				}
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sup := "tap to join"
					if len(waiting[i].Team) > 0 {
						names := make([]string, len(waiting[i].Team))
						for k, n := range waiting[i].Team {
							names[k] = title(n)
						}
						sup = "bringing " + strings.Join(names, ", ")
					}
					initial := strings.ToUpper(waiting[i].Trainer[:1])
					return listItem(gtx, th, &a.lobbyBtns[i], initial,
						title(waiting[i].Trainer), sup, "", false)
				})
			})
		}),
	)
}

type waitingItem struct {
	ID, Trainer string
	// Who they are bringing. The terminal shows this on a second line;
	// a phone has the width for it inline, and it is what a player
	// needs to pick a counter before tapping Join.
	Team []string
}

// --- nav ------------------------------------------------------------

// navBar is the bottom tab bar: the phone convention, and the only
// navigation that stays reachable by a thumb on a tall screen.
//
// Battle is only offered when one exists, so the tab does not lead to
// an empty screen.
func (a *ui) navBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	tabs := []struct {
		label string
		to    screen
		on    bool
	}{
		{"Pokedex", screenBrowse, true},
		{"Team", screenTeam, true},
		{"Lobby", screenLobby, true},
		{"Battle", screenBattle, a.battle != nil},
	}
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Top: gapS, Bottom: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		var children []layout.FlexChild
		for i, t := range tabs {
			i, t := i, t
			children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return a.navItem(gtx, th, &a.navBtns[i], t.label, a.screen == t.to, t.on)
			}))
		}
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
	})
	call := macro.Stop()
	// The bar sits on surfaceContainer, which is what separates it
	// from the content above without needing a divider line.
	paint.FillShape(gtx.Ops, m3.surfaceContainer,
		clip.Rect(image.Rectangle{Max: dims.Size}).Op())
	call.Add(gtx.Ops)
	return dims
}

// navItem is one destination. M3 marks the active one with a filled
// pill behind the label rather than by colouring the others grey -
// grey reads as disabled, which is what the first version looked like.
func (a *ui) navItem(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string, active, enabled bool) layout.Dimensions {
	fg := m3.onSurfaceVariant
	if active {
		fg = m3.onSecondaryContainer
	}
	if !enabled {
		fg = withAlpha(m3.onSurface, 0x61)
		gtx = gtx.Disabled()
	}
	return material.ButtonLayoutStyle{
		Background:   color.NRGBA{},
		CornerRadius: cornerFull,
		Button:       c,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			macro := op.Record(gtx.Ops)
			d := layout.Inset{
				Left: gapL, Right: gapL, Top: gapXS, Bottom: gapXS,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th, unit.Sp(12), label)
				l.Color = fg
				l.MaxLines = 1
				return l.Layout(gtx)
			})
			call := macro.Stop()
			if active {
				fillRRect(gtx, m3.secondaryContainer, cornerFull, d.Size)
			}
			call.Add(gtx.Ops)
			return d
		})
	})
}

// handleNav switches screens, and refreshes the lobby on arrival so it
// is never showing a stale list.
func (a *ui) handleNav(gtx layout.Context) {
	dest := []screen{screenBrowse, screenTeam, screenLobby, screenBattle}
	for i := range a.navBtns {
		if a.navBtns[i].Clicked(gtx) {
			a.screen = dest[i]
			if dest[i] == screenLobby {
				a.loadLobby()
			}
		}
	}
}
