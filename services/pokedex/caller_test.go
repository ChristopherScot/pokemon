package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/ogen-go/ogen/middleware"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The header is whatever the caller chose to send, so these are the
// shapes a caller can put in this service's logs.
func TestCallerField(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  bool
		sent string
		want string
	}{
		{name: "a named caller", set: true, sent: "pokedex-web", want: "pokedex-web"},
		{name: "no header at all", want: "unknown"},
		// A hand-rolled caller can send the header empty; the generated
		// client omits it. Both mean the same thing here.
		{name: "the header sent empty", set: true, want: "unknown"},
		{name: "a newline would split the line in a log reader", set: true,
			sent: "web\nlevel=ERROR", want: "weblevel=ERROR"},
		{name: "a carriage return does the same", set: true, sent: "web\r\nx", want: "webx"},
		{name: "control characters go", set: true, sent: "web\x00\x1b[31m", want: "web[31m"},
		// U+009B is the escape-sequence introducer, invisible and not
		// below U+0020.
		{name: "and so do the C1 controls", set: true, sent: "web\u0085\u009bX", want: "webX"},
		{name: "a value of only control characters names nobody", set: true,
			sent: "\n\r\x00", want: "unknown"},
		// strings.Map gives U+FFFD for free; pinned so a byte-wise
		// filter cannot quietly pass invalid UTF-8 to the encoder.
		{name: "invalid utf-8 is replaced, not passed through", set: true,
			sent: "web\xff\xfebad", want: "web\ufffd\ufffdbad"},
		{name: "an overlong value is cut", set: true,
			sent: strings.Repeat("a", 200), want: strings.Repeat("a", maxCallerLen)},
		{name: "and cut on a character, not a byte", set: true,
			sent: strings.Repeat("é", 80), want: strings.Repeat("é", maxCallerLen)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "http://x/types", nil)
			if err != nil {
				t.Fatal(err)
			}
			if tc.set {
				r.Header.Set(api.ClientNameHeader, tc.sent)
			}
			if got := callerField(r, api.ClientNameHeader); got != tc.want {
				t.Errorf("callerField(%q) = %q, want %q", tc.sent, got, tc.want)
			}
		})
	}
}

// The sanitiser being right is not the feature: the feature is that a
// name a caller sent reaches the log. Deleting the two fields from
// observe leaves every case above passing, so this drives the
// middleware and reads what came out.
//
// Asserts the VALUE, not the key. What the field is called is for
// whoever greps it, and renaming it should not fail a test.
func TestTheCallersNameReachesTheLog(t *testing.T) {
	var out bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	raw, err := http.NewRequest(http.MethodGet, "http://x/types", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw.Header.Set(api.ClientNameHeader, "pokedex-web")
	raw.Header.Set(api.ClientVersionHeader, "0.9.0")

	if _, err := observe(
		middleware.Request{Raw: raw, OperationID: "listTypes"},
		func(middleware.Request) (middleware.Response, error) {
			return middleware.Response{}, nil
		},
	); err != nil {
		t.Fatal(err)
	}

	var line map[string]any
	if err := json.Unmarshal(out.Bytes(), &line); err != nil {
		t.Fatalf("the log line did not parse: %v (%q)", err, out.String())
	}
	for _, want := range []string{"pokedex-web", "0.9.0"} {
		found := false
		for _, v := range line {
			if s, ok := v.(string); ok && s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("no field carried %q; the log line was %s", want, out.String())
		}
	}
}
