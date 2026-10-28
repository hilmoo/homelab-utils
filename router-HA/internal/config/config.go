// Package config loads the YAML configuration file.
package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	PingHost     string        `koanf:"ping_host"`
	PingInterval time.Duration `koanf:"ping_interval"`
	PingTimeout  time.Duration `koanf:"ping_timeout"`
	Fallback     time.Duration `koanf:"fallback"`

	DiscordWebhook string `koanf:"discord_webhook"`

	KeepIDs       []int         `koanf:"keep_ids"`
	GuestWait     time.Duration `koanf:"guest_wait"`
	GuestForceOff bool          `koanf:"guest_force"`

	RouterURL    string        `koanf:"router_url"`
	RouterUser   string        `koanf:"router_user"`
	RouterPass   string        `koanf:"router_pass"`
	SSIDs        []int         `koanf:"ssids"`
	SSIDOffAfter time.Duration `koanf:"ssid_off_after"`

	PlugBase    string        `koanf:"plug_url"`
	PlugOnRetry time.Duration `koanf:"on_retry"`
	OffDelay    time.Duration `koanf:"off_delay"`
	OffRetry    time.Duration `koanf:"off_retry"`
	OffMaxTries int           `koanf:"off_tries"`
	HTTPTimeout time.Duration `koanf:"http_timeout"`
	ShutdownCmd string        `koanf:"shutdown_cmd"`
	DryRun      bool          `koanf:"dry_run"`

	// ShutdownArgs is ShutdownCmd split into program and arguments.
	ShutdownArgs []string `koanf:"-"`
}

func defaults() *Config {
	return &Config{
		PingHost:      "192.168.1.4",
		PingInterval:  10 * time.Second,
		PingTimeout:   2 * time.Second,
		Fallback:      2 * time.Minute,
		GuestWait:     3 * time.Minute,
		GuestForceOff: true,
		RouterURL:     "http://192.168.1.1",
		RouterUser:    "admin",
		SSIDs:         []int{1, 5},
		SSIDOffAfter:  5 * time.Minute,
		PlugBase:      "http://192.168.1.3",
		PlugOnRetry:   5 * time.Second,
		OffDelay:      5 * time.Minute,
		OffRetry:      5 * time.Second,
		OffMaxTries:   5,
		HTTPTimeout:   10 * time.Second,
		ShutdownCmd:   "systemctl poweroff",
	}
}

// Load reads path on top of the defaults. dryRun forces DryRun on.
func Load(path string, dryRun bool) (*Config, error) {
	c := defaults()
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	err := k.UnmarshalWithConf("", c, koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
			WeaklyTypedInput: true,
			ErrorUnused:      true, // catch typos in keys
		},
	})
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if dryRun {
		c.DryRun = true
	}
	c.ShutdownArgs = strings.Fields(c.ShutdownCmd)
	if len(c.ShutdownArgs) == 0 {
		return nil, errors.New("shutdown_cmd must not be empty")
	}
	return c, nil
}
