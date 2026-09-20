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

	return &harness{
		t: t, th: th, win: w,
		ui:  &ui{results: make(chan result, 8)},
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

// drag scrolls a list, which on a phone is the only way to reach past
// the first screenful.
func (h *harness) drag(x, y0, y1 int) {
	h.t.Helper()
	now := time.Since(time.Time{})
	steps := []struct {
		k pointer.Kind
		y int
	}{{pointer.Press, y0}, {pointer.Move, (y0 + y1) / 2}, {pointer.Move, y1}, {pointer.Release, y1}}
	for i, s := range steps {
		h.rtr.Queue(pointer.Event{
			Kind:     s.k,
			Source:   pointer.Touch,
			Position: f32.Pt(float32(x), float32(s.y)),
			Time:     now + time.Duration(i)*10*time.Millisecond,
		})
	}
	h.frame()
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
