package main

// Seeding the reference data from pokedex.json into Postgres.
//
// The file stays embedded and stays the source of truth: it is what
// fetch_pokedex.py writes, what review sees as a diff, and what a
// fresh database is built from. The database is where the service
// reads it, not where it is authored.
//
// Runs at startup, after migrate. Two replicas start together, so this
// has to be safe concurrently - every statement is an upsert, and the
// whole thing is one transaction, so the loser of a race rewrites the
// same rows with the same values rather than colliding.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/internal/dbgen"
)

// seed writes the embedded Pokedex into the database.
//
// Unconditional rather than "only if empty": the data file changes
// when fetch_pokedex.py is re-run, and a seed that skipped a populated
// database would leave the cluster on an old Pokedex with nothing to
// show for it. Upserts make the repeat cheap.
func seed(ctx context.Context, pool *pgxpool.Pool) error {
	var doc dataset
	if err := json.Unmarshal(pokedexJSON, &doc); err != nil {
		return fmt.Errorf("decoding embedded pokedex: %w", err)
	}
	if len(doc.Pokemon) == 0 || len(doc.Moves) == 0 {
		return fmt.Errorf("embedded pokedex is empty")
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning seed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := dbgen.New(tx)

	// Moves first: pokemon_moves has a foreign key to both sides, and
	// a link cannot be written before the move it names.
	for _, m := range doc.Moves {
		var accuracy *int32
		if m.Accuracy != nil {
			a := int32(*m.Accuracy)
			accuracy = &a
		}
		if err := q.UpsertMove(ctx, dbgen.UpsertMoveParams{
			Name:        m.Name,
			Type:        m.Type,
			Power:       int32(m.Power),
			Description: m.Description,
			Effect:      m.Effect,
			Accuracy:    accuracy,
			Pp:          int32(m.PP),
			DamageClass: m.DamageClass,
		}); err != nil {
			return fmt.Errorf("seeding move %q: %w", m.Name, err)
		}
	}

	for _, e := range doc.Pokemon {
		if err := q.UpsertPokemon(ctx, dbgen.UpsertPokemonParams{
			ID:                 int32(e.ID),
			Name:               e.Name,
			Description:        e.Description,
			Genus:              e.Genus,
			Habitat:            e.Habitat,
			Types:              e.Types,
			Height:             int32(e.Height),
			Weight:             int32(e.Weight),
			Sprite:             e.Sprite,
			Artwork:            e.Artwork,
			EvolvesFrom:        e.EvolvesFrom,
			Legendary:          e.Legendary,
			BaseHp:             int32(e.BaseHp),
			BaseAttack:         int32(e.BaseAttack),
			BaseDefense:        int32(e.BaseDefense),
			BaseSpecialAttack:  int32(e.BaseSpecialAttack),
			BaseSpecialDefense: int32(e.BaseSpecialDefense),
			BaseSpeed:          int32(e.BaseSpeed),
		}); err != nil {
			return fmt.Errorf("seeding pokemon %q: %w", e.Name, err)
		}

		// Cleared and rewritten rather than upserted in place. A move
		// dropped from a Pokemon upstream has no upsert that removes
		// it, and a stale link would put a move in a battle that the
		// data file no longer lists.
		if err := q.DeletePokemonMovesFor(ctx, int32(e.ID)); err != nil {
			return fmt.Errorf("clearing moves for %q: %w", e.Name, err)
		}
		for i, name := range e.Moves {
			if err := q.UpsertPokemonMove(ctx, dbgen.UpsertPokemonMoveParams{
				PokemonID: int32(e.ID),
				MoveName:  name,
				Kind:      "battle",
				Slot:      int32(i),
			}); err != nil {
				return fmt.Errorf("linking battle move %q to %q: %w", name, e.Name, err)
			}
		}
		for i, name := range e.LearnableMoves {
			if err := q.UpsertPokemonMove(ctx, dbgen.UpsertPokemonMoveParams{
				PokemonID: int32(e.ID),
				MoveName:  name,
				Kind:      "learnable",
				Slot:      int32(i),
			}); err != nil {
				return fmt.Errorf("linking learnable move %q to %q: %w", name, e.Name, err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing seed: %w", err)
	}
	return nil
}

// loadPokedexFromDB reads the reference data back out, into the same
// shape loadPokedex builds from the file.
//
// Held in memory for the life of the process: 100 Pokemon and 562
// moves that cannot change without a deploy, so a query per request
// would put the database on the path of every /pokemon call and buy
// nothing. The database is the source of truth; this is a cache of it.
func loadPokedexFromDB(ctx context.Context, pool *pgxpool.Pool) (*pokedex, error) {
	q := dbgen.New(pool)

	rawMoves, err := q.ListMoves(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading moves: %w", err)
	}
	if len(rawMoves) == 0 {
		return nil, fmt.Errorf("no moves in the database - seeding did not run")
	}

	rawMons, err := q.ListPokemon(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading pokemon: %w", err)
	}
	if len(rawMons) == 0 {
		return nil, fmt.Errorf("no pokemon in the database - seeding did not run")
	}

	// One query for every link rather than one per Pokemon: a hundred
	// round trips at startup for data that fits in a single result.
	links, err := q.ListPokemonMoves(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading pokemon moves: %w", err)
	}

	p := &pokedex{
		ordered:    make([]api.Pokemon, 0, len(rawMons)),
		byName:     make(map[string]api.Pokemon, len(rawMons)),
		stats:      make(map[int]baseStats, len(rawMons)),
		moves:      make([]api.Move, 0, len(rawMoves)),
		moveByName: make(map[string]api.Move, len(rawMoves)),
		learnable:  make(map[string][]string, len(rawMons)),
	}

	for _, m := range rawMoves {
		mv := api.Move{
			Name:        m.Name,
			Type:        m.Type,
			Power:       int(m.Power),
			Description: m.Description,
			Effect:      m.Effect,
			Pp:          int(m.Pp),
			DamageClass: m.DamageClass,
		}
		if m.Accuracy != nil {
			mv.Accuracy = api.NewOptInt(int(*m.Accuracy))
		}
		p.moves = append(p.moves, mv)
		p.moveByName[m.Name] = mv
	}

	// Links arrive ordered by (pokemon_id, kind, slot), so appending in
	// order preserves the slot ordering the battle depends on.
	battleMoves := map[int32][]string{}
	learnMoves := map[int32][]string{}
	for _, l := range links {
		switch l.Kind {
		case "battle":
			battleMoves[l.PokemonID] = append(battleMoves[l.PokemonID], l.MoveName)
		case "learnable":
			learnMoves[l.PokemonID] = append(learnMoves[l.PokemonID], l.MoveName)
		}
	}

	for _, e := range rawMons {
		moves := make([]api.Move, 0, len(battleMoves[e.ID]))
		for _, name := range battleMoves[e.ID] {
			mv, ok := p.moveByName[name]
			if !ok {
				return nil, fmt.Errorf("pokemon %q lists move %q, which is not in the catalogue", e.Name, name)
			}
			moves = append(moves, mv)
		}

		mon := api.Pokemon{
			ID:          int(e.ID),
			Name:        e.Name,
			Description: e.Description,
			Genus:       e.Genus,
			Types:       e.Types,
			Height:      int(e.Height),
			Weight:      int(e.Weight),
			Sprite:      e.Sprite,
			Moves:       moves,
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

		p.stats[int(e.ID)] = baseStats{
			hp:      int(e.BaseHp),
			attack:  int(e.BaseAttack),
			defense: int(e.BaseDefense),
			speed:   int(e.BaseSpeed),
		}
		p.ordered = append(p.ordered, mon)
		p.byName[strings.ToLower(mon.Name)] = mon
		p.learnable[strings.ToLower(mon.Name)] = learnMoves[e.ID]
	}
	if err := p.finish(); err != nil {
		return nil, err
	}
	return p, nil
}
