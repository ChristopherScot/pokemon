package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// battleState is persisted as JSON, so anything encoding/json cannot
// see gets silently dropped and reloaded as zero. Walks the whole
// persisted shape so new fields are covered automatically.
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

		// A struct with its own MarshalJSON is opaque.
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

// End-to-end pin of the round-trip: a persisted battle must hit as
// hard as a fresh one.
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
		status: api.BattleStatusActive,
		sides: []*side{
			{trainer: "ash", token: "a", team: mine},
			{trainer: "misty", token: "m", team: theirs},
		},
	}

	raw, err := json.Marshal(toState(b))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	reloaded, err := fromRow(b.id, string(b.status), 1, 0, 1, "", now, now, raw)
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

// Round-trip walk above proves everything IN battleState survives,
// but a field only on `combatant` and not on `combatantState` compiles
// clean and silently resets every turn under Postgres. Compare the two
// shapes directly to catch that class.
func TestEveryEngineFieldHasSomewhereToBePersisted(t *testing.T) {
	// Deliberately-partial pairs list themselves in `except` with why.
	pairs := []struct {
		live, stored any
		except       map[string]string
	}{
		{live: combatant{}, stored: combatantState{}},
		{live: side{}, stored: sideState{}},
	}

	for _, p := range pairs {
		lt := reflect.TypeOf(p.live)
		st := reflect.TypeOf(p.stored)

		have := map[string]bool{}
		for i := 0; i < st.NumField(); i++ {
			have[strings.ToLower(st.Field(i).Name)] = true
		}

		for i := 0; i < lt.NumField(); i++ {
			f := lt.Field(i)
			name := strings.ToLower(f.Name)
			if why, ok := p.except[f.Name]; ok {
				t.Logf("%s.%s is deliberately not persisted: %s", lt.Name(), f.Name, why)
				continue
			}
			if !have[name] {
				t.Errorf("%s.%s has no field in %s, so it is dropped on every write: "+
					"it will work under memStore and silently reset every turn against "+
					"postgres. Add it to %s, or list it as a deliberate exception.",
					lt.Name(), f.Name, st.Name(), st.Name())
			}
		}
	}
}
