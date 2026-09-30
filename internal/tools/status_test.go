package tools

import (
	"net/http"
	"testing"
)

func TestStatus(t *testing.T) {
	cs := session(t, nil, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/status/":
			w.Write([]byte(`{"netbox-version":"4.5.2","rq-workers-running":0}`))
		case "/api/authentication-check/":
			w.Write([]byte(`{"username":"mcp"}`))
		}
	})
	out, isErr, _ := call(t, cs, "netbox_status", nil)
	if isErr || out["authenticated"] != true || out["user"] != "mcp" || out["netbox_version"] != "4.5.2" {
		t.Fatalf("status: %v", out)
	}
	if w, _ := out["warnings"].([]any); len(w) != 2 {
		t.Errorf("want old-version and no-worker warnings, got %v", out["warnings"])
	}

	cs = session(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/status/" {
			w.Write([]byte(`{"netbox-version":"4.6.9","rq-workers-running":1}`))
			return
		}
		w.WriteHeader(403)
		w.Write([]byte(`{"detail":"Invalid v2 token"}`))
	})
	out, _, _ = call(t, cs, "netbox_status", nil)
	if out["reachable"] != true || out["authenticated"] == true || out["warnings"] != nil {
		t.Fatalf("bad token: %v", out)
	}
}

func TestAtLeast(t *testing.T) {
	for v, want := range map[string]bool{"4.6.9": true, "4.7": true, "5.0.0": true, "4.5.10": false, "": false, "x.y": false} {
		if atLeast(v, 4, 6) != want {
			t.Errorf("atLeast(%q) != %v", v, want)
		}
	}
}
