package main

import (
	"context"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/julia-elikhis/romana-learning/internal/app"
	"github.com/julia-elikhis/romana-learning/internal/config"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("APP_MODE") != "local" {
		log.Fatal("Only APP_MODE=local is implemented; authentication is required before public deployment")
	}
	dbConfig, err := config.Database()
	if err != nil {
		log.Fatal(err)
	}
	db := stdlib.OpenDB(*dbConfig)
	defer db.Close()
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	for {
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		err = db.PingContext(probeCtx)
		probeCancel()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			cancel()
			log.Fatal("Database did not become ready within 60 seconds")
		case <-time.After(time.Second):
		}
	}
	if err = app.Migrate(ctx, db); err != nil {
		cancel()
		log.Fatal("Database migration failed")
	}
	cancel()
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	mux := http.NewServeMux()
	api := app.Server{DB: db}.Routes()
	mux.Handle("/api/", api)
	mux.Handle("/healthz", api)
	mux.Handle("/readyz", api)
	mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	log.Printf("Local practice app listening on %s", addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
