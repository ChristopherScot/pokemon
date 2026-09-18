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
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// RegisterTrainer claims a name and mints the token that authorises this
// trainer's moves.
func (s service) RegisterTrainer(_ context.Context, req *api.RegisterTrainer) (api.RegisterTrainerRes, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return &api.Error{Message: "name cannot be empty"}, nil
	}
	token, err := s.battles.registerTrainer(name)
	if err != nil {
		// The spec's only non-201 here is 409, which ogen models as the
		// bare Error type.
		return &api.Error{Message: fmt.Sprintf("%q is taken, pick another", name)}, nil
	}
	return &api.Trainer{Name: name, Token: token}, nil
}

// ListWaitingTrainers is the lobby: who is looking for a battle.
func (s service) ListWaitingTrainers(context.Context) (*api.WaitingList, error) {
	open := s.battles.waiting()
	out := &api.WaitingList{Count: len(open)}
	for _, b := range open {
		side := b.sides[0]
		w := api.WaitingBattle{
			BattleId:  b.id,
			Trainer:   side.trainer,
			CreatedAt: b.created,
		}
		for _, c := range side.team {
			w.Team = append(w.Team, c.mon.Name)
		}
		out.Waiting = append(out.Waiting, w)
	}
	return out, nil
}

// CreateBattle opens an invitation. The battle sits in `waiting` until
// someone joins, which is when turn order is decided.
func (s service) CreateBattle(_ context.Context, req *api.CreateBattle, params api.CreateBattleParams) (api.CreateBattleRes, error) {
	trainer, ok := s.battles.trainerByToken(params.XTrainerToken)
	if !ok {
		return &api.CreateBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	team, err := newCombatants(s.dex, req.Team)
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
	s.battles.create(b)
	return b.toAPI(), nil
}

// GetBattle is what clients poll. Cheap on purpose: `version` lets a
// caller skip re-rendering when nothing has changed.
func (s service) GetBattle(_ context.Context, params api.GetBattleParams) (api.GetBattleRes, error) {
	b, ok := s.battles.get(params.ID)
	if !ok {
		return &api.Error{Message: "no such battle"}, nil
	}
	return b.toAPI(), nil
}

// JoinBattle fills the second side and starts play.
func (s service) JoinBattle(_ context.Context, req *api.JoinBattle, params api.JoinBattleParams) (api.JoinBattleRes, error) {
	trainer, ok := s.battles.trainerByToken(params.XTrainerToken)
	if !ok {
		return &api.JoinBattleUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	b, ok := s.battles.get(params.ID)
	if !ok {
		return &api.JoinBattleNotFound{Message: "no such battle"}, nil
	}
	if b.status != "waiting" {
		return &api.JoinBattleConflict{Message: errBattleFull.Error()}, nil
	}
	if b.sides[0].token == params.XTrainerToken {
		return &api.JoinBattleConflict{Message: errAlreadyIn.Error()}, nil
	}
	team, err := newCombatants(s.dex, req.Team)
	if err != nil {
		return &api.JoinBattleBadRequest{Message: err.Error()}, nil
	}

	b.sides = append(b.sides, &side{
		trainer: trainer,
		token:   params.XTrainerToken,
		team:    team,
	})
	b.status = "active"
	// The player who waited moves first: a small reward for opening the
	// invitation, and it makes turn order deterministic rather than a
	// coin flip nobody can see.
	b.turn = 0
	b.version++
	b.touched = time.Now()
	b.log = append(b.log, api.BattleEvent{
		TurnNumber: 0,
		Text:       fmt.Sprintf("%s joined. %s moves first!", trainer, b.sides[0].trainer),
	})
	return b.toAPI(), nil
}

// TakeTurn resolves one attack.
func (s service) TakeTurn(_ context.Context, req *api.TakeTurn, params api.TakeTurnParams) (api.TakeTurnRes, error) {
	if _, ok := s.battles.trainerByToken(params.XTrainerToken); !ok {
		return &api.TakeTurnUnauthorized{Message: "unknown trainer token; register first"}, nil
	}
	b, ok := s.battles.get(params.ID)
	if !ok {
		return &api.TakeTurnNotFound{Message: "no such battle"}, nil
	}

	err := b.takeTurn(params.XTrainerToken, req.Attacker, req.Move, req.Target, s.rng)
	switch {
	case err == nil:
		return b.toAPI(), nil
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

// rngFor keeps damage rolls deterministic in tests while staying
// unpredictable in production.
func rngFor(seed int64) *rand.Rand { return rand.New(rand.NewSource(seed)) }
