// Package outage detects a dead host and runs the shutdown sequence.
package outage

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"time"
)

// Ping returns true if host answers one ping within timeout.
func Ping(ctx context.Context, host string, timeout time.Duration) bool {
	secs := strconv.Itoa(max(1, int(timeout.Seconds())))
	ctx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "ping", "-c", "1", "-W", secs, host).Run() == nil
}

// Wait pings host every interval until it has been unreachable for
// fallback. It returns true when the outage is confirmed, false if ctx was
// cancelled first. A successful ping resets the downtime.
func Wait(ctx context.Context, host string, interval, timeout, fallback time.Duration) bool {
	var downSince time.Time
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
		if Ping(ctx, host, timeout) {
			if !downSince.IsZero() {
				log.Printf("%s is back, resetting", host)
			}
			downSince = time.Time{}
			continue
		}
		if downSince.IsZero() {
			downSince = time.Now()
		}
		log.Printf("%s unreachable for %s/%s", host, time.Since(downSince).Round(time.Second), fallback)
		if time.Since(downSince) >= fallback {
			return true
		}
	}
}
