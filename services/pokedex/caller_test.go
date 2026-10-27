package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The header is whatever the caller chose to send, so these are the
// shapes a caller can put in this service's logs.
func TestCallerField(t *testing.T) {
	for _, tc := range []struct {
		name string
		sent string
		want string
	}{
		{"a named caller", "pokedex-web", "pokedex-web"},
		{"no header at all", "", "unknown"},
		{"a newline would split the line in a log reader", "web\nlevel=ERROR", "weblevel=ERROR"},
		{"a carriage return does the same", "web\r\nx", "webx"},
		{"control characters go", "web\x00\x1b[31m", "web[31m"},
		{"a value of only control characters names nobody", "\n\r\x00", "unknown"},
		{"an overlong value is cut", strings.Repeat("a", 200), strings.Repeat("a", 64)},
		// Cutting on a byte boundary would halve a character and leave
		// invalid UTF-8 for the log encoder to deal with.
		{"and cut on a character, not a byte", strings.Repeat("é", 80), strings.Repeat("é", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://x/api/types", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.sent != "" {
				r.Header.Set(api.ClientNameHeader, tc.sent)
			}
			if got := callerField(r, api.ClientNameHeader); got != tc.want {
				t.Errorf("callerField(%q) = %q, want %q", tc.sent, got, tc.want)
			}
		})
	}
}
