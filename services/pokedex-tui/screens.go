package main

// Screens. The model carries one `screen` field and Update/View dispatch
// on it, which is the pattern the upstream `views` example uses: one
// model, per-screen handlers, no nested Programs.
//
// Three screens: browsing the Pokedex (where this started), the lobby of
// open battles, and a battle itself.

type screen int

const (
	screenBrowse screen = iota
	screenLobby
	screenTeam
	screenBattle
)

func (s screen) String() string {
	switch s {
	case screenLobby:
		return "lobby"
	case screenTeam:
		return "team"
	case screenBattle:
		return "battle"
	default:
		return "browse"
	}
}
