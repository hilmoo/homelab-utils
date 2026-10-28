// Package devices holds clients for the things we control: Proxmox guests,
// the F6201B router, the /on /off plug and the Discord webhook.
package devices

import (
	"context"
	"log"
	"time"

	ssid "github.com/hilmoo/f6201B-ssid"
)

type Router struct {
	URL      string
	User     string
	Password string
	SSIDs    []int
	DryRun   bool
}

// Set turns the configured SSIDs on or off.
func (c *Router) Set(enable bool) error {
	if c.DryRun {
		log.Printf("dry-run: would set SSIDs %v enable=%v", c.SSIDs, enable)
		return nil
	}
	cl, err := ssid.Login(ssid.Config{RouterURL: c.URL, Username: c.User, Password: c.Password})
	if err != nil {
		return err
	}
	return cl.ToggleSSIDs(enable, c.SSIDs...)
}

// DisableAfter waits delay, then turns the SSIDs off, retrying every retry
// until the router accepts it. It is meant to run as a background job.
func (c *Router) DisableAfter(ctx context.Context, delay, retry time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(delay):
	}
	for {
		err := c.Set(false)
		if err == nil {
			log.Printf("[ssid-job] SSIDs %v disabled, job finished", c.SSIDs)
			return
		}
		log.Printf("[ssid-job] %v, retrying in %s", err, retry)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
	}
}
