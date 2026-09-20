package main

// The battle screen.
//
// The terminal moves a cursor between three columns - attacker, move,
// target - because a keyboard has arrow keys. A thumb does not: you tap
// the thing you mean. So this is three stages of direct tapping, with
// the banner naming the next one, and the target stage skipped entirely
// when only one opponent is standing.
//
// HP bars rather than numbers, because at a glance on a small screen a
// bar reads faster than "14/20" - and the number is still there.

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func (a *ui) battleScreen(gtx layout.Context, th *material.Theme) layout.Dimensions {
	b := a.battle
	if b == nil {
		return material.Body1(th, "No battle.").Layout(gtx)
	}
	if a.leaveBtn.Clicked(gtx) {
		a.battle = nil
		a.sel.reset()
		a.screen = screenLobby
		a.loadLobby()
		return layout.Dimensions{}
	}

	mine, theirs, ok := a.bc.SideFor(b)
	if !ok {
		return material.Body1(th, "This battle is not yours.").Layout(gtx)
	}

	a.handleBattleTaps(gtx, b, mine, theirs)
	a.keepWatching(b)

	// Controls are laid out BEFORE the log in flex order, so they get
	// their space first. The log then flexes into whatever is left.
	//
	// The first version made the log Flexed(1) and the controls Rigid
	// after it, which rendered the buttons at zero height: the log had
	// already taken the screen. A battle with no move buttons is not a
	// game, and it looked fine in a screenshot until you tried to tap.
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.banner(gtx, th, b)
		}),
		spacer(6),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.sideRow(gtx, th, theirs, "Opponent", false)
		}),
		spacer(6),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.logPane(gtx, th, b)
		}),
		spacer(6),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.sideRow(gtx, th, mine, "You", true)
		}),
		spacer(6),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.controls(gtx, th, b, mine, theirs)
		}),
	)
}

// banner says whose turn it is and, on your turn, what to tap next.
//
// On a phone this is the only always-visible instruction, so it carries
// the stage rather than relying on a highlighted column.
func (a *ui) banner(gtx layout.Context, th *material.Theme, b *api.Battle) layout.Dimensions {
	txt := turnBanner(b, a.bc)
	if a.bc.MyTurn(b) && b.Status != "finished" {
		txt += " — " + a.sel.stage()
	}
	l := material.Body1(th, txt)
	l.Color = accent
	return l.Layout(gtx)
}

func (a *ui) sideRow(gtx layout.Context, th *material.Theme, s api.Side, label string, ours bool) layout.Dimensions {
	children := []layout.FlexChild{
		rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Caption(th, label+" — "+s.Trainer)
			l.Color = dim
			return l.Layout(gtx)
		}),
	}
	for i := range s.Team {
		i := i
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return a.monLine(gtx, th, s.Team[i], ours && i == a.sel.attacker && a.sel.haveAttacker)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// monLine is one Pokemon: name, conditions, and an HP bar.
func (a *ui) monLine(gtx layout.Context, th *material.Theme, p api.BattlePokemon, selected bool) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions {
				txt := summarise(p) + "   " + itoa(p.Hp) + "/" + itoa(p.MaxHp)
				if selected {
					txt = "▶ " + txt
				}
				l := material.Body2(th, txt)
				if p.Fainted {
					l.Color = dim
				}
				return l.Layout(gtx)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return hpBar(gtx, hpFraction(p))
			}),
		)
	})
}

// hpBar draws the health bar. Colour carries the same information as
// the length, so it reads at a glance and survives a colourblind eye
// via the length alone.
func hpBar(gtx layout.Context, frac float32) layout.Dimensions {
	h := gtx.Dp(unit.Dp(6))
	w := gtx.Constraints.Max.X
	track := image.Rect(0, 0, w, h)
	paint.FillShape(gtx.Ops, color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xFF},
		clip.Rect(track).Op())

	fillW := int(float32(w) * frac)
	c := color.NRGBA{R: 0x3B, G: 0xA5, B: 0x55, A: 0xFF} // green
	switch {
	case frac <= 0.2:
		c = color.NRGBA{R: 0xD3, G: 0x2F, B: 0x2F, A: 0xFF} // red
	case frac <= 0.5:
		c = color.NRGBA{R: 0xE0, G: 0xA3, B: 0x2E, A: 0xFF} // amber
	}
	if fillW > 0 {
		paint.FillShape(gtx.Ops, c, clip.Rect(image.Rect(0, 0, fillW, h)).Op())
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func (a *ui) logPane(gtx layout.Context, th *material.Theme, b *api.Battle) layout.Dimensions {
	evs := recentLog(b, 30)
	return material.List(th, &a.logList).Layout(gtx, len(evs), func(gtx layout.Context, i int) layout.Dimensions {
		// Min.X as well as Max.X: without a minimum the list packs
		// rows side by side and the whole log reads as one run-on
		// line, which is exactly what it did the first time.
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Inset{Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return material.Body2(th, eventLine(evs[i])).Layout(gtx)
		})
	})
}

