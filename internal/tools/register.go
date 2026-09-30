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
	must("netbox_api", registerGeneric(s, d))
	n := 2
	if d.Config.Enabled("ipam") {
		must("netbox_available", registerAvailable(s, d))
		n++
	}
	for _, r := range Resources {
		if !d.Config.Enabled(r.Group) {
			continue
		}
		must(r.Name, registerResource(s, d, r))
		n++
	}
	return n
}

// must panics on a schema error: the tool set is static, so only a
// programming error lands here, and it fails every test.
func must(name string, err error) {
	if err != nil {
		panic(name + ": " + err.Error())
	}
}
