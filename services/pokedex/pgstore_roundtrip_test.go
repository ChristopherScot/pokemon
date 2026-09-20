package main

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// A battle in postgres is stored as JSON, so anything reachable from
// battleState that encoding/json cannot see is silently dropped: it
// marshals as {} and comes back zeroed.
//
// That is what made every move deal exactly 1 damage. baseStats and
// stages both had unexported fields, so a reloaded combatant fought with
// attack and defense of ZERO, the damage formula divided 0 by 0, and
// every roll landed on the `if d < 1 { d = 1 }` floor. A battle took
// ~140 turns instead of ~4, in every client.
//
// This walks the whole persisted shape rather than naming the two
// structs that were wrong, so the next struct added to it is covered
// without anyone remembering to come back here.
func TestEveryPersistedFieldCanRoundTrip(t *testing.T) {
	// Types the encoder handles itself; do not walk into them.
	opaque := map[reflect.Type]bool{
		reflect.TypeOf(api.Pokemon{}):     true,
		reflect.TypeOf(api.BattleEvent{}): true,
	}

	var walk func(t *testing.T, typ reflect.Type, path string, seen map[reflect.Type]bool)
	walk = func(t *testing.T, typ reflect.Type, path string, seen map[reflect.Type]bool) {
		for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Map {
			walk(t, typ.Elem(), path+"[]", seen)
			return
		}
		if typ.Kind() != reflect.Struct || seen[typ] || opaque[typ] {
			return
		}
		seen[typ] = true

		// A struct that marshals itself is its own business.
		if typ.Implements(reflect.TypeOf((*interface{ MarshalJSON() ([]byte, error) })(nil)).Elem()) ||
			reflect.PtrTo(typ).Implements(reflect.TypeOf((*interface{ MarshalJSON() ([]byte, error) })(nil)).Elem()) {
			return
		}

		exported := 0
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.IsExported() {
				exported++
				if f.Tag.Get("json") != "-" {
					walk(t, f.Type, path+"."+f.Name, seen)
				}
				continue
			}
			t.Errorf("%s.%s is unexported, so postgres stores it as nothing "+
				"and it comes back zeroed (%s)", path, f.Name, typ.String())
		}
		if typ.NumField() > 0 && exported == 0 {
			t.Errorf("%s (%s) has no exported fields: it persists as {}",
				path, typ.String())
		}
	}

	walk(t, reflect.TypeOf(battleState{}), "battleState", map[reflect.Type]bool{})
	if t.Failed() {
		t.Log("fix: give the struct exported fields, or a MarshalJSON/UnmarshalJSON pair")
	}
}

// The symptom the class test above exists to prevent, pinned end to end:
// a battle that has been through postgres must still hit as hard as one
// that has not.
func TestDamageSurvivesPersistence(t *testing.T) {
	dex, err := loadPokedex()
	if err != nil {
		t.Fatal(err)
	}
	mine, err := newCombatants(dex, []string{"pikachu", "onix", "gengar"})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := newCombatants(dex, []string{"charizard", "blastoise", "venusaur"})
	if err != nil {
		t.Fatal(err)
	}

	b := &battle{
		id:     "persist",
		status: "active",
		sides: []*side{
			{trainer: "ash", token: "a", team: mine},
			{trainer: "misty", token: "m", team: theirs},
		},
	}

	raw, err := json.Marshal(toState(b))
	if err != nil {
		t.Fatal(err)
	}
	// fromRow is the real load path, so this is exactly what a pod does
	// when it picks a battle back up out of postgres.
	now := time.Now()
	reloaded, err := fromRow(b.id, b.status, 1, 0, 1, "", now, now, raw)
	if err != nil {
		t.Fatal(err)
	}

	move := mine[0].mon.Moves[0]
	fresh, _ := damage(mine[0], theirs[0], move, fixedRoll{})
	after, _ := damage(reloaded.sides[0].team[0], reloaded.sides[1].team[0], move, fixedRoll{})

	if after != fresh {
		t.Errorf("%s dealt %d before postgres and %d after", move.Name, fresh, after)
	}
	if after <= 1 {
		t.Errorf("%s (power %d) dealt %d damage - the 1-damage floor is back",
			move.Name, move.Power, after)
	}
}

type fixedRoll struct{}

func (fixedRoll) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return 0
}
func (fixedRoll) Float64() float64 { return 0.5 }
