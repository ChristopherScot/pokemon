package main

import "sort"

// The type effectiveness chart, and the damage it implies.
//
// Kept here rather than in each client: the server decides damage, so a
// client carrying its own copy can only ever agree or be wrong. /types
// serves this to whoever wants to show a matchup hint.

// effectiveness maps attacking type -> defending type -> multiplier.
// Anything absent is 1. Values follow the modern Pokemon chart.
var effectiveness = map[string]map[string]float64{
	"normal":   {"rock": 0.5, "ghost": 0, "steel": 0.5},
	"fire":     {"fire": 0.5, "water": 0.5, "grass": 2, "ice": 2, "bug": 2, "rock": 0.5, "dragon": 0.5, "steel": 2},
	"water":    {"fire": 2, "water": 0.5, "grass": 0.5, "ground": 2, "rock": 2, "dragon": 0.5},
	"electric": {"water": 2, "electric": 0.5, "grass": 0.5, "ground": 0, "flying": 2, "dragon": 0.5},
	"grass":    {"fire": 0.5, "water": 2, "grass": 0.5, "poison": 0.5, "ground": 2, "flying": 0.5, "bug": 0.5, "rock": 2, "dragon": 0.5, "steel": 0.5},
	"ice":      {"fire": 0.5, "water": 0.5, "grass": 2, "ice": 0.5, "ground": 2, "flying": 2, "dragon": 2, "steel": 0.5},
	"fighting": {"normal": 2, "ice": 2, "poison": 0.5, "flying": 0.5, "psychic": 0.5, "bug": 0.5, "rock": 2, "ghost": 0, "dark": 2, "steel": 2, "fairy": 0.5},
	"poison":   {"grass": 2, "poison": 0.5, "ground": 0.5, "rock": 0.5, "ghost": 0.5, "steel": 0, "fairy": 2},
	"ground":   {"fire": 2, "electric": 2, "grass": 0.5, "poison": 2, "flying": 0, "bug": 0.5, "rock": 2, "steel": 2},
	"flying":   {"electric": 0.5, "grass": 2, "fighting": 2, "bug": 2, "rock": 0.5, "steel": 0.5},
	"psychic":  {"fighting": 2, "poison": 2, "psychic": 0.5, "dark": 0, "steel": 0.5},
	"bug":      {"fire": 0.5, "grass": 2, "fighting": 0.5, "poison": 0.5, "flying": 0.5, "psychic": 2, "ghost": 0.5, "dark": 2, "steel": 0.5, "fairy": 0.5},
	"rock":     {"fire": 2, "ice": 2, "fighting": 0.5, "ground": 0.5, "flying": 2, "bug": 2, "steel": 0.5},
	"ghost":    {"normal": 0, "psychic": 2, "ghost": 2, "dark": 0.5},
	"dragon":   {"dragon": 2, "steel": 0.5, "fairy": 0},
	"dark":     {"fighting": 0.5, "psychic": 2, "ghost": 2, "dark": 0.5, "fairy": 0.5},
	"steel":    {"fire": 0.5, "water": 0.5, "electric": 0.5, "ice": 2, "rock": 2, "steel": 0.5, "fairy": 2},
	"fairy":    {"fire": 0.5, "fighting": 2, "poison": 0.5, "dragon": 2, "dark": 2, "steel": 0.5},
}

// multiplier is how much damage moveType does to a Pokemon with these
// types. Dual types multiply, so water/flying takes 4x from electric.
func multiplier(moveType string, defenderTypes []string) float64 {
	row, ok := effectiveness[moveType]
	if !ok {
		return 1
	}
	m := 1.0
	for _, t := range defenderTypes {
		if v, ok := row[t]; ok {
			m *= v
		}
	}
	return m
}

// describeEffect is the wording every client shows, so a battle reads
// the same in a browser, a terminal and a TUI.
func describeEffect(m float64) string {
	switch {
	case m == 0:
		return "It has no effect"
	case m >= 4:
		return "It's devastatingly effective"
	case m > 1:
		return "It's super effective"
	case m == 1:
		return ""
	case m > 0 && m <= 0.25:
		return "It barely scratches"
	default:
		return "It's not very effective"
	}
}

// matchups splits one attacking type's row into the three buckets a
// client wants to display. Sorted so the output is stable across calls.
func matchups(attacking string) (strong, weak, none []string) {
	for def, m := range effectiveness[attacking] {
		switch {
		case m == 0:
			none = append(none, def)
		case m > 1:
			strong = append(strong, def)
		case m < 1:
			weak = append(weak, def)
		}
	}
	sort.Strings(strong)
	sort.Strings(weak)
	sort.Strings(none)
	return strong, weak, none
}
