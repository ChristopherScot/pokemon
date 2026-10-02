package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A wrapped pgconn error stringifies with SQLSTATE, constraint and
// table names, which must not reach the client.
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
	// The correlation id ties the 500 to its Loki entry.
	if !strings.Contains(body, "internal error (") {
		t.Errorf("body %q carries no correlation id", body)
	}
}
