// pokedex-mobile
package main

// A Gio app: one Go binary that builds for a phone and for your desktop.
//
// THIS WHOLE REPO IS YOURS. A mobile app has no config.yaml, so
// `regen`, `render`, `check` and `vault` do not apply to it.
//
// The game protocol is battleclient's, shared with pokedex-cli and
// pokedex-tui through a replace directive, and the wording is
// battletext's. This file is the UI: what a thumb can reach, and when
// to ask the server something. It reimplements no game logic.
//
// Run it on your desktop with `go run .` - the same code, in a window.
// That is the whole reason to write a phone app in Gio rather than in
// Kotlin: the edit-run loop does not involve a device.

import (
	"image/color"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// version is set at build time via ldflags; "dev" for a local build.
var version = "dev"

func main() {
	go func() {
		w := new(app.Window)
		w.Option(app.Title("Pokedex"))
		if err := loop(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	// app.Main blocks forever and must run on the main goroutine: on
	// Android it IS the platform's UI thread, and the window above
	// only starts once it is running.
	app.Main()
}

func loop(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	applyScheme(th)

	a := newUI(w)
	a.start()

	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			close(a.done)
			return e.Err
		case app.FrameEvent:
			a.drain()
			gtx := app.NewContext(&ops, e)
			a.layout(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// ui is every piece of state the UI draws from.
//
// Gio is immediate mode: layout runs each frame and draws whatever
// this holds, so there is no widget tree to keep in sync. Change a
// field and the next frame shows it.
type ui struct {
	w       *app.Window
	results chan result

	// done is closed when the window dies, so a background goroutine
	// blocked on delivery exits instead of leaking. Its lifetime is
	// then obvious: it lives until the UI takes its result, or until
	// there is no UI left to take it.
	done chan struct{}

	// confirmLeave arms the second back press that leaves a live
	// battle, so one stray gesture does not forfeit it.
	confirmLeave bool

	// sprites decodes and caches the images the API points at.
	sprites *spriteCache

	api *api.Client
	bc  *battleclient.Client
	id  battleclient.Identity

	screen screen
	busy   bool
	status string

	// browse
	dex       []api.Pokemon
	dexList   widget.List
	dexClicks []widget.Clickable
	filter    widget.Editor

	// register
	nameInput widget.Editor
	regBtn    widget.Clickable

	// team
	joining  string
	team     []string
	startBtn widget.Clickable
	teamBack widget.Clickable

	// lobby
	lobby      *api.WaitingList
	lobbyList  widget.List
	lobbyBtns  []widget.Clickable
	newBattle  widget.Clickable
	refreshBtn widget.Clickable

	// battle
	battle *api.Battle
	sel    pick
	// lastSent is the turn most recently dispatched, so a test can
	// assert which turn went out rather than only that one did.
	lastSent pick
	monBtns  []widget.Clickable
	moveBtns []widget.Clickable
	tgtBtns  []widget.Clickable
	leaveBtn widget.Clickable
	backBtn  widget.Clickable
	logList  widget.List
	watching bool
	lastSeen int

	// nav
	navBtns [4]widget.Clickable
}

func newUI(w *app.Window) *ui {
	a := &ui{w: w, results: make(chan result, 8), done: make(chan struct{})}
	a.dexList.Axis = layout.Vertical
	a.lobbyList.Axis = layout.Vertical
	a.logList.Axis = layout.Vertical
	a.filter.SingleLine = true
	a.nameInput.SingleLine = true
	a.monBtns = make([]widget.Clickable, 8)
	a.moveBtns = make([]widget.Clickable, 8)
	a.tgtBtns = make([]widget.Clickable, 8)
	a.sprites = newSpriteCache(a.invalidate)
	return a
}

// start restores a saved trainer, or asks for a name.
//
// The identity file is the same one the CLI writes, so a phone and a
// terminal share a trainer when they share a home directory - which on
// Android they do not, hence the register screen.
func (a *ui) start() {
	// No saved identity is the normal first run, not a failure.
	id, err := battleclient.LoadIdentity()
	if err == nil {
		a.useIdentity(id)
		a.screen = screenBrowse
	} else {
		a.screen = screenRegister
	}
	c, err := api.NewClient(apiBase())
	if err != nil {
		a.status = statusFor(err)
		return
	}
	a.api = c
	a.loadDex()
}

func (a *ui) useIdentity(id battleclient.Identity) {
	a.id = id
	bc, err := battleclient.New(id)
	if err != nil {
		// Silently leaving bc nil produced "register a trainer
		// first" from the lobby - actively misleading to someone who
		// had just registered successfully.
		a.status = statusFor(err)
		return
	}
	a.bc = bc
}

// invalidate asks for a redraw. Tolerates a nil window so a headless
// test can drive the same code the app runs.
func (a *ui) invalidate() {
	if a.w != nil {
		a.w.Invalidate()
	}
}

// drain applies everything the background goroutines finished.
//
// Called once per frame before layout, so a frame never renders a
// half-applied result.
func (a *ui) drain() {
	for {
		select {
		case r := <-a.results:
			a.apply(r)
		default:
			return
		}
	}
}

func (a *ui) apply(r result) {
	a.busy = false
	if r.err != nil {
		a.status = statusFor(r.err)
		// A failed watch should not stop us watching, or the battle
		// silently stops updating and looks frozen.
		if r.kind == resBattle {
			a.watching = false
		}
		return
	}
	a.status = ""
	switch r.kind {
	case resDex:
		a.dex = r.dex
		a.dexClicks = make([]widget.Clickable, len(r.dex))
	case resRegistered:
		a.useIdentity(r.ident)
		a.screen = screenBrowse
	case resLobby:
		a.lobby = r.lobby
		if r.lobby != nil {
			a.lobbyBtns = make([]widget.Clickable, len(r.lobby.Waiting))
		}
	case resWatchIdle:
		// Nothing happened; let keepWatching poll again.
		a.watching = false
	case resBattle:
		a.battle = r.battle
		a.watching = false
		a.sel.reset()
		if r.battle != nil {
			a.screen = screenBattle
			a.lastSeen = len(r.battle.Log)
		}
	}
}

// statusFor turns an error into the one line the UI has room for.
//
// Raw client errors are unreadable on a phone. The worst is ogen's
// own request validation, which answers with {"error_message": ...}
// where the spec declares {"message": ...} - so the generated client
// cannot decode it and surfaces "decode response: default (code 400):
// decode application/json: invalid: message (field required)". That
// string tells a player nothing and looks like a crash.
func statusFor(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "decode response"), strings.Contains(msg, "field required"):
		return "The server rejected that. Try a different name."
	case strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "Client.Timeout"):
		return "No answer from the server. Check your connection."
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "dial tcp"):
		return "Cannot reach the server."
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"):
		return "That trainer is not recognised. Register again."
	}

	// Anything else: show it, but trimmed on a RUNE boundary so a
	// multi-byte character is never cut in half into U+FFFD.
	const limit = 100
	if len(msg) > limit {
		cut := msg[:limit]
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
		msg = cut + "\u2026"
	}
	return msg
}

// --- layout ---------------------------------------------------------

var (
	accent = color.NRGBA{R: 0xD3, G: 0x2F, B: 0x2F, A: 0xFF}
	dim    = color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xFF}
)

