package main

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Tunables. Named rather than inline so the balance is in one place.
const (
	teamSize = 3

	battleTTL = 2 * time.Hour

	damageSpread = 0.15

	damageScale = 0.85
)

var (
	errNoBattle      = errors.New("no such battle")
	errNoTrainer     = errors.New("unknown trainer token")
	errNotYourTurn   = errors.New("not your turn")
	errIllegalMove   = errors.New("illegal move")
	errBattleOver    = errors.New("battle is already finished")
	errBattleFull    = errors.New("battle already has two trainers")
	errAlreadyIn     = errors.New("you are already in this battle")
	errNotWaiting    = errors.New("battle is not waiting for an opponent")
	errUnknownMon    = errors.New("unknown pokemon")
	errNotYourMon    = errors.New("that pokemon is not yours")
	errTargetFainted = errors.New("that target has already fainted")
)

const (
	battleLevel = 50
	battleIV    = 0
	battleEV    = 0
)

func maxHP(base, iv, ev, level int) int {
	if base <= 0 {
		return 1
	}
	return (2*base+iv+ev/4)*level/100 + level + 10
}

type battle struct {
	id      string
	status  string
	version int
	sides   []*side
	log     []api.BattleEvent

	// turn is the index into sides whose move it is.
	turn    int
	winner  string
	created time.Time
	touched time.Time

	// turnNumber counts resolved turns, for ordering the log.
	turnNumber int
}

type side struct {
	trainer string
	token   string
	team    []*combatant
}

type combatant struct {
	mon   api.Pokemon
	hp    int
	maxHP int

	// The game's base stats, which the damage formula needs.
	base baseStats

	stages stages

	confused bool

	// disabled is a move index this Pokemon cannot use, or -1.
	disabled int
}

func (c *combatant) fainted() bool { return c.hp <= 0 }

func (s *side) defeated() bool {
	for _, c := range s.team {
		if !c.fainted() {
			return false
		}
	}
	return true
}

func newCombatants(dex *pokedex, names []string) ([]*combatant, error) {
	if len(names) != teamSize {
		return nil, fmt.Errorf("need %d pokemon, got %d", teamSize, len(names))
	}
	team := make([]*combatant, 0, teamSize)
	for _, n := range names {
		mon, ok := dex.get(strings.ToLower(strings.TrimSpace(n)))
		if !ok {
			return nil, fmt.Errorf("%w: %q", errUnknownMon, n)
		}
		st := dex.stats[mon.ID]
		hp := maxHP(st.hp, battleIV, battleEV, battleLevel)
		team = append(team, &combatant{
			mon: mon, hp: hp, maxHP: hp, base: st, disabled: -1,
		})
	}
	return team, nil
}

type roller interface {
	Intn(n int) int
	Float64() float64
}

func fillTeam(dex *pokedex, chosen []string, rng roller) []string {
	if len(chosen) >= teamSize {
		return chosen
	}
	taken := make(map[string]bool, len(chosen))
	for _, n := range chosen {
		taken[strings.ToLower(strings.TrimSpace(n))] = true
	}

	out := append([]string(nil), chosen...)
	all := dex.list("", 0)
	// Walk a shuffled order rather than drawing until something new
	// comes up. Rejection sampling here was an unbounded loop on a
	// request path: `continue` without consuming an attempt, so a
	// roller that keeps returning the same index spins forever inside
	// CreateBattle with no ctx check. That is one roller
	// implementation away from a wedged pod, and fixedRoll in the
	// test files is already such an implementation.
	for _, i := range shuffledIndexes(len(all), rng) {
		if len(out) >= teamSize {
			break
		}
		if taken[all[i].Name] {
			continue
		}
		taken[all[i].Name] = true
		out = append(out, all[i].Name)
	}
	return out
}

// shuffledIndexes returns 0..n-1 in a random order.
//
// One pass, no rejection, so every caller terminates in exactly n
// steps whatever the roller does.
func shuffledIndexes(n int, rng roller) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	// Fisher-Yates. rng.Intn(i+1) is in range for any roller that
	// honours its contract, and a roller that does not still
	// terminates - it just shuffles badly.
	for i := n - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		if j < 0 || j > i {
			j = 0
		}
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

func randomTeam(dex *pokedex, rng roller) []string {
	all := dex.list("", 0)
	if len(all) < teamSize {
		names := make([]string, 0, len(all))
		for _, m := range all {
			names = append(names, m.Name)
		}
		return names
	}

	// Same shuffle as fillTeam, for the same reason: the previous
	// draw-until-new loop could not terminate for some rollers.
	team := make([]string, 0, teamSize)
	for _, i := range shuffledIndexes(len(all), rng) {
		if len(team) >= teamSize {
			break
		}
		team = append(team, all[i].Name)
	}
	return team
}

