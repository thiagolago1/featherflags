package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thiagolago1/featherflags/api/internal/server"
	"github.com/thiagolago1/featherflags/api/internal/store"
)

func main() {
	databaseURL := mustEnv("DATABASE_URL")
	adminToken := mustEnv("ADMIN_TOKEN")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx := context.Background()
	st, err := store.New(ctx, databaseURL)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		if err := st.EnableCache(ctx, redisURL); err != nil {
			// Cache is an optimization: log and keep serving from Postgres.
			log.Printf("redis cache disabled: %v", err)
		} else {
			log.Printf("redis cache enabled")
		}
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           server.New(st, adminToken),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("featherflags listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var %s", key)
	}
	return v
}
