package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netbox-mcp/internal/config"
	"github.com/lcleveland/netbox-mcp/internal/netbox"
)

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

func TestHTTP(t *testing.T) {
	u, _ := url.Parse("http://netbox.invalid")
	cfg := &config.Config{URL: u, MaxBulk: 50, Path: "/mcp", HTTPAuthToken: "s3cret"}
	s, _ := New(cfg, netbox.New(u, "nbt_x.y", nil), nil)
	srv := httptest.NewServer(Handler(cfg, s, nil))
	defer srv.Close()

	if r, err := http.Get(srv.URL + "/healthz"); err != nil || r.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", r, err)
	}
	if r, err := http.Post(srv.URL+"/mcp", "application/json", nil); err != nil || r.StatusCode != 401 {
		t.Fatalf("no bearer: %v %v", r.StatusCode, err)
	}

	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{"s3cret"}},
	}, nil)
	if err != nil {
		t.Fatalf("initialize with bearer: %v", err)
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil || len(res.Tools) == 0 {
		t.Fatalf("list tools: %v", err)
	}

	if _, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: bearer{"wrong"}},
	}, nil); err == nil {
		t.Fatal("wrong bearer accepted")
	}
}
