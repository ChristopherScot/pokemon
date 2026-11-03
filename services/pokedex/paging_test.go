package main

import (
	"fmt"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// A dex built here rather than from pokedex.json: the embedded file
// holds exactly 100, which is also the spec's maximum, so a test that
// used it would walk one page and prove nothing about the second. It
// would also change meaning the day someone edits that file.
//
// Every third one is water, so the type filter crosses page boundaries
// too rather than landing on one.
func dexOf(t *testing.T, n int) *pokedex {
	t.Helper()
	mons := make([]entry, 0, n)
	for i := 1; i <= n; i++ {
		types := []string{"normal"}
		if i%3 == 0 {
			types = []string{"water"}
		}
		mons = append(mons, entry{
			ID: i, Name: fmt.Sprintf("mon%03d", i), Types: types,
			BaseHp: 50, BaseAttack: 50, BaseDefense: 50,
			BaseSpeed: 50, BaseSpecialAttack: 50, BaseSpecialDefense: 50,
		})
	}
	p, err := buildPokedex(mons, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// walk pages to exhaustion the way a client is meant to, and report
// what it saw plus how many requests it took.
func walk(t *testing.T, p *pokedex, typ string, limit int) (names []string, pages int) {
	t.Helper()
	after := ""
	for {
		page, next := p.list(typ, after, limit)
		pages++
		for _, mon := range page {
			names = append(names, mon.Name)
		}
		if next == "" {
			return names, pages
		}
		if next == after {
			t.Fatalf("cursor %q repeated; the walk would not terminate", next)
		}
		after = next
		if pages > 100 {
			t.Fatal("more than 100 pages for a dex this size; the cursor is not advancing")
		}
	}
}

// The bug this endpoint had: 101 pokemon behind a limit of 100 returned
// 100 and said nothing about the hundred-and-first.
func TestAFullPageSaysThereIsMore(t *testing.T) {
	p := dexOf(t, 101)

	page, next := p.list("", "", 100)

	if len(page) != 100 {
		t.Errorf("len(page) = %d, want 100", len(page))
	}
	if next == "" {
		t.Fatal("no cursor on a full page with one left over: a client reads 100 of 101 and cannot tell")
	}
}

func TestTheLastPageSaysSo(t *testing.T) {
	p := dexOf(t, 101)

	// 101 in pages of 100: the second page holds the one left over.
	_, next := p.list("", "", 100)
	page, next := p.list("", next, 100)

	if len(page) != 1 {
		t.Errorf("len(page) = %d, want 1", len(page))
	}
	if next != "" {
		t.Errorf("cursor %q on the last page: a client fetches an empty page to find out it was done", next)
	}
}

// A page that is full AND final: the count divides exactly, so the last
// page looks like every other one from the inside. Without a look ahead
// the server hands back a cursor for a page that turns out to be empty,
// and the client spends a round trip learning it was already finished.
func TestAFullFinalPageSaysNothingFollows(t *testing.T) {
	p := dexOf(t, 20)

	page, next := p.list("", "", 10)
	if len(page) != 10 {
		t.Fatalf("len(page) = %d, want 10", len(page))
	}
	if next == "" {
		t.Fatal("no cursor after the first of two full pages")
	}

	page, next = p.list("", next, 10)
	if len(page) != 10 {
		t.Errorf("len(page) = %d, want 10", len(page))
	}
	if next != "" {
		t.Errorf("cursor %q after the final full page: nothing follows it", next)
	}
}

func TestWalkingReachesEveryoneOnce(t *testing.T) {
	const n = 101
	p := dexOf(t, n)

	names, pages := walk(t, p, "", 7)

	if len(names) != n {
		t.Errorf("walked %d pokemon, want %d", len(names), n)
	}
	if pages < 2 {
		t.Fatalf("%d page(s) for %d pokemon at 7 a page: the pages are not being walked", pages, n)
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			t.Fatalf("%s appeared twice; the cursor went backwards or stalled", name)
		}
		seen[name] = true
	}
	for i := 1; i <= n; i++ {
		want := fmt.Sprintf("mon%03d", i)
		if !seen[want] {
			t.Errorf("%s never appeared", want)
		}
	}
}

// A cursor is a position in the filtered sequence, so the filter has to
// still apply on the other side of it.
func TestAFilteredWalkStaysFiltered(t *testing.T) {
	p := dexOf(t, 101)

	names, pages := walk(t, p, "water", 7)

	if pages < 2 {
		t.Fatalf("%d page(s) for the water walk: it is not crossing a page boundary", pages)
	}
	for _, name := range names {
		var id int
		if _, err := fmt.Sscanf(name, "mon%d", &id); err != nil {
			t.Fatalf("unreadable name %q", name)
		}
		if id%3 != 0 {
			t.Errorf("%s is not water, so the filter was dropped after the first page", name)
		}
	}
	if len(names) != 33 { // 3,6..99
		t.Errorf("walked %d water pokemon, want 33", len(names))
	}
}

// Nothing to validate: a cursor the dex has never issued selects the
// overlap rather than failing.
func TestAnUnknownCursorIsNotAnError(t *testing.T) {
	p := dexOf(t, 101)

	page, _ := p.list("", "99999", 10)
	if len(page) != 0 {
		t.Errorf("len(page) = %d past the end, want 0", len(page))
	}

	page, _ = p.list("", "not-a-cursor", 10)
	if len(page) != 10 {
		t.Errorf("len(page) = %d for an unreadable cursor, want the first page of 10", len(page))
	}
}

// mustListAll is the whole dex, for tests that are about something
// else. Limit 0 means no limit, so the cursor is always empty here.
func mustListAll(p *pokedex) []api.Pokemon {
	all, _ := p.list("", "", 0)
	return all
}
