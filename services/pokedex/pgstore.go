package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	randv2 "math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/internal/dbgen"
)

const serializationFailure = "40001"

const maxRetries = 10

const retryBackoff = 2 * time.Millisecond

// maxRetryBackoff caps the doubling. Ten attempts at 50ms is a
// defensible worst case for a turn; ten doublings from 2ms is not.
const maxRetryBackoff = 50 * time.Millisecond

type pgStore struct {
	pool *pgxpool.Pool
	rng  *rand.Rand
}

func newPGStore(pool *pgxpool.Pool, seed int64) *pgStore {
	return &pgStore{pool: pool, rng: rand.New(rand.NewSource(seed))}
}

type battleState struct {
	Sides   []sideState       `json:"sides"`
	Log     []api.BattleEvent `json:"log"`
	Created time.Time         `json:"created"`
}

type sideState struct {
	Trainer string           `json:"trainer"`
	Token   string           `json:"token"`
	Team    []combatantState `json:"team"`
}

type combatantState struct {
	Mon      api.Pokemon `json:"mon"`
	HP       int         `json:"hp"`
	MaxHP    int         `json:"maxHp"`
	Base     baseStats   `json:"base"`
	Stages   stages      `json:"stages"`
	Confused bool        `json:"confused"`
	Disabled int         `json:"disabled"`
}

func toState(b *battle) battleState {
	out := battleState{
		Log:     b.log,
		Created: b.created,
		Sides:   make([]sideState, 0, len(b.sides)),
	}
	for _, s := range b.sides {
		side := sideState{
			Trainer: s.trainer,
			Token:   s.token,
			Team:    make([]combatantState, 0, len(s.team)),
		}
		for _, c := range s.team {
			side.Team = append(side.Team, combatantState{
				Mon:      c.mon,
				HP:       c.hp,
				MaxHP:    c.maxHP,
				Base:     c.base,
				Stages:   c.stages,
				Confused: c.confused,
				Disabled: c.disabled,
			})
		}
		out.Sides = append(out.Sides, side)
	}
	return out
}

func fromRow(id, status string, version, turn, turnNumber int, winner string,
	created, touched time.Time, raw []byte) (*battle, error) {
	var st battleState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("decoding battle %q: %w", id, err)
	}
	b := &battle{
		id: id,
		// The column is TEXT; the field is the enum. This cast is
		// the one place a database string becomes a status, which
		// is why the CHECK constraint on the column matters.
		status:     api.BattleStatus(status),
		version:    version,
		turn:       turn,
		turnNumber: turnNumber,
		winner:     winner,
		log:        st.Log,
		created:    created,
		touched:    touched,
		sides:      make([]*side, 0, len(st.Sides)),
	}
	for _, s := range st.Sides {
		sd := &side{trainer: s.Trainer, token: s.Token}
		for _, c := range s.Team {
			sd.team = append(sd.team, &combatant{
				mon:      c.Mon,
				hp:       c.HP,
				maxHP:    c.MaxHP,
				base:     c.Base,
				stages:   c.Stages,
				confused: c.Confused,
				disabled: c.Disabled,
			})
		}
		b.sides = append(b.sides, sd)
	}
	return b, nil
}

// ---------------------------------------------------------------- store

