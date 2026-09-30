// Package server builds the MCP server and serves it over stdio or HTTP.
package server

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netbox-mcp/internal/config"
	"github.com/lcleveland/netbox-mcp/internal/netbox"
	"github.com/lcleveland/netbox-mcp/internal/tools"
	"github.com/lcleveland/netbox-mcp/internal/version"
)

// New builds the server with the enabled tools and reports how many.
func New(cfg *config.Config, c *netbox.Client, log *slog.Logger) (*mcp.Server, int) {
	s := mcp.NewServer(&mcp.Implementation{Name: "netbox-mcp", Title: "NetBox", Version: version.Version},
		&mcp.ServerOptions{Logger: log, Instructions: instructions(cfg)})
	return s, tools.Register(s, tools.Deps{Client: c, Config: cfg, Log: log})
}

func instructions(cfg *config.Config) string {
	return "Tools for the NetBox instance at " + cfg.URL.String() + " (IPAM/DCIM source of truth), over the REST API.\n\n" +
		"Call netbox_status first if anything fails: it separates a wrong URL from a rejected token from a missing permission."
}

// ServeStdio runs until ctx is cancelled. Nothing else may write to stdout.
func ServeStdio(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}
