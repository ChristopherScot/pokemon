package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

// What a player is told when their identity is the problem, and what
// they are told when it is not.
func TestTheIdentityHintSaysWhatToDo(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   error
		want  []string
		quiet bool
	}{
		{
			name: "no trainer registered",
			err:  battleclient.ErrNoIdentity,
			want: []string{"no trainer registered", "register <your-name>"},
		},
		{
			name: "the server does not know this trainer",
			err:  battleclient.ErrStaleIdentity,
			want: []string{"does not recognise", "register <your-name>"},
		},
		{
			name:  "an unrelated failure says nothing about identity",
			err:   errors.New("connection refused"),
			quiet: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			identityHintTo(&out, c.err)

			if c.quiet {
				if out.Len() != 0 {
					t.Errorf("an unrelated error produced identity advice: %q", out.String())
				}
				return
			}
			for _, want := range c.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("advice does not mention %q: %q", want, out.String())
				}
			}
		})
	}
}

// The advice must not claim a restart loses trainers: they are rows in
// Postgres and survive one. A token stops working for reasons the
// client cannot know, so the text says only what is observed.
func TestTheHintDoesNotBlameARestart(t *testing.T) {
	var out bytes.Buffer
	identityHintTo(&out, battleclient.ErrStaleIdentity)
	if strings.Contains(out.String(), "restart") {
		t.Errorf("the advice blames a restart, which does not lose trainers: %q", out.String())
	}
}
