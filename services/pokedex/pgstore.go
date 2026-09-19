package main

// The Postgres store: the same interface memStore implements, backed
// by a database so state survives a restart and two replicas agree.
//
// What makes it safe is update(): a read, the caller's mutation and
// the write all inside one SERIALIZABLE transaction. Postgres detects
// two transactions that would not be equivalent to running them one
// after the other and fails the second with 40001; this retries it.
// Nothing here holds a lock across a request.

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

// serializationFailure is the SQLSTATE Postgres returns when it cannot
// order two concurrent transactions. It is the retry signal, not an
// error to report.
const serializationFailure = "40001"

// maxRetries bounds the retry loop, so a pathological case cannot hold
// a request open forever.
//
// Ten, not five. Five looks generous for two players taking turns, and
// it is - but retries are not independent: every loser of a conflict
// retries at once and collides again, so the attempts a single writer
// needs grows with how many are contending. Ten concurrent writers on
// one battle exhausted five retries for four of them, losing their
// writes. That is the corruption this design exists to prevent,
// arriving through the mechanism meant to stop it.
const maxRetries = 10

// retryBackoff is the base for the wait between attempts.
//
// Without a wait, every conflicting writer retries in lockstep and
// collides again on the same schedule. Exponential with jitter
// spreads them out, which is what turns a thundering herd into a
// queue.
const retryBackoff = 2 * time.Millisecond

type pgStore struct {
	pool *pgxpool.Pool
	rng  *rand.Rand
}

func newPGStore(pool *pgxpool.Pool, seed int64) *pgStore {
	return &pgStore{pool: pool, rng: rand.New(rand.NewSource(seed))}
}

// ---------------------------------------------------------------- persistence

// battleState is the JSONB document: everything about a battle that no
// query needs to see.
//
// Explicit rather than marshalling `battle` directly - its fields are
// unexported, and a type the database depends on should change only
// when someone means it to.
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
	// The Pokemon as served, so a battle renders without a second
	// lookup and keeps showing what it started with even if the
	// Pokedex is re-seeded mid-battle.
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
		id:         id,
		status:     status,
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
		// toState produces only marshalable types, so a failure here
		// is a programming error rather than a runtime condition.
		return fmt.Errorf("marshalling battle state: %w", err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := dbgen.New(tx)

	// Swept here rather than from a timer: with several replicas a
	// timer in each would mean several sweeps racing, and a store
	// nobody writes to does not grow.
	//
	// Opportunistic - a failed sweep costs disk, not correctness, and
	// must not fail the battle somebody is trying to open.
	if err := q.SweepBattles(ctx, pgTime(time.Now().Add(-battleTTL))); err != nil {
		slog.Warn("sweeping old battles", "err", err)
	}

	if err := q.CreateBattle(ctx, dbgen.CreateBattleParams{
		ID:         b.id,
		Status:     b.status,
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

// get converts before returning, matching memStore. The pgStore path
// builds a fresh *battle per call so nothing is shared, but the
// interface is the same either way and a caller should not have to know
// which implementation it is talking to.
func (p *pgStore) get(ctx context.Context, id string) (*api.Battle, bool) {
	row, err := dbgen.New(p.pool).GetBattle(ctx, id)
	if err != nil {
		return nil, false
	}
	b, err := fromRow(row.ID, row.Status, int(row.Version), int(row.Turn),
		int(row.TurnNumber), row.Winner,
		row.CreatedAt.Time, row.TouchedAt.Time, row.State)
	if err != nil {
		return nil, false
	}
	return b.toAPI(), true
}

// update is the one that matters. See the interface comment in
// battle.go for why the closure form rather than get-then-save.
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

		// A conflict means the other transaction committed, so the
		// next read sees its result and fn runs against current
		// state. The dice are re-rolled with it; nobody observed the
		// discarded attempt.
		//
		// Back off before trying again, with jitter. Retrying
		// immediately means colliding with every other loser at the
		// same instant, which is how a burst of writers exhausts its
		// retries without any of them making progress.
		// math/rand/v2's top-level functions rather than the store's
		// own generator: several goroutines retry at once, and
		// *rand.Rand is not safe for concurrent use - the race
		// detector catches it. Jitter needs no reproducibility, so
		// the shared global source is the right one.
		wait := retryBackoff << attempt
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

	// The caller's mutation. An error here is the caller's - an
	// illegal move, not your turn - and rolls back without retrying:
	// re-running it would produce the same error against the same
	// state.
	if err := fn(b); err != nil {
		return err
	}

	state, err := json.Marshal(toState(b))
	if err != nil {
		return fmt.Errorf("marshalling battle %s: %w", id, err)
	}
	if err := q.UpdateBattle(ctx, dbgen.UpdateBattleParams{
		ID:         b.id,
		Status:     b.status,
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

func (p *pgStore) waiting(ctx context.Context) []api.WaitingBattle {
	q := dbgen.New(p.pool)
	// Opportunistic: the sweep is garbage collection, not part of
	// answering this request, so a failure is logged and the lobby is
	// still served. Discarding it silently meant a sweep that had been
	// failing for weeks would look exactly like one that never ran.
	if err := q.SweepBattles(ctx, pgTime(time.Now().Add(-battleTTL))); err != nil {
		slog.Warn("sweeping expired battles", "error", err)
	}

	rows, err := q.ListWaitingBattles(ctx)
	if err != nil {
		// An empty lobby and a broken database look identical to the
		// caller, so say which one this is.
		slog.Error("listing waiting battles", "error", err)
		return nil
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
	return out
}

func (p *pgStore) trainerByToken(ctx context.Context, token string) (string, bool) {
	t, err := dbgen.New(p.pool).TrainerByToken(ctx, token)
	if err != nil {
		return "", false
	}
	return t.Name, true
}

// registerTrainer claims a name.
//
// The race - two clients registering "ash" at the same instant - is
// settled by the UNIQUE index on lower(name) rather than by checking
// first. A check-then-insert has a gap between the two, and with
// several replicas there is no lock that closes it.
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

// pgTime wraps a time for a query parameter. Always valid: every
// caller passes a real time, and a NULL cutoff would sweep nothing
// silently.
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
