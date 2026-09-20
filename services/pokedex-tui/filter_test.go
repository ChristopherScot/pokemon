package main

import (
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestTheTeamPickerUpdatesTheListItRenders(t *testing.T) {
	for _, s := range []screen{screenBrowse, screenTeam} {
		m := model{screen: s}
		if !m.screenUsesList() {
			t.Errorf("screen %v renders the list but is not routed its messages", s)
		}
	}
}

func TestScreensWithoutTheListAreNotRoutedToIt(t *testing.T) {
	for _, s := range []screen{screenLobby, screenBattle} {
		m := model{screen: s}
		if m.screenUsesList() {
			t.Errorf("screen %v does not render the list but is routed its messages", s)
		}
	}
}

func TestEscapeReachesTheListFilterOnThePicker(t *testing.T) {
	m := newModel(nil, "http://example.invalid")
	m.screen = screenTeam
	// Items, so the list has something to filter.
	m.list.SetItems([]list.Item{
		item{p: api.Pokemon{Name: "bulbasaur"}},
		item{p: api.Pokemon{Name: "charizard"}},
	})
	m.list.SetSize(80, 20)

	// Open the filter the way a user does.
	got, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = got.(model)
	if m.list.FilterState() != list.Filtering {
		t.Skip("this list has filtering disabled; nothing to trap")
	}

	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m2 := got.(model); m2.list.FilterState() == list.Filtering {
		t.Error("escape did not close the filter; the picker still eats every key")
	}
}
