package main

// A headless harness that renders the REAL UI and taps it.
//
// Gio can draw to an offscreen buffer (gpu/headless), so a test can
// run a frame, look at the pixels, and synthesise a tap at a point -
// no display, no device, no emulator. That makes a playthrough
// something CI can run, rather than something only a human with a
// phone can check.
//
// What this does NOT cover is how it feels under a thumb. Sizing is
// asserted instead: every control is at least Android's 48dp floor.

import (
	"image"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/gpu/headless"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// phoneSize is a mid-range Android in density-independent pixels.
const phoneW, phoneH = 411, 891

type harness struct {
	t   *testing.T
	ui  *ui
	th  *material.Theme
	win *headless.Window
	rtr input.Router
	ops op.Ops
	img *image.RGBA
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	w, err := headless.NewWindow(phoneW, phoneH)
	if err != nil {
		t.Skipf("no GPU available for headless rendering: %v", err)
	}
	t.Cleanup(w.Release)

	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	// newUI, not &ui{}: the constructor allocates the widget slices,
	// and a harness that skips it renders a battle with no buttons -
	// which is exactly the bug this harness existed to catch, found
	// in the harness itself.
	return &harness{
		t: t, th: th, win: w,
		ui:  newUI(nil),
		img: image.NewRGBA(image.Rect(0, 0, phoneW, phoneH)),
	}
}

// frame renders one frame of the real UI and returns the pixels.
func (h *harness) frame() *image.RGBA {
	h.t.Helper()
	h.ops.Reset()
	gtx := layout.Context{
		Ops:         &h.ops,
		Constraints: layout.Exact(image.Pt(phoneW, phoneH)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Source:      h.rtr.Source(),
	}
	h.ui.drain()
	h.ui.layout(gtx, h.th)
	h.rtr.Frame(gtx.Ops)
	if err := h.win.Frame(gtx.Ops); err != nil {
		h.t.Fatalf("render: %v", err)
	}
	if err := h.win.Screenshot(h.img); err != nil {
		h.t.Fatalf("screenshot: %v", err)
	}
	return h.img
}

// tap sends a press and release at a point, then renders so the click
// is observed by the widget that owns it.
func (h *harness) tap(x, y int) {
	h.t.Helper()
	pos := f32.Pt(float32(x), float32(y))
	for _, k := range []pointer.Kind{pointer.Press, pointer.Release} {
		h.rtr.Queue(pointer.Event{
			Kind:     k,
			Source:   pointer.Touch,
			Position: pos,
			Time:     time.Since(time.Time{}),
		})
	}
	h.frame()
}

// drag scrolls a list the way a thumb does: press, move, release.
//
// Only Press, Move, Leave and Release can be injected - the router
// DERIVES Drag from a Move while pressed, and queueing a Drag
// directly panics with "unsupported pointer event type".
func (h *harness) drag(x, y0, y1 int) {
	h.t.Helper()
	base := time.Since(time.Time{})
	steps := []struct {
		k  pointer.Kind
		y  int
		dt time.Duration
	}{
		{pointer.Press, y0, 0},
		{pointer.Move, y0 - (y0-y1)/3, 16 * time.Millisecond},
		{pointer.Move, y0 - 2*(y0-y1)/3, 32 * time.Millisecond},
		{pointer.Move, y1, 48 * time.Millisecond},
		{pointer.Release, y1, 64 * time.Millisecond},
	}
	for _, s := range steps {
		h.rtr.Queue(pointer.Event{
			Kind:     s.k,
			Source:   pointer.Touch,
			Position: f32.Pt(float32(x), float32(s.y)),
			Time:     base + s.dt,
		})
		h.frame()
	}
}

// scroll moves a list by a wheel-style scroll, which is what a
// material.List consumes directly.
func (h *harness) scroll(x, y, dy int) {
	h.t.Helper()
	h.rtr.Queue(pointer.Event{
		Kind:     pointer.Scroll,
		Source:   pointer.Mouse,
		Position: f32.Pt(float32(x), float32(y)),
		Scroll:   f32.Pt(0, float32(dy)),
		Time:     time.Since(time.Time{}),
	})
	h.frame()
}

// find returns the on-screen bounds of the widget whose accessibility
// description matches, so a test can say "tap Pikachu" rather than
// "tap at 205,208".
//
// Coordinates worked out by hand are a liability: they encode the
// layout into every test, so changing a padding breaks tests that
// have nothing to do with padding - which is exactly what happened
// twice while restyling this app. Semantics are what the widget
// already publishes for TalkBack, so this reuses a thing that has to
// be right anyway.
func (h *harness) find(desc string) (image.Point, bool) {
	h.t.Helper()
	// SemanticNode carries no public bounds, so this asks the router
	// what is under each point of a coarse grid and returns the first
	// hit. A 4px step is finer than any control here is small.
	want := map[input.SemanticID]bool{}
	var walk func([]input.SemanticNode)
	walk = func(ns []input.SemanticNode) {
		for _, n := range ns {
			if n.Desc.Description == desc || n.Desc.Label == desc {
				want[n.ID] = true
			}
			walk(n.Children)
		}
	}
	walk(h.rtr.AppendSemantics(nil))
	if len(want) == 0 {
		return image.Point{}, false
	}
	for y := 0; y < phoneH; y += 4 {
		for x := 0; x < phoneW; x += 4 {
			if id, ok := h.rtr.SemanticAt(f32.Pt(float32(x), float32(y))); ok && want[id] {
				return image.Pt(x, y), true
			}
		}
	}
	return image.Point{}, false
}

// tapOn taps the centre of the named widget.
func (h *harness) tapOn(desc string) {
	h.t.Helper()
	p, ok := h.find(desc)
	if !ok {
		h.t.Fatalf("no widget described %q on screen; visible: %v", desc, h.visible())
	}
	h.tap(p.X, p.Y)
}

// visible lists what a test could have tapped, so a failure names the
// alternatives instead of just saying no.
func (h *harness) visible() []string {
	var out []string
	var walk func([]input.SemanticNode)
	walk = func(ns []input.SemanticNode) {
		for _, n := range ns {
			if d := n.Desc.Description; d != "" {
				out = append(out, d)
			}
			walk(n.Children)
		}
	}
	walk(h.rtr.AppendSemantics(nil))
	return out
}

// notBlank reports whether anything was drawn, so a test that taps into
// a void fails rather than passing silently.
func notBlank(img *image.RGBA) bool {
	first := img.RGBAAt(0, 0)
	for y := 0; y < img.Bounds().Dy(); y += 7 {
		for x := 0; x < img.Bounds().Dx(); x += 7 {
			if img.RGBAAt(x, y) != first {
				return true
			}
		}
	}
	return false
}

var _ = app.FrameEvent{} // the app package is what the UI is built on
var _ event.Event = pointer.Event{}
