package main

// Material 3 colour roles and the surfaces built from them.
//
// Gio's material.Palette carries four colours - Bg, Fg, ContrastBg,
// ContrastFg - which is enough for a button and not enough for a
// design system. M3 separates "a filled button" from "a container that
// holds content" from "an outline", and each has a paired on- colour
// that is guaranteed to be legible on it. Those pairs are what stop a
// UI looking like coloured rectangles.
//
// Values are the M3 baseline scheme, the one Google ships when an app
// has no dynamic colour to derive from.

import (
	"image"
	"image/color"

	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type scheme struct {
	primary, onPrimary                   color.NRGBA
	primaryContainer, onPrimaryContainer color.NRGBA
	secondary, onSecondary               color.NRGBA
	secondaryContainer                   color.NRGBA
	onSecondaryContainer                 color.NRGBA
	surface, onSurface                   color.NRGBA
	surfaceVariant, onSurfaceVariant     color.NRGBA
	surfaceContainer                     color.NRGBA
	outline, outlineVariant              color.NRGBA
	errorColor, onError                  color.NRGBA
	success, warning                     color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xFF}
}

// m3 is the baseline light scheme.
//
// Not the purple default: a Pokedex is a red device, so primary is
// derived from that. The rest keeps M3's relationships - a container
// is a desaturated tint of its role, and every on- colour clears the
// contrast floor against its pair.
var m3 = scheme{
	primary:              rgb(0xB3261E),
	onPrimary:            rgb(0xFFFFFF),
	primaryContainer:     rgb(0xF9DEDC),
	onPrimaryContainer:   rgb(0x410E0B),
	secondary:            rgb(0x625B71),
	onSecondary:          rgb(0xFFFFFF),
	secondaryContainer:   rgb(0xE8DEF8),
	onSecondaryContainer: rgb(0x1D192B),
	surface:              rgb(0xFFFBFE),
	onSurface:            rgb(0x1C1B1F),
	surfaceVariant:       rgb(0xE7E0EC),
	onSurfaceVariant:     rgb(0x49454F),
	surfaceContainer:     rgb(0xF3EDF7),
	outline:              rgb(0x79747E),
	outlineVariant:       rgb(0xCAC4D0),
	errorColor:           rgb(0xB3261E),
	onError:              rgb(0xFFFFFF),
	success:              rgb(0x386A20),
	warning:              rgb(0x7D5700),
}

// applyScheme points Gio's four-colour palette at the M3 roles it
// corresponds to, so the stock widgets inherit the theme.
func applyScheme(th *material.Theme) {
	th.Palette.Bg = m3.surface
	th.Palette.Fg = m3.onSurface
	th.Palette.ContrastBg = m3.primary
	th.Palette.ContrastFg = m3.onPrimary
}

// M3 shape scale. Corners are what make a container read as a card
// rather than as a painted rectangle.
const (
	cornerSmall  = unit.Dp(8)
	cornerMedium = unit.Dp(12)
	cornerLarge  = unit.Dp(16)
	cornerFull   = unit.Dp(999) // pill
)

// M3 spacing: a 4dp grid. Consistent gaps are most of what separates a
// laid-out screen from a stacked one.
const (
	gapXS = unit.Dp(4)
	gapS  = unit.Dp(8)
	gapM  = unit.Dp(12)
	gapL  = unit.Dp(16)
	gapXL = unit.Dp(24)
)

// fillRRect paints a rounded rectangle behind a widget.
func fillRRect(gtx layout.Context, c color.NRGBA, r unit.Dp, size image.Point) {
	rr := gtx.Dp(r)
	if max := min(size.X, size.Y) / 2; rr > max {
		rr = max
	}
	defer clip.RRect{Rect: image.Rectangle{Max: size}, SE: rr, SW: rr, NE: rr, NW: rr}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, c)
}

// card is an M3 filled card: a surface container with a corner radius
// and interior padding, which is how related things get grouped
// instead of floating loose on the background.
func card(gtx layout.Context, bg color.NRGBA, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(gapM).Layout(gtx, w)
	call := macro.Stop()

	fillRRect(gtx, bg, cornerMedium, dims.Size)
	call.Add(gtx.Ops)
	return dims
}

// --- M3 components --------------------------------------------------

// topBar is M3's small top app bar: a surface-container strip with the
// title at 22sp, not a coloured banner.
func topBar(gtx layout.Context, th *material.Theme, title, subtitle string) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := layout.Inset{
		Left: gapL, Right: gapL, Top: gapM, Bottom: gapM,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th, unit.Sp(22), title)
				l.Color = m3.onSurface
				return l.Layout(gtx)
			}),
			rigid(func(gtx layout.Context) layout.Dimensions {
				if subtitle == "" {
					return layout.Dimensions{}
				}
				l := material.Label(th, unit.Sp(14), subtitle)
				l.Color = m3.onSurfaceVariant
				return l.Layout(gtx)
			}),
		)
	})
	call := macro.Stop()
	paint.FillShape(gtx.Ops, m3.surfaceContainer,
		clip.Rect(image.Rectangle{Max: dims.Size}).Op())
	call.Add(gtx.Ops)
	return dims
}

