package main

// The Pokedex itself: 100 Pokemon, embedded rather than fetched.
//
// The data comes from pokeapi.co, but this service does not call it at
// runtime. A read-only dataset that never changes is a file, not a
// dependency - embedding it means no network call on the request path,
// no upstream rate limit, no third-party outage in our error budget, and
// a container that behaves identically in CI and in the cluster.
//
// Refreshing it is a deliberate act: re-run the fetch, commit the new
// pokedex.json, and the change is visible in review as data rather than
// appearing silently in production.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

//go:embed pokedex.json
var pokedexJSON []byte

// dataset mirrors pokedex.json's top level.
//
// Moves live in one catalogue rather than inlined per Pokemon: tackle
// is known by dozens of them, and a copy of its description in each
// would be dozens of places for the text to drift.
type dataset struct {
	Pokemon []entry     `json:"pokemon"`
	Moves   []moveEntry `json:"moves"`
}

// moveEntry mirrors one record in that catalogue.
type moveEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Power       int    `json:"power"`
	Description string `json:"description"`
	Effect      string `json:"effect"`
	// Nil for a move that cannot miss, which is not the same as 100.
	Accuracy    *int   `json:"accuracy"`
	PP          int    `json:"pp"`
	DamageClass string `json:"damageClass"`
}

// entry mirrors pokedex.json. It is deliberately separate from
// api.Pokemon: the generated type is the API contract, and decoding
// straight into it would make a spec change a silent data-format change.
type entry struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Types  []string `json:"types"`
	Height int      `json:"height"`
	Weight int      `json:"weight"`
	// BaseHp is the games' base HP stat, which the battle formula needs.
	// It is data rather than anything derivable: weight correlates with
	// it at 0.33 and inverts for the cases players notice - Onix
	// outweighs Jigglypuff 38x and has a third the HP.
	BaseHp int `json:"baseHp"`
	// The rest of the battle stats, same source and same reason: a
	// damage formula needs attack and defense, and stat-changing moves
	// need something to change.
	BaseAttack  int `json:"baseAttack"`
	BaseDefense int `json:"baseDefense"`
	BaseSpeed   int `json:"baseSpeed"`
	// The Pokedex entry and the games' one-line label, e.g. "Seed
	// Pokemon". Descriptive only; nothing in a battle reads them.
	Description string `json:"description"`
	Genus       string `json:"genus"`
	Habitat     string `json:"habitat"`
	Sprite      string `json:"sprite"`
	Artwork     string `json:"artwork"`
	EvolvesFrom string `json:"evolvesFrom"`
	Legendary   bool   `json:"legendary"`
	// Move names, resolved against the catalogue. The six it brings to
	// a battle.
	Moves []string `json:"moves"`
	// Everything it can learn - 86 for Bulbasaur - served by its own
	// endpoint rather than on every Pokemon in a list response.
	LearnableMoves []string `json:"learnableMoves"`
}

// pokedex is the loaded dataset, indexed for the two lookups the API
// makes: by name, and in Pokedex order.
type pokedex struct {
	ordered []api.Pokemon
	byName  map[string]api.Pokemon

	// The move catalogue: every move any Pokemon here can learn, in
	// name order, plus an index for single lookups.
	moves      []api.Move
	moveByName map[string]api.Move
	// Learnable move names per Pokemon, resolved on demand rather than
	// held as []api.Move per Pokemon - 100 Pokemon averaging 100 moves
	// would be 10,000 copies of records the catalogue already holds.
	learnable map[string][]string

	// Battle stats by dex number. Not on api.Pokemon: a client is shown
	// the HP a battle computes from these, not the inputs.
	stats map[int]baseStats
}

