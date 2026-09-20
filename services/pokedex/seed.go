package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/internal/dbgen"
)

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
	var links moveLinks

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

		if err := q.DeletePokemonMovesFor(ctx, int32(e.ID)); err != nil {
			return fmt.Errorf("clearing moves for %q: %w", e.Name, err)
		}
		for i, name := range e.Moves {
			links.add(e.ID, name, "battle", i)
		}
		for i, name := range e.LearnableMoves {
			links.add(e.ID, name, "learnable", i)
		}
	}

	// One statement for every link. This was one round trip per row -
	// about 9,000 of them - which took minutes against a real database.
	if err := q.UpsertPokemonMoves(ctx, links.params()); err != nil {
		return fmt.Errorf("linking moves: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing seed: %w", err)
	}
	return nil
}

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

// runSeed loads the embedded reference data into the database and
// returns. It is what `pokedex seed` runs.
//
// Deliberately not part of startup: the data changes when the binary
// changes, so a pod restart has nothing to do. Doing it on every boot
// rewrote ~9,000 rows that were already correct and shared one 30s
// budget with connect, migrate and load - which crashlooped the pod
// whenever the cluster was slow enough to miss it.
func runSeed() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is not set")
	}

	// Its own generous budget: this writes thousands of rows and is a
	// job, not a request.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connecting: %w", err)
	}
	defer pool.Close()

	if err := migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrating: %w", err)
	}
	if err := seed(ctx, pool); err != nil {
		return err
	}
	slog.Info("seeded")
	return nil
}

// moveLinks collects every pokemon-to-move link so they can be written
// in one statement rather than one per row.
type moveLinks struct {
	pokemonIDs []int32
	names      []string
	kinds      []string
	slots      []int32
}

func (l *moveLinks) add(pokemonID int, name, kind string, slot int) {
	l.pokemonIDs = append(l.pokemonIDs, int32(pokemonID))
	l.names = append(l.names, name)
	l.kinds = append(l.kinds, kind)
	l.slots = append(l.slots, int32(slot))
}

func (l *moveLinks) params() dbgen.UpsertPokemonMovesParams {
	return dbgen.UpsertPokemonMovesParams{
		PokemonIds: l.pokemonIDs,
		MoveNames:  l.names,
		Kinds:      l.kinds,
		Slots:      l.slots,
	}
}
