package netbox

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const maxErrBody = 2 << 10

// APIError is a non-2xx reply from NetBox. Error() is written for the model:
// what failed, NetBox's own words, and what to try next.
type APIError struct {
	Status int
	Method string
	Path   string // never includes the query string
	Detail string // NetBox's message, capped at 2 KiB
	Hint   string
	ETag   string // current ETag on a 412
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("NetBox %s %s: HTTP %d", e.Method, e.Path, e.Status)
	if e.Detail != "" {
		s += ": " + e.Detail
	}
	if e.Hint != "" {
		s += "\nHint: " + e.Hint
	}
	return s
}

func newAPIError(method, path string, resp *http.Response, body []byte, scrub func(string) string) *APIError {
	e := &APIError{Status: resp.StatusCode, Method: method, Path: path, ETag: resp.Header.Get("ETag")}
	e.Detail = scrub(detail(body))
	if len(e.Detail) > maxErrBody {
		e.Detail = e.Detail[:maxErrBody] + "…"
	}
	e.Hint = hint(e)
	return e
}

// detail pulls the useful message out of NetBox's several error shapes:
// {"detail"}, the 500 {"error","exception"}, or a field-error map/list.
func detail(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if d, ok := m["detail"].(string); ok {
			return d
		}
		if msg, ok := m["error"].(string); ok {
			if ex, ok := m["exception"].(string); ok {
				return ex + ": " + msg
			}
			return msg
		}
	}
	return strings.TrimSpace(string(body))
}

func hint(e *APIError) string {
	switch e.Status {
	case http.StatusBadRequest:
		return "NetBox rejected the request body or filters; the message above names the offending fields. Fix them and retry."
	case http.StatusForbidden:
		// NetBox returns 403 for both a bad token and a missing permission.
		if !strings.Contains(e.Detail, "permission") {
			return "the API token was rejected (NetBox reports auth failures as 403). Check the token with netbox_status."
		}
		return "the token's user lacks permission for this object type or action; ask the operator to grant it. netbox_status confirms the token itself works."
	case http.StatusNotFound:
		if strings.Contains(e.Detail, "matches the given query") {
			return "no object with that id exists (or the token cannot see it). List with a filter to find the right id."
		}
		return "this API route does not exist on this NetBox; check the path (trailing slash required) or the NetBox version."
	case http.StatusMethodNotAllowed:
		return "this endpoint does not support that method."
	case http.StatusConflict:
		return "NetBox refused due to a conflict: an object depends on this one (delete), or there is not enough free space (allocation). Resolve the dependency or choose another parent."
	case http.StatusPreconditionFailed:
		return "the object changed since you read it. Re-read it with get and retry with the new if_match."
	case http.StatusTooManyRequests:
		return "NetBox is rate limiting; wait and retry with fewer requests."
	case http.StatusInternalServerError:
		if strings.Contains(strings.ToLower(e.Detail), "maintenance") {
			return "NetBox is in maintenance mode and refuses writes; retry later."
		}
		return "NetBox hit an internal error; this is usually a server-side bug or bad data, not something to retry blindly."
	case http.StatusServiceUnavailable:
		return "NetBox has no background worker running (rq-workers-running is 0); ask the operator to start netbox-rq."
	}
	return ""
}
