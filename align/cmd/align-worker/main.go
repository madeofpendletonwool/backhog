// Command align-worker is Backhog's optional alignment worker: the
// container that does the expensive half of the Books arena. It claims
// jobs from the API's /internal queue, transcribes each book's audio
// with ffmpeg and whisper.cpp, and streams a timestamped transcript back
// on the book's global timeline.
//
// It owns no database, mounts the media library read-only, and is
// entirely optional — a Backhog with no worker keeps a fully working
// Books arena, with alignment jobs simply sitting queued.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/collinpendleton/backhog/align/internal/config"
	"github.com/collinpendleton/backhog/align/internal/worker"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false,
		"probe this container's own status endpoint and exit; used by the compose healthcheck")
	flag.Parse()

	if *healthcheck {
		if err := probe(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		slog.Error("alignment worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel()}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	w := worker.New(cfg, log)
	// Preflight before the first claim: a missing model or an unmounted
	// media root should stop the container at startup, where it is
	// obvious, rather than failing one book at a time.
	if err := w.Preflight(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	status := &http.Server{
		Addr:              cfg.StatusAddr,
		Handler:           w.StatusHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := status.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("status endpoint stopped", "error", err)
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = status.Shutdown(shutdownCtx)
	}()

	return w.Run(ctx)
}

// probe is the container healthcheck. The runtime image has no shell and
// no curl, so — like the API image — the binary probes itself.
func probe() error {
	addr := os.Getenv("ALIGN_STATUS_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("ALIGN_STATUS_ADDR %q is not host:port: %w", addr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://" + net.JoinHostPort(host, port) + "/healthz"
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status endpoint returned %d", resp.StatusCode)
	}
	return nil
}

func logLevel() slog.Level {
	switch os.Getenv("LOG_LEVEL") {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "warn", "WARN":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
