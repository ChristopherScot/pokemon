package main

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

type entry struct {
	ID                 int      `json:"id"`
	Name               string   `json:"name"`
	Types              []string `json:"types"`
	Height             int      `json:"height"`
	Weight             int      `json:"weight"`
	BaseHp             int      `json:"baseHp"`
	BaseAttack         int      `json:"baseAttack"`
	BaseDefense        int      `json:"baseDefense"`
	BaseSpeed          int      `json:"baseSpeed"`
	BaseSpecialAttack  int      `json:"baseSpecialAttack"`
	BaseSpecialDefense int      `json:"baseSpecialDefense"`
	Description        string   `json:"description"`
	Genus              string   `json:"genus"`
	Habitat            string   `json:"habitat"`
	Sprite             string   `json:"sprite"`
	Artwork            string   `json:"artwork"`
	EvolvesFrom        string   `json:"evolvesFrom"`
	Legendary          bool     `json:"legendary"`
	Moves              []string `json:"moves"`
	LearnableMoves     []string `json:"learnableMoves"`
}

type pokedex struct {
	ordered []api.Pokemon
	byName  map[string]api.Pokemon

	moves      []api.Move
	moveByName map[string]api.Move
	learnable  map[string][]string

	stats map[int]baseStats
}

// loadPokedex builds the index from the JSON compiled into the
// binary, for a run with no database.
func loadPokedex() (*pokedex, error) {
	var doc dataset
	if err := json.Unmarshal(pokedexJSON, &doc); err != nil {
		return nil, fmt.Errorf("decoding embedded pokedex: %w", err)
	}
	if len(doc.Pokemon) == 0 {
		return nil, fmt.Errorf("embedded pokedex is empty")
	}
	if len(doc.Moves) == 0 {
		return nil, fmt.Errorf("embedded pokedex has no move catalogue")
	}
	return buildPokedex(doc.Pokemon, doc.Moves)
}

// buildPokedex turns rows into the index, and is the ONLY place that
// knows how.
//
// There used to be two of these - one over embedded JSON, one over
// sqlc rows - about ninety duplicated lines apiece. The sources
// genuinely differ, but only in how a row is obtained: int vs int32,
// moves inline vs joined through a link table. Everything after that
// was the same, so adding a field to api.Pokemon meant the same edit
// in two files.
//
// Worse, which one ran was decided by DATABASE_URL at startup, so
// local development exercised one path and production the other, and
// a divergence between them was invisible until it shipped.
func buildPokedex(mons []entry, moves []moveEntry) (*pokedex, error) {
	p := &pokedex{
		ordered: make([]api.Pokemon, 0, len(mons)),
		byName:  make(map[string]api.Pokemon, len(mons)),
		stats:   make(map[int]baseStats, len(mons)),

		moves:      make([]api.Move, 0, len(moves)),
		moveByName: make(map[string]api.Move, len(moves)),
		learnable:  make(map[string][]string, len(mons)),
	}

	for _, m := range moves {
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

	for _, e := range mons {
		p.stats[e.ID] = baseStats{
			hp:      e.BaseHp,
			attack:  e.BaseAttack,
			defense: e.BaseDefense,
			speed:   e.BaseSpeed,
		}

		battle := make([]api.Move, 0, len(e.Moves))
		for _, name := range e.Moves {
			mv, ok := p.moveByName[name]
			if !ok {
				return nil, fmt.Errorf("pokemon %q lists move %q, which is not in the catalogue", e.Name, name)
			}
			battle = append(battle, mv)
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
			Moves:       battle,
		}
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
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *pokedex) finish() error {
	for _, mon := range p.ordered {
		st, ok := p.stats[mon.ID]
		if !ok {
			return fmt.Errorf("pokedex entry #%d %q has no base stats", mon.ID, mon.Name)
		}
		if st.hp <= 0 || st.attack <= 0 || st.defense <= 0 || st.speed <= 0 {
			return fmt.Errorf("pokedex entry #%d %q is missing a base stat", mon.ID, mon.Name)
		}
	}
	sort.Slice(p.ordered, func(i, j int) bool { return p.ordered[i].ID < p.ordered[j].ID })
	return nil
}

// allMoves returns the whole catalogue, in name order.
func (p *pokedex) allMoves() []api.Move { return p.moves }

// move looks up one move by its hyphenated name.
func (p *pokedex) move(name string) (api.Move, bool) {
	mv, ok := p.moveByName[strings.ToLower(strings.TrimSpace(name))]
	return mv, ok
}

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

func (p *pokedex) get(name string) (api.Pokemon, bool) {
	mon, ok := p.byName[strings.ToLower(strings.TrimSpace(name))]
	return mon, ok
}

func (p *pokedex) types() []api.TypeSummary {
	counts := map[string]int{}
	for _, mon := range p.ordered {
		for _, t := range mon.Types {
			counts[t]++
		}
	}
	out := make([]api.TypeSummary, 0, len(counts))
	for name, n := range counts {
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

type baseStats struct {
	hp, attack, defense, speed int
}

// A battle is stored in postgres as JSON, and encoding/json cannot see
// unexported fields: without these, base stats persisted as {} and came
// back zeroed, so the damage formula divided by a defense of 0 and every
// move dealt exactly 1.
type baseStatsJSON struct {
	HP      int `json:"hp"`
	Attack  int `json:"attack"`
	Defense int `json:"defense"`
	Speed   int `json:"speed"`
}

func (b baseStats) MarshalJSON() ([]byte, error) {
	return json.Marshal(baseStatsJSON{b.hp, b.attack, b.defense, b.speed})
}

func (b *baseStats) UnmarshalJSON(data []byte) error {
	var j baseStatsJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	b.hp, b.attack, b.defense, b.speed = j.HP, j.Attack, j.Defense, j.Speed
	return nil
}
