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

// entry mirrors pokedex.json. It is deliberately separate from
// api.Pokemon: the generated type is the API contract, and decoding
// straight into it would make a spec change a silent data-format change.
type entry struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Types  []string `json:"types"`
	Height int      `json:"height"`
	Weight int      `json:"weight"`
	Sprite string   `json:"sprite"`
	Moves  []struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Power int    `json:"power"`
	} `json:"moves"`
}

// pokedex is the loaded dataset, indexed for the two lookups the API
// makes: by name, and in Pokedex order.
type pokedex struct {
	ordered []api.Pokemon
	byName  map[string]api.Pokemon
}

// loadPokedex decodes the embedded dataset once at startup.
//
// It returns an error rather than panicking so main can report it and
// exit non-zero: a pod that crashloops with a clear message is easier to
// diagnose than one that panics inside an init function.
func loadPokedex() (*pokedex, error) {
	var raw []entry
	if err := json.Unmarshal(pokedexJSON, &raw); err != nil {
		return nil, fmt.Errorf("decoding embedded pokedex: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("embedded pokedex is empty")
	}

	p := &pokedex{
		ordered: make([]api.Pokemon, 0, len(raw)),
		byName:  make(map[string]api.Pokemon, len(raw)),
	}
	for _, e := range raw {
		moves := make([]api.Move, 0, len(e.Moves))
		for _, m := range e.Moves {
			moves = append(moves, api.Move{Name: m.Name, Type: m.Type, Power: m.Power})
		}
		mon := api.Pokemon{
			ID:     e.ID,
			Name:   e.Name,
			Types:  e.Types,
			Height: e.Height,
			Weight: e.Weight,
			Sprite: e.Sprite,
			Moves:  moves,
		}
		p.ordered = append(p.ordered, mon)
		p.byName[strings.ToLower(mon.Name)] = mon
	}
	sort.Slice(p.ordered, func(i, j int) bool { return p.ordered[i].ID < p.ordered[j].ID })
	return p, nil
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
		out = append(out, api.TypeSummary{Name: name, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}
