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
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
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
			gtx.Constraints.Min = gtx.Constraints.Max
			return card(gtx, m3.surfaceContainer, func(gtx layout.Context) layout.Dimensions {
				return a.logPane(gtx, th, b)
			})
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
	bg, fg := m3.secondaryContainer, m3.onSecondaryContainer
	if a.bc.MyTurn(b) && b.Status != "finished" {
		txt += " · " + a.sel.stage()
		bg, fg = m3.primaryContainer, m3.onPrimaryContainer
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{Left: gapM, Right: gapM, Top: gapS, Bottom: gapS}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			l := material.Label(th, unit.Sp(15), txt)
			l.Color = fg
			l.MaxLines = 1
			return l.Layout(gtx)
		})
	call := macro.Stop()
	fillRRect(gtx, bg, cornerSmall, dims.Size)
	call.Add(gtx.Ops)
	return dims
}

func (a *ui) sideRow(gtx layout.Context, th *material.Theme, s api.Side, label string, ours bool) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return card(gtx, m3.surfaceContainer, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th, unit.Sp(12), strings.ToUpper(label)+" · "+title(s.Trainer))
				l.Color = m3.onSurfaceVariant
				return l.Layout(gtx)
			}),
			rigid(layout.Spacer{Height: gapS}.Layout),
		}
		for i := range s.Team {
			i := i
			children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
				return a.monLine(gtx, th, s.Team[i], ours && i == a.sel.attacker && a.sel.haveAttacker)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// monLine is one Pokemon: name, conditions, and an HP bar.
func (a *ui) monLine(gtx layout.Context, th *material.Theme, p api.BattlePokemon, selected bool) layout.Dimensions {
	name := m3.onSurface
	if p.Fainted {
		name = withAlpha(m3.onSurface, 0x61)
	}
	return layout.Inset{Bottom: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						txt := title(p.Name)
						if selected {
							txt = "▸ " + txt
						}
						if extra := summarise(p); extra != p.Name {
							txt += "  " + strings.TrimPrefix(extra, p.Name)
						}
						l := material.Label(th, unit.Sp(15), txt)
						l.Color = name
						l.MaxLines = 1
						return l.Layout(gtx)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Label(th, unit.Sp(13), itoa(p.Hp)+" / "+itoa(p.MaxHp))
						l.Color = m3.onSurfaceVariant
						return l.Layout(gtx)
					}),
				)
			}),
			rigid(layout.Spacer{Height: gapXS}.Layout),
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
	h := gtx.Dp(unit.Dp(8))
	w := gtx.Constraints.Max.X
	// Rounded ends, M3's own progress-indicator shape - a square bar
	// is the tell of a UI that was drawn rather than designed.
	fillRRect(gtx, m3.surfaceVariant, cornerFull, image.Pt(w, h))

	fillW := int(float32(w) * frac)
	c := m3.success
	switch {
	case frac <= 0.2:
		c = m3.errorColor
	case frac <= 0.5:
		c = m3.warning
	}
	if fillW > gtx.Dp(unit.Dp(2)) {
		fillRRect(gtx, c, cornerFull, image.Pt(fillW, h))
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
		return layout.Inset{Bottom: gapXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(th, unit.Sp(14), eventLine(evs[i]))
			l.Color = m3.onSurfaceVariant
			return l.Layout(gtx)
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
		l := material.Label(th, unit.Sp(14), "Waiting for the other trainer…")
		l.Color = m3.onSurfaceVariant
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(l.Layout),
			spacer(8),
			rigid(func(gtx layout.Context) layout.Dimensions {
				return m3Button(gtx, th, &a.leaveBtn, "Leave", btnOutlined, true)
			}),
		)
	}

	switch {
	case !a.sel.haveAttacker:
		return a.pickRow(gtx, th, len(mine.Team), a.monBtns, func(i int) (string, bool) {
			m := mine.Team[i]
			return title(m.Name), !m.Fainted
		})
	case !a.sel.haveMove:
		mon := mine.Team[a.sel.attacker]
		return a.pickRow(gtx, th, len(mon.Moves), a.moveBtns, func(i int) (string, bool) {
			return moveLabel(mon.Moves[i]), canAttack(b, a.bc, mon, i)
		})
	default:
		return a.pickRow(gtx, th, len(theirs.Team), a.tgtBtns, func(i int) (string, bool) {
			m := theirs.Team[i]
			return title(m.Name), !m.Fainted
		})
	}
}

// pickRow lays out one stage's options as full-width buttons.
func (a *ui) pickRow(gtx layout.Context, th *material.Theme, n int, btns []widget.Clickable, at func(int) (string, bool)) layout.Dimensions {
	// The team cards above show the same names, so the control
	// buttons get their own accessibility prefix: TalkBack then says
	// "choose Pikachu" for the actionable one and just "Pikachu" for
	// the status row, and a test can tell them apart.
	var children []layout.FlexChild
	for i := 0; i < n && i < len(btns); i++ {
		i := i
		label, enabled := at(i)
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return m3ButtonDesc(gtx, th, &btns[i], label, "choose "+label, btnTonal, enabled)
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
