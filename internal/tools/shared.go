package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
)

const (
	defaultLimit = 50
	maxItems     = 200      // below NetBox's 1000 page cap, so one request per call
	maxBytes     = 60 << 10 // keeps a result inside a sensible slice of context
)

// list fetches one page. Brief objects unless fields is given.
func (d Deps) list(ctx context.Context, path string, query map[string]any, fields string, limit, offset int) (map[string]any, error) {
	q, err := toValues(query)
	if err != nil {
		return nil, err
	}
	if fields != "" {
		q.Set("fields", fields)
	} else {
		q.Set("brief", "1") // any non-empty value turns brief on, even "false"
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxItems)
	q.Set("limit", strconv.Itoa(limit))
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	resp, err := d.Client.Do(ctx, http.MethodGet, path, q, nil, nil)
	if err != nil {
		return nil, err
	}
	v, err := resp.Decode()
	if err != nil {
		return nil, err
	}
	return page(strip(v), offset), nil
}

// page normalises a list reply (paged envelope or bare array) and applies the
// byte cap, halving until it fits.
func page(v any, offset int) map[string]any {
	var results []any
	count := -1
	out := map[string]any{}
	switch t := v.(type) {
	case map[string]any:
		results, _ = t["results"].([]any)
		if c, ok := t["count"].(float64); ok {
			count = int(c)
		}
		if t["next"] != nil {
			out["next_offset"] = offset + len(results)
		}
	case []any:
		results = t
	}
	total := len(results)
	if count >= 0 {
		out["count"] = count
	}
	for len(results) > 1 && size(results) > maxBytes {
		results = results[:len(results)/2]
	}
	if len(results) < total {
		out["next_offset"] = offset + len(results)
		out["_truncation"] = map[string]any{
			"returned": len(results),
			"of":       total,
			"note":     "result too large; narrow the filter, pass fields, or page with offset",
		}
	}
	if results == nil {
		results = []any{}
	}
	out["results"] = results
	return out
}

func size(v any) int {
	b, _ := json.Marshal(v)
	return len(b)
}

// strip removes link noise the model never needs.
func strip(v any) any {
	switch t := v.(type) {
	case map[string]any:
		delete(t, "url")
		delete(t, "display_url")
		for k, x := range t {
			t[k] = strip(x)
		}
	case []any:
		for i, x := range t {
			t[i] = strip(x)
		}
	}
	return v
}

// toValues turns {"site": "hq", "tag": ["a","b"], "vid": 10} into query params.
func toValues(m map[string]any) (url.Values, error) {
	q := url.Values{}
	for k, v := range m {
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				s, err := scalar(k, x)
				if err != nil {
					return nil, err
				}
				q.Add(k, s)
			}
		default:
			s, err := scalar(k, t)
			if err != nil {
				return nil, err
			}
			q.Add(k, s)
		}
	}
	return q, nil
}

func scalar(k string, v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(t), nil
	case nil:
		return "null", nil
	}
	return "", fmt.Errorf("query %q: values must be strings, numbers, booleans or arrays of them", k)
}

// write is one gated write request. Every write, from any tool, goes through
// doWrite so reason, bulk cap and audit logging cannot be skipped.
type write struct {
	tool, action string
	method, path string
	body         any
	reason       string
	hdr          http.Header
}

func (d Deps) doWrite(ctx context.Context, w write) (any, error) {
	if w.reason == "" {
		return nil, errors.New("reason is required for writes; say why, it is recorded in the NetBox changelog")
	}
	body, n, err := withReason(w.body, w.reason)
	if err != nil {
		return nil, err
	}
	if n > d.Config.MaxBulk {
		return nil, fmt.Errorf("bulk write of %d objects exceeds the limit of %d; split it", n, d.Config.MaxBulk)
	}
	resp, err := d.Client.Do(ctx, w.method, w.path, nil, body, w.hdr)
	if err != nil {
		return nil, err
	}
	v, err := resp.Decode()
	if err != nil {
		return nil, err
	}
	reqID := resp.Header.Get("X-Request-ID")
	d.Log.Info("netbox write",
		"tool", w.tool, "action", w.action, "method", w.method, "path", w.path,
		"ids", ids(v, body), "reason", w.reason, "request_id", reqID)

	out := map[string]any{"status": resp.Status}
	if reqID != "" {
		out["request_id"] = reqID
		out["changelog"] = "netbox_object_change list with query {\"request_id\": \"" + reqID + "\"}"
	}
	switch t := strip(v).(type) {
	case nil:
	case []any:
		out["results"] = t
	default:
		out["result"] = t
	}
	return out, nil
}

// withReason sets changelog_message on the object, or on each object of a
// bulk array (bare ids in a bulk delete become {"id": n}). It reports the
// number of objects.
func withReason(body any, reason string) (any, int, error) {
	switch t := body.(type) {
	case map[string]any:
		m := maps.Clone(t)
		m["changelog_message"] = reason
		return m, 1, nil
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			switch o := x.(type) {
			case map[string]any:
				m := maps.Clone(o)
				m["changelog_message"] = reason
				out[i] = m
			case float64:
				out[i] = map[string]any{"id": o, "changelog_message": reason}
			default:
				return nil, 0, fmt.Errorf("body[%d]: bulk items must be objects (or ids for delete)", i)
			}
		}
		return out, len(t), nil
	case nil:
		return map[string]any{"changelog_message": reason}, 1, nil
	}
	return nil, 0, errors.New("body must be an object or an array of objects")
}

// ids collects object ids from the response, falling back to the request.
func ids(resp, req any) []any {
	var out []any
	collect := func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if id, ok := t["id"]; ok {
				out = append(out, id)
			}
		case []any:
			for _, x := range t {
				if m, ok := x.(map[string]any); ok && m["id"] != nil {
					out = append(out, m["id"])
				}
			}
		}
	}
	collect(resp)
	if len(out) == 0 {
		collect(req)
	}
	return out
}
