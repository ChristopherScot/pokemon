package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ogen-go/ogen/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

var (
	requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		// route rather than operation: the status is only known at
		// the http layer, below ogen, where the operation id is not.
		// Latency keeps the operation label, which it can see.
		Help: "Requests by route, method and status.",
	}, []string{"route", "method", "status"})

	latency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Request latency by operation and method.",
		Buckets: prometheus.DefBuckets,
	}, []string{"operation", "method"})
)

type service struct {
	dex *pokedex

	battles store

	rng *lockedRand

	// ready backs the readiness probe. Nil without a database.
	ready func(context.Context) error
}

// GetReadyz is readiness, and unlike /healthz it does touch the
// database.
//
// Both probes pointed at a static OK, so a pod that had lost its
// database still reported itself Ready and kept taking traffic -
// every battle request 500ing while Kubernetes saw nothing wrong.
func (s service) GetReadyz(ctx context.Context) (api.GetReadyzRes, error) {
	if s.ready == nil {
		// No database: the in-memory store is always ready.
		return &api.Health{Status: api.HealthStatusOk}, nil
	}
	// Shorter than the probe's own 3s timeout, so a hung check fails
	// as unready rather than as a probe timeout.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		slog.Warn("readiness check failed", "err", err)
		return &api.Error{Message: "database unreachable"}, nil
	}
	return &api.Health{Status: api.HealthStatusOk}, nil
}

// GetHealthz is liveness, and is static on purpose: a liveness probe
// that fails on a database blip kills a pod that would have
// recovered, turning a brief outage into a crash loop.
func (service) GetHealthz(context.Context) (*api.Health, error) {
	return &api.Health{Status: api.HealthStatusOk}, nil
}

func (service) GetRoot(context.Context) (*api.Identity, error) {
	return &api.Identity{Service: "pokedex", Version: version}, nil
}

func (s service) ListPokemon(_ context.Context, params api.ListPokemonParams) (*api.PokemonList, error) {
	limit := 0
	if params.Limit.Set {
		limit = params.Limit.Value
	}
	found := s.dex.list(params.Type.Or(""), limit)
	return &api.PokemonList{Count: len(found), Pokemon: found}, nil
}

func (s service) GetPokemon(_ context.Context, params api.GetPokemonParams) (api.GetPokemonRes, error) {
	mon, ok := s.dex.get(params.Name)
	if !ok {
		return &api.Error{Message: "no pokemon named " + params.Name}, nil
	}
	return &mon, nil
}

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

func (s service) ListTypes(context.Context) (*api.TypeList, error) {
	return &api.TypeList{Types: s.dex.types()}, nil
}

// NewError is the last resort: anything a handler returns as a real
// error, rather than as a typed response, arrives here.
//
// The message is deliberately generic. Errors are wrapped with
// operation context on the way up - "inserting battle %s", "writing
// battle %s" - and a wrapped *pgconn.PgError stringifies with
// SQLSTATE, the constraint name and often the table and column. That
// was going straight into the 500 body of an internet-reachable
// service, which is free schema reconnaissance.
//
// The id is what keeps this debuggable: it appears in the response
// and in the log line, so a player can quote it and the real error is
// one Loki query away.
func (service) NewError(_ context.Context, err error) *api.ErrorStatusCode {
	id := newToken()[:8]
	slog.Error("handler failed", "err", err, "error_id", id)

	return &api.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   api.Error{Message: "internal error (" + id + ")"},
	}
}

// statusWriter remembers the code the handler wrote.
type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// countStatus records the status a request ACTUALLY returned.
//
// This has to live at the http layer rather than in ogen middleware.
// ogen hands middleware a typed response, and a handler returning a
// 401 returns it as a value with a nil error - so the previous
// version, which inferred `200, or 500 if err != nil`, labelled every
// 401, 404 and 409 as a 200. The metric could not show an auth bug, a
// flood of conflicts, or the mobile client's limit=200 requests that
// 400'd on every launch for days, and an alert on 5xx was silent
// through all of it.
//
// Deriving it from the response type's name would work but is a
// second copy of the generator's own switch, and it would drift. The
// ResponseWriter knows the real answer.
func countStatus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 200 by default: a handler that writes a body without
		// calling WriteHeader has implicitly sent one.
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			// Probes run every few seconds and would swamp the
			// counters without saying anything about the service.
			return
		}
		requests.WithLabelValues(routeOf(r), r.Method, strconv.Itoa(sw.code)).Inc()
	})
}

// routeOf is a low-cardinality label for the path.
//
// Raw paths carry battle ids, which would give the metric one series
// per battle - a textbook cardinality explosion.
func routeOf(r *http.Request) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		// Ids are the only variable segments here, and they are the
		// only lowercase-alphanumeric runs that are not a known noun.
		switch p {
		case "api", "battles", "trainers", "pokemon", "moves", "types",
			"waiting", "join", "turn", "healthz", "readyz", "":
		default:
			parts[i] = "{id}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func observe(req middleware.Request, next middleware.Next) (middleware.Response, error) {
	if req.OperationID == "getHealthz" {
		return next(req)
	}

	start := time.Now()
	resp, err := next(req)

	elapsed := time.Since(start)
	latency.WithLabelValues(req.OperationID, req.Raw.Method).Observe(elapsed.Seconds())

	slog.Info("request",
		"method", req.Raw.Method,
		"operation", req.OperationID,
		"duration_ms", float64(elapsed.Nanoseconds())/1e6)
	return resp, err
}

func handler() (http.Handler, error) {
	rngSeed := time.Now().UnixNano()

	var (
		dex   *pokedex
		store store
		// readyFn backs the readiness probe; nil without a database.
		readyFn func(context.Context) error
		err     error
	)
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		var pool *pgxpool.Pool
		// Connect, migrate and load. NOT seed - that is `pokedex seed`,
		// run when the data changes rather than when a pod restarts.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		pool, err = pgxpool.New(ctx, dsn)
		if err != nil {
			return nil, fmt.Errorf("connecting to the database: %w", err)
		}
		if err = migrate(ctx, pool); err != nil {
			return nil, fmt.Errorf("migrating: %w", err)
		}
		if dex, err = loadPokedexFromDB(ctx, pool); err != nil {
			return nil, fmt.Errorf("loading the pokedex: %w", err)
		}
		store = newPGStore(pool)
		readyFn = pool.Ping
		slog.Info("state is in postgres")
	} else {
		if dex, err = loadPokedex(); err != nil {
			return nil, err
		}
		store = newMemStore()
		slog.Info("state is in memory; set DATABASE_URL to use postgres")
	}

	svc := service{
		dex:     dex,
		battles: store,
		rng:     rngFor(rngSeed),
		ready:   readyFn,
	}

	srv, err := api.NewServer(svc, api.WithMiddleware(observe))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", countStatus(srv))
	return mux, nil
}
