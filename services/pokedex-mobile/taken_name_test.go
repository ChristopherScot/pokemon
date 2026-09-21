package main

import (
	"errors"
	"strings"
	"testing"
)

// "that name is taken" reaches the player as itself.
//
// The server answers a duplicate registration with a message that is
// already written for a player: `"ash" is taken, pick another`. Without
// a case for it, statusFor fell through to the generic tail, which
// trims to a rune boundary and prefixes nothing - so the one error a
// player is most likely to hit arrived as truncated noise, and the
// advice it carries ("pick another") was the part most likely to be
// cut.
func TestATakenNameSaysSo(t *testing.T) {
	// A long name is the case that matters. The generic tail already
	// returns the message, so a short one passes either way - but it
	// truncates at 100 bytes, and "pick another" is at the END, so the
	// advice is exactly what a long name loses.
	long := strings.Repeat("a", 95)
	err := errors.New(`"` + long + `" is taken, pick another`)
	got := statusFor(err)

	if !strings.Contains(got, "is taken") {
		t.Errorf("statusFor(long name) = %q; a player needs to be told the name is taken", got)
	}
	if !strings.Contains(got, "pick another") {
		t.Errorf("statusFor(long name) = %q; the advice is at the end of the message, "+
			"so truncation drops exactly the half that tells them what to do", got)
	}
	if strings.Contains(got, "\u2026") {
		t.Errorf("statusFor(long name) = %q; it was truncated", got)
	}
}

// The passthrough must not swallow errors that need translating - a
// case ordered wrongly would turn a timeout into raw Go text.
func TestOtherErrorsAreStillTranslated(t *testing.T) {
	for _, tc := range []struct{ in, wantNot string }{
		{"context deadline exceeded", "context deadline"},
		{"dial tcp 10.0.0.1:443: connect: connection refused", "dial tcp"},
		{"unexpected status 401", "401"},
	} {
		got := statusFor(errors.New(tc.in))
		if strings.Contains(got, tc.wantNot) {
			t.Errorf("statusFor(%q) = %q; it leaked the raw error instead of "+
				"translating it for a player", tc.in, got)
		}
	}
}
