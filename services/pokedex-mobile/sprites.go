package main

// Sprite loading.
//
// Gio is immediate mode: layout runs every frame, so an image has to
// be ready before it can be drawn - there is no "load then re-render"
// callback. The cache below is what bridges that. A miss starts a
// fetch and draws a placeholder; the fetch invalidates the window when
// it lands, and the next frame finds the image ready.
//
// Sprites come from the API as absolute URLs to PokeAPI's CDN, so
// nothing here knows how to build one.

import (
	"context"
	"image"
	_ "image/png" // sprites are PNG; the decoder registers itself
	"net/http"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// spriteTimeout bounds one image fetch. Short: a sprite is decoration,
// and a phone that has wandered off wifi should fall back to the
// monogram rather than hold a slot open.
const spriteTimeout = 10 * time.Second

// maxSprites caps the cache. A full Pokedex of 96px PNGs is a few MB
// decoded, which is fine, but an unbounded map on a phone is not a
// thing to leave lying around.
const maxSprites = 400

type spriteCache struct {
	mu      sync.Mutex
	imgs    map[string]*image.RGBA
	pending map[string]bool
	failed  map[string]bool

	// invalidate is the window redraw, so a sprite that arrives after
	// its frame still gets drawn.
	invalidate func()
}

func newSpriteCache(invalidate func()) *spriteCache {
	return &spriteCache{
		imgs:       map[string]*image.RGBA{},
		pending:    map[string]bool{},
		failed:     map[string]bool{},
		invalidate: invalidate,
	}
}

// get returns a decoded sprite, starting a fetch on the first miss.
//
// Returns nil while loading or after a failure, which the caller draws
// as a monogram - a Pokedex that shows a gap where a picture should be
// looks broken, where a letter looks deliberate.
func (c *spriteCache) get(url string) *image.RGBA {
	if url == "" {
		return nil
	}
	c.mu.Lock()
	if img, ok := c.imgs[url]; ok {
		c.mu.Unlock()
		return img
	}
	if c.pending[url] || c.failed[url] {
		c.mu.Unlock()
		return nil
	}
	c.pending[url] = true
	c.mu.Unlock()

	go c.fetch(url)
	return nil
}

func (c *spriteCache) fetch(url string) {
	ctx, cancel := context.WithTimeout(context.Background(), spriteTimeout)
	defer cancel()

	img, err := loadImage(ctx, url)

	c.mu.Lock()
	delete(c.pending, url)
	if err != nil {
		// Remembered as failed so a broken URL is attempted once
		// rather than on every frame forever.
		c.failed[url] = true
	} else {
		if len(c.imgs) >= maxSprites {
			// Crude, but a Pokedex is browsed in order: whatever is
			// oldest is furthest from the viewport.
			for k := range c.imgs {
				delete(c.imgs, k)
				break
			}
		}
		c.imgs[url] = img
	}
	c.mu.Unlock()

	if c.invalidate != nil {
		c.invalidate()
	}
}

func loadImage(ctx context.Context, url string) (*image.RGBA, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errSprite(resp.Status)
	}
	src, _, err := image.Decode(resp.Body)
	if err != nil {
		return nil, err
	}
	// Converted to RGBA once here rather than per frame: paint.NewImageOp
	// re-uploads whatever it is given, and decoding formats vary.
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Set(x-b.Min.X, y-b.Min.Y, src.At(x, y))
		}
	}
	return dst, nil
}

type errSprite string

func (e errSprite) Error() string { return "sprite: " + string(e) }

// spriteOrMonogram draws the sprite at size, falling back to the
// lettered circle while it loads or if it never does.
func (a *ui) spriteOrMonogram(gtx layout.Context, th *material.Theme, url, fallback string, size unit.Dp, selected bool) layout.Dimensions {
	img := a.sprites.get(url)
	if img == nil {
		return avatarSized(gtx, th, fallback, size, selected)
	}
	d := gtx.Dp(size)
	// Circular, matching the monogram it replaces, so a list does not
	// reflow as sprites arrive.
	defer clipCircle(gtx, d).Pop()
	paint.FillShape(gtx.Ops, m3.surfaceVariant, clipRectOp(d))

	op.Affine(scaleTo(img.Bounds(), d)).Add(gtx.Ops)
	paint.NewImageOp(img).Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d, d)}
}

// clipCircle clips to a circle of diameter d.
func clipCircle(gtx layout.Context, d int) clip.Stack {
	return clip.RRect{
		Rect: image.Rectangle{Max: image.Pt(d, d)},
		SE:   d / 2, SW: d / 2, NE: d / 2, NW: d / 2,
	}.Push(gtx.Ops)
}

func clipRectOp(d int) clip.Op {
	return clip.Rect(image.Rectangle{Max: image.Pt(d, d)}).Op()
}

// scaleTo fits a sprite into a d-by-d box.
//
// Sprites are 96px squares and the slots here are smaller, so this is
// nearly always a shrink. Uniform, because a stretched Pokemon is
// immediately wrong to anyone who knows them.
func scaleTo(b image.Rectangle, d int) f32.Affine2D {
	w, h := float32(b.Dx()), float32(b.Dy())
	if w == 0 || h == 0 {
		return f32.Affine2D{}
	}
	s := float32(d) / w
	if sh := float32(d) / h; sh < s {
		s = sh
	}
	// Centred in the box after scaling.
	dx := (float32(d) - w*s) / 2
	dy := (float32(d) - h*s) / 2
	return f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(s, s)).Offset(f32.Pt(dx, dy))
}
