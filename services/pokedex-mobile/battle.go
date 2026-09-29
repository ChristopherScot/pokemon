package main

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
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

	// Controls must be laid out before the log in flex order: a Flexed log with Rigid controls after renders the buttons at zero height.
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

func (a *ui) monLine(gtx layout.Context, th *material.Theme, p api.BattlePokemon, selected bool) layout.Dimensions {
	name := m3.onSurface
	if p.Fainted {
		name = withAlpha(m3.onSurface, 0x61)
	}
	return layout.Inset{Bottom: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Right: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return a.spriteOrMonogram(gtx, th, p.Sprite,
						strings.ToUpper(p.Name[:1]), unit.Dp(36), selected)
				})
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
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
								l := material.Label(th, unit.Sp(13),
									strconv.Itoa(p.Hp)+" / "+strconv.Itoa(p.MaxHp))
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
			}),
		)
	})
}

// hpBar draws the health bar; colour duplicates the length so it survives a colourblind eye.
func hpBar(gtx layout.Context, frac float32) layout.Dimensions {
	h := gtx.Dp(unit.Dp(8))
	w := gtx.Constraints.Max.X
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
		// Min.X needed as well as Max.X: without it the list packs rows side by side.
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Inset{Bottom: gapXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Label(th, unit.Sp(14), eventLine(evs[i]))
			l.Color = m3.onSurfaceVariant
			return l.Layout(gtx)
		})
	})
}

// controls is the bottom third: one stage's options at a time, so buttons stay big enough to hit.
func (a *ui) controls(gtx layout.Context, th *material.Theme, b *api.Battle, mine, theirs api.Side) layout.Dimensions {
	if b.Status == "finished" {
		return tapBtn(gtx, th, &a.leaveBtn, "Back to lobby")
	}
	// While a turn is in flight the controls stay visible but dead, so a laggy tap cannot fire twice.
	if a.busy {
		l := material.Label(th, unit.Sp(14), "Sending…")
		l.Color = m3.onSurfaceVariant
		return l.Layout(gtx)
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

	// Undo above the options, so a mis-tap costs one tap rather than a whole unwanted turn.
	withBack := func(w layout.Widget) layout.Dimensions {
		if !a.sel.canGoBack() {
			return w(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: gapS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return m3Button(gtx, th, &a.backBtn, "Back", btnOutlined, true)
				})
			}),
			rigid(w),
		)
	}

	switch {
	case !a.sel.haveAttacker:
		return a.pickRow(gtx, th, len(mine.Team), a.monBtns, func(i int) (string, bool) {
			m := mine.Team[i]
			return title(m.Name), battleclient.CanAct(m)
		})
	case !a.sel.haveMove:
		mon := mine.Team[a.sel.attacker]
		return withBack(func(gtx layout.Context) layout.Dimensions {
			return a.pickRow(gtx, th, len(mon.Moves), a.moveBtns, func(i int) (string, bool) {
				return moveLabel(mon.Moves[i]), canAttack(b, a.bc, mon, i)
			})
		})
	default:
		return withBack(func(gtx layout.Context) layout.Dimensions {
			return a.pickRow(gtx, th, len(theirs.Team), a.tgtBtns, func(i int) (string, bool) {
				m := theirs.Team[i]
				return title(m.Name), battleclient.CanBeTargeted(m)
			})
		})
	}
}

// pickRow lays out one stage's options; the "choose X" a11y prefix distinguishes buttons from the same names in the team cards above.
func (a *ui) pickRow(gtx layout.Context, th *material.Theme, n int, btns []widget.Clickable, at func(int) (string, bool)) layout.Dimensions {
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

// handleBattleTaps advances the three-stage selection and sends the turn once it is complete.
func (a *ui) handleBattleTaps(gtx layout.Context, b *api.Battle, mine, theirs api.Side) {
	if !a.bc.MyTurn(b) || b.Status == "finished" || a.busy {
		return
	}
	if a.backBtn.Clicked(gtx) {
		a.sel.back()
		return
	}
	switch {
	case !a.sel.haveAttacker:
		for i := range mine.Team {
			// Re-check CanAct on the tap so a stale frame cannot slip one through.
			if i < len(a.monBtns) && a.monBtns[i].Clicked(gtx) && battleclient.CanAct(mine.Team[i]) {
				a.sel.attacker = i
				a.sel.haveAttacker = true
			}
		}
	case !a.sel.haveMove:
		for i := range mine.Team[a.sel.attacker].Moves {
			if i < len(a.moveBtns) && a.moveBtns[i].Clicked(gtx) {
				a.sel.move = i
				a.sel.haveMove = true
				if onlyOneTarget(theirs) {
					a.sel.target = defaultTarget(theirs)
					a.send(b)
				}
			}
		}
	default:
		for i := range theirs.Team {
			if i < len(a.tgtBtns) && a.tgtBtns[i].Clicked(gtx) && battleclient.CanBeTargeted(theirs.Team[i]) {
				a.sel.target = i
				a.send(b)
			}
		}
	}
}

func (a *ui) send(b *api.Battle) {
	// Recorded before reset so a test can assert which turn went out.
	a.lastSent = a.sel
	a.attack(b.ID, a.sel.attacker, a.sel.move, a.sel.target)
	a.sel.reset()
}

// keepWatching long-polls while it is not our turn so the opponent's move appears without a manual refresh.
func (a *ui) keepWatching(b *api.Battle) {
	if a.watching || b.Status == "finished" || a.bc.MyTurn(b) {
		return
	}
	a.watching = true
	a.watch(b.ID, a.lastSeen)
}
