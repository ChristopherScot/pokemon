package main

// The battle endpoints. Thin: they authenticate the caller, translate
// between wire types and the engine, and map engine errors onto the
// statuses the spec declares. Every rule lives in battle.go.

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// RegisterTrainer claims a name and mints the token that authorises this
// trainer's moves.
func (s service) RegisterTrainer(ctx context.Context, req *api.RegisterTrainer) (api.RegisterTrainerRes, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return &api.Error{Message: "name cannot be empty"}, nil
	}
	token, err := s.battles.registerTrainer(ctx, name)
	switch {
	case errors.Is(err, errNameTaken):
		// The spec's only non-201 here is 409, which ogen models as the
		// bare Error type.
		return &api.Error{Message: fmt.Sprintf("%q is taken, pick another", name)}, nil
	case err != nil:
		// Anything else is not the player's fault. Reporting a database
		// outage as "that name is taken" sends them off to invent a new
		// one against a problem no name can fix.
		return nil, fmt.Errorf("registering %q: %w", name, err)
	}
	return &api.Trainer{Name: name, Token: token}, nil
}

// ListWaitingTrainers is the lobby: who is looking for a battle.
func (s service) ListWaitingTrainers(ctx context.Context) (*api.WaitingList, error) {
	open := s.battles.waiting(ctx)
	return &api.WaitingList{Count: len(open), Waiting: open}, nil
}

// CreateBattle opens an invitation. The battle sits in `waiting` until
// someone joins, which is when turn order is decided.
func (s service) CreateBattle(ctx context.Context, req *api.CreateBattle, params api.CreateBattleParams) (api.CreateBattleRes, error) {
	trainer, ok := s.battles.trainerByToken(ctx, params.XTrainerToken)
	if !ok {
		return &api.CreateBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	// A short or absent team is filled at random, so a client can offer
	// "pick the ones you care about" rather than all-or-nothing.
	names := fillTeam(s.dex, req.Team, s.rng)
	team, err := newCombatants(s.dex, names)
	if err != nil {
		return &api.CreateBattleBadRequest{Message: err.Error()}, nil
	}

	now := time.Now()
	b := &battle{
		id:      randomID(s.rng, 6),
		status:  "waiting",
		version: 1,
		created: now,
		touched: now,
		sides: []*side{{
			trainer: trainer,
			token:   params.XTrainerToken,
			team:    team,
		}},
	}
	b.log = append(b.log, api.BattleEvent{
		TurnNumber: 0,
		Text:       fmt.Sprintf("%s is looking for a battle.", trainer),
	})
	// Surfaced, not swallowed. This used to be a bare call and the
	// handler answered 201 regardless, so a failed insert handed the
	// player a battle id that was never written - they shared it, and
	// every later read 404'd with nothing in the logs to say why.
	//
	// NewError logs it and renders a 500, which is the honest answer:
	// the battle does not exist.
	if err := s.battles.create(ctx, b); err != nil {
		return nil, fmt.Errorf("creating battle: %w", err)
	}
	return b.toAPI(), nil
}

// GetBattle is what clients poll. Cheap on purpose: `version` lets a
// caller skip re-rendering when nothing has changed.
func (s service) GetBattle(ctx context.Context, params api.GetBattleParams) (api.GetBattleRes, error) {
	b, ok := s.battles.get(ctx, params.ID)
	if !ok {
		return &api.Error{Message: "no such battle"}, nil
	}
	return b, nil
}

// JoinBattle fills the second side and starts play.
func (s service) JoinBattle(ctx context.Context, req *api.JoinBattle, params api.JoinBattleParams) (api.JoinBattleRes, error) {
	trainer, ok := s.battles.trainerByToken(ctx, params.XTrainerToken)
	if !ok {
		return &api.JoinBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	// The whole join happens inside update(), including the checks.
	// Two players racing for the last seat would both pass a check
	// made outside it, and the second would overwrite the first.
	var (
		out     *api.Battle
		badTeam error
	)
	err := s.battles.update(ctx, params.ID, func(b *battle) error {
		if b.status != "waiting" {
			return errBattleFull
		}
		if b.sides[0].token == params.XTrainerToken {
			return errAlreadyIn
		}
		names := fillTeam(s.dex, req.Team, s.rng)
		team, err := newCombatants(s.dex, names)
		if err != nil {
			// Not a conflict: the request itself is wrong, and
			// retrying would fail the same way. Carried out rather
			// than returned so the caller can tell 400 from 409.
			badTeam = err
			return err
		}

		b.sides = append(b.sides, &side{
			trainer: trainer,
			token:   params.XTrainerToken,
			team:    team,
		})
		b.status = "active"
		// The player who waited moves first: a small reward for
		// opening the invitation, and it makes turn order
		// deterministic rather than a coin flip nobody can see.
		b.turn = 0
		b.version++
		b.touched = time.Now()
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: 0,
			Text:       fmt.Sprintf("%s joined. %s moves first!", trainer, b.sides[0].trainer),
		})
		out = b.toAPI()
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, errNoBattle):
		return &api.JoinBattleNotFound{Message: "no such battle"}, nil
	case badTeam != nil:
		return &api.JoinBattleBadRequest{Message: badTeam.Error()}, nil
	case errors.Is(err, errBattleFull), errors.Is(err, errAlreadyIn):
		return &api.JoinBattleConflict{Message: err.Error()}, nil
	default:
		return nil, err
	}
}

