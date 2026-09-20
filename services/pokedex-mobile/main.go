// pokedex-mobile
package main

// A Gio app: one Go binary that builds for a phone and for your desktop.
//
// THIS WHOLE REPO IS YOURS. A mobile app has no config.yaml, so
// `regen`, `render`, `check` and `vault` do not apply to it - `init`
// scaffolded these files once and nothing rewrites any of them.
//
// Two couplings survive that, and neither is checked at build time:
//
//   - VERSION drives releases. CI publishes only when its first line
//     changes on main, tagging what it says and attaching the APK.
//   - The app id (com.github.christopherscot.pokemon.services.pokedex_mobile) is what Android uses to decide whether
//     an install is an UPGRADE or a different app. Change it and a
//     phone treats the next build as a separate app, keeping the old
//     one installed alongside.
//
// Run it on your desktop with `go run .` - the same code, in a window.
// That is the whole reason to write a phone app in Gio rather than in
// Kotlin: the edit-run loop does not involve a device.

import (
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// version is set at build time via ldflags; "dev" for a local build.
var version = "dev"

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("Pokedex Mobile"))
		if err := loop(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	// app.Main blocks forever and must run on the main goroutine: on
	// Android and iOS it IS the platform's UI thread, and the window
	// above only starts once it is running.
	app.Main()
}

func loop(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	var ops op.Ops
	ui := newUI()

	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ui.layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// ui is the app's state. Gio is immediate mode: layout runs every
// frame and draws whatever this holds, so there is no widget tree to
// keep in sync - change a field and the next frame shows it.
type ui struct {
	count  int
	button widget.Clickable

	// A placeholder until this talks to the API. The TYPE is the point:
	// it is the same api.BattlePokemon the CLI and TUI render, so the
	// display code here is shared rather than reimplemented.
	mon api.BattlePokemon
}

func newUI() *ui {
	return &ui{mon: api.BattlePokemon{Name: "pikachu", Types: []string{"electric"}}}
}

func (u *ui) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Clicked() is drained here rather than watched elsewhere: the
	// event is consumed by asking, and asking twice in one frame
	// reports one click as two.
	for u.button.Clicked(gtx) {
		u.count++
	}

	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceAround}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.H4(th, "Pokedex Mobile").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, summarise(u.mon)).Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Button(th, &u.button, "Tap me").Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Caption(th, "version "+version).Layout(gtx)
			}),
		)
	})
}
