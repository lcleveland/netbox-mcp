package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type APIInput struct {
	Method  string         `json:"method" jsonschema:"HTTP method"`
	Path    string         `json:"path" jsonschema:"NetBox REST path starting with /api/, e.g. /api/dcim/device-types/ or /api/plugins/<plugin>/..."`
	Query   map[string]any `json:"query,omitempty" jsonschema:"query params, e.g. {\"brief\": 1, \"limit\": 50, \"q\": \"core\"}"`
	Body    any            `json:"body,omitempty" jsonschema:"JSON body for POST/PATCH/PUT/DELETE; an array for bulk (all or nothing)"`
	Reason  string         `json:"reason,omitempty" jsonschema:"required for writes: why, recorded as the NetBox changelog message"`
	IfMatch string         `json:"if_match,omitempty" jsonschema:"PATCH/PUT on a single object: the ETag from a previous GET"`
}

func (d Deps) allowedMethods() []string {
	m := []string{http.MethodGet}
	if d.Config.AllowCreate {
		m = append(m, http.MethodPost)
	}
	if d.Config.AllowUpdate {
		m = append(m, http.MethodPatch, http.MethodPut)
	}
	if d.Config.AllowDelete {
		m = append(m, http.MethodDelete)
	}
	return m
}

func registerGeneric(s *mcp.Server, d Deps) error {
	methods := d.allowedMethods()
	schema, err := jsonschema.For[APIInput](nil)
	if err != nil {
		return err
	}
	schema.Properties["method"].Enum = make([]any, len(methods))
	for i, m := range methods {
		schema.Properties["method"].Enum[i] = m
	}
	schema.Required = []string{"method", "path"}

	desc := "Call any NetBox REST endpoint under /api/ for objects without a dedicated tool " +
		"(device types, platforms, circuits, clusters, plugins under /api/plugins/, ...). Prefer the netbox_* tools when one fits. " +
		"Allowed methods: " + strings.Join(methods, ", ") + ". List responses are capped; pass brief or fields and a limit."
	if len(methods) > 1 {
		desc += fmt.Sprintf(" Writes require reason (stored as the changelog message); bulk arrays are limited to %d objects.", d.Config.MaxBulk)
	} else {
		desc += " Writes are disabled by the operator."
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "netbox_api",
		Title:       "NetBox REST API (generic)",
		Description: desc,
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    len(methods) == 1,
			DestructiveHint: new(slices.Contains(methods, http.MethodDelete)),
			OpenWorldHint:   new(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in APIInput) (*mcp.CallToolResult, any, error) {
		method := strings.ToUpper(in.Method)
		if !slices.Contains(methods, method) {
			return nil, nil, fmt.Errorf("method %q is not allowed (allowed: %s)", in.Method, strings.Join(methods, ", "))
		}
		path, err := apiPath(in.Path)
		if err != nil {
			return nil, nil, err
		}
		if method != http.MethodGet {
			w := write{tool: "netbox_api", action: strings.ToLower(method), method: method, path: path, body: in.Body, reason: in.Reason}
			if in.IfMatch != "" {
				w.hdr = http.Header{"If-Match": {in.IfMatch}}
			}
			out, err := d.doWrite(ctx, w)
			return nil, out, err
		}
		q, err := toValues(in.Query)
		if err != nil {
			return nil, nil, err
		}
		resp, err := d.Client.Do(ctx, http.MethodGet, path, q, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		v, err := resp.Decode()
		if err != nil {
			return nil, nil, err
		}
		v = strip(v)
		if m, ok := v.(map[string]any); ok && m["results"] == nil {
			return nil, m, nil // a single object
		}
		offset, _ := strconv.Atoi(q.Get("offset"))
		return nil, page(v, offset), nil
	})
	return nil
}

// apiPath confines the generic tool to NetBox's REST API: a relative path
// under /api/, with no traversal, encoding tricks, query or fragment.
func apiPath(p string) (string, error) {
	bad := errors.New("path must be a NetBox REST path like /api/dcim/devices/ (no host, query string, '..' or encoded characters)")
	if !strings.HasPrefix(p, "/api/") || strings.ContainsAny(p, "%?#\\ \t\r\n") || strings.Contains(p, "//") {
		return "", bad
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "." || seg == ".." {
			return "", bad
		}
	}
	if !strings.HasSuffix(p, "/") {
		p += "/" // NetBox requires the trailing slash
	}
	return p, nil
}
