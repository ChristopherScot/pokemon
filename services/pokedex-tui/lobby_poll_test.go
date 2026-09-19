package main

import (
	"testing"

	"errors"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// The lobby used to load once and never change: a battle opened by
// somebody else after you arrived never appeared, so you sat on
// "nobody is waiting" while an opponent sat on theirs.
//
// Any arrival at the lobby must start the loop, and every tick must
// re-arm it - otherwise one result stops the refresh for good.
func TestLobbyKeepsPollingWhileItIsOnScreen(t *testing.T) {
	m := model{screen: screenLobby, bc: &battleclient.Client{}}
	_, cmd := m.Update(lobbyMsg{list: &api.WaitingList{}, polled: true})
	if cmd == nil {
		t.Fatal("a lobby refresh on the lobby screen did not re-arm the poll")
	}
}

// A tick that keeps firing during a battle spends requests on a list
// nobody is looking at.
func TestLobbyStopsPollingOffScreen(t *testing.T) {
	m := model{screen: screenBattle, bc: &battleclient.Client{}}
	_, cmd := m.Update(lobbyMsg{list: &api.WaitingList{}, polled: true})
	if cmd != nil {
		t.Error("the lobby kept polling while a battle was on screen")
	}
}

// A background refresh the user did not ask for must not overwrite what
// they were last told.
func TestAPolledFailureLeavesTheStatusAlone(t *testing.T) {
	m := model{screen: screenLobby, bc: &battleclient.Client{}, status: "important"}
	got, _ := m.Update(lobbyMsg{err: errTest, polled: true})
	if got.(model).status != "important" {
		t.Errorf("status = %q, want it untouched by a background failure", got.(model).status)
	}
}

// An explicit refresh that fails SHOULD say so - the user asked.
func TestAnAskedForFailureIsReported(t *testing.T) {
	m := model{screen: screenLobby, bc: &battleclient.Client{}, status: "important"}
	got, _ := m.Update(lobbyMsg{err: errTest})
	if got.(model).status == "important" {
		t.Error("an explicit refresh failure was swallowed")
	}
}

var errTest = errors.New("boom")
