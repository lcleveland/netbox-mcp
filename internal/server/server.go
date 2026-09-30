// Package server builds the MCP server and serves it over stdio or HTTP.
package server

import (
	"context"
	"log/slog"
	"strings"

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
	s := "Tools for the NetBox instance at " + cfg.URL.String() + ", the IPAM/DCIM source of truth, over the REST API.\n\n" +
		"Call netbox_status first if anything fails: it separates a wrong URL from a rejected token from a missing permission.\n\n"
	var on, off []string
	for _, v := range []struct {
		name string
		ok   bool
	}{{"create", cfg.AllowCreate}, {"update", cfg.AllowUpdate}, {"delete", cfg.AllowDelete}} {
		if v.ok {
			on = append(on, v.name)
		} else {
			off = append(off, v.name)
		}
	}
	if len(on) == 0 {
		return s + "This server is read-only: writes are disabled by the operator. Do not suggest workarounds; ask the user to change the server configuration if a write is needed."
	}
	s += "Enabled writes: " + strings.Join(on, ", ") + ". Every write needs a reason, which NetBox records in its changelog. NetBox has no undo: confirm with the user before bulk changes or deletes."
	if len(off) > 0 {
		s += " Disabled by the operator: " + strings.Join(off, ", ") + "."
	}
	return s
}

// ServeStdio runs until ctx is cancelled. Nothing else may write to stdout.
func ServeStdio(ctx context.Context, s *mcp.Server) error {
	return s.Run(ctx, &mcp.StdioTransport{})
}
