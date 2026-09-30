package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type AvailableInput struct {
	Action   string `json:"action" jsonschema:"list free space, or allocate from it"`
	Kind     string `json:"kind" jsonschema:"ip: free IPs in a prefix; ip_in_range: free IPs in an IP range; prefix: free child prefixes in a prefix; vlan: free VLAN IDs in a VLAN group"`
	ParentID int    `json:"parent_id" jsonschema:"id of the parent prefix, IP range or VLAN group"`
	Limit    int    `json:"limit,omitempty" jsonschema:"list only: max results (default 50, max 200)"`
	Body     any    `json:"body,omitempty" jsonschema:"allocate: fields for the new object(s), e.g. {\"description\": \"...\"} for an IP, {\"prefix_length\": 24} for a prefix, {\"name\": \"...\"} for a VLAN; an array allocates several at once (all or nothing). Omit for one IP with defaults"`
	Reason   string `json:"reason,omitempty" jsonschema:"required for allocate: why, recorded as the NetBox changelog message"`
}

var availablePaths = map[string]string{
	"ip":          "/api/ipam/prefixes/%d/available-ips/",
	"ip_in_range": "/api/ipam/ip-ranges/%d/available-ips/",
	"prefix":      "/api/ipam/prefixes/%d/available-prefixes/",
	"vlan":        "/api/ipam/vlan-groups/%d/available-vlans/",
}

func registerAvailable(s *mcp.Server, d Deps) error {
	actions := []any{"list"}
	desc := "Find free IP addresses, child prefixes or VLAN IDs inside a parent prefix, IP range or VLAN group."
	if d.Config.AllowCreate {
		actions = append(actions, "allocate")
		desc += fmt.Sprintf(" allocate atomically creates the next free object(s) (up to %d per call; all or nothing, "+
			"and a conflict means the parent is full), so use it rather than list-then-create, which races other users. "+
			"allocate requires reason.", d.Config.MaxBulk)
	} else {
		desc += " Allocating is disabled by the operator (create is off)."
	}
	schema, err := jsonschema.For[AvailableInput](nil)
	if err != nil {
		return err
	}
	schema.Properties["action"].Enum = actions
	schema.Properties["kind"].Enum = []any{"ip", "ip_in_range", "prefix", "vlan"}
	schema.Required = []string{"action", "kind", "parent_id"}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "netbox_available",
		Title:       "Free IPs, prefixes and VLANs",
		Description: desc,
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: !d.Config.AllowCreate, DestructiveHint: new(false), OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in AvailableInput) (*mcp.CallToolResult, any, error) {
		tmpl, ok := availablePaths[in.Kind]
		if !ok {
			return nil, nil, fmt.Errorf("unknown kind %q", in.Kind)
		}
		if in.ParentID <= 0 {
			return nil, nil, errors.New("parent_id is required")
		}
		path := fmt.Sprintf(tmpl, in.ParentID)
		switch {
		case in.Action == "list":
			limit := in.Limit
			if limit <= 0 {
				limit = defaultLimit
			}
			limit = min(limit, maxItems)
			resp, err := d.Client.Do(ctx, http.MethodGet, path, url.Values{"limit": {strconv.Itoa(limit)}}, nil, nil)
			if err != nil {
				return nil, nil, err
			}
			v, err := resp.Decode()
			if err != nil {
				return nil, nil, err
			}
			// available-prefixes ignores limit; enforce it here.
			if arr, ok := v.([]any); ok && len(arr) > limit {
				v = arr[:limit]
			}
			return nil, page(strip(v), 0), nil
		case in.Action == "allocate" && d.Config.AllowCreate:
			body := in.Body
			if body == nil {
				body = map[string]any{}
			}
			out, err := d.doWrite(ctx, write{tool: "netbox_available", action: "allocate", method: http.MethodPost, path: path, body: body, reason: in.Reason})
			return nil, out, err
		}
		return nil, nil, fmt.Errorf("action %q is not available", in.Action)
	})
	return nil
}
