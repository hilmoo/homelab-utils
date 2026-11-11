package devices

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GuestOptions struct {
	Keep   []int         // IDs that must not be shut down
	Wait   time.Duration // max time to wait for guests to stop
	Force  bool          // force stop guests still running after Wait
	DryRun bool
}

type Guest struct {
	ID   int
	Kind string // "qm" or "pct"
}

// ShutdownGuests gracefully shuts down every running guest except GuestOptions.Keep.
// It returns the guests it issued a shutdown for, even on error, so the caller
// can start them again. If ctx is cancelled it stops waiting, but lets the
// shutdowns already issued finish.
func ShutdownGuests(ctx context.Context, o GuestOptions) ([]Guest, error) {
	keep := map[int]bool{}
	for _, id := range o.Keep {
		keep[id] = true
	}

	guests, err := running(ctx, keep)
	if err != nil {
		return nil, err
	}
	if len(guests) == 0 {
		log.Println("no running guests to shut down")
		return nil, nil
	}

	var issued []Guest
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, g := range guests {
		log.Printf("shutting down %s %d", g.Kind, g.ID)
		if o.DryRun {
			continue
		}
		// --timeout makes proxmox wait for the guest, so run them in parallel.
		// Not CommandContext: an abort must not kill a half-done shutdown.
		cmd := exec.Command(g.Kind, "shutdown", strconv.Itoa(g.ID),
			"--timeout", strconv.Itoa(int(o.Wait.Seconds())))
		if err := cmd.Start(); err != nil {
			log.Printf("%s shutdown %d: %v", g.Kind, g.ID, err)
			continue
		}
		issued = append(issued, g)
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd.Wait()
		}()
	}
	if o.DryRun {
		return nil, nil
	}

	deadline := time.Now().Add(o.Wait + 10*time.Second)
	for time.Now().Before(deadline) {
		left, err := running(ctx, keep)
		if err != nil {
			return issued, err
		}
		if len(left) == 0 {
			return issued, nil
		}
		select {
		case <-ctx.Done():
			return issued, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}

	left, _ := running(ctx, keep)
	if len(left) == 0 {
		return issued, nil
	}
	if !o.Force {
		return issued, fmt.Errorf("%d guests still running after %s", len(left), o.Wait)
	}
	for _, g := range left {
		log.Printf("force stopping %s %d", g.Kind, g.ID)
		if err := exec.CommandContext(ctx, g.Kind, "stop", strconv.Itoa(g.ID)).Run(); err != nil {
			log.Printf("%s stop %d: %v", g.Kind, g.ID, err)
		}
	}
	return issued, nil
}

// StartGuests starts guests again through Proxmox's startall (pvenode), which honours
// each guest's configured startup order and up-delays (the "startup" option).
// Guests that are already running are skipped.
func StartGuests(ctx context.Context, guests []Guest) error {
	if len(guests) == 0 {
		return nil
	}
	ids := make([]string, len(guests))
	for i, g := range guests {
		ids[i] = strconv.Itoa(g.ID)
	}
	list := strings.Join(ids, ",")
	log.Printf("starting guests %s in their startup order", list)
	// --force: start even guests with onboot unset/0.
	out, err := exec.CommandContext(ctx, "pvenode", "startall", "--vms", list, "--force").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pvenode startall: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// running lists running VMs (qm) and containers (pct) whose ID is not in keep.
func running(ctx context.Context, keep map[int]bool) ([]Guest, error) {
	var out []Guest
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
				out = append(out, Guest{id, kind})
			}
		}
	}
	return out, nil
}
