# EBS Netwatch

EBS Netwatch is a small Linux-only, local-first network availability monitor.

## Scope for v1

- Linux only
- Local JSONL check logs
- Local static dashboard
- Local dashboard server on `127.0.0.1:8080`
- Standard library first

## What it does

- Loads configuration from `config.json`
- Checks configurable HTTP endpoints
- Ships with these default endpoints:
  - `google_204`: `https://www.google.com/generate_204`, expected status `204`
  - `cloudflare_trace`: `https://www.cloudflare.com/cdn-cgi/trace`, expected status `200-399`
  - `gstatic_204`: `https://www.gstatic.com/generate_204`, expected status `204`
- Detects the default Linux network interface
- Detects connection type as `ethernet`, `wifi`, or `unknown`
- Reads link speed from `/sys/class/net/<iface>/speed` when available
- Uses a 30 second normal interval
- Uses a 10 second non-operational interval
- Runs gateway and DNS diagnostics when endpoint checks are abnormal
- Writes daily raw logs to `data/checks-YYYY-MM-DD.jsonl`
- Keeps raw logs for 14 days by default
- Maintains `data/manifest.json` for static publishing and GitHub Pages loading

## Project layout

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

## Run it locally

1. Copy the example config:

   ```bash
   cp config.example.json config.json
   ```

2. Start the monitor:

   ```bash
   go run ./cmd/ebs-netwatch
   ```

3. Open the dashboard:

   ```text
   http://127.0.0.1:8080
   ```

## Raw logs and manifest

- Daily raw logs are written to `data/checks-YYYY-MM-DD.jsonl`
- `data/checks.jsonl` is treated as a legacy local file
- `data/manifest.json` lists the daily raw log files within the retention window
- The dashboard uses `/api/status` in local mode
- The static GitHub Pages page uses `data/manifest.json` and the raw JSONL files directly

## Static GitHub Pages mode

The published static site is prepared under `docs/` so GitHub Pages can serve it as a plain static site.

The static page loads:

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
- refreshes `docs/`
- commits only if staged tracked files changed
- pushes to the current branch

## Publish schedule

Publish every 30 minutes with either cron or a systemd timer.

### Cron

```cron
*/30 * * * * cd %h/projects/ebs-netwatch && ./scripts/publish-raw-logs.sh
```

### systemd timer

`/etc/systemd/system/ebs-netwatch-publish.service`

```ini
[Unit]
Description=Publish EBS Netwatch raw logs

[Service]
Type=oneshot
WorkingDirectory=%h/projects/ebs-netwatch
ExecStart=%h/projects/ebs-netwatch/scripts/publish-raw-logs.sh
```

`/etc/systemd/system/ebs-netwatch-publish.timer`

```ini
[Unit]
Description=Run EBS Netwatch publish every 30 minutes

[Timer]
OnBootSec=5min
OnUnitActiveSec=30min
Persistent=true

[Install]
WantedBy=timers.target
```

Enable it with:

```bash
systemctl enable --now ebs-netwatch-publish.timer
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

- The implementation stays intentionally small.
- The dashboard and logs stay on the local machine unless you publish the raw logs and static site.
- `config.json` stays ignored by Git unless you choose to track it explicitly.
