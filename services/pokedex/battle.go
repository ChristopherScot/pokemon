package main

// The battle engine. Every rule lives here: damage, turn order, legality
// and the win condition. Clients post intents and render what comes
// back, so three client implementations cannot disagree about what
// happened.
//
// State is in memory behind the store interface. The service runs
// replicas: 1 and has no database, which makes that viable; the
// interface is what keeps the swap to a real store from being a rewrite.

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Tunables. Named rather than inline so the balance is in one place.
const (
	teamSize = 3

	// A battle nobody touches is collected. Without this an abandoned
	// match leaks its memory until the process restarts.
	battleTTL = 2 * time.Hour

	// Damage varies by +/-15%, as the games do, so identical matchups do
	// not play out identically every time.
	damageSpread = 0.15

	// Scales raw move power into the HP range below. Tuned so a neutral
	// hit takes roughly a fifth of a healthy Pokemon's HP: long enough to
	// make targeting choices matter, short enough to finish.
	damageScale = 0.55
)

var (
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

// Battle level, and the IV/EV the formula is evaluated at.
//
// Level 50 is the competitive standard and keeps the spread readable: at
// level 50 the formula reduces to base + 60, so the frailest Pokemon in
// this dataset (Diglett, base 10) has 70 HP and the bulkiest (Wigglytuff,
// base 140) has 200. Level 100 does not buy more variety - the flat
// +level+10 term grows with it, so the bulk ratio stays ~3x - it only
// makes battles longer.
//
// IVs and EVs are pinned to their minimums for now. They are parameters
// rather than constants folded into the formula so per-Pokemon values
// become a caller decision later rather than a rewrite.
const (
	battleLevel = 50
	battleIV    = 0
	battleEV    = 0
)

// maxHP is the Generation III+ HP formula:
//
//	HP = floor((2*Base + IV + floor(EV/4)) * Level / 100) + Level + 10
//
// Integer division in Go truncates toward zero, which equals floor for
// the non-negative inputs here - so no math.Floor and no float64
// round-trip.
//
// The multiply must come before the divide. Dividing first loses about
// 43%: base 45 at level 50 is 105 the right way round and 60 the wrong
// way, which is a difference no test of a single Pokemon would catch.
func maxHP(base, iv, ev, level int) int {
	if base <= 0 {
		// Shedinja is the one Pokemon whose HP the formula does not
		// describe - it is always 1. Not in this dataset, but loading
		// refuses a zero base anyway, so reaching here means a caller
		// passed something impossible.
		return 1
	}
	return (2*base+iv+ev/4)*level/100 + level + 10
}

// battle is the server's copy. The API type is derived from it, so
// internal bookkeeping - tokens, timestamps - never leaks to clients.
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

// newCombatants builds a team from names, rejecting anything the Pokedex
// does not know. Validating here rather than at the handler keeps the
// rule with the engine that depends on it.
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
		hp := maxHP(dex.baseHP[mon.ID], battleIV, battleEV, battleLevel)
		team = append(team, &combatant{mon: mon, hp: hp, maxHP: hp})
	}
	return team, nil
}

// randomTeam picks three distinct Pokemon.
//
// Distinct because a team of three identical Pokemon is both a worse
// game and confusing to read: the board would show the same name three
// times with different HP, and a target index would be the only way to
// tell them apart.
func randomTeam(dex *pokedex, rng *rand.Rand) []string {
	all := dex.list("", 0)
	if len(all) < teamSize {
		// Cannot happen with the shipped dataset, which is why this
		// returns what it has rather than an error: a caller cannot do
		// anything useful with "the pokedex is too small".
		names := make([]string, 0, len(all))
		for _, m := range all {
			names = append(names, m.Name)
		}
		return names
	}

	picked := make(map[int]bool, teamSize)
	team := make([]string, 0, teamSize)
	for len(team) < teamSize {
		i := rng.Intn(len(all))
		if picked[i] {
			continue
		}
		picked[i] = true
		team = append(team, all[i].Name)
	}
	return team
}

