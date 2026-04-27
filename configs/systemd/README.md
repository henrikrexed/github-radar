# systemd user units for github-radar

The `github-radar-other-audit.{service,timer}` pair runs the monthly
`<cat>/other` drift audit (T9 / [ISI-720](/ISI/issues/ISI-720),
implementation [ISI-751](/ISI/issues/ISI-751)) on the 1st of every
month at 03:00 local time.

## Install

```sh
mkdir -p ~/.config/systemd/user
install -m 0644 github-radar-other-audit.service ~/.config/systemd/user/
install -m 0644 github-radar-other-audit.timer   ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now github-radar-other-audit.timer
```

Confirm the next firing:

```sh
systemctl --user list-timers github-radar-other-audit.timer
```

## Configure environment

The `--file` mode auto-files graduation-proposal issues via the Paperclip
API. The service unit needs the following environment variables. Set them
via a drop-in:

```sh
systemctl --user edit github-radar-other-audit.service
```

```ini
[Service]
Environment=PAPERCLIP_API_URL=http://127.0.0.1:3100
Environment=PAPERCLIP_API_KEY=...
Environment=PAPERCLIP_COMPANY_ID=...
Environment=GITHUB_RADAR_PROJECT_ID=...
Environment=GITHUB_RADAR_AUDIT_ASSIGNEE_AGENT=...
Environment=GITHUB_RADAR_AUDIT_PARENT_ISSUE_ID=...
# Optional: override report directory (default ~/.local/share/github-radar/audits)
Environment=GITHUB_RADAR_AUDIT_DIR=%h/.local/share/github-radar/audits
```

The standard github-radar config (`~/.config/github-radar/config.yaml`)
is still required — `audit other-drift` reuses the GitHub-API token from
that file to live-fetch topics per [ISI-743](/ISI/issues/ISI-743).

## Verify

Run the audit on demand without filing anything:

```sh
github-radar audit other-drift --dry-run
```

The rendered report is printed to stdout. Persisted reports land in
`~/.local/share/github-radar/audits/YYYY-MM.md` (only in `--file` mode).

## Tail the journal after a run

```sh
journalctl --user -u github-radar-other-audit.service -n 200 --no-pager
```

Look for the structured `audit complete` line — it carries
`aggregate_share_pct`, `escalated`, `filed_count`, and `watch_count`. A
silent failure should be obvious: any non-zero `failed_count` counterpart
in the daemon's classification health metrics ([ISI-775](/ISI/issues/ISI-775))
will surface in OTel before this audit runs again.
