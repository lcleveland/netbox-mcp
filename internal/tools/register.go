// Package tools defines the MCP tools and registers the enabled ones.
package tools

import (
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netbox-mcp/internal/config"
	"github.com/lcleveland/netbox-mcp/internal/netbox"
)

type Deps struct {
	Client *netbox.Client
	Config *config.Config
	Log    *slog.Logger
}

// Register adds every enabled tool and returns how many it added.
func Register(s *mcp.Server, d Deps) int {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	registerStatus(s, d)
	return 1
}
