package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"router-HA/internal/config"
	"router-HA/internal/devices"
	"router-HA/internal/outage"
)

// Set at build time via -ldflags by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	path := flag.String("config", "/etc/router-ha.yaml", "path to the YAML config file")
	dry := flag.Bool("dry-run", false, "log destructive actions instead of running them (overrides config)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("router-ha %s (commit %s, built %s)\n", version, commit, date)
		return
	}

	c, err := config.Load(*path, *dry)
	if err != nil {
		log.Fatal(err)
	}

	rt := &devices.Router{URL: c.RouterURL, User: c.RouterUser, Password: c.RouterPass, SSIDs: c.SSIDs, DryRun: c.DryRun}
	pl := &devices.Plug{BaseURL: c.PlugBase, Timeout: c.HTTPTimeout, DryRun: c.DryRun}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Startup background jobs: /on until 200 OK, and SSIDs off after a delay.
	jobsCtx, cancelJobs := context.WithCancel(ctx)
	defer cancelJobs()
	go pl.OnUntilOK(jobsCtx, c.PlugOnRetry)
	go rt.DisableAfter(jobsCtx, c.SSIDOffAfter, c.PlugOnRetry)

	log.Printf("pinging %s every %s, procedure starts after %s unreachable (dry-run=%v)",
		c.PingHost, c.PingInterval, c.Fallback, c.DryRun)

	if !outage.Wait(ctx, c.PingHost, c.PingInterval, c.PingTimeout, c.Fallback) {
		log.Println("exiting")
		return
	}

	cancelJobs() // the host is going down; stop any pending retries
	outage.Run(ctx, c, rt, pl)

	log.Printf("[final] running %v", c.ShutdownArgs)
	if c.DryRun {
		log.Println("dry-run: not shutting down")
		return
	}
	if err := exec.Command(c.ShutdownArgs[0], c.ShutdownArgs[1:]...).Run(); err != nil {
		log.Fatalf("shutdown failed: %v", err)
	}
}
