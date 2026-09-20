package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A 500 body must not carry internal detail. Errors are wrapped with
// operation context on the way up, and a wrapped pgconn error
// stringifies with SQLSTATE, the constraint name and often the table
// and column - which was going straight to an internet-reachable
// client.
func TestInternalErrorsDoNotReachTheClient(t *testing.T) {
	internal := fmt.Errorf("writing battle abc123: %w",
		errors.New(`ERROR: duplicate key value violates unique constraint "trainers_name_key" (SQLSTATE 23505)`))

	res := service{}.NewError(context.Background(), internal)
	body := res.Response.Message

	for _, leak := range []string{"SQLSTATE", "constraint", "trainers_name_key", "battle abc123"} {
		if strings.Contains(body, leak) {
			t.Errorf("the 500 body %q leaks %q", body, leak)
		}
	}
	// It still has to be actionable: the id is what ties the response
	// to the log line.
	if !strings.Contains(body, "internal error (") {
		t.Errorf("body %q carries no correlation id", body)
	}
}