// TakeTurn resolves one attack.
func (s service) TakeTurn(ctx context.Context, req *api.TakeTurn, params api.TakeTurnParams) (api.TakeTurnRes, error) {
	if _, ok := s.battles.trainerByToken(ctx, params.XTrainerToken); !ok {
		return &api.TakeTurnUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	// The turn resolves inside the transaction that reads and writes
	// the battle. Two players moving at once used to be impossible
	// because one process held a mutex; with several replicas this is
	// what replaces it.
	var out *api.Battle
	err := s.battles.update(ctx, params.ID, func(b *battle) error {
		if err := b.takeTurn(params.XTrainerToken, req.Attacker, req.Move, req.Target, s.rng); err != nil {
			return err
		}
		out = b.toAPI()
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, errNoBattle):
		return &api.TakeTurnNotFound{Message: "no such battle"}, nil
	case errors.Is(err, errNotYourTurn),
		errors.Is(err, errIllegalMove),
		errors.Is(err, errTargetFainted),
		errors.Is(err, errBattleOver),
		errors.Is(err, errNotWaiting):
		// A 409 rather than a 400: the request is well-formed, it is the
		// state that makes it impossible. A client can retry after the
		// state moves on, which is not true of a malformed request.
		return &api.TakeTurnConflict{Message: err.Error()}, nil
	default:
		return nil, err
	}
}

// lockedRand is a *rand.Rand every handler can reach.
//
// One generator is shared by every request, and *rand.Rand is not safe
// for concurrent use: two simultaneous turns race on its internal
// state, which the race detector reports inside rngSource.Uint64.
//
// The obvious fix - switch to math/rand/v2's top-level functions, as
// the Postgres store's retry jitter already does - would cost the
// seeding, and seeding is why this field exists: a test seeds it and
// gets the same battle twice. So the generator stays and gains a lock.
//
// Only the two methods the engine actually calls are exposed, so the
// unguarded ones cannot be reached by accident.
type lockedRand struct {
	mu sync.Mutex
	r  *rand.Rand
}

func (l *lockedRand) Intn(n int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Intn(n)
}

func (l *lockedRand) Float64() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Float64()
}

// rngFor keeps damage rolls deterministic in tests while staying
// unpredictable in production.
func rngFor(seed int64) *lockedRand {
	return &lockedRand{r: rand.New(rand.NewSource(seed))}
}
