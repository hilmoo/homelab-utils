# router-HA

Small Go daemon for a Proxmox host. It watches a device (the router/upstream
at `192.168.1.4` by default). When that device stays unreachable, it gracefully
shuts down the guests, turns on the backup Wi-Fi SSIDs on an F6201B router
(via [`github.com/hilmoo/f6201B-ssid`](https://github.com/hilmoo/f6201B-ssid)),
switches off a device through `http://192.168.1.3/off`, and finally powers off
the host.

**Must run as root on the Proxmox host** (it calls `qm`, `pct` and `systemctl poweroff`).

## Flow

On start, two background jobs run:

- `GET <plug_url>/on`, retried every `on_retry` until it receives `200 OK`, then the job stops.
- After `ssid_off_after`, turn the SSIDs in `ssids` **off** again (the router may still have them on from the last outage). It retries every `on_retry` until it succeeds, then stops.

Both jobs are cancelled if the outage procedure begins.

Main loop:

1. Ping `ping_host` every `ping_interval`.
2. If it stays unreachable for `fallback`, start the procedure. Any successful ping resets the timer.
3. Send a message to the Discord webhook.
4. Shut down all running VMs (`qm`) and LXCs (`pct`) except the IDs in `keep_ids`.
   It waits up to `guest_wait`, then force-stops leftovers if `guest_force` is true.
5. Log in to the router and enable the SSIDs in `ssids` (default `1,5`).
6. Wait `off_delay`, then `GET <plug_url>/off`. It must return `200 OK`. It retries up to `off_tries` times.
7. Run `shutdown_cmd` (default `systemctl poweroff`).

A failing step (3–6) is logged and the procedure continues, because the host should still shut down.

## Configuration

Settings live in a YAML file (default `/etc/router-ha.yaml`, override with `-config`).
Copy [`config.example.yaml`](config.example.yaml) and edit it. Every key is optional;
omitted keys use the defaults shown in the example. Unknown keys are rejected (parsed with [koanf](https://github.com/knadh/koanf) + YAML), so typos fail loudly.

The only command-line flags are:

| Flag | Default | Description |
|---|---|---|
| `-config` | `/etc/router-ha.yaml` | Path to the YAML config |
| `-dry-run` | `false` | Log destructive actions instead of running them (overrides `dry_run`) |

| Key | Default | Description |
|---|---|---|
| `ping_host` | `192.168.1.4` | Host to ping |
| `ping_interval` | `10s` | Time between pings |
| `ping_timeout` | `2s` | Timeout of a single ping |
| `fallback` | `2m` | Continuous downtime before the procedure starts |
| `discord_webhook` | _(empty)_ | Discord webhook URL; empty skips the notification |
| `keep_ids` | `[]` | VM/LXC IDs that are not shut down |
| `guest_wait` | `3m` | Max wait for guests to shut down |
| `guest_force` | `true` | Force stop guests still running after `guest_wait` |
| `router_url` | `http://192.168.1.1` | F6201B router URL |
| `router_user` | `admin` | Router username |
| `router_pass` | _(empty)_ | Router password |
| `ssids` | `[1, 5]` | SSIDs to turn on during an outage and off after start |
| `ssid_off_after` | `5m` | Delay after app start before the SSIDs are turned off |
| `plug_url` | `http://192.168.1.3` | Base URL of the device with `/on` and `/off` |
| `on_retry` | `5s` | Retry interval of the startup `/on` and SSID-off jobs |
| `off_delay` | `5m` | Wait between enabling SSIDs and `/off` |
| `off_retry` | `5s` | Retry interval for `/off` |
| `off_tries` | `5` | Max `/off` attempts |
| `http_timeout` | `10s` | Timeout of each HTTP request |
| `shutdown_cmd` | `systemctl poweroff` | Final shutdown command |
| `dry_run` | `false` | Same as `-dry-run` |

The config holds the router password and webhook URL, so make it root-only: `chmod 600 /etc/router-ha.yaml`.

## Project layout

```
cmd/router-ha/    entry point: wires everything together
internal/config/  YAML config loading (koanf)
internal/outage/  monitor.go (ping, outage detection), procedure.go (ordered outage steps)
internal/devices/ proxmox.go (VMs/LXCs), router.go (SSIDs), plug.go (/on /off), discord.go (webhook)
```

## Build and run

```sh
GOOS=linux GOARCH=amd64 go build -o router-ha ./cmd/router-ha
cp config.example.yaml /etc/router-ha.yaml && chmod 600 /etc/router-ha.yaml
./router-ha -config /etc/router-ha.yaml -dry-run
```

## systemd example

`/etc/systemd/system/router-ha.service`:

```ini
[Unit]
Description=router-HA
After=network-online.target pve-guests.service

[Service]
ExecStart=/usr/local/bin/router-ha -config /etc/router-ha.yaml
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Make sure `ping_host` is up before you enable the service, otherwise the
procedure starts after `fallback`. Test with `-dry-run` first.
