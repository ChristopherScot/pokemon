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
	crand "crypto/rand"
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
	// Tuned after attack and defense entered the formula: the ratio
	// averages near 1 but swings from about 0.3 (Gengar into Onix) to
	// 3 (Machamp into a frail target), and the old 0.55 left the slow
	// end at nine turns for one Pokemon.
	damageScale = 0.85
)

var (
	// errNoBattle is update()'s "no such id", distinct from any error
	// the closure itself returns - a caller needs to tell "the battle
	// is gone" (404) from "that move is illegal" (409).
	errNoBattle      = errors.New("no such battle")
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

	// The game's base stats, which the damage formula needs.
	base baseStats

	// Stat changes accumulated this battle, from moves like
	// swords-dance and leer.
	stages stages

	// confused halves nothing directly - it gives a chance to hit
	// yourself instead, resolved at attack time.
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
		st := dex.stats[mon.ID]
		hp := maxHP(st.hp, battleIV, battleEV, battleLevel)
		team = append(team, &combatant{
			mon: mon, hp: hp, maxHP: hp, base: st, disabled: -1,
		})
	}
	return team, nil
}

// randomTeam picks three distinct Pokemon.
//
// Distinct because a team of three identical Pokemon is both a worse
// game and confusing to read: the board would show the same name three
// times with different HP, and a target index would be the only way to
// tell them apart.
// fillTeam completes a partial selection with random Pokemon.
//
// A client offering "pick the ones you care about" sends one or two
// names, and the rest are the server's to choose - which is what makes
// the empty slots in the web UI mean something rather than being a
// validation error waiting to happen.
func fillTeam(dex *pokedex, chosen []string, rng *rand.Rand) []string {
	if len(chosen) >= teamSize {
		return chosen
	}
	// Not already on the team: a random fill that duplicates a pick
	// gives two of the same Pokemon with different HP, which reads as a
	// bug rather than a roster.
	taken := make(map[string]bool, len(chosen))
	for _, n := range chosen {
		taken[strings.ToLower(strings.TrimSpace(n))] = true
	}

	out := append([]string(nil), chosen...)
	all := dex.list("", 0)
	for len(out) < teamSize && len(taken) < len(all) {
		pick := all[rng.Intn(len(all))]
		if taken[pick.Name] {
			continue
		}
		taken[pick.Name] = true
		out = append(out, pick.Name)
	}
	return out
}

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

	// Attack over defense, each scaled by its stat stages. This is what
	// makes swords-dance and harden mean something, and what makes a
	// Machamp (130 attack) hit harder than a Gengar (65) with the same
	// move.
	atk := float64(attacker.base.attack) * statMultiplier(attacker.stages.attack)
	def := float64(defender.base.defense) * statMultiplier(defender.stages.defense)

	base := float64(move.Power) * damageScale * mult * stab * (atk / def)
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

	// update applies fn to a battle and persists the result, as one
	// atomic step.
	//
	// The closure form is what makes this safe across replicas. `get`
	// then mutate works in one process because the pointer IS the
	// stored battle; against a database it is a read-modify-write with
	// a gap, and two pods resolving a turn at once would both read
	// version 5 and one would overwrite the other with no error.
	//
	// The Postgres store runs fn inside a SERIALIZABLE transaction and
	// retries on a serialization failure, so fn may run more than
	// once. It must therefore not have side effects outside the battle
	// it is given - no logging a turn, no sending a notification. The
	// memory store just takes its mutex.
	//
	// Returns errNoBattle if there is none with that id, otherwise
	// whatever fn returned.
	update(id string, fn func(*battle) error) error
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

// newToken mints a trainer token.
//
// crypto/rand, not the store's math/rand. randomID draws from a
// generator seeded with time.Now().UnixNano() at startup, so anyone who
// knows roughly when the process started can reproduce the whole token
// stream in order - the first token, the second, all of them. That did
// not matter while this ran on the LAN and the worst outcome was moving
// someone else's Pokemon. It matters on a public address, where the
// cost of getting it right is these fifteen lines.
//
// Battle IDs stay on math/rand deliberately: they are shared aloud
// between players, the alphabet skips 0/1/l to keep them readable, and
// knowing one grants nothing - the token is what authorises a move.
func newToken() string {
	// Rejection sampling, not modulo. 256 is not a multiple of 33, so
	// b[i]%33 would favour the first 25 characters of the alphabet -
	// a small bias, and free to avoid.
	const max = 256 - (256 % len(idAlphabet))
	out := make([]byte, 0, 24)
	buf := make([]byte, 32)
	for len(out) < 24 {
		if _, err := crand.Read(buf); err != nil {
			// crypto/rand does not fail on any platform this runs on,
			// and a token from a degraded source is worse than none.
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

func (m *memStore) registerTrainer(name string) (string, error) {
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

// update runs fn under the store's lock.
//
// In memory the pointer IS the stored battle, so there is nothing to
// write back - holding the lock for the duration is the whole job, and
// it is what stops two requests interleaving a turn. The Postgres
// store has real work to do here; see pgstore.update.
func (m *memStore) update(id string, fn func(*battle) error) error {
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
	if moveIdx == attacker.disabled {
		return fmt.Errorf("%w: %s is disabled", errIllegalMove, move.Name)
	}

	b.turnNumber++

	// Confusion resolves before the move: a confused Pokemon can hit
	// itself instead of attacking, which is the whole point of
	// supersonic.
	if attacker.confused {
		// A third of the time, and it wears off on the same roll that
		// spares it, so confusion is a real cost rather than permanent.
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

	// A status move does something other than damage, and every one in
	// the dataset now does what it does in the games.
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

// finishTurn records a faint, checks the win condition and hands over.
// Shared because a turn can end after damage, after a status move, or
// after a confused Pokemon hits itself.
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

	b.version++
	b.touched = time.Now()
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
			bp := api.BattlePokemon{
				Name:    c.mon.Name,
				Types:   c.mon.Types,
				Hp:      c.hp,
				MaxHp:   c.maxHP,
				Fainted: c.fainted(),
				Sprite:  c.mon.Sprite,
				Moves:   c.mon.Moves,
			}
			// Only when something has actually changed: a client that
			// draws every zero would put +0 badges on six Pokemon for
			// the whole battle.
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

// resolveStatus applies a power-0 move.
//
// Every one in the dataset does what it does in the games; a move with
// no entry says it had no effect, which is honest rather than silent.
func (b *battle) resolveStatus(attacker, target *combatant, move api.Move, rng *rand.Rand) {
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
			// Already at the cap. Saying so beats a turn that appears
			// to do nothing.
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
