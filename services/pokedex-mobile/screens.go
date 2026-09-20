package main

// The four screens, plus the nav bar that moves between them.
//
// Every tappable thing is at least tapTarget high. That is Android's
// 48dp accessibility floor, and on the battle screen a missed tap costs
// a turn - the terminal can afford a one-line row, a thumb cannot.

import (
	"strings"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// tapBtn is a button sized for a thumb.
func tapBtn(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string) layout.Dimensions {
	gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
	return material.Button(th, c, label).Layout(gtx)
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
			gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
			return material.Editor(th, &a.filter, "search").Layout(gtx)
		}),
		spacer(8),
		rigid(func(gtx layout.Context) layout.Dimensions {
			n := len(a.team)
			t := "Team: none picked"
			if n > 0 {
				t = "Team: " + strings.Join(a.team, ", ")
			}
			l := material.Body2(th, t)
			if teamReady(a.team) {
				l.Color = accent
			} else {
				l.Color = dim
			}
			return l.Layout(gtx)
		}),
		spacer(8),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return material.List(th, &a.dexList).Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
				return a.dexRow(gtx, th, rows[i], i)
			})
		}),
	)
}

func (a *ui) dexRow(gtx layout.Context, th *material.Theme, p api.Pokemon, i int) layout.Dimensions {
	if i >= len(a.dexClicks) {
		return layout.Dimensions{}
	}
	pos := teamPosition(a.team, p.Name)
	label := p.Name + "   " + strings.Join(p.Types, "/")
	if pos > 0 {
		label = itoa(pos) + ".  " + label
	}
	return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
		b := material.Button(th, &a.dexClicks[i], label)
		if pos == 0 {
			b.Background = dim
		}
		return b.Layout(gtx)
	})
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
			return tapBtn(gtx, th, &a.teamBack, "Back to Pokedex")
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
			return tapBtn(gtx, th, &a.refreshBtn, "Refresh")
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
					gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
					lbl := waiting[i].Trainer
					if len(waiting[i].Team) > 0 {
						lbl += "  vs  " + strings.Join(waiting[i].Team, ", ")
					}
					return material.Button(th, &a.lobbyBtns[i], lbl).Layout(gtx)
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
	var children []layout.FlexChild
	for i, t := range tabs {
		i, t := i, t
		children = append(children, layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(2), Right: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if !t.on {
					gtx = gtx.Disabled()
				}
				gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
				b := material.Button(th, &a.navBtns[i], t.label)
				if a.screen != t.to {
					b.Background = dim
				}
				return b.Layout(gtx)
			})
		}))
	}
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
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
