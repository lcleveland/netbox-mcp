package tools

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/lcleveland/netbox-mcp/internal/config"
)

func TestAPIPath(t *testing.T) {
	for _, p := range []string{"../etc", "/api/../admin/", "/api/%2e%2e/", "//evil.example/api/", "https://x/api/",
		"/graphql/", "/api", "/api/dcim//devices/", "/api/x/?q=1", "/api/./x/", `/api\x/`} {
		if _, err := apiPath(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	for in, want := range map[string]string{"/api/dcim/devices/": "/api/dcim/devices/", "/api/plugins/x/y": "/api/plugins/x/y/"} {
		if got, err := apiPath(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v", in, got, err)
		}
	}
}

func TestGeneric(t *testing.T) {
	var paths []string
	h := func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		fmt.Fprint(w, `{"count":1,"next":null,"results":[{"id":1,"url":"x"}]}`)
	}
	cs := session(t, nil, h)
	if e := methodEnum(t, cs); !slices.Equal(e, []string{"GET"}) {
		t.Errorf("read-only methods %v", e)
	}
	for _, m := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		if _, isErr, _ := call(t, cs, "netbox_api", map[string]any{"method": m, "path": "/api/dcim/platforms/", "reason": "r", "body": map[string]any{}}); !isErr {
			t.Errorf("%s allowed while disabled", m)
		}
	}
	out, isErr, text := call(t, cs, "netbox_api", map[string]any{"method": "GET", "path": "/api/dcim/platforms"})
	if isErr || out["count"] != float64(1) || len(paths) != 1 || paths[0] != "GET /api/dcim/platforms/" {
		t.Fatalf("get: %v %s %v", out, text, paths)
	}

	cs = session(t, &config.Config{AllowDelete: true}, h)
	if e := methodEnum(t, cs); !slices.Equal(e, []string{"GET", "DELETE"}) {
		t.Errorf("delete methods %v", e)
	}
	if _, isErr, text := call(t, cs, "netbox_api", map[string]any{"method": "DELETE", "path": "/api/dcim/platforms/3/"}); !isErr || !strings.Contains(text, "reason") {
		t.Errorf("delete without reason: %v %s", isErr, text)
	}
	if _, isErr, text := call(t, cs, "netbox_api", map[string]any{"method": "DELETE", "path": "/api/dcim/platforms/3/", "reason": "gone"}); isErr {
		t.Errorf("delete: %s", text)
	}
}

func TestAvailable(t *testing.T) {
	var got []string
	cs := session(t, &config.Config{AllowCreate: true}, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/5/") {
			w.WriteHeader(409)
			fmt.Fprint(w, `{"detail":"Insufficient space is available to accommodate the requested prefix size(s)"}`)
			return
		}
		fmt.Fprint(w, `[{"address":"10.0.0.1/24"},{"address":"10.0.0.2/24"},{"address":"10.0.0.3/24"}]`)
	})
	out, _, _ := call(t, cs, "netbox_available", map[string]any{"action": "list", "kind": "prefix", "parent_id": 4, "limit": 2})
	if len(out["results"].([]any)) != 2 || got[0] != "GET /api/ipam/prefixes/4/available-prefixes/" {
		t.Errorf("list: %v %v", out, got)
	}
	if _, isErr, _ := call(t, cs, "netbox_available", map[string]any{"action": "allocate", "kind": "vlan", "parent_id": 4}); !isErr {
		t.Error("allocate without reason allowed")
	}
	out, isErr, _ := call(t, cs, "netbox_available", map[string]any{"action": "allocate", "kind": "vlan", "parent_id": 4, "reason": "r", "body": []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}}})
	if isErr || got[1] != "POST /api/ipam/vlan-groups/4/available-vlans/" || out["results"] == nil {
		t.Errorf("allocate: %v %v", out, got)
	}
	if _, isErr, text := call(t, cs, "netbox_available", map[string]any{"action": "allocate", "kind": "ip", "parent_id": 5, "reason": "r"}); !isErr || !strings.Contains(text, "free space") {
		t.Errorf("409: %s", text)
	}

	cs = session(t, nil, nil)
	if e := schemaEnum(t, cs, "netbox_available", "action"); !slices.Equal(e, []string{"list"}) {
		t.Errorf("allocate should be absent with create off: %v", e)
	}
}

func methodEnum(t *testing.T, cs sessionT) []string { return schemaEnum(t, cs, "netbox_api", "method") }
