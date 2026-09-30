package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netbox-mcp/internal/config"
	"github.com/lcleveland/netbox-mcp/internal/netbox"
)

// session starts a fake NetBox and an in-memory MCP client/server pair.
func session(t *testing.T, cfg *config.Config, h http.HandlerFunc) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.MaxBulk == 0 {
		cfg.MaxBulk = 50
	}
	cfg.URL = u
	c := netbox.New(u, "nbt_k.secretsecret", srv.Client())
	c.Attempts = 1

	s := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	Register(s, Deps{Client: c, Config: cfg})
	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "client"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured (or text) result.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, bool, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := ""
	if len(res.Content) > 0 {
		if tc, ok := res.Content[0].(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	var out map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		json.Unmarshal(b, &out)
	}
	return out, res.IsError, text
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		m[tl.Name] = tl
	}
	return m
}
