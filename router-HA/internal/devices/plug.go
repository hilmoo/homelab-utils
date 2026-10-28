package devices

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type Plug struct {
	BaseURL string
	Timeout time.Duration
	DryRun  bool
}

// OnUntilOK requests /on every retry until it returns 200 OK, then returns.
// It is meant to run as a background job.
func (c *Plug) OnUntilOK(ctx context.Context, retry time.Duration) {
	url := c.url("/on")
	for {
		err := get(ctx, url, c.Timeout)
		if err == nil {
			log.Printf("[on-job] %s returned 200 OK, job finished", url)
			return
		}
		log.Printf("[on-job] %v, retrying in %s", err, retry)
		if !sleep(ctx, retry) {
			return
		}
	}
}

// Off waits delay, then requests /off until it returns 200 OK or tries run out.
func (c *Plug) Off(ctx context.Context, delay time.Duration, tries int, retry time.Duration) error {
	log.Printf("waiting %s before sending /off", delay)
	if !sleep(ctx, delay) {
		return ctx.Err()
	}
	url := c.url("/off")
	var err error
	for i := 1; i <= tries; i++ {
		if c.DryRun {
			log.Printf("dry-run: would GET %s", url)
			return nil
		}
		if err = get(ctx, url, c.Timeout); err == nil {
			return nil
		}
		log.Printf("/off attempt %d/%d: %v", i, tries, err)
		if !sleep(ctx, retry) {
			return ctx.Err()
		}
	}
	return err
}

func (c *Plug) url(path string) string {
	return strings.TrimRight(c.BaseURL, "/") + path
}

// get returns nil only on 200 OK.
func get(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return nil
}

// sleep waits d; it returns false if ctx was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
