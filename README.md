# EBS Netwatch

Linux-only network availability monitor.

## Scope

- Linux only
- Local JSONL check logs
- Static dashboard
- Dashboard bind defaults to `0.0.0.0:58080`
- Standard library first

## Behavior

- Reads `config.json` when present, otherwise uses built-in defaults
- Checks configurable HTTP endpoints
- Default endpoints:
  - `google_204`: `https://www.google.com/generate_204`, expected status `204`
  - `cloudflare_trace`: `https://www.cloudflare.com/cdn-cgi/trace`, expected status `200-399`
  - `gstatic_204`: `https://www.gstatic.com/generate_204`, expected status `204`
- Detects the default Linux network interface
- Detects connection type as `ethernet`, `wifi`, or `unknown`
- Reads link speed from `/sys/class/net/<iface>/speed` when available
- Uses a 30 second normal interval
- Uses a 10 second suspect interval
- Runs gateway and DNS diagnostics when endpoint checks are abnormal
- Writes daily raw logs to `data/checks-YYYY-MM-DD.jsonl`
- Keeps raw logs for 14 days by default
- Maintains `data/manifest.json` for static publishing and GitHub Pages loading

## Layout

```text
.
├── cmd/
│   └── ebs-netwatch/main.go
├── config.example.json
├── data/
│   ├── checks-YYYY-MM-DD.jsonl
│   └── manifest.json
├── docs/
│   ├── app.js
│   ├── data/
│   │   ├── checks-YYYY-MM-DD.jsonl
│   │   └── manifest.json
│   ├── index.html
│   └── style.css
├── go.mod
├── internal/
│   ├── config/
│   ├── monitor/
│   ├── network/
│   ├── report/
│   ├── server/
│   └── storage/
├── scripts/
│   └── publish-raw-logs.sh
└── web/
    ├── app.js
    ├── index.html
    └── style.css
```

## Run locally

1. Optional: copy the example config if you want to customize settings:

   ```bash
   cp config.example.json config.json
   ```

2. Start the monitor:

   ```bash
   go run ./cmd/ebs-netwatch
   ```

3. Open the dashboard:

   ```text
   http://127.0.0.1:58080
   ```

The server prints a local URL and detected LAN URLs at startup. LAN access depends on your firewall and network isolation.

To force a local-only bind:

```bash
go run ./cmd/ebs-netwatch -bind 127.0.0.1:58080
```

To use a different port on all interfaces:

```bash
go run ./cmd/ebs-netwatch -bind 0.0.0.0:18080
```

## Run as a user service

Use a systemd user service to keep the monitor running.

### Install the monitor service

```bash
mkdir -p ~/.config/systemd/user
cp scripts/systemd/ebs-netwatch.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now ebs-netwatch.service
systemctl --user status ebs-netwatch.service
journalctl --user -u ebs-netwatch.service -n 100 --no-pager
```

### Verify logs

```bash
wc -l data/checks-*.jsonl
sleep 90
wc -l data/checks-*.jsonl
tail -n 3 data/checks-*.jsonl
```

## Raw logs

- Daily raw logs are written to `data/checks-YYYY-MM-DD.jsonl`
- `data/checks.jsonl` is treated as a legacy local file
- `data/manifest.json` lists the daily raw log files within the retention window
- The dashboard uses `/api/status` in local mode
- The static GitHub Pages page uses `data/manifest.json` and the raw JSONL files directly

## Static GitHub Pages

GitHub Pages serves the prepared static site from `docs/`.

- `docs/index.html`
- `docs/style.css`
- `docs/app.js`
- `docs/data/manifest.json`
- `docs/data/checks-YYYY-MM-DD.jsonl`

## Publish manually

Run the publish script from the repository root:

```bash
./scripts/publish-raw-logs.sh
```

The script:

- runs `go test ./...`
- runs the publish preparation command
- updates `data/manifest.json`
- mirrors raw log files into `docs/data/`
- stages only raw log artifacts
- commits only if those staged publish files changed
- pushes to the current branch

Static assets under `docs/` are updated by normal functional commits.

## Publish schedule

Run publishing every 30 minutes with a systemd user timer. The monitor stays separate.

### Install the user timer

```bash
mkdir -p ~/.config/systemd/user
cp scripts/systemd/ebs-netwatch-publish.* ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now ebs-netwatch-publish.timer
systemctl --user list-timers ebs-netwatch-publish.timer
journalctl --user -u ebs-netwatch-publish.service -n 100 --no-pager
```

### Timer files

`~/.config/systemd/user/ebs-netwatch-publish.service`

```ini
[Unit]
Description=Publish EBS Netwatch raw logs

[Service]
Type=oneshot
WorkingDirectory=%h/projects/ebs-netwatch
ExecStart=%h/projects/ebs-netwatch/scripts/publish-raw-logs.sh
```

`~/.config/systemd/user/ebs-netwatch-publish.timer`

```ini
[Unit]
Description=Run EBS Netwatch publish every 30 minutes

[Timer]
OnBootSec=5min
OnUnitActiveSec=30min
Persistent=true
Unit=ebs-netwatch-publish.service

[Install]
WantedBy=timers.target
```

## Manifest format

`data/manifest.json` looks like this:

```json
{
  "generatedAt": "2026-06-01T00:00:00Z",
  "retentionDays": 14,
  "files": [
    {
      "date": "2026-06-01",
      "path": "data/checks-2026-06-01.jsonl"
    }
  ]
}
```

## Notes

- The dashboard and logs stay local unless you publish the raw logs and static site.
- `config.json` stays ignored by Git unless you choose to track it explicitly.
