# gharchive Fallback — GCP Operations Runbook

> **Scope:** day-to-day operations of the BigQuery side of the github-radar
> [gharchive fallback collector](../architecture.md). The Go-side implementation lives
> in `internal/metrics/gharchive.go` (see [ISI-815](https://github.com/henrikrexed/github-radar/issues)).
> This page covers provisioning, key rotation, revocation, and the
> budget-breach playbook.

## Architecture summary

When the GitHub PAT budget headroom drops below 25% of target (or the
collector hits HTTP 403/429 mid-cycle), the router flips to a BigQuery
backend that queries the public dataset
`bigquery-public-data:githubarchive.day.YYYYMMDD`. The router re-evaluates
every cycle — there is no sticky degraded mode.

The collector authenticates with a single service account JSON key. The
account holds **only** `roles/bigquery.jobUser` on the dedicated project;
read access to the public dataset is implicit. There are no write roles
anywhere.

## GCP layout

| Resource                    | Value                                                         |
|-----------------------------|---------------------------------------------------------------|
| GCP project                 | `isitobservable-radar-gharchive`                              |
| Service account             | `radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com` |
| Project IAM                 | `roles/bigquery.jobUser`                                      |
| Per-user/day query quota    | 100 GB scanned                                                |
| Monthly budget alert        | $50 USD at 50% / 90% / 100% → henrik@perfbytes.com            |
| GitHub Actions secret       | `GHARCHIVE_GCP_SA_JSON`                                       |
| Local daemon key path       | `~/.config/github-radar/gcp-key.json`                         |

## One-time provisioning

Run from a workstation with `gcloud >= 470` and the
`alpha` component installed. The operator must hold
`roles/resourcemanager.projectCreator` and `roles/billing.user` on the
target billing account.

```bash
export BILLING_ACCOUNT_ID="0X0X0X-0X0X0X-0X0X0X"   # gcloud billing accounts list
export BUDGET_NOTIFY_EMAIL="henrik@perfbytes.com"
./scripts/provision-gharchive-gcp.sh
```

The script is idempotent — re-running reuses an existing project, SA, and
role binding. It refuses to overwrite an existing key file. See the
header of [`scripts/provision-gharchive-gcp.sh`](https://github.com/henrikrexed/github-radar/blob/main/scripts/provision-gharchive-gcp.sh)
for the full input contract.

After it finishes:

1. Verify the key works:
   ```bash
   gcloud auth activate-service-account --key-file=./gcp-key.json
   bq --project_id=isitobservable-radar-gharchive query --use_legacy_sql=false \
     'SELECT COUNT(*) FROM `bigquery-public-data.githubarchive.day.20260420`'
   ```
2. Wire it into CI:
   ```bash
   gh secret set GHARCHIVE_GCP_SA_JSON \
     --repo=henrikrexed/github-radar < ./gcp-key.json
   ```
3. For the long-running daemon, drop the same key at
   `~/.config/github-radar/gcp-key.json` (matches the
   `collector.gharchive.credentials_file` config key).
4. **Securely delete the local copy** of the key:
   ```bash
   shred -u ./gcp-key.json
   ```

## Key rotation

Default cadence: **every 90 days.** Earlier rotation is required after any
suspected exposure (laptop loss, accidental commit, contractor offboard).

```bash
# 1. Mint the new key.
gcloud iam service-accounts keys create ./gcp-key.json \
  --iam-account=radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com

# 2. Roll the GitHub Actions secret.
gh secret set GHARCHIVE_GCP_SA_JSON \
  --repo=henrikrexed/github-radar < ./gcp-key.json

# 3. Roll the daemon-side copy (replace on each host that runs the daemon).
scp ./gcp-key.json daemon-host:~/.config/github-radar/gcp-key.json
ssh daemon-host 'systemctl --user restart github-radar'

# 4. Wait one full collector cycle, confirm `collector_active{backend="gharchive"}`
#    metric does not error, then revoke the old key.
gcloud iam service-accounts keys list \
  --iam-account=radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com
gcloud iam service-accounts keys delete <OLD_KEY_ID> \
  --iam-account=radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com

# 5. Securely delete the local copy.
shred -u ./gcp-key.json
```

Update the rotation date in MemPalace under the agent diary entry for
DevOps Engineer (`SESSION:YYYY-MM-DD|gharchive.key.rotated|key_id=...`).

## Revocation (incident response)

Trigger conditions:
- Key found in a public repo / Slack message / email
- Spike in `gharchive_query_bytes_scanned_total` that does not match
  collector traffic
- Budget alert at 90% in the first 14 days of the month

Steps (do these in order, fast):

1. **Disable the SA immediately** — this stops all queries on the spot:
   ```bash
   gcloud iam service-accounts disable \
     radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com
   ```
2. **Confirm with audit logs** what the key was used for in the last 24h:
   ```bash
   gcloud logging read \
     'protoPayload.authenticationInfo.principalEmail="radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com"' \
     --project=isitobservable-radar-gharchive --limit=200 --format=json
   ```
3. **List + delete every existing key**:
   ```bash
   gcloud iam service-accounts keys list \
     --iam-account=radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com
   gcloud iam service-accounts keys delete <KEY_ID> \
     --iam-account=radar-gharchive-reader@isitobservable-radar-gharchive.iam.gserviceaccount.com
   ```
4. **File an incident issue** referencing this runbook and the audit-log
   findings.
5. **Re-provision** by re-running [`scripts/provision-gharchive-gcp.sh`](https://github.com/henrikrexed/github-radar/blob/main/scripts/provision-gharchive-gcp.sh)
   to mint a new key, then re-enable the SA.
6. **Roll the github-radar config** to ensure
   `collector.gharchive.enabled: false` until the new key is wired and
   verified end-to-end.

## Budget breach playbook

Trigger: budget alert email at 50% / 90% / 100%.

| Stage | First action                                                                                       |
|-------|----------------------------------------------------------------------------------------------------|
| 50%   | Note in #ops. No action unless trending past 90% by mid-month.                                     |
| 90%   | Inspect query cost trend: `bq ls -j --max_results=50 isitobservable-radar-gharchive`. Identify whether spend is from collector cycles or one-off ad-hoc queries. If ad-hoc, drop the per-user quota in console (IAM & Admin → Quotas) to throttle. |
| 100%  | **Disable the collector**: in `config.yaml`, set `collector.gharchive.enabled: false` and restart the daemon. The router falls back to LiveAPI-only with no data loss. Then run the 90% diagnostics. |

The collector design assumes a worst-case Path-A burn ceiling of ~$37/mo
(8.4 TB scanned). A breach above that ceiling is a strong signal of
either (a) misconfigured query templates scanning too many partitions,
or (b) a compromised key. Treat any breach above $40/mo as a pre-revocation
condition until verified otherwise.

## Verification cheat-sheet

End-to-end smoke test after any provisioning or rotation:

```bash
gcloud auth activate-service-account --key-file=~/.config/github-radar/gcp-key.json
bq --project_id=isitobservable-radar-gharchive query --use_legacy_sql=false --dry_run \
  'SELECT repo.name, COUNT(*) AS events
   FROM `bigquery-public-data.githubarchive.day.20260420`
   WHERE repo.name = "kubernetes/kubernetes"
   GROUP BY repo.name'
```

`--dry_run` reports bytes that would be scanned without spending quota.
A passing dry-run plus the live `SELECT COUNT(*)` query in the
[Verify](#one-time-provisioning) step is sufficient to ship a key change.

## Related work

- [ISI-815](https://github.com/henrikrexed/github-radar/issues) — Go implementation of the fallback collector
- [ISI-813](https://github.com/henrikrexed/github-radar/issues) — design memo (Path A vs Path B trade-off)
- [`internal/github/ratelimit.go`](https://github.com/henrikrexed/github-radar/blob/main/internal/github/ratelimit.go) — budget telemetry that drives the router circuit breaker