// damage resolves one attack. Returns the damage dealt and the type
// multiplier that produced it, so the caller can narrate the hit.
//
// rng is passed in so tests are deterministic: the engine never reaches
// for a package-level source.
func damage(attacker, defender *combatant, move api.Move, rng *rand.Rand) (int, float64) {
	mult := multiplier(move.Type, defender.mon.Types)
	if mult == 0 || move.Power == 0 {
		return 0, mult
	}

	// Same-type attack bonus, as the games have: a fire Pokemon hits
	// harder with a fire move.
	stab := 1.0
	for _, t := range attacker.mon.Types {
		if t == move.Type {
			stab = 1.5
			break
		}
	}

	base := float64(move.Power) * damageScale * mult * stab
	spread := 1 + (rng.Float64()*2-1)*damageSpread
	d := int(base * spread)
	if d < 1 {
		// A move that connects always does something; rounding to zero
		// reads as a bug to the player.
		d = 1
	}
	return d, mult
}

// store holds battles. An interface because replicas: 1 is what makes a
// map correct today, and that is a deployment detail rather than a
// design one.
type store interface {
	create(*battle)
	get(id string) (*battle, bool)
	waiting() []*battle
	trainerByToken(token string) (string, bool)
	registerTrainer(name string) (string, error)
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

var errNameTaken = errors.New("name already taken")

func (m *memStore) registerTrainer(name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if m.names[strings.ToLower(name)] {
		return "", errNameTaken
	}
	token := randomID(m.rng, 24)
	m.names[strings.ToLower(name)] = true
	m.trainers[token] = name
	return token, nil
}

func (m *memStore) trainerByToken(token string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.trainers[token]
	return n, ok
}

func (m *memStore) create(b *battle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	m.battles[b.id] = b
}

func (m *memStore) get(id string) (*battle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.battles[id]
	return b, ok
}

// waiting lists open invitations, newest first, so a lobby shows the
// freshest at the top.
func (m *memStore) waiting() []*battle {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	var out []*battle
	for _, b := range m.battles {
		if b.status == "waiting" {
			out = append(out, b)
		}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].created.After(out[i].created) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// sweepLocked drops battles nobody has touched within the TTL. Called on
// write rather than from a goroutine: there is no background work to
// supervise, and a store that is never written does not grow.
func (m *memStore) sweepLocked() {
	cutoff := time.Now().Add(-battleTTL)
	for id, b := range m.battles {
		if b.touched.Before(cutoff) {
			delete(m.battles, id)
		}
	}
}

const idAlphabet = "abcdefghijkmnopqrstuvwxyz23456789"

// randomID avoids 0/1/l to keep an id readable aloud, which matters when
// one player reads a battle id to another.
func randomID(rng *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = idAlphabet[rng.Intn(len(idAlphabet))]
	}
	return string(b)
}

// takeTurn resolves one attack and advances the battle.
//
// Every illegal case is an error rather than a silent no-op: a client
// bug that sends the wrong index should be visible, not look like lag.
func (b *battle) takeTurn(token string, attackerIdx, moveIdx, targetIdx int, rng *rand.Rand) error {
	if b.status == "finished" {
		return errBattleOver
	}
	if b.status != "active" {
		return errNotWaiting
	}

	me, opponent := b.sides[b.turn], b.sides[1-b.turn]
	if me.token != token {
		// Either it is the other player's turn, or a spectator is
		// posting. Both are "not your turn" from the caller's side.
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
	dealt, mult := damage(attacker, target, move, rng)
	target.hp -= dealt
	if target.hp < 0 {
		target.hp = 0
	}

	b.turnNumber++
	b.appendEvent(attacker, target, move, dealt, mult)

	if target.fainted() {
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s fainted!", title(target.mon.Name)),
			Fainted:    api.NewOptBool(true),
			Target:     api.NewOptString(target.mon.Name),
		})
	}

	if opponent.defeated() {
		b.status = "finished"
		b.winner = me.trainer
		b.log = append(b.log, api.BattleEvent{
			TurnNumber: b.turnNumber,
			Text:       fmt.Sprintf("%s wins!", me.trainer),
		})
	} else {
		b.turn = 1 - b.turn
	}

	b.version++
	b.touched = time.Now()
	return nil
}

// appendEvent narrates a hit. The server writes the prose so three
// clients tell the same story rather than each inventing wording.
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

// toAPI converts server state into the wire type. Tokens and timestamps
// stay on this side of the boundary.
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
			side.Team = append(side.Team, api.BattlePokemon{
				Name:    c.mon.Name,
				Types:   c.mon.Types,
				Hp:      c.hp,
				MaxHp:   c.maxHP,
				Fainted: c.fainted(),
				Sprite:  c.mon.Sprite,
				Moves:   c.mon.Moves,
			})
		}
		out.Sides = append(out.Sides, side)
	}
	return out
}
