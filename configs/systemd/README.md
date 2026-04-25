# systemd units

This directory ships the systemd user units for the monthly `<cat>/other`
drift audit job (T9, ISI-720).

## Install

```bash
mkdir -p ~/.config/systemd/user
cp configs/systemd/github-radar-other-audit.service ~/.config/systemd/user/
cp configs/systemd/github-radar-other-audit.timer   ~/.config/systemd/user/

# Set environment for the unit (see comments inside the .service file).
systemctl --user set-environment PAPERCLIP_API_KEY=<token>
systemctl --user set-environment PAPERCLIP_API_URL=http://127.0.0.1:3100
systemctl --user set-environment PAPERCLIP_COMPANY_ID=<uuid>
systemctl --user set-environment GITHUB_RADAR_PAPERCLIP_PROJECT_ID=<uuid>
systemctl --user set-environment GITHUB_RADAR_PAPERCLIP_AUDIT_PARENT_ID=<issue-id>
systemctl --user set-environment GITHUB_RADAR_PAPERCLIP_AUDIT_ASSIGNEE_ID=<agent-id>

systemctl --user daemon-reload
systemctl --user enable --now github-radar-other-audit.timer
```

## Verify

```bash
systemctl --user list-timers github-radar-other-audit.timer
journalctl --user -u github-radar-other-audit.service -n 100
```

## Manual run (for first-run validation per T9 plan §8)

```bash
github-radar audit other-drift --file
# or render to stdout without filing:
github-radar audit other-drift --dry-run
```

The persistent report lands at
`$XDG_DATA_HOME/github-radar/audits/<YYYY-MM>.md` (defaults to
`~/.local/share/github-radar/audits/<YYYY-MM>.md`).
