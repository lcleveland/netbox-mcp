package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type TraceInput struct {
	Kind string `json:"kind" jsonschema:"interface: trace the cable path from a device interface; front_port / rear_port: list the cable paths passing through a patch-panel port"`
	ID   int    `json:"id" jsonschema:"id of the interface, front port or rear port"`
}

var tracePaths = map[string]string{
	"interface":  "/api/dcim/interfaces/%d/trace/",
	"front_port": "/api/dcim/front-ports/%d/paths/",
	"rear_port":  "/api/dcim/rear-ports/%d/paths/",
}

func registerTrace(s *mcp.Server, d Deps) error {
	schema, err := jsonschema.For[TraceInput](nil)
	if err != nil {
		return err
	}
	schema.Properties["kind"].Enum = []any{"interface", "front_port", "rear_port"}
	schema.Required = []string{"kind", "id"}

	mcp.AddTool(s, &mcp.Tool{
		Name:  "netbox_trace",
		Title: "Cable trace",
		Description: "Answer \"what is this port plugged into\". interface returns the cable path as segments of " +
			"[near-end terminations, cable, far-end terminations], through patch panels and circuits. " +
			"front_port/rear_port return every cable path that passes through that port. Find ids with netbox_interface or netbox_api.",
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in TraceInput) (*mcp.CallToolResult, any, error) {
		tmpl, ok := tracePaths[in.Kind]
		if !ok {
			return nil, nil, fmt.Errorf("unknown kind %q", in.Kind)
		}
		if in.ID <= 0 {
			return nil, nil, errors.New("id is required")
		}
		resp, err := d.Client.Do(ctx, http.MethodGet, fmt.Sprintf(tmpl, in.ID), nil, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		v, err := resp.Decode()
		if err != nil {
			return nil, nil, err
		}
		return nil, page(strip(v), 0), nil
	})
	return nil
}
