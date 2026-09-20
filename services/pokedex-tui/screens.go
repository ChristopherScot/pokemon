package main

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