// btnStyle is which of M3's button emphases to draw.
type btnStyle int

const (
	btnFilled   btnStyle = iota // the primary action on a screen
	btnTonal                    // a secondary action, still prominent
	btnOutlined                 // a low-emphasis action
	btnSelected                 // a tonal button showing an on state
)

// m3Button draws a button at one of M3's emphasis levels, sized to the
// 48dp touch floor with the full-pill corner M3 uses.
func m3Button(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string, style btnStyle, enabled bool) layout.Dimensions {
	return m3ButtonDesc(gtx, th, c, label, label, style, enabled)
}

// m3ButtonDesc is m3Button with the accessibility name separated from
// the visible label, for when two controls share a caption.
func m3ButtonDesc(gtx layout.Context, th *material.Theme, c *widget.Clickable, label, desc string, style btnStyle, enabled bool) layout.Dimensions {
	bg, fg := m3.primary, m3.onPrimary
	switch style {
	case btnTonal:
		bg, fg = m3.secondaryContainer, m3.onSecondaryContainer
	case btnOutlined:
		bg, fg = m3.surface, m3.primary
	case btnSelected:
		bg, fg = m3.primaryContainer, m3.onPrimaryContainer
	}
	if !enabled {
		// M3 disabled: the same shape at 12% / 38% opacity, so it
		// still reads as a control rather than vanishing.
		bg = withAlpha(m3.onSurface, 0x1F)
		fg = withAlpha(m3.onSurface, 0x61)
	}

	b := material.ButtonLayoutStyle{
		Background:   bg,
		CornerRadius: cornerFull,
		Button:       c,
	}
	if !enabled {
		gtx = gtx.Disabled()
	}
	gtx.Constraints.Min.Y = gtx.Dp(tapTarget)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	// Semantics, so TalkBack reads the button and a test can find it
	// by name instead of by a coordinate someone worked out by hand.
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Inside the button's own layout, so the description applies
		// to THIS button's area. Added at the outer scope it lands on
		// the shared parent instead, and the last button drawn
		// silently claims every earlier one's name.
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(desc).Add(gtx.Ops)
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: gapL, Right: gapL, Top: gapS, Bottom: gapS,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Label(th, unit.Sp(14), label)
				l.Color = fg
				l.MaxLines = 1
				return l.Layout(gtx)
			})
		})
	})
}

func withAlpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

// listItem is an M3 list row: a tappable container with a leading
// slot, a two-line body and an optional trailing label.
func listItem(gtx layout.Context, th *material.Theme, c *widget.Clickable, leading, title, supporting, trailing string, selected bool) layout.Dimensions {
	bg := m3.surface
	fg := m3.onSurface
	if selected {
		bg, fg = m3.primaryContainer, m3.onPrimaryContainer
	}
	b := material.ButtonLayoutStyle{
		Background:   bg,
		CornerRadius: cornerMedium,
		Button:       c,
	}
	gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(56)) // M3 two-line list item
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(title).Add(gtx.Ops)
		if selected {
			semantic.SelectedOp(true).Add(gtx.Ops)
		}
		return layout.Inset{
			Left: gapL, Right: gapL, Top: gapS, Bottom: gapS,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				rigid(func(gtx layout.Context) layout.Dimensions {
					if leading == "" {
						return layout.Dimensions{}
					}
					return layout.Inset{Right: gapM}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return avatar(gtx, th, leading, selected)
					})
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Label(th, unit.Sp(16), title)
							l.Color = fg
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
						rigid(func(gtx layout.Context) layout.Dimensions {
							if supporting == "" {
								return layout.Dimensions{}
							}
							l := material.Label(th, unit.Sp(14), supporting)
							l.Color = m3.onSurfaceVariant
							l.MaxLines = 1
							return l.Layout(gtx)
						}),
					)
				}),
				rigid(func(gtx layout.Context) layout.Dimensions {
					if trailing == "" {
						return layout.Dimensions{}
					}
					l := material.Label(th, unit.Sp(14), trailing)
					l.Color = m3.onSurfaceVariant
					return l.Layout(gtx)
				}),
			)
		})
	})
}

// avatar is the circular leading slot of a list item - M3 calls it a
// monogram, and it is what makes a list read as rows rather than as a
// wall of text.
func avatar(gtx layout.Context, th *material.Theme, s string, selected bool) layout.Dimensions {
	d := gtx.Dp(unit.Dp(40))
	bg := m3.secondaryContainer
	fg := m3.onSecondaryContainer
	if selected {
		bg, fg = m3.primary, m3.onPrimary
	}
	size := image.Pt(d, d)
	fillRRect(gtx, bg, cornerFull, size)
	gtx.Constraints = layout.Exact(size)
	layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(15), s)
		l.Color = fg
		l.MaxLines = 1
		return l.Layout(gtx)
	})
	return layout.Dimensions{Size: size}
}