func damage(attacker, defender *combatant, move api.Move, rng roller) (int, float64) {
	mult := multiplier(move.Type, defender.mon.Types)
	if mult == 0 || move.Power == 0 {
		return 0, mult
	}

	stab := 1.0
	for _, t := range attacker.mon.Types {
		if t == move.Type {
			stab = 1.5
			break
		}
	}

	atk := float64(attacker.base.attack) * statMultiplier(attacker.stages.attack)
	def := float64(defender.base.defense) * statMultiplier(defender.stages.defense)

	base := float64(move.Power) * damageScale * mult * stab * (atk / def)
	spread := 1 + (rng.Float64()*2-1)*damageSpread
	d := int(base * spread)
	if d < 1 {
		d = 1
	}
	return d, mult
}

type store interface {
	create(ctx context.Context, b *battle) error
	// get returns errNoBattle when there is no such battle, and any
	// other error when the store itself failed. A bool cannot tell
	// those apart, and the version that returned one reported a dead
	// database to the client as "no such battle".
	get(ctx context.Context, id string) (*api.Battle, error)
	waiting(ctx context.Context) ([]api.WaitingBattle, error)
	// trainerByToken returns errNoTrainer for an unknown token. Same
	// reason: a store failure used to surface as a 401, which looks
	// like an auth bug to whoever debugs it.
	trainerByToken(ctx context.Context, token string) (string, error)
	registerTrainer(ctx context.Context, name string) (string, error)

	update(ctx context.Context, id string, fn func(*battle) error) error
}

type memStore struct {
	mu       sync.Mutex
	battles  map[string]*battle
	trainers map[string]string // token -> name
	names    map[string]bool   // claimed names
	rng      *rand.Rand
}

func newMemStore(seed int64) *memStore {
	return &memStore{
		battles:  map[string]*battle{},
		trainers: map[string]string{},
		names:    map[string]bool{},
		rng:      rand.New(rand.NewSource(seed)),
	}
}

func newToken() string {
	const max = 256 - (256 % len(idAlphabet))
	out := make([]byte, 0, 24)
	buf := make([]byte, 32)
	for len(out) < 24 {
		if _, err := crand.Read(buf); err != nil {
			panic("crypto/rand: " + err.Error())
		}
		for _, v := range buf {
			if int(v) >= max {
				continue
			}
			out = append(out, idAlphabet[int(v)%len(idAlphabet)])
			if len(out) == 24 {
				break
			}
		}
	}
	return string(out)
}

var errNameTaken = errors.New("name already taken")

func (m *memStore) registerTrainer(_ context.Context, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if m.names[strings.ToLower(name)] {
		return "", errNameTaken
	}
	token := newToken()
	m.names[strings.ToLower(name)] = true
	m.trainers[token] = name
	return token, nil
}

func (m *memStore) trainerByToken(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.trainers[token]
	if !ok {
		return "", errNoTrainer
	}
	return n, nil
}

func (m *memStore) create(_ context.Context, b *battle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	m.battles[b.id] = b
	// Cannot fail: the error exists for the Postgres implementation.
	return nil
}

func (m *memStore) get(_ context.Context, id string) (*api.Battle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.battles[id]
	if !ok {
		return nil, errNoBattle
	}
	return b.toAPI(), nil
}

func (m *memStore) rawForTest(id string) (*battle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.battles[id]
	return b, ok
}

func (m *memStore) update(_ context.Context, id string, fn func(*battle) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.battles[id]
	if !ok {
		return errNoBattle
	}
	if err := fn(b); err != nil {
		return err
	}
	b.touched = time.Now()
	return nil
}

