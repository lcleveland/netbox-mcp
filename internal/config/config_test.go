package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTokenLookupOrder(t *testing.T) {
	dir := t.TempDir()
	flagFile := write(t, dir, "flag", "nbt_flag.secretsecret\n")
	envFile := write(t, dir, "env", "nbt_envfile.secretsecret")
	cred := t.TempDir()
	write(t, cred, "api-token", " nbt_cred.secretsecret ")

	env := map[string]string{
		"NETBOX_URL":            "https://nb.example/",
		"NETBOX_API_TOKEN_FILE": envFile,
		"NETBOX_API_TOKEN":      "nbt_env.secretsecret",
		"CREDENTIALS_DIRECTORY": cred,
	}
	get := func(k string) string { return env[k] }

	cases := []struct {
		args   []string
		drop   []string
		want   string
		warned bool
	}{
		{[]string{"--api-token-file", flagFile}, nil, "nbt_flag.secretsecret", false},
		{nil, nil, "nbt_envfile.secretsecret", false},
		{nil, []string{"NETBOX_API_TOKEN_FILE"}, "nbt_env.secretsecret", true},
		{nil, []string{"NETBOX_API_TOKEN_FILE", "NETBOX_API_TOKEN"}, "nbt_cred.secretsecret", false},
	}
	for _, tc := range cases {
		for _, k := range tc.drop {
			delete(env, k)
		}
		c, warns, err := Parse(tc.args, get)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if c.Token != tc.want {
			t.Errorf("token = %q, want %q", c.Token, tc.want)
		}
		if (len(warns) > 0) != tc.warned {
			t.Errorf("want %q: warnings %v", tc.want, warns)
		}
		if c.URL.String() != "https://nb.example" {
			t.Errorf("url = %s", c.URL)
		}
	}
	delete(env, "CREDENTIALS_DIRECTORY")
	if _, _, err := Parse(nil, get); err == nil {
		t.Error("expected error with no token source")
	}
}

func TestRedaction(t *testing.T) {
	c, _, err := Parse([]string{"--url", "https://nb.example"}, func(k string) string {
		if k == "NETBOX_API_TOKEN" {
			return "nbt_abc.supersecretvalue"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("x", "config", c)
	if strings.Contains(buf.String(), "supersecretvalue") {
		t.Fatalf("token leaked: %s", buf.String())
	}
}

func TestValidation(t *testing.T) {
	get := func(k string) string {
		return map[string]string{"NETBOX_URL": "https://nb", "NETBOX_API_TOKEN": "nbt_a.b"}[k]
	}
	for _, args := range [][]string{
		{"--url", "ftp://x"},
		{"--tool-groups", "ipam,bogus"},
		{"--max-bulk", "0"},
		{"--http", "--addr", "0.0.0.0:8231"},
		{"--http", "--stdio"},
	} {
		if _, _, err := Parse(args, get); err == nil {
			t.Errorf("%v: expected error", args)
		}
	}
	c, _, err := Parse([]string{"--tool-groups", "ipam"}, get)
	if err != nil || !c.Enabled("core") || !c.Enabled("ipam") || c.Enabled("dcim") {
		t.Errorf("groups: %v %v", c, err)
	}
}
