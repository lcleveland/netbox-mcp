package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/lcleveland/netbox-mcp/internal/config"
)

func enumOf(t *testing.T, cs sessionT, tool string) []string {
	return schemaEnum(t, cs, tool, "action")
}

func schemaEnum(t *testing.T, cs sessionT, tool, prop string) []string {
	t.Helper()
	tl := toolNames(t, cs)[tool]
	if tl == nil {
		return nil
	}
	b, _ := json.Marshal(tl.InputSchema)
	var s struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	json.Unmarshal(b, &s)
	return s.Properties[prop].Enum
}

func TestListBriefFieldsAndStrip(t *testing.T) {
	var got []string
	cs := session(t, nil, func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		fmt.Fprint(w, `{"count":120,"next":"http://x/?offset=50","results":[{"id":1,"url":"http://x/1/","display_url":"u","site":{"id":2,"url":"http://x/2/","name":"hq"},"custom_fields":{"url":"keep"}}]}`)
	})
	out, isErr, text := call(t, cs, "netbox_prefix", map[string]any{"action": "list", "query": map[string]any{"site": "hq", "tag": []any{"a", "b"}}})
	if isErr {
		t.Fatal(text)
	}
	if !strings.Contains(got[0], "brief=1") || !strings.Contains(got[0], "tag=a&tag=b") || !strings.Contains(got[0], "limit=50") {
		t.Errorf("query %q", got[0])
	}
	if out["count"] != float64(120) || out["next_offset"] != float64(1) {
		t.Errorf("paging: %v", out)
	}
	res := out["results"].([]any)[0].(map[string]any)
	if res["url"] != nil || res["display_url"] != nil || res["site"].(map[string]any)["url"] != nil || res["custom_fields"].(map[string]any)["url"] != "keep" {
		t.Errorf("url not stripped: %v", res)
	}

	call(t, cs, "netbox_prefix", map[string]any{"action": "list", "fields": "id,prefix", "limit": 5000, "offset": 50})
	if strings.Contains(got[1], "brief") || !strings.Contains(got[1], "fields=id%2Cprefix") || !strings.Contains(got[1], "limit=200") || !strings.Contains(got[1], "offset=50") {
		t.Errorf("fields query %q", got[1])
	}
}

func TestListTruncation(t *testing.T) {
	big := strings.Repeat("x", 1000)
	cs := session(t, nil, func(w http.ResponseWriter, r *http.Request) {
		var rs []string
		for i := range 200 {
			rs = append(rs, fmt.Sprintf(`{"id":%d,"description":%q}`, i, big))
		}
		fmt.Fprintf(w, `{"count":200,"next":null,"results":[%s]}`, strings.Join(rs, ","))
	})
	out, _, _ := call(t, cs, "netbox_device", map[string]any{"action": "list", "limit": 200})
	tr, ok := out["_truncation"].(map[string]any)
	n := len(out["results"].([]any))
	if !ok || n >= 200 || n == 0 || out["next_offset"] != float64(n) {
		t.Fatalf("truncation: n=%d %v", n, tr)
	}
}

func TestGetETag(t *testing.T) {
	cs := session(t, nil, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/dcim/devices/7/" || r.URL.RawQuery != "" {
			t.Errorf("get path %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("ETag", `W/"2026-01-01"`)
		fmt.Fprint(w, `{"id":7,"name":"sw1","url":"x"}`)
	})
	out, _, _ := call(t, cs, "netbox_device", map[string]any{"action": "get", "id": 7})
	if out["_etag"] != `W/"2026-01-01"` || out["name"] != "sw1" || out["url"] != nil {
		t.Fatalf("get: %v", out)
	}
}