func (m *memStore) waiting(_ context.Context) ([]api.WaitingBattle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()

	var out []api.WaitingBattle
	for _, b := range m.battles {
		if b.status == "waiting" {
			out = append(out, b.toWaiting())
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	// Cannot fail: the error exists for the Postgres implementation.
	return out, nil
}

func (b *battle) toWaiting() api.WaitingBattle {
	w := api.WaitingBattle{
		BattleId:  b.id,
		Trainer:   b.sides[0].trainer,
		CreatedAt: b.created,
	}
	for _, c := range b.sides[0].team {
		w.Team = append(w.Team, c.mon.Name)
	}
	return w
}

func (m *memStore) sweepLocked() {
	cutoff := time.Now().Add(-battleTTL)
	for id, b := range m.battles {
		if b.touched.Before(cutoff) {
			delete(m.battles, id)
		}
	}
}

const idAlphabet = "abcdefghijkmnopqrstuvwxyz23456789"

func randomID(rng roller, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idAlphabet[rng.Intn(len(idAlphabet))]
	}
	return string(b)
}

func (b *battle) takeTurn(token string, attackerIdx, moveIdx, targetIdx int, rng roller) error {
	if b.status == "finished" {
		return errBattleOver
	}
	if b.status != "active" {
		return errNotWaiting
	}

	me, opponent := b.sides[b.turn], b.sides[1-b.turn]
	if me.token != token {
		return errNotYourTurn
	}
	if attackerIdx < 0 || attackerIdx >= len(me.team) {
		return fmt.Errorf("%w: no attacker %d", errIllegalMove, attackerIdx)
	}
	if targetIdx < 0 || targetIdx >= len(opponent.team) {
		return fmt.Errorf("%w: no target %d", errIllegalMove, targetIdx)
	}

	attacker := me.team[attackerIdx]
	if attacker.fainted() {
		return fmt.Errorf("%w: %s has fainted", errIllegalMove, attacker.mon.Name)
	}
	if moveIdx < 0 || moveIdx >= len(attacker.mon.Moves) {
		return fmt.Errorf("%w: %s has no move %d", errIllegalMove, attacker.mon.Name, moveIdx)
	}
	target := opponent.team[targetIdx]
	if target.fainted() {
		return fmt.Errorf("%w: %s", errTargetFainted, target.mon.Name)
	}

	move := attacker.mon.Moves[moveIdx]
	if moveIdx == attacker.disabled {
		return fmt.Errorf("%w: %s is disabled", errIllegalMove, move.Name)
	}

	b.turnNumber++

	if attacker.confused {
		if rng.Float64() < 0.33 {
			self := move.Power / 2
			if self < 1 {
				self = 1
			}
			attacker.hp -= self
			if attacker.hp < 0 {
				attacker.hp = 0
			}
			b.log = append(b.log, api.BattleEvent{
				TurnNumber: b.turnNumber,
				Text:       fmt.Sprintf("%s is confused and hurt itself!", title(attacker.mon.Name)),
				Attacker:   api.NewOptString(attacker.mon.Name),
				Damage:     api.NewOptInt(self),
			})
			b.finishTurn(me, opponent, attacker)
			return nil
		}
		attacker.confused = false
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s snapped out of its confusion.", title(attacker.mon.Name)),
		})
	}

	if move.Power == 0 {
		b.resolveStatus(attacker, target, move, rng)
		b.finishTurn(me, opponent, target)
		return nil
	}

	dealt, mult := damage(attacker, target, move, rng)
	target.hp -= dealt
	if target.hp < 0 {
		target.hp = 0
	}

	b.appendEvent(attacker, target, move, dealt, mult)

	b.finishTurn(me, opponent, target)
	return nil
}

func (b *battle) finishTurn(me, opponent *side, hurt *combatant) {
	if hurt != nil && hurt.fainted() {
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s fainted!", title(hurt.mon.Name)),
			Fainted:    api.NewOptBool(true),
			Target:     api.NewOptString(hurt.mon.Name),
		})
	}

	switch {
	case opponent.defeated():
		b.status = "finished"
		b.winner = me.trainer
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s wins!", me.trainer),
		})
	case me.defeated():
		// Reachable through confusion: a Pokemon can knock itself out.
		b.status = "finished"
		b.winner = opponent.trainer
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s wins!", opponent.trainer),
		})
	default:
		b.turn = 1 - b.turn
	}

	// Bumped here AND by `version = version + 1` in UpdateBattle,
	// which reads as a double increment and is not one: pgStore.update
	// re-reads the row inside its transaction, so this value is
	// overwritten before it is ever written back. The SQL owns the
	// number for the Postgres path.
	//
	// It cannot simply be deleted, though - memStore has no SQL, so
	// this line is the only thing that moves the version there.
	// Removing it fails TestVersionAdvancesOnEveryChange, which is
	// how I found out.
	b.version++
	b.touched = time.Now()
}

func (b *battle) appendEvent(attacker, target *combatant, move api.Move, dealt int, mult float64) {
	text := fmt.Sprintf("%s used %s on %s", title(attacker.mon.Name), title(move.Name), title(target.mon.Name))
	if move.Power == 0 {
		text += ", but it does no damage"
	} else if effect := describeEffect(mult); effect != "" {
		text += ". " + effect + "!"
	} else {
		text += "!"
	}

	ev := api.BattleEvent{
		TurnNumber:    b.turnNumber,
		Text:          text,
		Attacker:      api.NewOptString(attacker.mon.Name),
		Target:        api.NewOptString(target.mon.Name),
		Move:          api.NewOptString(move.Name),
		Damage:        api.NewOptInt(dealt),
		Effectiveness: api.NewOptFloat64(mult),
	}
	b.log = append(b.log, ev)
}

