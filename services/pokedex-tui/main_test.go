package main

import (
	"errors"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func testModel(t *testing.T) model {
	t.Helper()
	c, err := api.NewClient("http://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return newModel(c, "http://127.0.0.1:0")
}

func loaded(t *testing.T) model {
	t.Helper()
	m, _ := testModel(t).Update(loadedMsg([]api.Pokemon{
		{ID: 1, Name: "bulbasaur", Types: []string{"grass", "poison"}, Height: 7, Weight: 69,
			Moves: []api.Move{{Name: "tackle", Type: "normal", Power: 40}}},
		{ID: 4, Name: "charmander", Types: []string{"fire"}, Height: 6, Weight: 85},
	}))
	return m.(model)
}

func TestLoadingStateRendersBeforeTheAPIAnswers(t *testing.T) {
	if content := testModel(t).View().Content; !strings.Contains(content, "loading") {
		t.Errorf("no loading state on the first frame:\n%s", content)
	}
}

func TestAPIErrorIsShownRatherThanFatal(t *testing.T) {
	m, _ := testModel(t).Update(errMsg{errors.New("connection refused")})

	content := m.(model).View().Content
	if !strings.Contains(content, "could not reach the API") {
		t.Errorf("error not surfaced:\n%s", content)
	}
	if !strings.Contains(content, "connection refused") {
		t.Errorf("underlying cause not shown:\n%s", content)
	}
}

func TestDetailPaneShowsTheSelection(t *testing.T) {
	content := loaded(t).View().Content

	for _, want := range []string{"bulbasaur", "grass", "poison", "0.7 m", "6.9 kg", "tackle"} {
		if !strings.Contains(content, want) {
			t.Errorf("detail pane missing %q:\n%s", want, content)
		}
	}
}

func TestFilterMatchesOnType(t *testing.T) {
	m := loaded(t)

	var found bool
	for _, it := range m.list.Items() {
		if i := it.(item); i.p.Name == "charmander" && strings.Contains(i.FilterValue(), "fire") {
			found = true
		}
	}
	if !found {
		t.Error("type is not part of the filter value, so searching a type finds nothing")
	}
}

func TestQuitKey(t *testing.T) {
	for _, tc := range []struct {
		name      string
		filtering bool
		wantQuit  bool
	}{
		{"idle", false, true},
		{"filtering", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := loaded(t)
			if tc.filtering {
				updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
				m = updated.(model)
				if m.list.FilterState() != list.Filtering {
					t.Fatal("filter did not open")
				}
			}

			_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})

			quit := false
			if cmd != nil {
				if _, ok := cmd().(tea.QuitMsg); ok {
					quit = true
				}
			}
			if quit != tc.wantQuit {
				t.Errorf("quit = %v, want %v", quit, tc.wantQuit)
			}
		})
	}
}

func TestListRendersWithinItsPane(t *testing.T) {
	m := loaded(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 28})
	m = u.(model)

	for i, line := range strings.Split(m.list.View(), "\n") {
		if w := lipgloss.Width(line); w > listWidth {
			t.Errorf("list line %d is %d wide, pane is %d: %q", i, w, listWidth, line)
		}
	}
}

func TestPanesFillTheWindowWidth(t *testing.T) {
	m := loaded(t)
	u, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 28})

	for i, line := range strings.Split(u.(model).View().Content, "\n") {
		if w := lipgloss.Width(line); w > 96 {
			t.Errorf("line %d is %d wide, window is 96", i, w)
		}
	}
}

func TestStatusForTellsTheTwoIdentityFailuresApart(t *testing.T) {
	never := statusFor(battleclient.ErrNoIdentity)
	stale := statusFor(battleclient.ErrStaleIdentity)

	if never == stale {
		t.Fatalf("both identity failures say the same thing: %q", never)
	}
	if strings.Contains(never, "no longer") {
		t.Errorf("a player who never registered was told %q", never)
	}
	if !strings.Contains(stale, "no longer") {
		t.Errorf("a stale token should say the server forgot them, got %q", stale)
	}
	// Both must still say what to do about it.
	for _, s := range []string{never, stale} {
		if !strings.Contains(s, "register") {
			t.Errorf("status %q does not say how to fix it", s)
		}
	}
}
