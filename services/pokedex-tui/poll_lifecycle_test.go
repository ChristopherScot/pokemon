package main

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// An attack's reply must not start a second poll chain.
//
// pollBattle and attack both return a battleMsg, and the handler
// re-armed pollBattle on every one it saw. So each turn added a
// permanent poll chain on top of the one already running - they never
// merged and never stopped. Ten turns meant eleven GETs a second
// against the API, growing for as long as the battle lasted.
func TestAnAttackReplyDoesNotStartAnotherPollChain(t *testing.T) {
	m := modelInBattle(t)

	// The reply to an attack: not polled. It may legitimately return a
	// tick to animate the damage - what it must not return is another
	// pollBattle. Counting the battleMsgs that come back distinguishes
	// them: a poll produces one, a tick produces a frameMsg.
	_, cmd := m.Update(battleMsg{id: "abc", battle: battleAt(2), polled: false})
	if polls := pollsIn(cmd); polls != 0 {
		t.Errorf("an attack reply armed %d poll(s), want 0 - re-arming here adds "+
			"a poll chain per turn that never stops", polls)
	}
}

// pollsIn runs a command and reports how many battleMsgs it yields,
// which is how many poll chains it just armed. A tick yields a
// frameMsg instead and does not count.
func pollsIn(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	msg := cmd()
	switch v := msg.(type) {
	case battleMsg:
		return 1
	case tea.BatchMsg:
		n := 0
		for _, c := range v {
			n += pollsIn(c)
		}
		return n
	}
	return 0
}

// The poll chain itself must keep going, or the battle freezes.
func TestAPollReplyKeepsPolling(t *testing.T) {
	m := modelInBattle(t)
	_, cmd := m.Update(battleMsg{id: "abc", battle: battleAt(2), polled: true})
	if polls := pollsIn(cmd); polls != 1 {
		t.Errorf("a poll reply armed %d poll(s), want exactly 1 - the chain must "+
			"re-arm itself or the battle stops updating, but only once", polls)
	}
}

// One failed poll is not a dead battle.
//
// maxPollMisses is 5, but err was set on the first miss and the view
// short-circuits on it - so miss 1 of 5 replaced the whole battle with
// "battle error, press esc to go back", and the remaining four were
// invisible to a player who had already been told it was broken.
func TestOneMissedPollKeepsTheBattleOnScreen(t *testing.T) {
	m := modelInBattle(t)
	out, _ := m.Update(battleMsg{id: "abc", err: errors.New("connection reset"), polled: true})
	got := out.(model)

	if got.battle == nil {
		t.Fatal("one missed poll dropped the battle entirely")
	}
	if got.battle.err != nil {
		t.Errorf("one missed poll set a fatal error (%v); the retry budget is %d, "+
			"so the battle should stay on screen", got.battle.err, maxPollMisses)
	}
	if got.battle.misses != 1 {
		t.Errorf("misses = %d, want 1", got.battle.misses)
	}
	if v := got.View().Content; !strings.Contains(strings.ToLower(v), "pikachu") {
		t.Errorf("the battle vanished from the view after one missed poll:\n%s", v)
	}
}

// And the budget still runs out.
func TestEnoughMissedPollsGivesUp(t *testing.T) {
	m := modelInBattle(t)
	var out tea.Model = m
	for i := 0; i < maxPollMisses; i++ {
		out, _ = out.(model).Update(battleMsg{id: "abc", err: errors.New("gone"), polled: true})
	}
	got := out.(model)
	if got.battle != nil {
		t.Errorf("after %d misses the battle is still open; the budget should be spent",
			maxPollMisses)
	}
	if got.screen != screenLobby {
		t.Errorf("after the budget is spent the screen is %v, want the lobby", got.screen)
	}
}

// modelInBattle is a model sitting in an active battle, built the way
// the existing battle tests build one.
func modelInBattle(t *testing.T) model {
	t.Helper()
	bs := testBattleState(t)
	bs.applyBattle(battleAt(1))
	return model{screen: screenBattle, battle: bs, bc: bs.client}
}

func battleAt(version int) *api.Battle {
	return twoSided(version,
		[]api.BattlePokemon{mon("pikachu", 95, 95, false)},
		[]api.BattlePokemon{mon("onix", 95, 95, false)})
}

// A poll for a battle you have left must not land on the one you
// joined next.
//
// tea.Cmd cannot be cancelled, so pressing esc does not stop the
// poll - its reply still arrives about a second later. The handler
// only checked that SOME battle was open, so joining a new battle
// inside that window applied the old battle's HP, teams and log to
// the new one, under the new battle's id. The foreign Version also
// overwrote `seen`, so genuine updates at lower versions stopped
// animating for the rest of the game.
func TestAReplyForAnAbandonedBattleIsDropped(t *testing.T) {
	m := modelInBattle(t) // in battle "abc"

	stale := twoSided(9,
		[]api.BattlePokemon{mon("snorlax", 10, 200, false)},
		[]api.BattlePokemon{mon("mew", 1, 100, false)})
	stale.ID = "gone"

	out, cmd := m.Update(battleMsg{id: "gone", polled: true, battle: stale})
	got := out.(model)

	if got.battle.battle != nil && got.battle.battle.ID != "abc" {
		t.Errorf("a reply for battle %q was applied to battle %q",
			"gone", got.battle.id)
	}
	if got.battle.seen == 9 {
		t.Error("the abandoned battle's version overwrote seen; real updates " +
			"at lower versions will stop animating")
	}
	if pollsIn(cmd) != 0 {
		t.Error("the orphaned chain re-armed itself; dropping the reply is what " +
			"lets it die")
	}
}

// Only one frame chain runs at a time.
//
// frameMsg re-arms itself while advance() reports movement, and a
// version bump used to schedule another tick with nothing checking
// whether one was already running. Both chains called advance(), so
// HP drained and floats expired at double speed - and faster still
// the more turns were played, because each bump could add another.
func TestASecondVersionBumpDoesNotDoubleTheFrameRate(t *testing.T) {
	m := modelInBattle(t)

	// First bump: starts animating.
	out, _ := m.Update(battleMsg{id: "abc", polled: true, battle: battleAt(2)})
	first := out.(model)
	if !first.animating {
		t.Fatal("the first version bump did not start a frame chain")
	}

	// Second bump while it is still running: must not start another.
	out2, cmd := first.Update(battleMsg{id: "abc", polled: true, battle: battleAt(3)})
	if ticksIn(cmd) != 0 {
		t.Error("a version bump during an animation started a second frame " +
			"chain; both call advance(), so everything animates at double speed")
	}
	if !out2.(model).animating {
		t.Error("the flag was cleared while a chain is still running")
	}
}

// ticksIn reports how many frame chains a command starts.
func ticksIn(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	switch v := cmd().(type) {
	case frameMsg:
		return 1
	case tea.BatchMsg:
		n := 0
		for _, c := range v {
			n += ticksIn(c)
		}
		return n
	}
	return 0
}
