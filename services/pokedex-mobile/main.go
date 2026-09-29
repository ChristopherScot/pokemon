package main

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
	// app.Main blocks forever on the main goroutine — on Android it IS the platform's UI thread.
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

// ui holds every piece of state the layout draws from.
type ui struct {
	w       *app.Window
	results chan result

	// done is closed when the window dies so background senders exit rather than leak.
	done chan struct{}

	// confirmLeave arms a two-tap-to-forfeit gesture.
	confirmLeave bool

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
	// lastSent is the turn most recently dispatched, so tests can assert which turn went out.
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
		// bc must not stay nil — the lobby would say "register a trainer first" to someone who just did.
		a.status = statusFor(err)
		return
	}
	a.bc = bc
}

// invalidate asks for a redraw; tolerates a nil window so headless tests can drive the same code.
func (a *ui) invalidate() {
	if a.w != nil {
		a.w.Invalidate()
	}
}

// drain applies everything background goroutines finished, once per frame before layout.
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
		// A failed watch must not stop us watching — the battle would silently freeze.
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
	case strings.Contains(msg, "is taken"):
		// Server message is already player-facing; pass through.
		return msg
	}

	// Trim on a rune boundary so a multi-byte char isn't cut to U+FFFD.
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

var (
	accent = color.NRGBA{R: 0xD3, G: 0x2F, B: 0x2F, A: 0xFF}
	dim    = color.NRGBA{R: 0x77, G: 0x77, B: 0x77, A: 0xFF}
)

// tapTarget: 48dp is Android's accessibility floor for a reliable hit.
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