// title renders a dataset name for display: "razor-wind" -> "Razor Wind".
func title(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func (b *battle) toAPI() *api.Battle {
	out := &api.Battle{
		ID:      b.id,
		Status:  api.BattleStatus(b.status),
		Version: b.version,
		Log:     b.log,
	}
	if b.status == "active" {
		out.Turn = api.NewOptString(b.sides[b.turn].trainer)
	}
	if b.winner != "" {
		out.Winner = api.NewOptString(b.winner)
	}
	for _, s := range b.sides {
		side := api.Side{Trainer: s.trainer}
		for _, c := range s.team {
			bp := api.BattlePokemon{
				Name:    c.mon.Name,
				Types:   c.mon.Types,
				Hp:      c.hp,
				MaxHp:   c.maxHP,
				Fainted: c.fainted(),
				Sprite:  c.mon.Sprite,
				Moves:   c.mon.Moves,
			}
			if c.stages != (stages{}) {
				bp.Stages = api.NewOptStatStages(api.StatStages{
					Attack:   api.NewOptInt(c.stages.attack),
					Defense:  api.NewOptInt(c.stages.defense),
					Speed:    api.NewOptInt(c.stages.speed),
					Accuracy: api.NewOptInt(c.stages.accuracy),
				})
			}
			if c.confused {
				bp.Confused = api.NewOptBool(true)
			}
			if c.disabled >= 0 {
				bp.DisabledMove = api.NewOptInt(c.disabled)
			}
			side.Team = append(side.Team, bp)
		}
		out.Sides = append(out.Sides, side)
	}
	return out
}

func (b *battle) resolveStatus(attacker, target *combatant, move api.Move, rng roller) {
	eff, known := statusMoves[move.Name]
	say := func(format string, a ...any) {
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf(format, a...),
			Attacker:   api.NewOptString(attacker.mon.Name),
			Move:       api.NewOptString(move.Name),
		})
	}

	if !known || eff == (effect{}) {
		say("%s used %s, but nothing happened.", title(attacker.mon.Name), title(move.Name))
		return
	}

	// Accuracy first: a move that misses does nothing at all.
	if !lands(eff.accuracy, attacker.stages.accuracy, rng.Float64()) {
		say("%s used %s, but it missed!", title(attacker.mon.Name), title(move.Name))
		return
	}

	switch {
	case eff.ohko:
		target.hp = 0
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text: fmt.Sprintf("%s used %s. It's a one-hit KO!",
				title(attacker.mon.Name), title(move.Name)),
			Attacker: api.NewOptString(attacker.mon.Name),
			Target:   api.NewOptString(target.mon.Name),
			Move:     api.NewOptString(move.Name),
			Damage:   api.NewOptInt(target.maxHP),
		})

	case eff.fixedDamage > 0:
		// Ignores types and stats entirely, which is the point of it.
		dealt := eff.fixedDamage
		if dealt > target.hp {
			dealt = target.hp
		}
		target.hp -= dealt
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text: fmt.Sprintf("%s used %s on %s!",
				title(attacker.mon.Name), title(move.Name), title(target.mon.Name)),
			Attacker: api.NewOptString(attacker.mon.Name),
			Target:   api.NewOptString(target.mon.Name),
			Move:     api.NewOptString(move.Name),
			Damage:   api.NewOptInt(dealt),
		})

	case eff.confuse:
		target.confused = true
		say("%s used %s. %s became confused!",
			title(attacker.mon.Name), title(move.Name), title(target.mon.Name))

	case eff.disable:
		// The move it would most likely use again: its strongest.
		best, power := -1, 0
		for i, m := range target.mon.Moves {
			if m.Power > power {
				best, power = i, m.Power
			}
		}
		if best < 0 {
			say("%s used %s, but there was nothing to disable.",
				title(attacker.mon.Name), title(move.Name))
			return
		}
		target.disabled = best
		say("%s used %s. %s's %s was disabled!",
			title(attacker.mon.Name), title(move.Name),
			title(target.mon.Name), title(target.mon.Moves[best].Name))

	case eff.stat != "":
		on := target
		if eff.self {
			on = attacker
		}
		applied := on.stages.add(eff.stat, eff.delta)
		if applied == 0 {
			say("%s used %s, but %s's %s cannot go any %s.",
				title(attacker.mon.Name), title(move.Name), title(on.mon.Name),
				eff.stat, map[bool]string{true: "higher", false: "lower"}[eff.delta > 0])
			return
		}
		say("%s used %s. %s's %s %s%s!",
			title(attacker.mon.Name), title(move.Name), title(on.mon.Name), eff.stat,
			map[bool]string{true: "rose", false: "fell"}[applied > 0],
			map[bool]string{true: " sharply", false: ""}[applied > 1 || applied < -1])

	default:
		say("%s used %s, but nothing happened.", title(attacker.mon.Name), title(move.Name))
	}
}
