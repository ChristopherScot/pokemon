package main

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

// m3 is the app's light scheme, built from the Pokemon brand palette; saturated brand tones sit only under white text.
var m3 = scheme{
	primary:            rgb(0xC21B17),
	onPrimary:          rgb(0xFFFFFF),
	primaryContainer:   rgb(0xFFDAD5),
	onPrimaryContainer: rgb(0x410100),

	secondary:            rgb(0x3B4CCA),
	onSecondary:          rgb(0xFFFFFF),
	secondaryContainer:   rgb(0xDFE0FF),
	onSecondaryContainer: rgb(0x00105C),

	surface:          rgb(0xFCFCFC),
	onSurface:        rgb(0x1B1B1B),
	surfaceVariant:   rgb(0xE6E1E1),
	onSurfaceVariant: rgb(0x494646),
	surfaceContainer: rgb(0xF1EFEF),
	outline:          rgb(0x7A7676),
	outlineVariant:   rgb(0xCBC6C6),

	errorColor: rgb(0xBA1A1A),
	onError:    rgb(0xFFFFFF),

	success: rgb(0x2E7D32),
	warning: rgb(0xF2A600),
}

// applyScheme points Gio's four-colour palette at M3 roles so stock widgets inherit the theme.
func applyScheme(th *material.Theme) {
	th.Palette.Bg = m3.surface
	th.Palette.Fg = m3.onSurface
	th.Palette.ContrastBg = m3.primary
	th.Palette.ContrastFg = m3.onPrimary
}

const (
	cornerSmall  = unit.Dp(8)
	cornerMedium = unit.Dp(12)
	cornerLarge  = unit.Dp(16)
	cornerFull   = unit.Dp(999) // pill
)

const (
	gapXS = unit.Dp(4)
	gapS  = unit.Dp(8)
	gapM  = unit.Dp(12)
	gapL  = unit.Dp(16)
	gapXL = unit.Dp(24)
)

func fillRRect(gtx layout.Context, c color.NRGBA, r unit.Dp, size image.Point) {
	rr := gtx.Dp(r)
	if max := min(size.X, size.Y) / 2; rr > max {
		rr = max
	}
	defer clip.RRect{Rect: image.Rectangle{Max: size}, SE: rr, SW: rr, NE: rr, NW: rr}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, c)
}

func card(gtx layout.Context, bg color.NRGBA, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(gapM).Layout(gtx, w)
	call := macro.Stop()

	fillRRect(gtx, bg, cornerMedium, dims.Size)
	call.Add(gtx.Ops)
	return dims
}

// typeColour returns the franchise's conventional colour for a type; container tones only, since saturated fills fail contrast under small text.
func typeColour(t string) (bg, fg color.NRGBA) {
	switch t {
	case "fire":
		return rgb(0xFFDAD4), rgb(0x410002)
	case "water":
		return rgb(0xD3E4FF), rgb(0x001B3D)
	case "grass":
		return rgb(0xC8EFC0), rgb(0x00210B)
	case "electric":
		return rgb(0xFBE38A), rgb(0x241A00)
	case "psychic":
		return rgb(0xFFD9E2), rgb(0x3E001D)
	case "ice":
		return rgb(0xCDEDF6), rgb(0x001F26)
	case "dragon":
		return rgb(0xE0DDFF), rgb(0x18005D)
	case "dark":
		return rgb(0xDCC3B4), rgb(0x2B1709)
	case "fairy":
		return rgb(0xFFD8ED), rgb(0x3A0025)
	case "fighting":
		return rgb(0xFFDAD6), rgb(0x410002)
	case "poison":
		return rgb(0xF0D9FF), rgb(0x2D0050)
	case "ground":
		return rgb(0xF5E0B8), rgb(0x261A00)
	case "flying":
		return rgb(0xE2E0F9), rgb(0x1B1B2C)
	case "bug":
		return rgb(0xDBE8B4), rgb(0x191E00)
	case "rock":
		return rgb(0xE9E0CF), rgb(0x211B10)
	case "ghost":
		return rgb(0xE6DEFF), rgb(0x21005D)
	case "steel":
		return rgb(0xDEE3EB), rgb(0x171C22)
	default: // normal
		return m3.surfaceVariant, m3.onSurfaceVariant
	}
}

