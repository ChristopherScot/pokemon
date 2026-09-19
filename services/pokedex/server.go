package main

// Everything in this file knows the API exists. main.go does not: it
// calls handler() and starts whatever comes back.
//
// That is the seam. A service whose shape the spec cannot express - a
// reverse proxy, a websocket endpoint, anything with a catch-all route -
// replaces THIS file with a hand-written handler(), and keeps main.go,
// api/client.go, the Dockerfile, CI and every manifest unchanged. The
// generated api/ package stays useful either way: its types and its
// client are worth having even when its router is not.
//
// A replacement must:
//
//   - keep the signature `func handler() (http.Handler, error)`. The
//     error is not dead weight: api.NewServer cannot currently fail, but
//     hand-written setup - parsing a proxy target, loading a cert - can,
//     and main.go already exits with the message rather than panicking.
//   - serve /metrics itself. deployment.yaml annotates this pod to be
//     scraped there; without it the annotation points at a 404.
//   - record to `requests` and `latency` below, or delete them. They are
//     registered at init, so a handler that ignores them still exports
//     two permanently-empty metric families - and a dashboard built on
//     http_requests_total flatlines instead of disappearing, which is
//     harder to notice.

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ogen-go/ogen/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Request metrics, labelled by the spec's operation ID. ogen guarantees
// it is non-empty and it comes from the spec, so the label set is bounded
// by the API rather than by whatever a caller puts in the path - which
// would be unbounded cardinality, and would write request paths into
// metric labels.
var (
	requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Requests by operation, method and status.",
	}, []string{"operation", "method", "status"})

	latency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Request latency by operation and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation", "method"})
)

// service implements the generated api.Handler. Adding an operation to
// openapi.yml and regenerating makes this fail to compile until the
// handler exists - that is the point of generating from the spec.
type service struct {
	dex *pokedex

	// Battle state. In memory because this runs replicas: 1 with no
	// database; behind an interface so that stops being true without a
	// rewrite.
	battles store

	// One source for damage rolls, so a test can seed it and get the
	// same battle twice.
	rng *rand.Rand
}

func (service) GetHealthz(context.Context) (*api.Health, error) {
	return &api.Health{Status: api.HealthStatusOk}, nil
}

func (service) GetRoot(context.Context) (*api.Identity, error) {
	return &api.Identity{Service: "pokedex", Version: version}, nil
}

// ListPokemon returns the Pokedex, optionally filtered by type.
//
// An unknown type is an empty list rather than a 404: the question "which
// fire Pokemon do you have" has a valid answer of "none", and a filter
// that 404s makes every caller special-case a result they can already
// handle.
func (s service) ListPokemon(_ context.Context, params api.ListPokemonParams) (*api.PokemonList, error) {
	// The spec's default of 100 only applies when the caller omits the
	// parameter; ogen hands us the zero value otherwise, and 0 here means
	// "no limit" rather than "return nothing".
	limit := 0
	if params.Limit.Set {
		limit = params.Limit.Value
	}
	found := s.dex.list(params.Type.Or(""), limit)
	return &api.PokemonList{Count: len(found), Pokemon: found}, nil
}

// GetPokemon returns one Pokemon, or the spec's 404.
//
// Returning *api.Error is what makes it a 404 rather than a 500: the
// generated encoder maps that type to the status the spec declares, so
// the handler never writes a status code itself.
func (s service) GetPokemon(_ context.Context, params api.GetPokemonParams) (api.GetPokemonRes, error) {
	mon, ok := s.dex.get(params.Name)
	if !ok {
		return &api.Error{Message: "no pokemon named " + params.Name}, nil
	}
	return &mon, nil
}

// ListPokemonMoves returns everything a Pokemon can learn.
//
// Its own endpoint rather than a field: Bulbasaur learns 86 moves and
// Mewtwo 167, so putting the full records on api.Pokemon would make
// every list response an order of magnitude larger for a field most
// callers do not read.
func (s service) ListPokemonMoves(_ context.Context, params api.ListPokemonMovesParams) (api.ListPokemonMovesRes, error) {
	moves, ok := s.dex.learnableFor(params.Name)
	if !ok {
		return &api.Error{Message: "no pokemon named " + params.Name}, nil
	}
	return &api.MoveList{Count: len(moves), Moves: moves}, nil
}

// ListMoves returns the whole move catalogue.
func (s service) ListMoves(context.Context) (*api.MoveList, error) {
	moves := s.dex.allMoves()
	return &api.MoveList{Count: len(moves), Moves: moves}, nil
}

