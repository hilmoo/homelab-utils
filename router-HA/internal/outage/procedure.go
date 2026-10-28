package outage

import (
	"context"
	"fmt"
	"log"
	"os"

	"router-HA/internal/config"

	"router-HA/internal/devices"
)

// Run executes every step in order. A failing step is logged and the sequence
// continues, because the host must still end up powered off.
func Run(ctx context.Context, c *config.Config, rt *devices.Router, pl *devices.Plug) {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"send Discord webhook", func() error { return notify(ctx, c) }},
		{"shut down VMs and LXCs", func() error {
			return devices.ShutdownGuests(ctx, devices.GuestOptions{
				Keep: c.KeepIDs, Wait: c.GuestWait, Force: c.GuestForceOff, DryRun: c.DryRun,
			})
		}},
		{"turn on SSIDs", func() error { return rt.Set(true) }},
		{"send /off", func() error { return pl.Off(ctx, c.OffDelay, c.OffMaxTries, c.OffRetry) }},
	}
	for i, s := range steps {
		log.Printf("[step %d/%d] %s ...", i+1, len(steps), s.name)
		if err := s.fn(); err != nil {
			log.Printf("[step %d/%d] %s FAILED: %v", i+1, len(steps), s.name, err)
			continue
		}
		log.Printf("[step %d/%d] %s ok", i+1, len(steps), s.name)
	}
}

func notify(ctx context.Context, c *config.Config) error {
	if c.DiscordWebhook == "" {
		log.Println("no discord_webhook set, skipping")
		return nil
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("⚠️ %s unreachable for %s. Shutting down Proxmox host `%s`.", c.PingHost, c.Fallback, host)
	return devices.SendDiscord(ctx, c.DiscordWebhook, msg, c.HTTPTimeout)
}
