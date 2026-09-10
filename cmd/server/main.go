package main

import (
	"context"
	"flag"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/julia-elikhis/romana-learning/internal/app"
	"github.com/julia-elikhis/romana-learning/internal/config"
	"github.com/julia-elikhis/romana-learning/internal/filestore"
	"github.com/julia-elikhis/romana-learning/internal/materials"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	grantAdminID := flag.Int64("grant-admin-github-id", 0, "Grant admin access to an existing GitHub account, then exit")
	flag.Parse()
	mode := os.Getenv("APP_MODE")
	if mode != "local" && mode != "public" {
		log.Fatal("APP_MODE must be local or public")
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
	if *grantAdminID != 0 {
		if err = app.GrantAdmin(ctx, db, *grantAdminID); err != nil {
			log.Fatal(err)
		}
		cancel()
		log.Print("Administrator role granted")
		return
	}
	cancel()
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	mux := http.NewServeMux()
	files, err := filestore.FromEnv(context.Background())
	if err != nil {
		log.Fatal("Could not initialize course storage; check COURSE_STORAGE settings and credentials")
	}
	if files != nil {
		defer files.Close()
	}
	generator, err := materials.APIFromEnv()
	if err != nil {
		log.Fatal("Invalid generation API configuration; check EXERCISE_API settings")
	}
	auth, err := app.GitHubAuthFromEnv()
	if err != nil {
		log.Fatal("Invalid GitHub authentication configuration; check GITHUB and APP_BASE_URL settings")
	}
	if mode == "public" && (auth == nil || !auth.PublicReady()) {
		log.Fatal("Public mode requires GitHub authentication and an HTTPS APP_BASE_URL")
	}
	api := app.Server{DB: db, Files: files, Generator: generator, Auth: auth}.Routes()
	mux.Handle("/api/", api)
	mux.Handle("/auth/", api)
	mux.Handle("/healthz", api)
	mux.Handle("/readyz", api)
	mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: materials.GenerationTimeout + 10*time.Second, IdleTimeout: 60 * time.Second}
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
