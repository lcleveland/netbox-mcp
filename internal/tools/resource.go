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

// Resource is one first-class tool over a NetBox list endpoint.
// Adding a tool is adding an entry to the table; there is no per-resource code.
type Resource struct {
	Name        string // tool name, netbox_<model>
	Group       string
	Path        string // list endpoint, e.g. /api/ipam/prefixes/
	Title       string
	Description string // what it is and useful filters; action help is appended
	ReadOnly    bool   // only list/get, whatever the flags say
}

type Action string

const (
	List   Action = "list"
	Get    Action = "get"
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
)

func (a Action) write() bool { return a == Create || a == Update || a == Delete }

// Input is shared by every resource tool.
type Input struct {
	Action  Action         `json:"action" jsonschema:"what to do"`
	ID      int            `json:"id,omitempty" jsonschema:"object id for get, and for single-object update/delete"`
	Query   map[string]any `json:"query,omitempty" jsonschema:"list filters as NetBox query params, e.g. {\"site\": \"hq\", \"q\": \"core\", \"tag\": [\"a\",\"b\"]}"`
	Fields  string         `json:"fields,omitempty" jsonschema:"list only: comma-separated fields to return instead of the default brief objects"`
	Limit   int            `json:"limit,omitempty" jsonschema:"list only: max objects (default 50, max 200)"`
	Offset  int            `json:"offset,omitempty" jsonschema:"list only: skip this many objects (use next_offset from the previous page)"`
	Body    any            `json:"body,omitempty" jsonschema:"create/update: an object, or an array for bulk (bulk update objects need id). delete: optional array of ids for bulk delete"`
	Reason  string         `json:"reason,omitempty" jsonschema:"required for create/update/delete: why, recorded as the NetBox changelog message"`
	IfMatch string         `json:"if_match,omitempty" jsonschema:"single update only: the _etag from get; the update fails if the object changed since"`
}

// allowedActions filters a resource's actions by the operator's verb flags.
func (d Deps) allowedActions(readOnly bool) []Action {
	acts := []Action{List, Get}
	if readOnly {
		return acts
	}
	if d.Config.AllowCreate {
		acts = append(acts, Create)
	}
	if d.Config.AllowUpdate {
		acts = append(acts, Update)
	}
	if d.Config.AllowDelete {
		acts = append(acts, Delete)
	}
	return acts
}

// registerResource enforces the verb flags three times: the action enum omits
// disabled actions, the description never mentions them, and the handler
// refuses them. Only the last is load-bearing; the first two stop the model
// trying.
func registerResource(s *mcp.Server, d Deps, r Resource) error {
	actions := d.allowedActions(r.ReadOnly)
	schema, err := jsonschema.For[Input](nil)
	if err != nil {
		return err
	}
	schema.Properties["action"].Enum = make([]any, len(actions))
	for i, a := range actions {
		schema.Properties["action"].Enum[i] = string(a)
	}
	schema.Required = []string{"action"}
	writes := slices.ContainsFunc(actions, Action.write)

	mcp.AddTool(s, &mcp.Tool{
		Name:        r.Name,
		Title:       r.Title,
		Description: r.Description + actionHelp(actions, d.Config.MaxBulk, r.ReadOnly),
		InputSchema: schema,
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    !writes,
			DestructiveHint: new(slices.Contains(actions, Delete)),
			OpenWorldHint:   new(true),
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in Input) (*mcp.CallToolResult, any, error) {
		if !slices.Contains(actions, in.Action) {
			return nil, nil, fmt.Errorf("action %q is not available on %s (enabled: %s)", in.Action, r.Name, joinActions(actions))
		}
		out, err := d.resourceCall(ctx, r, in)
		return nil, out, err
	})
	return nil
}

func (d Deps) resourceCall(ctx context.Context, r Resource, in Input) (any, error) {
	item := func(id int) string { return r.Path + strconv.Itoa(id) + "/" }
	switch in.Action {
	case List:
		return d.list(ctx, r.Path, in.Query, in.Fields, in.Limit, in.Offset)
	case Get:
		if in.ID <= 0 {
			return nil, errors.New("get needs id")
		}
		resp, err := d.Client.Do(ctx, http.MethodGet, item(in.ID), nil, nil, nil)
		if err != nil {
			return nil, err
		}
		v, err := resp.Decode()
		if err != nil {
			return nil, err
		}
		m, _ := strip(v).(map[string]any)
		if m != nil && resp.Header.Get("ETag") != "" {
			m["_etag"] = resp.Header.Get("ETag")
		}
		return m, nil
	}

	// Writes.
	w := write{tool: r.Name, action: string(in.Action), reason: in.Reason, body: in.Body}
	switch in.Action {
	case Create:
		if in.Body == nil {
			return nil, errors.New("create needs body")
		}
		w.method, w.path = http.MethodPost, r.Path
	case Update:
		if in.Body == nil {
			return nil, errors.New("update needs body")
		}
		w.method, w.path = http.MethodPatch, r.Path
		if in.ID > 0 {
			w.path = item(in.ID)
		}
		if in.IfMatch != "" {
			if in.ID <= 0 {
				return nil, errors.New("if_match applies only to a single update with id")
			}
			w.hdr = http.Header{"If-Match": {in.IfMatch}}
		}
	case Delete:
		w.method, w.path = http.MethodDelete, r.Path
		if in.ID > 0 {
			w.path, w.body = item(in.ID), map[string]any{}
		} else if in.Body == nil {
			return nil, errors.New("delete needs id, or body with an array of ids")
		}
	}
	return d.doWrite(ctx, w)
}

func actionHelp(actions []Action, maxBulk int, readOnly bool) string {
	var b strings.Builder
	b.WriteString("\n\nActions: " + joinActions(actions) + ". ")
	b.WriteString("list returns brief objects by default (pass fields for specific ones) with count and next_offset; get returns the full object plus _etag.")
	if slices.ContainsFunc(actions, Action.write) {
		fmt.Fprintf(&b, " Writes require reason (stored as the changelog message). body may be an array of up to %d objects for an all-or-nothing bulk write.", maxBulk)
		b.WriteString(" Writing tags replaces the whole set; use add_tags/remove_tags to change it incrementally. custom_fields on update are merged into existing values; look up definitions with netbox_custom_field.")
		if slices.Contains(actions, Update) {
			b.WriteString(" Pass if_match (the _etag from get) to avoid overwriting a concurrent edit.")
		}
	}
	if readOnly {
		b.WriteString(" This object type is read-only.")
		return b.String()
	}
	var off []string
	for _, a := range []Action{Create, Update, Delete} {
		if !slices.Contains(actions, a) {
			off = append(off, string(a))
		}
	}
	if len(off) > 0 {
		b.WriteString(" " + strings.Join(off, "/") + " through this tool is disabled by the operator.")
	}
	return b.String()
}

func joinActions(as []Action) string {
	s := make([]string, len(as))
	for i, a := range as {
		s[i] = string(a)
	}
	return strings.Join(s, ", ")
}
