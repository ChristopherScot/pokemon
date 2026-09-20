package main

// A live render with real sprites, so the layout is checked against
// actual images rather than the monogram fallback.

import (
	"os"
	"strconv"
	"testing"
	"time"

	"gioui.org/widget"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestBrowseWithRealSprites(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("set SHOTS to render against the live sprite CDN")
	}
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.screen = screenBrowse
	h.ui.dex = []api.Pokemon{
		{ID: 1, Name: "bulbasaur", Types: []string{"grass", "poison"},
			Sprite: "https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/1.png"},
		{ID: 4, Name: "charmander", Types: []string{"fire"},
			Sprite: "https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/4.png"},
		{ID: 7, Name: "squirtle", Types: []string{"water"},
			Sprite: "https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/7.png"},
		{ID: 25, Name: "pikachu", Types: []string{"electric"},
			Sprite: "https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/25.png"},
	}
	h.ui.dexClicks = make([]widget.Clickable, len(h.ui.dex))
	h.ui.team = []string{"pikachu"}

	// First frame starts the fetches and draws monograms.
	h.frame()
	// Give the CDN a moment, then draw again with the images in hand.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		h.ui.sprites.mu.Lock()
		got := len(h.ui.sprites.imgs)
		h.ui.sprites.mu.Unlock()
		if got == len(h.ui.dex) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.frame()
	h.shot("browse-sprites")

	h.ui.sprites.mu.Lock()
	n := len(h.ui.sprites.imgs)
	h.ui.sprites.mu.Unlock()
	if n == 0 {
		t.Error("no sprites decoded; the list is all monograms")
	}
	t.Logf("decoded %d/%d sprites", n, len(h.ui.dex))
}

// The battle screen with real sprites, which is where they matter
// most - two teams of small images is the whole readout.
func TestBattleWithRealSprites(t *testing.T) {
	if os.Getenv("SHOTS") == "" {
		t.Skip("set SHOTS to render against the live sprite CDN")
	}
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	b := testBattle("ash", "misty")
	sprite := func(id int) string {
		return "https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/" +
			strconv.Itoa(id) + ".png"
	}
	b.Sides[0].Team[0].Sprite = sprite(25) // pikachu
	b.Sides[0].Team[1].Sprite = sprite(74) // geodude
	b.Sides[1].Team[0].Sprite = sprite(120)
	b.Sides[1].Team[1].Sprite = sprite(54)
	h.ui.battle = b

	h.frame()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		h.ui.sprites.mu.Lock()
		got := len(h.ui.sprites.imgs)
		h.ui.sprites.mu.Unlock()
		if got == 4 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.frame()
	h.shot("battle-sprites")
}
