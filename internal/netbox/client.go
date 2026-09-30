// Package netbox is a small REST client for NetBox 4.6+: v2 bearer auth,
// retry with backoff, and errors that tell the model what to do next.
package netbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	base  *url.URL
	token string
	hc    *http.Client

	// Retry tuning; tests shrink these.
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// New builds a client. hc may be nil; its Timeout is the per-request timeout.
func New(base *url.URL, token string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: base, token: token, hc: hc, Attempts: 4, BaseDelay: 250 * time.Millisecond, MaxDelay: 5 * time.Second}
}

func (c *Client) BaseURL() string { return c.base.String() }

// Response is a successful (2xx) reply.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Decode unmarshals the body; an empty body (204) decodes to nil.
func (r *Response) Decode() (any, error) {
	if len(bytes.TrimSpace(r.Body)) == 0 {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		return nil, fmt.Errorf("decoding NetBox response: %w", err)
	}
	return v, nil
}

// Do sends one request. path is relative to the base URL and must start with
// /api/. body is JSON-encoded when non-nil. Non-2xx replies return *APIError.
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body any, hdr http.Header) (*Response, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = q.Encode()

	for attempt := 1; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, vs := range hdr {
			req.Header[k] = vs
		}

		resp, err := c.hc.Do(req)
		if err != nil {
			if ctx.Err() == nil && idempotent(method) && attempt < c.Attempts {
				if werr := c.wait(ctx, attempt, ""); werr != nil {
					return nil, werr
				}
				continue
			}
			return nil, fmt.Errorf("%s %s: %s", method, path, c.scrub(err.Error()))
		}
		b, rerr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("%s %s: reading response: %w", method, path, rerr)
		}
		if resp.StatusCode/100 == 2 {
			return &Response{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
		}
		retry := resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= 500 && resp.StatusCode != 501 && idempotent(method))
		if retry && attempt < c.Attempts {
			if werr := c.wait(ctx, attempt, resp.Header.Get("Retry-After")); werr != nil {
				return nil, werr
			}
			continue
		}
		return nil, newAPIError(method, path, resp, b, c.scrub)
	}
}

func idempotent(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// wait sleeps with full-jitter exponential backoff, or Retry-After if given.
func (c *Client) wait(ctx context.Context, attempt int, retryAfter string) error {
	d := c.BaseDelay << (attempt - 1)
	if d > c.MaxDelay || d <= 0 {
		d = c.MaxDelay
	}
	d = time.Duration(rand.Int64N(int64(d) + 1))
	if s, err := strconv.Atoi(retryAfter); err == nil && s >= 0 {
		d = min(time.Duration(s)*time.Second, 60*time.Second)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) scrub(s string) string {
	if c.token == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.token, "[redacted]")
	// The secret half of a v2 token is also worth hiding on its own.
	if _, secret, ok := strings.Cut(c.token, "."); ok && len(secret) >= 8 {
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
	return s
}
