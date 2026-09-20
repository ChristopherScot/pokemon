package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", "pokedex",
		"team", "me-myself-and-i",
		"version", version,
	))

	// `pokedex seed` loads the reference data and exits. It is a
	// separate command because the data changes when the binary does,
	// not when a pod restarts - seeding on every boot rewrote ~9,000
	// rows that were already correct, and crashlooped the pod when the
	// cluster was slow enough for that to miss its deadline.
	if len(os.Args) > 1 && os.Args[1] == "seed" {
		if err := runSeed(); err != nil {
			slog.Error("seeding", "err", err)
			os.Exit(1)
		}
		return
	}

	h, err := handler()
	if err != nil {
		slog.Error("building handler", "err", err)
		os.Exit(1)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	srv := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		slog.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			slog.Error("shutdown", "err", err)
		}
		close(idle)
	}()

	slog.Info("started", "addr", ":"+port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
	<-idle
}
