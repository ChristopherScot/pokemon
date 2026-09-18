package main

// Stat stages, and the moves that change them.
//
// Every stat-changing move in the dataset works through this: a stage
// between -6 and +6 per stat per Pokemon, reset when the battle ends.
// The multipliers are the Generation III+ ones rather than anything
// invented, because a player who knows the games should be able to
// predict what swords-dance does.

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

// statMultiplier is the Gen III+ formula for attack, defense and speed:
//
//	stage >= 0:  (2 + stage) / 2
//	stage <  0:  2 / (2 - stage)
//
// So +1 is 1.5x, +2 doubles, +6 quadruples, and -6 quarters.
func statMultiplier(stage int) float64 {
	stage = clampStage(stage)
	if stage >= 0 {
		return float64(2+stage) / 2
	}
	return 2 / float64(2-stage)
}

// accuracyMultiplier uses a different table from the other stats - the
// 3/(3+n) shape - because accuracy swings hard enough at the same
// fractions to make a move useless.
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

// add applies a change and reports what actually happened, because a
// stat already at +6 cannot go higher and the player should be told
// that rather than watching a turn vanish.
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

// effect is what a status move does. Kept as data rather than a switch
// in the middle of turn resolution, so adding a move is a table entry.
type effect struct {
	// stat and delta describe a stat change; self decides whose.
	stat  string
	delta int
	self  bool

	// accuracy is the move's chance to land, 0-100. Zero means it never
	// misses, which is how the games treat swords-dance and harden.
	accuracy int

	// fixedDamage is dealt instead of the damage formula, for moves like
	// sonic-boom that always do exactly 20.
	fixedDamage int

	// ohko faints the target outright when it lands.
	ohko bool

	// confuse and disable are the remaining two shapes.
	confuse bool
	disable bool
}

// statusMoves is every power-0 move in the dataset, with what it does.
//
// Sourced from the games rather than invented: a player who knows
// swords-dance should find it does what they expect. A move that is not
// here deals no damage and says so, which is the old behaviour and is
// at least honest.
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

	// One-hit KOs: rare on purpose, at 30% they are a gamble rather
	// than a strategy.
	"guillotine": {ohko: true, accuracy: 30},
	"horn-drill": {ohko: true, accuracy: 30},

	// whirlwind ends a wild battle or forces a switch. With no
	// switching and no wild battles, it does nothing here - and saying
	// so beats pretending otherwise.
	"whirlwind": {},
}

// lands rolls a move's accuracy against the attacker's accuracy stage.
//
// Gen III subtracts the target's evasion from the attacker's accuracy
// rather than stacking two multipliers; there is no evasion-raising
// move in this dataset, so only the accuracy stage matters.
func lands(acc int, accuracyStage int, roll float64) bool {
	if acc <= 0 {
		return true // never misses
	}
	chance := float64(acc) * accuracyMultiplier(accuracyStage)
	return roll*100 < math.Min(chance, 100)
}
