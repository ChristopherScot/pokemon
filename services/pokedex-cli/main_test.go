package main

import (
	"strings"
	"testing"
)

func TestVersionIsSet(t *testing.T) {
	if Version == "" {
		t.Error("Version is empty; it should default to \"dev\" and be set via ldflags at release")
	}
}

// The root command wires every subcommand; if one is dropped the CLI still
// compiles and the command silently disappears.
func TestRootHasExpectedCommands(t *testing.T) {
	want := map[string]bool{"update": false, "version": false}
	for _, c := range rootCmd().Commands() {
		name := strings.Fields(c.Use)[0]
		if _, ok := want[name]; ok {
			want[name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("root command is missing %q", name)
		}
	}
}

func TestIsNewer(t *testing.T) {
	for _, tc := range []struct {
		latest, current string
		want            bool
		why             string
	}{
		{"v1.2.0", "v1.1.0", true, "ordinary bump"},
		{"v1.1.0", "v1.2.0", false, "older is not newer"},
		{"v1.1.0", "v1.1.0", false, "equal is not newer"},
		{"v1.1.1", "v1.1", true, "an omitted patch reads as .0"},

		// Double digits. A comparison that reads components pairwise
		// gets these backwards and then reports "already up to date"
		// forever - a failure nobody notices, because it looks like
		// there simply being no new release.
		{"v1.10.0", "v1.2.0", true, "10 is newer than 2, not older"},
		{"v1.2.0", "v1.10.0", false, "and the reverse still holds"},
		{"v2.0.0", "v1.99.99", true, "major wins over any minor"},

		// A prerelease sorts before its release.
		{"v1.0.0", "v1.0.0-rc1", true, "release supersedes its rc"},
		{"v1.0.0-rc1", "v1.0.0", false, "an rc does not supersede the release"},

		// Unparseable means "not newer": leaving someone on a working
		// binary beats talking them into replacing it.
		{"not-a-version", "v1.0.0", false, "unparseable latest"},
		{"v1.0.0", "garbage", false, "unparseable current"},
		{"dev", "v1.0.0", false, "a dev build is not a version"},
	} {
		if got := isNewer(tc.latest, tc.current); got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v (%s)",
				tc.latest, tc.current, got, tc.want, tc.why)
		}
	}
}

// The caller hands isNewer whatever the tag and the ldflags say, which
// may or may not carry a v. semver.IsValid rejects the unprefixed
// spelling, and an invalid version means "not newer" - so if this
// normalisation broke, update would go silent rather than fail loudly.
func TestEnsureVAcceptsEitherSpelling(t *testing.T) {
	for _, in := range []string{"1.2.3", "v1.2.3"} {
		if got := ensureV(in); got != "v1.2.3" {
			t.Errorf("ensureV(%q) = %q, want %q", in, got, "v1.2.3")
		}
	}
	if !isNewer(ensureV("1.10.0"), ensureV("v1.2.0")) {
		t.Error("a mixed-spelling comparison did not reach semver intact")
	}
}

// TAB must complete every team slot, not just the first.
//
// makeCompleter deliberately stops after one argument, which is right
// for `show pikachu` and wrong for a team of three: `open pikachu <TAB>`
// went silent and sent the player to look the next two names up
// somewhere else. That is the CLI's version of the bug the web had -
// the names existed, the completer existed, and the two were never
// connected on the commands that take a team.
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

// A name already on the team is not offered again: the server rejects a
// duplicate team, so suggesting one is suggesting a mistake.
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

// join's first argument is the battle id, so the team starts one later.
func TestTeamCompleterSkipsTheBattleID(t *testing.T) {
	names := func() ([]string, error) { return []string{"pikachu", "onix", "gengar"}, nil }
	complete := makeTeamCompleter(names, 1, 3)

	// No id typed yet: nothing to complete, and definitely not a Pokemon.
	if got, _ := complete(nil, nil, ""); len(got) != 0 {
		t.Errorf("offered %v in the battle-id position", got)
	}
	// Id present: the first team slot is open.
	if got, _ := complete(nil, []string{"abc123"}, ""); len(got) != 3 {
		t.Errorf("completions after the id = %v, want all three", got)
	}
	// Id plus a full team: done.
	if got, _ := complete(nil, []string{"abc123", "pikachu", "onix", "gengar"}, ""); len(got) != 0 {
		t.Errorf("offered %v with a full team", got)
	}
}
