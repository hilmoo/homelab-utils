package devices

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type GuestOptions struct {
	Keep   []int         // IDs that must not be shut down
	Wait   time.Duration // max time to wait for guests to stop
	Force  bool          // force stop guests still running after Wait
	DryRun bool
}

type guest struct {
	id   int
	kind string // "qm" or "pct"
}

// ShutdownGuests gracefully shuts down every running guest except GuestOptions.Keep.
func ShutdownGuests(ctx context.Context, o GuestOptions) error {
	keep := map[int]bool{}
	for _, id := range o.Keep {
		keep[id] = true
	}

	guests, err := running(ctx, keep)
	if err != nil {
		return err
	}
	if len(guests) == 0 {
		log.Println("no running guests to shut down")
		return nil
	}
	for _, g := range guests {
		log.Printf("shutting down %s %d", g.kind, g.id)
		if o.DryRun {
			continue
		}
		// --timeout makes proxmox wait for the guest, so run them in parallel.
		cmd := exec.CommandContext(ctx, g.kind, "shutdown", strconv.Itoa(g.id),
			"--timeout", strconv.Itoa(int(o.Wait.Seconds())))
		if err := cmd.Start(); err != nil {
			log.Printf("%s shutdown %d: %v", g.kind, g.id, err)
			continue
		}
		go cmd.Wait()
	}
	if o.DryRun {
		return nil
	}

	deadline := time.Now().Add(o.Wait + 10*time.Second)
	for time.Now().Before(deadline) {
		left, err := running(ctx, keep)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	left, _ := running(ctx, keep)
	if len(left) == 0 {
		return nil
	}
	if !o.Force {
		return fmt.Errorf("%d guests still running after %s", len(left), o.Wait)
	}
	for _, g := range left {
		log.Printf("force stopping %s %d", g.kind, g.id)
		if err := exec.CommandContext(ctx, g.kind, "stop", strconv.Itoa(g.id)).Run(); err != nil {
			log.Printf("%s stop %d: %v", g.kind, g.id, err)
		}
	}
	return nil
}

// running lists running VMs (qm) and containers (pct) whose ID is not in keep.
func running(ctx context.Context, keep map[int]bool) ([]guest, error) {
	var out []guest
	for _, kind := range []string{"qm", "pct"} {
		b, err := exec.CommandContext(ctx, kind, "list").Output()
		if err != nil {
			return nil, fmt.Errorf("%s list: %w", kind, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if i == 0 || len(f) < 3 {
				continue
			}
			id, err := strconv.Atoi(f[0])
			if err != nil || keep[id] {
				continue
			}
			// qm: VMID NAME STATUS ...   pct: VMID Status Lock Name
			status := f[2]
			if kind == "pct" {
				status = f[1]
			}
			if status == "running" {
				out = append(out, guest{id, kind})
			}
		}
	}
	return out, nil
}