func typeChips(gtx layout.Context, th *material.Theme, types []string) layout.Dimensions {
	var children []layout.FlexChild
	for _, t := range types {
		t := t
		children = append(children, rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: gapXS}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg, fg := typeColour(t)
				macro := op.Record(gtx.Ops)
				d := layout.Inset{
					Left: gapS, Right: gapS, Top: unit.Dp(2), Bottom: unit.Dp(2),
				}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Label(th, unit.Sp(11), t)
					l.Color = fg
					l.MaxLines = 1
					return l.Layout(gtx)
				})
				call := macro.Stop()
				fillRRect(gtx, bg, cornerSmall, d.Size)
				call.Add(gtx.Ops)
				return d
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

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

type btnStyle int

const (
	btnFilled   btnStyle = iota
	btnTonal
	btnOutlined
	btnSelected
)

func m3Button(gtx layout.Context, th *material.Theme, c *widget.Clickable, label string, style btnStyle, enabled bool) layout.Dimensions {
	return m3ButtonDesc(gtx, th, c, label, label, style, enabled)
}

// m3ButtonDesc is m3Button with the accessibility name separated from the visible label.
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
		// M3 disabled: 12%/38% opacity so it still reads as a control.
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
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Semantics must go inside the button's own layout; at outer scope the last button claims every earlier one's name.
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

func listItem(gtx layout.Context, th *material.Theme, c *widget.Clickable, leading, title, supporting, trailing string, selected bool) layout.Dimensions {
	return listItemWith(gtx, th, c, nil, leading, title, supporting, trailing, selected)
}

// listItemWith is listItem with a custom leading widget in place of the lettered monogram.
func listItemWith(gtx layout.Context, th *material.Theme, c *widget.Clickable, lead layout.Widget, leading, title, supporting, trailing string, selected bool) layout.Dimensions {
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
					if lead == nil && leading == "" {
						return layout.Dimensions{}
					}
					return layout.Inset{Right: gapM}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						if lead != nil {
							return lead(gtx)
						}
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

// listItemTyped uses type chips in place of the supporting text line.
func listItemTyped(gtx layout.Context, th *material.Theme, c *widget.Clickable, lead layout.Widget, name string, types []string, trailing string, selected bool) layout.Dimensions {
	bg, fg := m3.surface, m3.onSurface
	if selected {
		bg, fg = m3.primaryContainer, m3.onPrimaryContainer
	}
	b := material.ButtonLayoutStyle{Background: bg, CornerRadius: cornerMedium, Button: c}
	gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(72)) // M3 two-line list item
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(name).Add(gtx.Ops)
		if selected {
			semantic.SelectedOp(true).Add(gtx.Ops)
		}
		return layout.Inset{Left: gapM, Right: gapL, Top: gapS, Bottom: gapS}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: gapM}.Layout(gtx, lead)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Label(th, unit.Sp(16), name)
								l.Color = fg
								l.MaxLines = 1
								return l.Layout(gtx)
							}),
							rigid(layout.Spacer{Height: gapXS}.Layout),
							rigid(func(gtx layout.Context) layout.Dimensions {
								return typeChips(gtx, th, types)
							}),
						)
					}),
					rigid(func(gtx layout.Context) layout.Dimensions {
						if trailing == "" {
							return layout.Dimensions{}
						}
						l := material.Label(th, unit.Sp(12), trailing)
						l.Color = m3.onSurfaceVariant
						return l.Layout(gtx)
					}),
				)
			})
	})
}

func avatar(gtx layout.Context, th *material.Theme, s string, selected bool) layout.Dimensions {
	return avatarSized(gtx, th, s, unit.Dp(40), selected)
}

// avatarSized is the monogram at an explicit size, so a missing sprite occupies the same slot as one that loaded.
func avatarSized(gtx layout.Context, th *material.Theme, s string, size unit.Dp, selected bool) layout.Dimensions {
	d := gtx.Dp(size)
	bg := m3.secondaryContainer
	fg := m3.onSecondaryContainer
	if selected {
		bg, fg = m3.primary, m3.onPrimary
	}
	box := image.Pt(d, d)
	fillRRect(gtx, bg, cornerFull, box)
	gtx.Constraints = layout.Exact(box)
	layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Label(th, unit.Sp(15), s)
		l.Color = fg
		l.MaxLines = 1
		return l.Layout(gtx)
	})
	return layout.Dimensions{Size: box}
}
