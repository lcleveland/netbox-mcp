// Command netbox-mcp is an MCP server for NetBox.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lcleveland/netbox-mcp/internal/config"
	"github.com/lcleveland/netbox-mcp/internal/netbox"
	"github.com/lcleveland/netbox-mcp/internal/server"
	"github.com/lcleveland/netbox-mcp/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "netbox-mcp:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, warnings, err := config.Parse(args, os.Getenv)
	if err != nil {
		return err
	}
	if cfg.ShowVersion {
		fmt.Println(version.Version)
		return nil
	}
	// stdout belongs to the stdio transport; logs go to stderr.
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	for _, w := range warnings {
		log.Warn(w)
	}
	log.Info("starting", "version", version.Version, "config", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := netbox.New(cfg.URL, cfg.Token, &http.Client{Timeout: cfg.RequestTimeout})
	s, n := server.New(cfg, c, log)
	log.Info("registered tools", "count", n)
	return server.ServeStdio(ctx, s)
}
