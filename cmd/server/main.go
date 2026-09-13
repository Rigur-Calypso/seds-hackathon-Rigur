// Command server is the deployed Battlesnake. It must never import
// BattlesnakeOfficial/rules (AGPL, CLAUDE.md §8); CI enforces this.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	seds "github.com/Rigur-Calypso/seds-hackathon-Rigur"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/config"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/decide"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/opponent"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/search"
	"github.com/Rigur-Calypso/seds-hackathon-Rigur/internal/server"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server.TuneRuntime(log)

	profiles, err := config.Load(seds.ConfigFS)
	if err != nil {
		log.Error("config_load_failed_using_defaults", "err", err.Error())
		profiles = config.DefaultProfiles()
	}
	eng := decide.New(profiles, search.Evaluate)

	version := os.Getenv("RENDER_GIT_COMMIT")
	if len(version) > 7 {
		version = version[:7]
	}
	if version == "" {
		version = "dev"
	}
	srv := server.New(eng, log, version, opponent.NewRegistry(os.Getenv("PROFILES_PATH")))
	srv.Warmup()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	hs := &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
		<-sig
		// Let in-flight moves finish during a redeploy.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(ctx)
	}()

	log.Info("listening", "addr", hs.Addr, "version", version, "gomaxprocs", runtime.GOMAXPROCS(0))
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("listen", "err", err.Error())
		os.Exit(1)
	}
}
