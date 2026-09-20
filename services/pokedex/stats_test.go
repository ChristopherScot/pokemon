package main

import (
	"math"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestStatMultiplierMatchesTheGames(t *testing.T) {
	for _, tc := range []struct {
		stage int
		want  float64
	}{
		{-6, 2.0 / 8}, {-4, 2.0 / 6}, {-2, 2.0 / 4}, {-1, 2.0 / 3},
		{0, 1},
		{+1, 3.0 / 2}, {+2, 2}, {+4, 3}, {+6, 4},
	} {
		if got := statMultiplier(tc.stage); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("statMultiplier(%+d) = %v, want %v", tc.stage, got, tc.want)
		}
	}
	// Beyond the cap is the cap, not more.
	if statMultiplier(99) != statMultiplier(6) {
		t.Error("stages are not clamped at +6")
	}
	if statMultiplier(-99) != statMultiplier(-6) {
		t.Error("stages are not clamped at -6")
	}
}

// Accuracy uses a different table from the other stats.
func TestAccuracyMultiplierUsesItsOwnTable(t *testing.T) {
	for _, tc := range []struct {
		stage int
		want  float64
	}{{-6, 3.0 / 9}, {-1, 3.0 / 4}, {0, 1}, {+1, 4.0 / 3}, {+6, 3}} {
		if got := accuracyMultiplier(tc.stage); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("accuracyMultiplier(%+d) = %v, want %v", tc.stage, got, tc.want)
		}
	}
	// And it is NOT the same table, or there would be no reason for two.
	if accuracyMultiplier(-1) == statMultiplier(-1) {
		t.Error("accuracy and the other stats share a table")
	}
}

func TestSwordsDanceIncreasesDamage(t *testing.T) {
	s := testService(t)
	mon, _ := s.dex.get("machamp")
	def, _ := s.dex.get("onix")

	var attack api.Move
	for _, m := range mon.Moves {
		if m.Power > 0 {
			attack = m
			break
		}
	}

	plain := &combatant{mon: mon, base: s.dex.stats[mon.ID], hp: 200, maxHP: 200, disabled: -1}
	buffed := &combatant{mon: mon, base: s.dex.stats[mon.ID], hp: 200, maxHP: 200, disabled: -1}
	buffed.stages.add("attack", +2)

	target := func() *combatant {
		return &combatant{mon: def, base: s.dex.stats[def.ID], hp: 300, maxHP: 300, disabled: -1}
	}

	var before, after int
	for seed := int64(1); seed <= 40; seed++ {
		a, _ := damage(plain, target(), attack, rngFor(seed))
		b, _ := damage(buffed, target(), attack, rngFor(seed))
		before += a
		after += b
	}
	if after <= before {
		t.Fatalf("swords-dance did not help: %d damage before, %d after", before, after)
	}
	// +2 stages is exactly 2x attack, so roughly double.
	if ratio := float64(after) / float64(before); ratio < 1.8 || ratio > 2.2 {
		t.Errorf("attack ratio %.2f, want about 2x for +2 stages", ratio)
	}
}

// harden raises defense, so the same attack hurts less.
func TestHardenReducesIncomingDamage(t *testing.T) {
	s := testService(t)
	att, _ := s.dex.get("charizard")
	def, _ := s.dex.get("bulbasaur")

	var move api.Move
	for _, m := range att.Moves {
		if m.Power > 0 {
			move = m
			break
		}
	}
	attacker := &combatant{mon: att, base: s.dex.stats[att.ID], hp: 200, maxHP: 200, disabled: -1}

	var soft, hard int
	for seed := int64(1); seed <= 40; seed++ {
		plain := &combatant{mon: def, base: s.dex.stats[def.ID], hp: 300, maxHP: 300, disabled: -1}
		tough := &combatant{mon: def, base: s.dex.stats[def.ID], hp: 300, maxHP: 300, disabled: -1}
		tough.stages.add("defense", +1)

		a, _ := damage(attacker, plain, move, rngFor(seed))
		b, _ := damage(attacker, tough, move, rngFor(seed))
		soft += a
		hard += b
	}
	if hard >= soft {
		t.Errorf("harden did not reduce damage: %d without, %d with", soft, hard)
	}
}

func TestAttackStatChangesDamage(t *testing.T) {
	s := testService(t)
	strong, _ := s.dex.get("machamp")
	weak, _ := s.dex.get("gengar")
	def, _ := s.dex.get("onix")

	move := api.Move{Name: "test", Type: "normal", Power: 80}
	target := func() *combatant {
		return &combatant{mon: def, base: s.dex.stats[def.ID], hp: 300, maxHP: 300, disabled: -1}
	}

	var big, small int
	for seed := int64(1); seed <= 30; seed++ {
		a, _ := damage(&combatant{mon: strong, base: s.dex.stats[strong.ID], disabled: -1}, target(), move, rngFor(seed))
		b, _ := damage(&combatant{mon: weak, base: s.dex.stats[weak.ID], disabled: -1}, target(), move, rngFor(seed))
		big += a
		small += b
	}
	if big <= small {
		t.Errorf("machamp (130 atk) did %d, gengar (65 atk) did %d", big, small)
	}
}

func TestEveryStatusMoveInTheDatasetIsImplemented(t *testing.T) {
	s := testService(t)
	seen := map[string]bool{}
	for _, mon := range s.dex.list("", 0) {
		for _, m := range mon.Moves {
			if m.Power == 0 {
				seen[m.Name] = true
			}
		}
	}
	for name := range seen {
		if _, ok := statusMoves[name]; !ok {
			t.Errorf("%s is in the dataset but does nothing", name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("no status moves found; the test is not exercising anything")
	}
	t.Logf("%d status moves, all implemented", len(seen))
}

// A stat already at its cap says so rather than silently wasting a turn.
func TestStatAtTheCapReportsIt(t *testing.T) {
	s := testService(t)
	b, ash, _ := activeBattle(t, s)

	me := b.sides[0].team[0]
	me.stages.attack = maxStage

	// Find a swords-dance on this team, or skip.
	var idx = -1
	for i, m := range me.mon.Moves {
		if m.Name == "swords-dance" {
			idx = i
		}
	}
	if idx < 0 {
		t.Skip("no swords-dance on this team")
	}

	before := len(b.log)
	if err := b.takeTurn(ash, 0, idx, 0, s.rng); err != nil {
		t.Fatal(err)
	}
	var said string
	for _, e := range b.log[before:] {
		said += e.Text
	}
	if !strings.Contains(said, "cannot go any higher") {
		t.Errorf("a capped stat did not say so: %q", said)
	}
}

// sonic-boom always does exactly 20, ignoring types and stats.
func TestSonicBoomDealsExactlyTwenty(t *testing.T) {
	s := testService(t)
	b, ash, _ := activeBattle(t, s)

	// Give the attacker the move directly, since the team is random.
	me := b.sides[0].team[0]
	me.mon.Moves = []api.Move{{Name: "sonic-boom", Type: "normal", Power: 0}}
	target := b.sides[1].team[0]
	target.hp = 100

	// 90% accuracy, so try until it lands rather than depending on one roll.
	for i := 0; i < 20 && target.hp == 100; i++ {
		b.turn = 0
		_ = b.takeTurn(ash, 0, 0, 0, s.rng)
	}
	if target.hp != 80 {
		t.Errorf("sonic-boom left %d hp, want exactly 80", target.hp)
	}
}