// loadPokedex decodes the embedded dataset once at startup.
//
// It returns an error rather than panicking so main can report it and
// exit non-zero: a pod that crashloops with a clear message is easier to
// diagnose than one that panics inside an init function.
func loadPokedex() (*pokedex, error) {
	var doc dataset
	if err := json.Unmarshal(pokedexJSON, &doc); err != nil {
		return nil, fmt.Errorf("decoding embedded pokedex: %w", err)
	}
	raw := doc.Pokemon
	if len(raw) == 0 {
		return nil, fmt.Errorf("embedded pokedex is empty")
	}
	if len(doc.Moves) == 0 {
		return nil, fmt.Errorf("embedded pokedex has no move catalogue")
	}

	p := &pokedex{
		ordered: make([]api.Pokemon, 0, len(raw)),
		byName:  make(map[string]api.Pokemon, len(raw)),
		// Deliberately not on api.Pokemon: a client is shown maxHp on a
		// BattlePokemon, which is what this feeds. Putting the base stat
		// in the API would expose an input to a calculation the server
		// owns, and invite a client to redo it differently.
		stats: make(map[int]baseStats, len(raw)),

		moves:      make([]api.Move, 0, len(doc.Moves)),
		moveByName: make(map[string]api.Move, len(doc.Moves)),
		learnable:  make(map[string][]string, len(raw)),
	}

	// The catalogue first: a Pokemon's moves are names, and resolving
	// them needs this populated.
	for _, m := range doc.Moves {
		mv := api.Move{
			Name:        m.Name,
			Type:        m.Type,
			Power:       m.Power,
			Description: m.Description,
			Effect:      m.Effect,
			Pp:          m.PP,
			DamageClass: m.DamageClass,
		}
		if m.Accuracy != nil {
			mv.Accuracy = api.NewOptInt(*m.Accuracy)
		}
		p.moves = append(p.moves, mv)
		p.moveByName[m.Name] = mv
	}
	for _, e := range raw {
		// A missing baseHp decodes to 0, which would give every Pokemon
		// a flat level+10 HP and make every battle identical - a failure
		// that is invisible until someone notices the numbers never
		// differ. Refuse to start instead.
		if e.BaseHp <= 0 || e.BaseAttack <= 0 || e.BaseDefense <= 0 || e.BaseSpeed <= 0 {
			return nil, fmt.Errorf("pokedex entry #%d %q is missing a base stat", e.ID, e.Name)
		}
		p.stats[e.ID] = baseStats{
			hp:      e.BaseHp,
			attack:  e.BaseAttack,
			defense: e.BaseDefense,
			speed:   e.BaseSpeed,
		}

		// A name with no catalogue entry means the data was built by
		// something that did not keep the two in step. Refusing to
		// start beats serving a Pokemon whose moves are blank.
		moves := make([]api.Move, 0, len(e.Moves))
		for _, name := range e.Moves {
			mv, ok := p.moveByName[name]
			if !ok {
				return nil, fmt.Errorf("pokedex entry %q lists move %q, which is not in the catalogue", e.Name, name)
			}
			moves = append(moves, mv)
		}
		p.learnable[strings.ToLower(e.Name)] = e.LearnableMoves

		mon := api.Pokemon{
			ID:          e.ID,
			Name:        e.Name,
			Description: e.Description,
			Genus:       e.Genus,
			Types:       e.Types,
			Height:      e.Height,
			Weight:      e.Weight,
			Sprite:      e.Sprite,
			Moves:       moves,
		}
		// Optional fields: empty means the upstream Pokedex has none,
		// and an absent key reads better than an empty string.
		if e.Habitat != "" {
			mon.Habitat = api.NewOptString(e.Habitat)
		}
		if e.Artwork != "" {
			mon.Artwork = api.NewOptString(e.Artwork)
		}
		if e.EvolvesFrom != "" {
			mon.EvolvesFrom = api.NewOptString(e.EvolvesFrom)
		}
		if e.Legendary {
			mon.Legendary = api.NewOptBool(true)
		}
		p.ordered = append(p.ordered, mon)
		p.byName[strings.ToLower(mon.Name)] = mon
	}
	sort.Slice(p.ordered, func(i, j int) bool { return p.ordered[i].ID < p.ordered[j].ID })
	return p, nil
}

// allMoves returns the whole catalogue, in name order.
func (p *pokedex) allMoves() []api.Move { return p.moves }

// move looks up one move by its hyphenated name.
func (p *pokedex) move(name string) (api.Move, bool) {
	mv, ok := p.moveByName[strings.ToLower(strings.TrimSpace(name))]
	return mv, ok
}

// learnableFor resolves a Pokemon's full learnable set against the
// catalogue, in the order the upstream Pokedex lists them.
//
// The second return distinguishes "no such Pokemon" from "a Pokemon
// that learns nothing", which the handler needs to choose between 404
// and an empty list.
func (p *pokedex) learnableFor(name string) ([]api.Move, bool) {
	names, ok := p.learnable[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return nil, false
	}
	out := make([]api.Move, 0, len(names))
	for _, n := range names {
		if mv, ok := p.moveByName[n]; ok {
			out = append(out, mv)
		}
	}
	return out, true
}

// list returns Pokemon in Pokedex order, optionally filtered by type and
// truncated to limit. An unknown type matches nothing, which is an empty
// list rather than an error: "no fire Pokemon here" is a valid answer.
func (p *pokedex) list(typ string, limit int) []api.Pokemon {
	out := make([]api.Pokemon, 0, len(p.ordered))
	typ = strings.ToLower(strings.TrimSpace(typ))
	for _, mon := range p.ordered {
		if typ != "" && !hasType(mon, typ) {
			continue
		}
		out = append(out, mon)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func hasType(mon api.Pokemon, typ string) bool {
	for _, t := range mon.Types {
		if strings.EqualFold(t, typ) {
			return true
		}
	}
	return false
}

// get looks a Pokemon up by name, case-insensitively. The bool is the
// 404: a missing name is an ordinary outcome, not a failure.
func (p *pokedex) get(name string) (api.Pokemon, bool) {
	mon, ok := p.byName[strings.ToLower(strings.TrimSpace(name))]
	return mon, ok
}

// types counts how many Pokemon carry each type, sorted by frequency then
// name so the order is stable across restarts - an endpoint that returns
// the same data in a different order every call is a bad citizen for
// caches and for anything diffing the response.
func (p *pokedex) types() []api.TypeSummary {
	counts := map[string]int{}
	for _, mon := range p.ordered {
		for _, t := range mon.Types {
			counts[t]++
		}
	}
	out := make([]api.TypeSummary, 0, len(counts))
	for name, n := range counts {
		// The matchup chart travels with the type, so a client can show
		// "super effective against..." without carrying its own copy of
		// a table the server computes damage from.
		strong, weak, none := matchups(name)
		out = append(out, api.TypeSummary{
			Name:            name,
			Count:           n,
			StrongAgainst:   strong,
			WeakAgainst:     weak,
			NoEffectAgainst: none,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// baseStats are the game's base stats for one Pokemon, which the battle
// engine turns into HP, damage and turn order.
type baseStats struct {
	hp, attack, defense, speed int
}
