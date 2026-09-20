package main

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

func (s service) RegisterTrainer(ctx context.Context, req *api.RegisterTrainer) (api.RegisterTrainerRes, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return &api.Error{Message: "name cannot be empty"}, nil
	}
	token, err := s.battles.registerTrainer(ctx, name)
	switch {
	case errors.Is(err, errNameTaken):
		return &api.Error{Message: fmt.Sprintf("%q is taken, pick another", name)}, nil
	case err != nil:
		return nil, fmt.Errorf("registering %q: %w", name, err)
	}
	return &api.Trainer{Name: name, Token: token}, nil
}

// ListWaitingTrainers is the lobby: who is looking for a battle.
func (s service) ListWaitingTrainers(ctx context.Context) (*api.WaitingList, error) {
	open, err := s.battles.waiting(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing the lobby: %w", err)
	}
	return &api.WaitingList{Count: len(open), Waiting: open}, nil
}

func (s service) CreateBattle(ctx context.Context, req *api.CreateBattle, params api.CreateBattleParams) (api.CreateBattleRes, error) {
	trainer, err := s.battles.trainerByToken(ctx, params.XTrainerToken)
	if err != nil && !errors.Is(err, errNoTrainer) {
		return nil, fmt.Errorf("authenticating: %w", err)
	}
	if errors.Is(err, errNoTrainer) {
		return &api.CreateBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
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
	if err := s.battles.create(ctx, b); err != nil {
		return nil, fmt.Errorf("creating battle: %w", err)
	}
	return b.toAPI(), nil
}

func (s service) GetBattle(ctx context.Context, params api.GetBattleParams) (api.GetBattleRes, error) {
	b, err := s.battles.get(ctx, params.ID)
	if err != nil && !errors.Is(err, errNoBattle) {
		return nil, fmt.Errorf("reading battle: %w", err)
	}
	if errors.Is(err, errNoBattle) {
		return &api.Error{Message: "no such battle"}, nil
	}
	return b, nil
}

// JoinBattle fills the second side and starts play.
func (s service) JoinBattle(ctx context.Context, req *api.JoinBattle, params api.JoinBattleParams) (api.JoinBattleRes, error) {
	trainer, err := s.battles.trainerByToken(ctx, params.XTrainerToken)
	if err != nil && !errors.Is(err, errNoTrainer) {
		return nil, fmt.Errorf("authenticating: %w", err)
	}
	if errors.Is(err, errNoTrainer) {
		return &api.JoinBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	names := fillTeam(s.dex, req.Team, s.rng)
	team, err := newCombatants(s.dex, names)
	if err != nil {
		return &api.JoinBattleBadRequest{Message: err.Error()}, nil
	}

	var out *api.Battle
	err = s.battles.update(ctx, params.ID, func(b *battle) error {
		if b.status != "waiting" {
			return errBattleFull
		}
		if b.sides[0].token == params.XTrainerToken {
			return errAlreadyIn
		}

		b.sides = append(b.sides, &side{
			trainer: trainer,
			token:   params.XTrainerToken,
			team:    team,
		})
		b.status = "active"
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
	case errors.Is(err, errBattleFull), errors.Is(err, errAlreadyIn):
		return &api.JoinBattleConflict{Message: err.Error()}, nil
	default:
		return nil, err
	}
}

// TakeTurn resolves one attack.
func (s service) TakeTurn(ctx context.Context, req *api.TakeTurn, params api.TakeTurnParams) (api.TakeTurnRes, error) {
	if _, err := s.battles.trainerByToken(ctx, params.XTrainerToken); err != nil {
		if !errors.Is(err, errNoTrainer) {
			return nil, fmt.Errorf("authenticating: %w", err)
		}
		return &api.TakeTurnUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
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
		return &api.TakeTurnConflict{Message: err.Error()}, nil
	default:
		return nil, err
	}
}

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

func rngFor(seed int64) *lockedRand {
	return &lockedRand{r: rand.New(rand.NewSource(seed))}
}