func TestVerbGating(t *testing.T) {
	calls := 0
	h := func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{}`) }

	cs := session(t, nil, h)
	tools := toolNames(t, cs)
	if len(tools) != 3+len(Resources) {
		t.Errorf("tool count %d", len(tools))
	}
	if e := enumOf(t, cs, "netbox_prefix"); !slices.Equal(e, []string{"list", "get"}) {
		t.Errorf("read-only enum %v", e)
	}
	if !tools["netbox_prefix"].Annotations.ReadOnlyHint {
		t.Error("want ReadOnlyHint")
	}
	for _, a := range []string{"create", "update", "delete"} {
		_, isErr, _ := call(t, cs, "netbox_prefix", map[string]any{"action": a, "id": 1, "body": map[string]any{}, "reason": "r"})
		if !isErr {
			t.Errorf("%s allowed while disabled", a)
		}
	}
	if calls != 0 {
		t.Errorf("disabled writes reached NetBox %d times", calls)
	}

	cs = session(t, &config.Config{AllowUpdate: true}, h)
	if e := enumOf(t, cs, "netbox_prefix"); !slices.Equal(e, []string{"list", "get", "update"}) {
		t.Errorf("update-only enum %v", e)
	}
	if e := enumOf(t, cs, "netbox_object_change"); !slices.Equal(e, []string{"list", "get"}) {
		t.Errorf("read-only resource enum %v", e)
	}
	if !strings.Contains(toolNames(t, cs)["netbox_prefix"].Description, "create/delete through this tool is disabled") {
		t.Error("description should name disabled verbs")
	}
}

func TestToolGroups(t *testing.T) {
	cs := session(t, &config.Config{Groups: []string{"tenancy"}}, nil)
	tools := toolNames(t, cs)
	if len(tools) != 3 || tools["netbox_status"] == nil || tools["netbox_api"] == nil || tools["netbox_tenant"] == nil {
		t.Fatalf("tools %v", tools)
	}
}

func TestWrites(t *testing.T) {
	type req struct {
		method, path, ifMatch string
		body                  any
	}
	var reqs []req
	var logs bytes.Buffer
	cfg := &config.Config{AllowCreate: true, AllowUpdate: true, AllowDelete: true, MaxBulk: 2}
	cs := sessionLog(t, cfg, slog.New(slog.NewTextHandler(&logs, nil)), func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var body any
		json.Unmarshal(b, &body)
		reqs = append(reqs, req{r.Method, r.URL.Path, r.Header.Get("If-Match"), body})
		w.Header().Set("X-Request-ID", "rid-1")
		switch {
		case r.Header.Get("If-Match") == "stale":
			w.WriteHeader(412)
			fmt.Fprint(w, `{"detail":"Precondition failed."}`)
		case r.Method == "DELETE":
			w.WriteHeader(204)
		default:
			fmt.Fprint(w, `{"id":9,"url":"x"}`)
		}
	})

	// Missing reason never reaches NetBox.
	if _, isErr, text := call(t, cs, "netbox_vlan", map[string]any{"action": "create", "body": map[string]any{"vid": 10}}); !isErr || !strings.Contains(text, "reason") {
		t.Fatalf("missing reason: %v %s", isErr, text)
	}
	// Bulk cap.
	if _, isErr, _ := call(t, cs, "netbox_vlan", map[string]any{"action": "create", "reason": "r", "body": []any{map[string]any{}, map[string]any{}, map[string]any{}}}); !isErr {
		t.Fatal("bulk over cap allowed")
	}
	if len(reqs) != 0 {
		t.Fatalf("rejected writes reached NetBox: %v", reqs)
	}

	out, isErr, text := call(t, cs, "netbox_vlan", map[string]any{"action": "create", "reason": "new vlan", "body": map[string]any{"vid": 10, "name": "v10"}})
	if isErr || out["request_id"] != "rid-1" || out["result"].(map[string]any)["url"] != nil {
		t.Fatalf("create: %v %s", out, text)
	}
	if reqs[0].method != "POST" || reqs[0].path != "/api/ipam/vlans/" || reqs[0].body.(map[string]any)["changelog_message"] != "new vlan" {
		t.Errorf("create req %+v", reqs[0])
	}

	call(t, cs, "netbox_vlan", map[string]any{"action": "update", "id": 9, "reason": "rename", "if_match": `W/"x"`, "body": map[string]any{"name": "v"}})
	if r := reqs[1]; r.method != "PATCH" || r.path != "/api/ipam/vlans/9/" || r.ifMatch != `W/"x"` {
		t.Errorf("update req %+v", r)
	}
	_, isErr, text = call(t, cs, "netbox_vlan", map[string]any{"action": "update", "id": 9, "reason": "r", "if_match": "stale", "body": map[string]any{}})
	if !isErr || !strings.Contains(text, "Re-read") {
		t.Errorf("412: %v %s", isErr, text)
	}

	call(t, cs, "netbox_vlan", map[string]any{"action": "delete", "reason": "cleanup", "body": []any{1, 2}})
	if r := reqs[3]; r.method != "DELETE" || r.path != "/api/ipam/vlans/" || len(r.body.([]any)) != 2 ||
		r.body.([]any)[1].(map[string]any)["changelog_message"] != "cleanup" {
		t.Errorf("bulk delete req %+v", r)
	}

	if !strings.Contains(logs.String(), "request_id=rid-1") || !strings.Contains(logs.String(), "reason=\"new vlan\"") || strings.Contains(logs.String(), "v10") {
		t.Errorf("audit log: %s", logs.String())
	}
}
