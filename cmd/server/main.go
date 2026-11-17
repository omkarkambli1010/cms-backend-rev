package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cms-backend/internal/config"
	"cms-backend/internal/db"
	"cms-backend/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The pool connects lazily, so a database that is down at startup does
	// not stop the service: requests answer 503 until it is back.
	database, err := db.New(ctx, cfg.DatabaseURL, cfg.PoolMax, cfg.StatementTimeout)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer database.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, database),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	go func() {
		log.Printf("cms-backend listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
