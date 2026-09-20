package main

import (
	"context"
	"fmt"
	"log/slog"
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

type service struct {
	dex *pokedex

	battles store

	rng *lockedRand
}

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

func (service) NewError(_ context.Context, err error) *api.ErrorStatusCode {
	slog.Error("handler failed", "err", err)

	return &api.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   api.Error{Message: err.Error()},
	}
}

func observe(req middleware.Request, next middleware.Next) (middleware.Response, error) {
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
		"duration_ms", float64(elapsed.Nanoseconds())/1e6)
	return resp, err
}

func handler() (http.Handler, error) {
	rngSeed := time.Now().UnixNano()

	var (
		dex   *pokedex
		store store
		err   error
	)
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		var pool *pgxpool.Pool
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
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", srv)
	return mux, nil
}