func (p *pgStore) create(ctx context.Context, b *battle) error {
	state, err := json.Marshal(toState(b))
	if err != nil {
		return fmt.Errorf("marshalling battle state: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	if err := q.SweepBattles(ctx, pgTime(time.Now().Add(-battleTTL))); err != nil {
		slog.Warn("sweeping old battles", "err", err)
	}

	if err := q.CreateBattle(ctx, dbgen.CreateBattleParams{
		ID:         b.id,
		Status:     string(b.status),
		Version:    int32(b.version),
		Turn:       int32(b.turn),
		TurnNumber: int32(b.turnNumber),
		Winner:     b.winner,
		State:      state,
	}); err != nil {
		return fmt.Errorf("inserting battle %s: %w", b.id, err)
	}
	for i, side := range b.sides {
		if err := q.AddBattleSide(ctx, dbgen.AddBattleSideParams{
			BattleID:     b.id,
			Idx:          int32(i),
			TrainerToken: side.token,
		}); err != nil {
			return fmt.Errorf("adding side %d to battle %s: %w", i, b.id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing battle %s: %w", b.id, err)
	}
	return nil
}

func (p *pgStore) get(ctx context.Context, id string) (*api.Battle, error) {
	row, err := dbgen.New(p.pool).GetBattle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNoBattle
	}
	if err != nil {
		return nil, fmt.Errorf("reading battle %s: %w", id, err)
	}
	b, err := fromRow(row.ID, row.Status, int(row.Version), int(row.Turn),
		int(row.TurnNumber), row.Winner,
		row.CreatedAt.Time, row.TouchedAt.Time, row.State)
	if err != nil {
		// A row that will not decode is a broken battle, not an
		// absent one, and saying "no such battle" would send someone
		// looking in the wrong place.
		return nil, fmt.Errorf("decoding battle %s: %w", id, err)
	}
	return b.toAPI(), nil
}

func (p *pgStore) update(ctx context.Context, id string, fn func(*battle) error) error {
	var lastErr error

	for attempt := range maxRetries {
		err := p.attemptUpdate(ctx, id, fn)
		if err == nil {
			return nil
		}
		if !isSerializationFailure(err) {
			return err
		}
		lastErr = err

		// Capped. Unbounded doubling reaches ~1s on the last attempt
		// alone and ~2s across ten, which is an eternity for a game
		// turn - and the caller is a phone waiting on a tap. A
		// serialization conflict resolves in microseconds; the
		// backoff only needs to break the tie.
		wait := retryBackoff << attempt
		if wait > maxRetryBackoff {
			wait = maxRetryBackoff
		}
		jitter := time.Duration(randv2.Int64N(int64(wait) + 1))
		select {
		case <-time.After(wait + jitter):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("battle %s: %d serialization retries exhausted: %w",
		id, maxRetries, lastErr)
}

func (p *pgStore) attemptUpdate(ctx context.Context, id string, fn func(*battle) error) error {
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("beginning update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	row, err := q.GetBattleForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNoBattle
	}
	if err != nil {
		return fmt.Errorf("reading battle %s: %w", id, err)
	}

	b, err := fromRow(row.ID, row.Status, int(row.Version), int(row.Turn),
		int(row.TurnNumber), row.Winner,
		row.CreatedAt.Time, row.TouchedAt.Time, row.State)
	if err != nil {
		return err
	}

	if err := fn(b); err != nil {
		return err
	}

	state, err := json.Marshal(toState(b))
	if err != nil {
		return fmt.Errorf("marshalling battle %s: %w", id, err)
	}
	if err := q.UpdateBattle(ctx, dbgen.UpdateBattleParams{
		ID:         b.id,
		Status:     string(b.status),
		Turn:       int32(b.turn),
		TurnNumber: int32(b.turnNumber),
		Winner:     b.winner,
		State:      state,
	}); err != nil {
		return fmt.Errorf("writing battle %s: %w", id, err)
	}

	// Sides can change: joining a waiting battle adds one.
	for i, s := range b.sides {
		if err := q.AddBattleSide(ctx, dbgen.AddBattleSideParams{
			BattleID:     b.id,
			Idx:          int32(i),
			TrainerToken: s.token,
		}); err != nil {
			return fmt.Errorf("writing side %d of %s: %w", i, id, err)
		}
	}

	return tx.Commit(ctx)
}

func (p *pgStore) waiting(ctx context.Context) ([]api.WaitingBattle, error) {
	q := dbgen.New(p.pool)
	if err := q.SweepBattles(ctx, pgTime(time.Now().Add(-battleTTL))); err != nil {
		slog.Warn("sweeping expired battles", "error", err)
	}

	rows, err := q.ListWaitingBattles(ctx)
	if err != nil {
		// Returned rather than logged-and-swallowed: an empty lobby
		// and a broken database look identical to a player, and only
		// one of them is worth retrying.
		return nil, fmt.Errorf("listing waiting battles: %w", err)
	}
	out := make([]api.WaitingBattle, 0, len(rows))
	for _, row := range rows {
		b, err := fromRow(row.ID, row.Status, int(row.Version), int(row.Turn),
			int(row.TurnNumber), row.Winner,
			row.CreatedAt.Time, row.TouchedAt.Time, row.State)
		if err != nil {
			// One unreadable row should not empty the lobby.
			continue
		}
		out = append(out, b.toWaiting())
	}
	return out, nil
}

func (p *pgStore) trainerByToken(ctx context.Context, token string) (string, error) {
	t, err := dbgen.New(p.pool).TrainerByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errNoTrainer
	}
	if err != nil {
		// Not errNoTrainer: a store failure here used to become a
		// 401, and an auth error is the last place anyone looks for
		// a database outage.
		return "", fmt.Errorf("looking up trainer: %w", err)
	}
	return t.Name, nil
}

func (p *pgStore) registerTrainer(ctx context.Context, name string) (string, error) {
	token := newToken()
	_, err := dbgen.New(p.pool).RegisterTrainer(ctx, dbgen.RegisterTrainerParams{
		Token: token,
		Name:  name,
	})
	if isUniqueViolation(err) {
		return "", errNameTaken
	}
	if err != nil {
		return "", fmt.Errorf("registering %q: %w", name, err)
	}
	return token, nil
}

// ---------------------------------------------------------------- helpers

func pgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == serializationFailure
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
