package netbox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const tok = "nbt_key123.topsecretvalue"

func newTest(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	c := New(u, tok, srv.Client())
	c.BaseDelay, c.MaxDelay = time.Millisecond, 2*time.Millisecond
	return c
}

func TestAuthHeaderAndDecode(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			t.Errorf("auth header %q", r.Header.Get("Authorization"))
		}
		w.Write([]byte(`{"ok":true}`))
	})
	resp, err := c.Do(context.Background(), "GET", "/api/status/", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := resp.Decode()
	if v.(map[string]any)["ok"] != true {
		t.Fatalf("decoded %v", v)
	}
}

func TestRetry(t *testing.T) {
	cases := []struct {
		method string
		status int
		calls  int32
	}{
		{"GET", 502, 4},  // idempotent 5xx retried to the limit
		{"POST", 502, 1}, // non-idempotent 5xx not retried
		{"POST", 429, 4}, // 429 always retried
		{"GET", 404, 1},  // 4xx not retried
	}
	for _, tc := range cases {
		var n atomic.Int32
		c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(tc.status)
		})
		_, err := c.Do(context.Background(), tc.method, "/api/x/", nil, nil, nil)
		if err == nil || n.Load() != tc.calls {
			t.Errorf("%s %d: calls=%d want %d err=%v", tc.method, tc.status, n.Load(), tc.calls, err)
		}
	}

	var n atomic.Int32
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{}`))
	})
	if _, err := c.Do(context.Background(), "GET", "/api/x/", nil, nil, nil); err != nil || n.Load() != 3 {
		t.Errorf("recover after retries: calls=%d err=%v", n.Load(), err)
	}
}

func TestErrorHints(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{403, `{"detail":"Invalid v2 token"}`, "token was rejected"},
		{403, `{"detail":"You do not have permission to perform this action."}`, "lacks permission"},
		{404, `{"detail":"No Device matches the given query."}`, "no object with that id"},
		{404, `<html>not found</html>`, "route does not exist"},
		{409, `{"detail":"Insufficient space"}`, "not enough free space"},
		{412, `{"detail":"Precondition failed."}`, "Re-read"},
		{500, `{"error":"operating in maintenance mode","exception":"ReadOnlyError"}`, "maintenance"},
		{400, `{"name":["This field is required."]}`, "This field is required"},
	}
	for _, tc := range cases {
		c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		c.Attempts = 1
		_, err := c.Do(context.Background(), "POST", "/api/dcim/devices/", url.Values{"q": {"secret-query"}}, map[string]any{}, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: %v does not contain %q", tc.status, err, tc.want)
		}
		if strings.Contains(err.Error(), "secret-query") {
			t.Errorf("query string leaked: %v", err)
		}
	}
}

func TestTokenScrubbed(t *testing.T) {
	c := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"detail":"bad header Bearer ` + tok + ` and topsecretvalue"}`))
	})
	_, err := c.Do(context.Background(), "GET", "/api/x/", nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "topsecretvalue") {
		t.Fatalf("token not scrubbed: %v", err)
	}
}
