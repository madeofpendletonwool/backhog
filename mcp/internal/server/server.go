// Package server assembles the backhog MCP server: the bridge that lets a
// user's own AI assistant read their library through backhog's spoiler-safe
// API. No model runs here — this is an adapter that turns the public HTTP
// surface (position-bounded by MAD-467, token-authenticated by MAD-465)
// into MCP tools, and nothing more.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/collinpendleton/backhog/mcp/internal/backhog"
)

// Options configures one server instance.
type Options struct {
	// Client is the authenticated backhog API client every tool calls.
	Client *backhog.Client
	// PublicURL is the origin deep links point at — the URL the human
	// opens in a browser. It defaults to the client's base URL; a
	// deployment whose API address differs from its public one (compose
	// networking, reverse proxies) overrides it.
	PublicURL string
}

// New builds the MCP server with every backhog tool registered. The same
// *mcp.Server serves any transport; the caller decides stdio or HTTP.
func New(opts Options) *mcp.Server {
	srv := mcpServer()
	addTools(srv, opts.Client, newLinker(opts))
	return srv
}

func mcpServer() *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{
		Name:    "backhog",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Read a personal book library with spoiler safety enforced by the server: " +
			"every tool is bounded to the user's reading position unless include_spoilers is set. " +
			"Cite answers with the deep_link each result carries, and when the tools return nothing " +
			"or truncated text, answer that the book does not say (so far) rather than drawing on " +
			"outside knowledge about the work.",
	})
}

// readOnly marks every tool: none of them writes, by design and by token.
var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}