// GetMove returns one move by its hyphenated name, or the spec's 404.
func (s service) GetMove(_ context.Context, params api.GetMoveParams) (api.GetMoveRes, error) {
	mv, ok := s.dex.move(params.Name)
	if !ok {
		return &api.Error{Message: "no move named " + params.Name}, nil
	}
	return &mv, nil
}

// ListTypes reports every type in the Pokedex with a count, so a client
// can build a filter UI without downloading all 100 Pokemon first.
func (s service) ListTypes(context.Context) (*api.TypeList, error) {
	return &api.TypeList{Types: s.dex.types()}, nil
}

// NewError renders a handler's error as the spec's Error schema, so a
// failure is a documented response rather than an empty 500.
//
// It also logs it. ogen returns the error to the caller but does not log
// it, so without this a 500 leaves nothing on the server saying what
// happened - and the caller only sees the message.
//
// What identifies the failing line is wrapping at the point it happens:
//
//	return nil, fmt.Errorf("fetching user %s: %w", id, err)
func (service) NewError(_ context.Context, err error) *api.ErrorStatusCode {
	slog.Error("handler failed", "err", err)

	return &api.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   api.Error{Message: err.Error()},
	}
}

// observe logs and measures every request that reaches a known operation.
// A request to an unrouted path is rejected by the generated router before
// this runs, so an arbitrary URL can never become a label or a log field.
func observe(req middleware.Request, next middleware.Next) (middleware.Response, error) {
	// The kubelet probes /healthz every few seconds; logging that buries
	// real traffic and inflates the counters with self-observation.
	if req.OperationID == "getHealthz" {
		return next(req)
	}

	start := time.Now()
	resp, err := next(req)

	status := strconv.Itoa(http.StatusOK)
	if err != nil {
		status = strconv.Itoa(http.StatusInternalServerError)
	}
	elapsed := time.Since(start)

	requests.WithLabelValues(req.OperationID, req.Raw.Method, status).Inc()
	latency.WithLabelValues(req.OperationID, req.Raw.Method).Observe(elapsed.Seconds())

	slog.Info("request",
		"method", req.Raw.Method,
		"operation", req.OperationID,
		"status", status,
		// Float milliseconds, not elapsed.Milliseconds(): that truncates to
		// an integer, and a handler serving from memory finishes well under
		// 1ms. Every line logged duration_ms=0, so the field was present and
		// carried nothing - a latency panel built on it reads flat zero
		// whether the service is fast or dying.
		"duration_ms", float64(elapsed.Nanoseconds())/1e6)
	return resp, err
}

// handler mounts the generated API alongside /metrics, which is not in
// the spec because it is the platform's endpoint rather than this
// service's API.
func handler() (http.Handler, error) {
	// Seeded from the clock: battles should not replay identically
	// across restarts. Tests construct the service directly with a fixed
	// seed instead.
	rngSeed := time.Now().UnixNano()

	// DATABASE_URL decides where state lives.
	//
	// Set, which is how the Deployment runs: migrate, seed the
	// reference data, and serve from Postgres. State survives a
	// restart and several replicas agree about it.
	//
	// Unset, which is how `go run .` and the tests run: the embedded
	// file and a map behind a mutex. No database to stand up to work
	// on the battle logic, and the behaviour is the same for one
	// replica.
	var (
		dex   *pokedex
		store store
		err   error
	)
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		var pool *pgxpool.Pool
		// Bounded: a database that never answers should fail startup
		// with a message, not hang the pod until the kubelet gives up.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool, err = pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("connecting to the database: %w", err)
		}
		if err = migrate(ctx, pool); err != nil {
			return nil, fmt.Errorf("migrating: %w", err)
		}
		if err = seed(ctx, pool); err != nil {
			return nil, fmt.Errorf("seeding: %w", err)
		}
		if dex, err = loadPokedexFromDB(ctx, pool); err != nil {
			return nil, fmt.Errorf("loading the pokedex: %w", err)
		}
		store = newPGStore(pool, rngSeed)
		slog.Info("state is in postgres")
	} else {
		// Loaded here rather than in a package-level var: a bad
		// dataset should fail startup with a message main can print,
		// not panic during package init where the error has nowhere
		// to go.
		if dex, err = loadPokedex(); err != nil {
			return nil, err
		}
		store = newMemStore(rngSeed)
		slog.Info("state is in memory; set DATABASE_URL to use postgres")
	}

	svc := service{
		dex:     dex,
		battles: store,
		rng:     rngFor(rngSeed),
	}

	srv, err := api.NewServer(svc, api.WithMiddleware(observe))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	// The deployment annotates this pod for scraping here; without this
	// handler those annotations point at a 404.
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", srv)
	return mux, nil
}