// tapTarget is the minimum height of anything tappable.
//
// 48dp is Android's accessibility floor - below it people miss, and on
// a battle screen a missed tap can cost a turn.
const tapTarget = unit.Dp(48)

func (a *ui) layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	a.handleNav(gtx)
	a.readKeys(gtx)
	paint.Fill(gtx.Ops, m3.surface)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.header(gtx, th)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: gapL, Right: gapL, Top: gapM,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				switch a.screen {
				case screenRegister:
					return a.registerScreen(gtx, th)
				case screenLobby:
					return a.lobbyScreen(gtx, th)
				case screenTeam:
					return a.teamScreen(gtx, th)
				case screenBattle:
					return a.battleScreen(gtx, th)
				default:
					return a.browseScreen(gtx, th)
				}
			})
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			return a.statusBar(gtx, th)
		}),
		rigid(func(gtx layout.Context) layout.Dimensions {
			if a.screen == screenRegister {
				return layout.Dimensions{}
			}
			return a.navBar(gtx, th)
		}),
	)
}

// header is the M3 top app bar: a surface-container strip, not a
// coloured banner. The trainer name is supporting text rather than
// being crammed into the title with an em dash.
func (a *ui) header(gtx layout.Context, th *material.Theme) layout.Dimensions {
	sub := ""
	if a.id.Name != "" {
		sub = "Trainer " + a.id.Name
	}
	return topBar(gtx, th, "Pokedex", sub)
}

func (a *ui) statusBar(gtx layout.Context, th *material.Theme) layout.Dimensions {
	msg := a.status
	if msg == "" && a.busy {
		msg = "working…"
	}
	if msg == "" {
		return layout.Dimensions{}
	}
	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(th, msg)
		l.Color = accent
		return l.Layout(gtx)
	})
}
