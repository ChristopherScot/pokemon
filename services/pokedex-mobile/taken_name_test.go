package main

import (
	"errors"
	"strings"
	"testing"
)

// The generic error tail truncates at 100 bytes and drops "pick another" off the end of a taken-name reply.
func TestATakenNameSaysSo(t *testing.T) {
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

func TestOtherErrorsAreStillTranslated(t *testing.T) {
	for _, tc := range []struct{ in, wantNot string }{
		{"context deadline exceeded", "context deadline"},
		{"dial tcp 10.0.0.1:443: connect: connection refused", "dial tcp"},
		{"unexpected status 401", "401"},
	} {
		got := statusFor(errors.New(tc.in))
		if strings.Contains(got, tc.wantNot) {
			t.Errorf("statusFor(%q) = %q; leaked the raw error", tc.in, got)
		}
	}
}
