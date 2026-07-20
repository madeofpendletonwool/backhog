package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/collinpendleton/backhog/api/internal/config"
	"github.com/collinpendleton/backhog/api/internal/db"
	apihttp "github.com/collinpendleton/backhog/api/internal/http"
	"github.com/collinpendleton/backhog/api/internal/metadata"
	"github.com/collinpendleton/backhog/api/internal/store"
)

func main() {
	// The distroless image has no shell or wget, so the container healthcheck
	// re-invokes this binary instead of shelling out.
	healthcheck := flag.Bool("healthcheck", false, "probe the local server and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

// probe performs the container healthcheck against the local listener.
func probe() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	addr := cfg.Addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/healthz")
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	database, err := db.Open(cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		return err
	}
	slog.Info("database ready", "path", cfg.DatabasePath)

	st := store.New(database)

	covers, err := metadata.NewCoverCache(cfg.CoverDir)
	if err != nil {
		return err
	}

	// Without IGDB credentials the app still serves whatever is already cached;
	// only lookups of new games fail, with a clear message.
	var provider metadata.Provider = metadata.Unconfigured{}
	if cfg.MetadataEnabled() {
		provider = metadata.NewIGDB(cfg.IGDBClientID, cfg.IGDBSecret)
		slog.Info("igdb metadata provider enabled")
	} else {
		slog.Warn("IGDB credentials not set; game search is disabled")
	}

	server := apihttp.NewServer(cfg, st, provider, covers)
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go purgeSessions(ctx, st)

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// purgeSessions clears expired sessions periodically so the table does not grow
// without bound on a long-lived instance.
func purgeSessions(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		if n, err := st.PurgeExpiredSessions(ctx); err != nil {
			slog.Warn("purge expired sessions", "error", err)
		} else if n > 0 {
			slog.Info("purged expired sessions", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
