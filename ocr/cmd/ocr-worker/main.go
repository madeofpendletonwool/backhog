// Command ocr-worker is Backhog's optional OCR lettering worker: the
// container that makes comics and picture books searchable without ever
// pretending they have a canonical text. It claims jobs from the API's
// /internal/ocr queue, streams each page's image over the same endpoint
// the paged reader serves, reads the drawn lettering with tesseract, and
// hands the per-page text back as a search-only corpus.
//
// It owns no database, mounts no volumes, and is entirely optional — a
// Backhog with no OCR worker keeps a fully working arena, with lettering
// search simply absent and everything else untouched.
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

	"github.com/collinpendleton/backhog/ocr/internal/config"
	"github.com/collinpendleton/backhog/ocr/internal/tesseract"
	"github.com/collinpendleton/backhog/ocr/internal/worker"
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
		slog.Error("ocr worker stopped", "error", err)
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

	// The checked tesseract identity is both the preflight and the model
	// name recorded on every corpus this worker produces.
	engine := tesseract.Tesseract{Bin: cfg.TesseractBin, Language: cfg.Language, PSM: cfg.PSM}
	version, err := engine.Check()
	if err != nil {
		return err
	}
	model := version + " (" + cfg.Language + ")"

	w := worker.New(cfg, model, log)
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
// no curl, so — like the API and alignment images — the binary probes
// itself.
func probe() error {
	addr := os.Getenv("OCR_STATUS_ADDR")
	if addr == "" {
		addr = ":8091"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("OCR_STATUS_ADDR %q is not host:port: %w", addr, err)
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
