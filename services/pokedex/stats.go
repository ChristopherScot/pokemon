package main

import "math"

// Stat stages are clamped to this range in every generation.
const (
	minStage = -6
	maxStage = 6
)

// stages tracks one combatant's stat modifications.
type stages struct {
	attack   int
	defense  int
	speed    int
	accuracy int
}

func statMultiplier(stage int) float64 {
	stage = clampStage(stage)
	if stage >= 0 {
		return float64(2+stage) / 2
	}
	return 2 / float64(2-stage)
}

func accuracyMultiplier(stage int) float64 {
	stage = clampStage(stage)
	if stage >= 0 {
		return float64(3+stage) / 3
	}
	return 3 / float64(3-stage)
}

func clampStage(s int) int {
	if s < minStage {
		return minStage
	}
	if s > maxStage {
		return maxStage
	}
	return s
}

func (s *stages) add(stat string, delta int) (applied int) {
	target := map[string]*int{
		"attack": &s.attack, "defense": &s.defense,
		"speed": &s.speed, "accuracy": &s.accuracy,
	}[stat]
	if target == nil {
		return 0
	}
	before := *target
	*target = clampStage(before + delta)
	return *target - before
}

type effect struct {
	// stat and delta describe a stat change; self decides whose.
	stat  string
	delta int
	self  bool

	accuracy int

	fixedDamage int

	// ohko faints the target outright when it lands.
	ohko bool

	// confuse and disable are the remaining two shapes.
	confuse bool
	disable bool
}

var statusMoves = map[string]effect{
	"swords-dance": {stat: "attack", delta: +2, self: true},
	"harden":       {stat: "defense", delta: +1, self: true},
	"iron-defense": {stat: "defense", delta: +2, self: true},

	"leer":        {stat: "defense", delta: -1, accuracy: 100},
	"tail-whip":   {stat: "defense", delta: -1, accuracy: 100},
	"string-shot": {stat: "speed", delta: -2, accuracy: 95},
	"sand-attack": {stat: "accuracy", delta: -1, accuracy: 100},

	"supersonic": {confuse: true, accuracy: 55},
	"disable":    {disable: true, accuracy: 100},

	// Fixed damage, ignoring types and stats entirely.
	"sonic-boom": {fixedDamage: 20, accuracy: 90},

	"guillotine": {ohko: true, accuracy: 30},
	"horn-drill": {ohko: true, accuracy: 30},

	"whirlwind": {},
}

func lands(acc int, accuracyStage int, roll float64) bool {
	if acc <= 0 {
		return true // never misses
	}
	chance := float64(acc) * accuracyMultiplier(accuracyStage)
	return roll*100 < math.Min(chance, 100)
}
