package outage

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"

	"router-HA/internal/config"

	"router-HA/internal/devices"
)

// Run executes every step in order. A failing step is logged and the sequence
// continues, because the host must still end up powered off.
//
// While the steps run, the ping host is watched. If it answers again
// c.RecoverPings times in a row, the sequence is aborted and what was done is
// undone. Run returns true when the caller may power the host off, and false
// when the sequence was aborted (power is back, or ctx was cancelled).
func Run(ctx context.Context, c *config.Config, rt *devices.Router, pl *devices.Plug) bool {
	seq, cancel := context.WithCancel(ctx)
	defer cancel()

	var recovered atomic.Bool
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		if WaitRecovered(seq, c.PingHost, c.PingInterval, c.PingTimeout, c.RecoverPings) {
			log.Printf("%s is back, aborting the shutdown sequence", c.PingHost)
			recovered.Store(true)
			cancel()
		}
	}()

	var stopped []devices.Guest // guests we shut down, to start again on abort
	plugReached := false        // /off may have been sent, so /on must be sent on abort

	steps := []struct {
		name string
		fn   func() error
	}{
		{"send Discord webhook", func() error { return notify(seq, c) }},
		{"shut down VMs and LXCs", func() (err error) {
			stopped, err = devices.ShutdownGuests(seq, devices.GuestOptions{
				Keep: c.KeepIDs, Wait: c.GuestWait, Force: c.GuestForceOff, DryRun: c.DryRun,
			})
			return err
		}},
		{"turn on SSIDs", func() error { return rt.Set(true) }},
		{"send /off", func() error {
			plugReached = true
			return pl.Off(seq, c.OffDelay, c.OffMaxTries, c.OffRetry)
		}},
		{"send final Discord webhook", func() error { return notifyFinal(seq, c) }},
	}
	for i, s := range steps {
		if seq.Err() != nil {
			break
		}
		log.Printf("[step %d/%d] %s ...", i+1, len(steps), s.name)
		if err := s.fn(); err != nil {
			log.Printf("[step %d/%d] %s FAILED: %v", i+1, len(steps), s.name, err)
			continue
		}
		log.Printf("[step %d/%d] %s ok", i+1, len(steps), s.name)
	}

	// Point of no return: stop watching. After this the caller runs
	// shutdown_cmd and nothing can be aborted any more.
	cancel()
	<-watchDone

	if recovered.Load() {
		undo(ctx, c, pl, stopped, plugReached)
		return false
	}
	if ctx.Err() != nil {
		log.Println("interrupted, not powering off")
		return false
	}
	return true
}

// undo reverses a sequence that was aborted because the host came back.
// SSIDs need nothing: the sequence only turns them on, their normal state is
// restored by the startup job once monitoring resumes.
func undo(ctx context.Context, c *config.Config, pl *devices.Plug, stopped []devices.Guest, plugReached bool) {
	log.Println("[undo] power is back, reverting")
	if plugReached {
		log.Println("[undo] sending /on")
		go SendOn(ctx, c, pl) // may be unreachable; must not block monitoring
	}
	if err := devices.StartGuests(ctx, stopped); err != nil {
		log.Printf("[undo] start guests FAILED: %v", err)
	}
	if err := notifyCancelled(ctx, c, len(stopped)); err != nil {
		log.Printf("[undo] discord webhook FAILED: %v", err)
	}
}

// SendOn waits for the configured guests to run (up to on_wait_timeout), then
// requests /on until it succeeds. Meant to run as a background job.
func SendOn(ctx context.Context, c *config.Config, pl *devices.Plug) {
	if len(c.OnWaitIDs) > 0 && !c.DryRun {
		log.Printf("[on-job] waiting for guests %v before /on", c.OnWaitIDs)
		if !devices.WaitGuestsRunning(ctx, c.OnWaitIDs, c.PlugOnRetry, c.OnWaitTimeout) {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[on-job] guests not running after %s, sending /on anyway", c.OnWaitTimeout)
		}
	}
	pl.OnUntilOK(ctx, c.PlugOnRetry)
}

// NotifyPowerBack tells Discord that the host has booted after an outage.
func NotifyPowerBack(ctx context.Context, c *config.Config) {
	if c.DiscordWebhook == "" {
		return
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("⚡ Power is back. Proxmox host `%s` has started.", host)
	if err := devices.SendDiscord(ctx, c.DiscordWebhook, msg, c.HTTPTimeout); err != nil {
		log.Printf("power-back discord webhook FAILED: %v", err)
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

func notifyFinal(ctx context.Context, c *config.Config) error {
	if c.DiscordWebhook == "" {
		log.Println("no discord_webhook set, skipping")
		return nil
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("🔌 Shutdown sequence finished. Powering off Proxmox host `%s` now.", host)
	return devices.SendDiscord(ctx, c.DiscordWebhook, msg, c.HTTPTimeout)
}

func notifyCancelled(ctx context.Context, c *config.Config, guests int) error {
	if c.DiscordWebhook == "" {
		return nil
	}
	host, _ := os.Hostname()
	msg := fmt.Sprintf("✅ %s is reachable again. Shutdown of `%s` cancelled; started %d guest(s) again.", c.PingHost, host, guests)
	return devices.SendDiscord(ctx, c.DiscordWebhook, msg, c.HTTPTimeout)
}