// controls is the bottom third: whatever the current stage needs.
//
// One stage at a time, because three rows of buttons on a phone means
// each is too small to hit reliably.
func (a *ui) controls(gtx layout.Context, th *material.Theme, b *api.Battle, mine, theirs api.Side) layout.Dimensions {
	if b.Status == "finished" {
		return tapBtn(gtx, th, &a.leaveBtn, "Back to lobby")
	}
	if !a.bc.MyTurn(b) {
		l := material.Body2(th, "Waiting for the other trainer…")
		l.Color = dim
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(l.Layout),
			spacer(8),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return tapBtn(gtx, th, &a.leaveBtn, "Leave")
			}),
		)
	}

	switch {
	case !a.sel.haveAttacker:
		return a.pickRow(gtx, th, len(mine.Team), a.monBtns, func(i int) (string, bool) {
			m := mine.Team[i]
			return m.Name, !m.Fainted
		})
	case !a.sel.haveMove:
		mon := mine.Team[a.sel.attacker]
		return a.pickRow(gtx, th, len(mon.Moves), a.moveBtns, func(i int) (string, bool) {
			return moveLabel(mon.Moves[i]), canAttack(b, a.bc, mon, i)
		})
	default:
		return a.pickRow(gtx, th, len(theirs.Team), a.tgtBtns, func(i int) (string, bool) {
			m := theirs.Team[i]
			return m.Name, !m.Fainted
		})
	}
}

// pickRow lays out one stage's options as full-width buttons.
func (a *ui) pickRow(gtx layout.Context, th *material.Theme, n int, btns []widget.Clickable, at func(int) (string, bool)) layout.Dimensions {
	var children []layout.FlexChild
	for i := 0; i < n && i < len(btns); i++ {
		i := i
		label, enabled := at(i)
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if !enabled {
					gtx = gtx.Disabled()
				}
				gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
				btn := material.Button(th, &btns[i], label)
				if !enabled {
					btn.Background = dim
				}
				return btn.Layout(gtx)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// handleBattleTaps advances the three-stage selection and sends the
// turn once it is complete.
func (a *ui) handleBattleTaps(gtx layout.Context, b *api.Battle, mine, theirs api.Side) {
	if !a.bc.MyTurn(b) || b.Status == "finished" {
		return
	}
	switch {
	case !a.sel.haveAttacker:
		for i := range mine.Team {
			if i < len(a.monBtns) && a.monBtns[i].Clicked(gtx) && !mine.Team[i].Fainted {
				a.sel.attacker = i
				a.sel.haveAttacker = true
			}
		}
	case !a.sel.haveMove:
		for i := range mine.Team[a.sel.attacker].Moves {
			if i < len(a.moveBtns) && a.moveBtns[i].Clicked(gtx) {
				a.sel.move = i
				a.sel.haveMove = true
				// One opponent left means the target is not a choice;
				// asking for a third tap there is busywork.
				if onlyOneTarget(theirs) {
					a.sel.target = defaultTarget(theirs)
					a.send(b)
				}
			}
		}
	default:
		for i := range theirs.Team {
			if i < len(a.tgtBtns) && a.tgtBtns[i].Clicked(gtx) && !theirs.Team[i].Fainted {
				a.sel.target = i
				a.send(b)
			}
		}
	}
}

func (a *ui) send(b *api.Battle) {
	a.attack(b.ID, a.sel.attacker, a.sel.move, a.sel.target)
	a.sel.reset()
}

// keepWatching long-polls while it is not our turn, so the opponent's
// move appears without the player pulling to refresh.
func (a *ui) keepWatching(b *api.Battle) {
	if a.watching || b.Status == "finished" || a.bc.MyTurn(b) {
		return
	}
	a.watching = true
	a.watch(b.ID, a.lastSeen)
}
