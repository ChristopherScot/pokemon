package main

import (
	"testing"
	"time"
)

// A roller that always returns 0 is a legal implementation of the
// interface - fixedRoll in pgstore_roundtrip_test.go is one. The old
// draw-until-new loops never terminated for it, inside a request
// handler with no ctx check.
type alwaysZero struct{}

func (alwaysZero) Intn(int) int     { return 0 }
func (alwaysZero) Float64() float64 { return 0 }

func TestTeamBuildingTerminatesForAnyRoller(t *testing.T) {
	dex, err := loadPokedex()
	if err != nil {
		t.Fatalf("pokedex: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fillTeam(dex, nil, alwaysZero{})
		randomTeam(dex, alwaysZero{})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("team building did not terminate; this hangs a request handler")
	}
}
