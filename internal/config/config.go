// Package config parses flags and environment into a Config.
//
// Secrets (the NetBox API token and the HTTP bearer token) are only ever read
// from files, a systemd credential, or, with a warning, the environment. There
// is deliberately no flag that takes a secret on argv, where any local user can
// read it from /proc/<pid>/cmdline.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Groups are the tool groups --tool-groups may name. core is always on.
var Groups = []string{"core", "ipam", "dcim", "tenancy", "virtualization", "extras"}

type Config struct {
	URL            *url.URL
	Token          string
	RequestTimeout time.Duration
	LogLevel       slog.Level

	AllowCreate bool
	AllowUpdate bool
	AllowDelete bool
	MaxBulk     int
	Groups      []string // empty means all

	HTTP          bool
	Addr          string
	Path          string
	HTTPAuthToken string

	ShowVersion bool
}

// LogValue keeps secrets out of logs however the config is printed.
func (c *Config) LogValue() slog.Value {
	u := ""
	if c.URL != nil {
		u = c.URL.String()
	}
	return slog.GroupValue(
		slog.String("url", u),
		slog.Bool("token_set", c.Token != ""),
		slog.Bool("allow_create", c.AllowCreate),
		slog.Bool("allow_update", c.AllowUpdate),
		slog.Bool("allow_delete", c.AllowDelete),
		slog.Int("max_bulk", c.MaxBulk),
		slog.Any("groups", c.Groups),
		slog.Bool("http", c.HTTP),
		slog.String("addr", c.Addr),
		slog.Bool("http_auth_set", c.HTTPAuthToken != ""),
	)
}

// Parse reads args and the environment. getenv is injected for tests.
// Warnings (non-fatal) are returned for the caller to log once a logger exists.
func Parse(args []string, getenv func(string) string) (*Config, []string, error) {
	var (
		c                        Config
		rawURL, tokenFile, hauth string
		groups, logLevel         string
		stdio                    bool
		warnings                 []string
	)
	fs := flag.NewFlagSet("netbox-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&rawURL, "url", getenv("NETBOX_URL"), "NetBox base URL (env NETBOX_URL)")
	fs.StringVar(&tokenFile, "api-token-file", getenv("NETBOX_API_TOKEN_FILE"), "file holding the v2 API token nbt_<key>.<token> (env NETBOX_API_TOKEN_FILE)")
	fs.DurationVar(&c.RequestTimeout, "request-timeout", 30*time.Second, "per-request timeout to NetBox")
	fs.StringVar(&logLevel, "log-level", or(getenv("NETBOX_MCP_LOG_LEVEL"), "info"), "debug|info|warn|error (env NETBOX_MCP_LOG_LEVEL)")
	fs.BoolVar(&c.AllowCreate, "allow-create", false, "enable create actions")
	fs.BoolVar(&c.AllowUpdate, "allow-update", false, "enable update actions")
	fs.BoolVar(&c.AllowDelete, "allow-delete", false, "enable delete actions")
	fs.IntVar(&c.MaxBulk, "max-bulk", 50, "maximum objects in one bulk write")
	fs.StringVar(&groups, "tool-groups", "", "comma-separated tool groups to enable (default all): "+strings.Join(Groups, ","))
	fs.BoolVar(&stdio, "stdio", false, "serve over stdio (default)")
	fs.BoolVar(&c.HTTP, "http", false, "serve over streamable HTTP")
	fs.StringVar(&c.Addr, "addr", "127.0.0.1:8231", "HTTP listen address")
	fs.StringVar(&c.Path, "path", "/mcp", "HTTP MCP endpoint path")
	fs.StringVar(&hauth, "http-auth-token-file", getenv("NETBOX_MCP_HTTP_AUTH_TOKEN_FILE"), "file holding the bearer token HTTP clients must send (env NETBOX_MCP_HTTP_AUTH_TOKEN_FILE)")
	fs.BoolVar(&c.ShowVersion, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if c.ShowVersion {
		return &c, nil, nil
	}
	if stdio && c.HTTP {
		return nil, nil, errors.New("--stdio and --http are mutually exclusive")
	}

	if err := c.LogLevel.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, nil, fmt.Errorf("--log-level: %w", err)
	}

	if rawURL == "" {
		return nil, nil, errors.New("--url (or NETBOX_URL) is required")
	}
	u, err := url.Parse(strings.TrimRight(rawURL, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, nil, fmt.Errorf("--url must be an http(s) URL, got %q", rawURL)
	}
	c.URL = u

	credDir := getenv("CREDENTIALS_DIRECTORY")
	switch {
	case tokenFile != "":
		c.Token, err = readSecret(tokenFile)
	case getenv("NETBOX_API_TOKEN") != "":
		c.Token = strings.TrimSpace(getenv("NETBOX_API_TOKEN"))
		warnings = append(warnings, "API token read from NETBOX_API_TOKEN; prefer --api-token-file, the environment is readable via /proc")
	case credDir != "":
		c.Token, err = readSecret(filepath.Join(credDir, "api-token"))
	default:
		err = errors.New("no API token: set --api-token-file, NETBOX_API_TOKEN_FILE, NETBOX_API_TOKEN or the systemd credential api-token")
	}
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(c.Token, "nbt_") {
		warnings = append(warnings, "API token does not look like a v2 token (nbt_<key>.<token>); v1 tokens are not supported")
	}

	if groups != "" {
		for g := range strings.SplitSeq(groups, ",") {
			g = strings.TrimSpace(g)
			if !slices.Contains(Groups, g) {
				return nil, nil, fmt.Errorf("--tool-groups: unknown group %q (want %s)", g, strings.Join(Groups, ","))
			}
			c.Groups = append(c.Groups, g)
		}
	}
	if c.MaxBulk < 1 {
		return nil, nil, errors.New("--max-bulk must be at least 1")
	}

	if c.HTTP {
		if !strings.HasPrefix(c.Path, "/") {
			return nil, nil, errors.New("--path must start with /")
		}
		switch {
		case hauth != "":
			c.HTTPAuthToken, err = readSecret(hauth)
		case credDir != "":
			if s, e := readSecret(filepath.Join(credDir, "http-auth-token")); e == nil {
				c.HTTPAuthToken = s
			}
		}
		if err != nil {
			return nil, nil, err
		}
		if c.HTTPAuthToken == "" && !loopback(c.Addr) {
			return nil, nil, fmt.Errorf("refusing to listen on non-loopback %s without --http-auth-token-file", c.Addr)
		}
	}
	return &c, warnings, nil
}

// Enabled reports whether a tool group is on.
func (c *Config) Enabled(group string) bool {
	return group == "core" || len(c.Groups) == 0 || slices.Contains(c.Groups, group)
}

func readSecret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret: %w", err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("secret file %s is empty", path)
	}
	return s, nil
}

func loopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
