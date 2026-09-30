package tools

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lcleveland/netbox-mcp/internal/netbox"
)

type StatusOutput struct {
	URL              string   `json:"url"`
	Reachable        bool     `json:"reachable"`
	Authenticated    bool     `json:"authenticated"`
	User             string   `json:"user,omitempty"`
	NetBoxVersion    string   `json:"netbox_version,omitempty"`
	RQWorkersRunning *float64 `json:"rq_workers_running,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
	Detail           string   `json:"detail,omitempty"`
}

func registerStatus(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "netbox_status",
		Title: "NetBox connectivity check",
		Description: "Check that NetBox is reachable, report its version and background-worker status, " +
			"and confirm the API token is accepted (and as which user).\n\n" +
			"Call this first when another tool fails: it tells a wrong URL apart from a rejected " +
			"token apart from a token that works but lacks permission for a specific object type.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusOutput, error) {
		out := StatusOutput{URL: d.Client.BaseURL()}

		var ae *netbox.APIError
		resp, err := d.Client.Do(ctx, http.MethodGet, "/api/status/", nil, nil, nil)
		switch {
		case err == nil:
			out.Reachable = true
			if v, _ := resp.Decode(); v != nil {
				m, _ := v.(map[string]any)
				out.NetBoxVersion, _ = m["netbox-version"].(string)
				if n, ok := m["rq-workers-running"].(float64); ok {
					out.RQWorkersRunning = &n
					if n == 0 {
						out.Warnings = append(out.Warnings, "no RQ workers running: background jobs and some bulk operations will fail with 503")
					}
				}
			}
			if !atLeast(out.NetBoxVersion, 4, 6) {
				out.Warnings = append(out.Warnings, "NetBox "+out.NetBoxVersion+" is older than the supported 4.6; some features (v2 tokens, add_tags, If-Match) may not work")
			}
		case errors.As(err, &ae):
			// An HTTP error still proves the host answers.
			out.Reachable = true
		default:
			out.Detail = err.Error()
			return nil, out, nil
		}

		resp, err = d.Client.Do(ctx, http.MethodGet, "/api/authentication-check/", nil, nil, nil)
		if err != nil {
			out.Detail = err.Error()
			return nil, out, nil
		}
		out.Authenticated = true
		if v, _ := resp.Decode(); v != nil {
			m, _ := v.(map[string]any)
			out.User, _ = m["username"].(string)
		}
		return nil, out, nil
	})
}

// atLeast compares a "major.minor[.patch]" version string.
func atLeast(v string, major, minor int) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(parts[0])
	mi, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return ma > major || (ma == major && mi >= minor)
}
