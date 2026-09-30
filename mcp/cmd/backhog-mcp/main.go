// Command backhog-mcp is the backhog MCP server: the bridge that lets a
// user's own AI assistant (Claude Code, Claude Desktop, any MCP client, a
// local model) ask about the books in their backhog library. It holds no
// model and no secrets beyond one personal API token, and talks only to
// the public HTTP API — so every spoiler clamp and permission check the
// server enforces applies to it automatically.
//
// Configuration is two environment variables (flags override):
//
//	BACKHOG_URL    the origin of the backhog instance, e.g. http://localhost:8080
//	               (an /api suffix is tolerated and stripped; deep links are
//	               built against this origin)
//	BACKHOG_TOKEN  a personal API token minted in Settings → API tokens
//	               (bh_…, read-only)
//
// BACKHOG_PUBLIC_URL optionally overrides the origin peek deep links point
// at, for deployments whose API address differs from the URL the human
// opens (compose networking, reverse proxies).
//
// The default transport is stdio, which is what Claude Code and Claude
// Desktop spawn. Serve streamable HTTP instead with -http addr (and
// -healthcheck for container healthchecks; the HTTP side answers /healthz).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/collinpendleton/backhog/mcp/internal/backhog"
	"github.com/collinpendleton/backhog/mcp/internal/server"
)

func main() {
	httpAddr := flag.String("http", envOr("BACKHOG_MCP_HTTP", ""),
		"listen address for the streamable HTTP transport (e.g. :8081); empty means stdio")
	backhogURL := flag.String("backhog-url", os.Getenv("BACKHOG_URL"), "backhog origin, e.g. http://backhog:8080 (env BACKHOG_URL)")
	token := flag.String("backhog-token", os.Getenv("BACKHOG_TOKEN"), "personal API token, bh_… (env BACKHOG_TOKEN)")
	publicURL := flag.String("public-url", os.Getenv("BACKHOG_PUBLIC_URL"), "origin for deep links when it differs from -backhog-url (env BACKHOG_PUBLIC_URL)")
	healthcheck := flag.Bool("healthcheck", false, "probe the HTTP server's /healthz once and exit (for container healthchecks)")
	flag.Parse()

	if *healthcheck {
		addr := *httpAddr
		if addr == "" {
			addr = envOr("BACKHOG_MCP_HTTP", ":8081")
		}
		if err := probeHealth(addr); err != nil {
			fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
			os.Exit(1)
		}
		return
	}

	client, err := backhog.New(*backhogURL, *token)
	if err != nil {
		// A config error on stdio must go to stderr: stdout is the protocol.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	srv := server.New(server.Options{Client: client, PublicURL: *publicURL})

	if *httpAddr == "" {
		runStdio(srv)
		return
	}
	runHTTP(srv, *httpAddr)
}

// runStdio serves the MCP protocol on stdin/stdout until the client closes
// the pipe. Logs go to stderr only — stdout is the protocol.
func runStdio(srv *mcp.Server) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := srv.Run(ctx, &mcp.StdioTransport{})
	// A closed stdin (the client going away) is a clean shutdown, not a
	// failure; everything else is real.
	if err != nil && !errors.Is(err, io.EOF) && !strings.Contains(err.Error(), "closing") {
		fmt.Fprintf(os.Stderr, "stdio run: %v\n", err)
		os.Exit(1)
	}
}

// runHTTP serves the streamable HTTP transport plus a /healthz for
// container orchestration, until the process is told to stop.
func runHTTP(srv *mcp.Server, addr string) {
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcpHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	hs := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("backhog-mcp listening (streamable HTTP)", "addr", addr, "endpoint", "/mcp")
	}()
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "http server: %v\n", err)
		os.Exit(1)
	}
}

// probeHealth GETs the health endpoint once, for a container healthcheck.
func probeHealth(addr string) error {
	if addr == "" || addr[0] != ':' {
		addr = "http://" + addr
	} else {
		addr = "http://localhost" + addr
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(addr + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz answered %d", resp.StatusCode)
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
