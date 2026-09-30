package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func TestTeamCompleterOffersEverySlot(t *testing.T) {
	names := func() ([]string, error) { return []string{"pikachu", "onix", "gengar"}, nil }
	complete := makeTeamCompleter(names, 0, 3)

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"first slot", nil, 3},
		{"second slot", []string{"pikachu"}, 2},
		{"third slot", []string{"pikachu", "onix"}, 1},
		{"team is full", []string{"pikachu", "onix", "gengar"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := complete(nil, tc.args, "")
			if len(got) != tc.want {
				t.Errorf("completions = %v (%d), want %d", got, len(got), tc.want)
			}
		})
	}
}

func TestTeamCompleterSkipsWhatIsAlreadyPicked(t *testing.T) {
	names := func() ([]string, error) { return []string{"pikachu", "onix", "gengar"}, nil }
	got, _ := makeTeamCompleter(names, 0, 3)(nil, []string{"ONIX "}, "")
	for _, n := range got {
		if strings.EqualFold(strings.TrimSpace(n), "onix") {
			t.Errorf("offered %q, which is already on the team", n)
		}
	}
	if len(got) != 2 {
		t.Errorf("completions = %v, want the two not yet picked", got)
	}
}

func TestTeamCompleterSkipsTheBattleID(t *testing.T) {
	names := func() ([]string, error) { return []string{"pikachu", "onix", "gengar"}, nil }
	complete := makeTeamCompleter(names, 1, 3)

	if got, _ := complete(nil, nil, ""); len(got) != 0 {
		t.Errorf("offered %v in the battle-id position", got)
	}
	if got, _ := complete(nil, []string{"abc123"}, ""); len(got) != 3 {
		t.Errorf("completions after the id = %v, want all three", got)
	}
	if got, _ := complete(nil, []string{"abc123", "pikachu", "onix", "gengar"}, ""); len(got) != 0 {
		t.Errorf("offered %v with a full team", got)
	}
}

func TestSpectatingAnActiveBattleDoesNotClaimItIsWaiting(t *testing.T) {
	mon := api.BattlePokemon{Name: "abra", Hp: 10, MaxHp: 10, Types: []string{"psychic"}}
	b := &api.Battle{
		ID:     "abc123",
		Status: api.BattleStatusActive,
		Turn:   api.NewOptString("ash"),
		Sides: []api.Side{
			{Trainer: "ash", Team: []api.BattlePokemon{mon}},
			{Trainer: "misty", Team: []api.BattlePokemon{mon}},
		},
	}

	c := &battleclient.Client{Name: "brock"}
	out := captureStdout(t, func() { printBattle(c, b) })

	if strings.Contains(out, "waiting for an opponent") {
		t.Errorf("an active two-sided battle was described as waiting:\n%s", out)
	}
	if !strings.Contains(out, "ash") || !strings.Contains(out, "misty") {
		t.Errorf("a spectator should see both trainers named:\n%s", out)
	}
	if strings.Contains(out, "\nyou\n") {
		t.Errorf("a spectator was told one of the teams was theirs:\n%s", out)
	}
}

func TestAOneSidedBattleStillReadsAsWaiting(t *testing.T) {
	b := &api.Battle{
		ID:     "abc123",
		Status: api.BattleStatusWaiting,
		Sides:  []api.Side{{Trainer: "ash", Team: []api.BattlePokemon{{Name: "abra"}}}},
	}
	c := &battleclient.Client{Name: "brock"}
	out := captureStdout(t, func() { printBattle(c, b) })
	if !strings.Contains(out, "waiting for an opponent") {
		t.Errorf("a one-sided battle should read as waiting:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()
	w.Close()
	var sb strings.Builder
	if _, err := io.Copy(&sb, r); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}
