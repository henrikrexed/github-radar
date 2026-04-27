#!/usr/bin/env bash
# Provision the GCP side of the github-radar gharchive fallback (ISI-816).
#
# Idempotent where the underlying gcloud command supports it. Re-running is
# safe: existing project / SA / role bindings are reused.
#
# Required inputs (env vars):
#   BILLING_ACCOUNT_ID     e.g. "0X0X0X-0X0X0X-0X0X0X" (gcloud billing accounts list)
#   PROJECT_ID             default: isitobservable-radar-gharchive
#   PROJECT_NAME           default: "IsItObservable Radar — gharchive"
#   BUDGET_NOTIFY_EMAIL    e.g. henrik@perfbytes.com
#   ORG_ID                 optional, attach project under an org/folder
#
# Outputs:
#   gcp-key.json in CWD — single JSON key for radar-gharchive-reader SA
#
# Prereqs on the operator's machine:
#   gcloud >= 470 with components: alpha (for quota override), billing
#   The operator must hold roles/resourcemanager.projectCreator + roles/billing.user
#   on the billing account.

set -euo pipefail

PROJECT_ID="${PROJECT_ID:-isitobservable-radar-gharchive}"
PROJECT_NAME="${PROJECT_NAME:-IsItObservable Radar — gharchive}"
SA_NAME="radar-gharchive-reader"
SA_EMAIL="${SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
KEY_FILE="${KEY_FILE:-./gcp-key.json}"
BUDGET_USD="${BUDGET_USD:-50}"
DAILY_QUERY_QUOTA_GB="${DAILY_QUERY_QUOTA_GB:-100}"

require() {
  local var="$1"
  if [[ -z "${!var:-}" ]]; then
    echo "ERROR: required env var $var is not set." >&2
    exit 64
  fi
}

require BILLING_ACCOUNT_ID
require BUDGET_NOTIFY_EMAIL

echo "==> Provisioning project ${PROJECT_ID}"
if ! gcloud projects describe "${PROJECT_ID}" >/dev/null 2>&1; then
  if [[ -n "${ORG_ID:-}" ]]; then
    gcloud projects create "${PROJECT_ID}" \
      --name="${PROJECT_NAME}" \
      --organization="${ORG_ID}"
  else
    gcloud projects create "${PROJECT_ID}" --name="${PROJECT_NAME}"
  fi
else
  echo "    project already exists, reusing."
fi

echo "==> Linking billing account ${BILLING_ACCOUNT_ID}"
gcloud billing projects link "${PROJECT_ID}" \
  --billing-account="${BILLING_ACCOUNT_ID}"

echo "==> Enabling APIs"
gcloud services enable \
  bigquery.googleapis.com \
  bigquerystorage.googleapis.com \
  iam.googleapis.com \
  serviceusage.googleapis.com \
  billingbudgets.googleapis.com \
  --project="${PROJECT_ID}"

echo "==> Creating service account ${SA_EMAIL}"
if ! gcloud iam service-accounts describe "${SA_EMAIL}" \
       --project="${PROJECT_ID}" >/dev/null 2>&1; then
  gcloud iam service-accounts create "${SA_NAME}" \
    --project="${PROJECT_ID}" \
    --display-name="github-radar gharchive reader" \
    --description="Read-only BigQuery access to bigquery-public-data:githubarchive for github-radar fallback collector (ISI-815)."
else
  echo "    SA already exists, reusing."
fi

echo "==> Granting least-privilege roles"
# roles/bigquery.jobUser is required to RUN queries (job-level permission).
# roles/bigquery.dataViewer on the public githubarchive dataset is implicit
# (public datasets grant allUsers / allAuthenticatedUsers viewer access),
# so we do NOT add a project-wide dataViewer. Document explicitly here:
#   project bigquery.jobUser  -> can submit query jobs in this project
#   public dataset access     -> implicit, no IAM grant needed
gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${SA_EMAIL}" \
  --role="roles/bigquery.jobUser" \
  --condition=None

echo "==> Setting BigQuery custom quota: ${DAILY_QUERY_QUOTA_GB} GB scanned per user per day"
# QuotaOverride API — caps the SA at DAILY_QUERY_QUOTA_GB GB scanned per day.
# Ref: https://cloud.google.com/service-usage/docs/manage-quota#consumer_quota_override
QUOTA_BYTES=$(( DAILY_QUERY_QUOTA_GB * 1024 * 1024 * 1024 ))
gcloud alpha services quota update \
  --service=bigquery.googleapis.com \
  --consumer="projects/${PROJECT_ID}" \
  --metric=bigquery.googleapis.com/quota/query/usage \
  --unit="1/d/{project}/{user}" \
  --value="${QUOTA_BYTES}" \
  --force \
  --quiet || {
    echo "    (alpha quota override failed — set manually in console:"
    echo "     IAM & Admin → Quotas → 'Query usage per user per day' →"
    echo "     filter by ${PROJECT_ID} → set ${DAILY_QUERY_QUOTA_GB} GiB)"
  }

echo "==> Generating JSON key for ${SA_EMAIL} -> ${KEY_FILE}"
if [[ -f "${KEY_FILE}" ]]; then
  echo "    key file already exists at ${KEY_FILE} — refusing to overwrite."
  echo "    (delete or move it before re-running, or set KEY_FILE=...)"
  exit 65
fi
gcloud iam service-accounts keys create "${KEY_FILE}" \
  --iam-account="${SA_EMAIL}" \
  --project="${PROJECT_ID}"
chmod 600 "${KEY_FILE}"

echo "==> Creating budget alert (\$${BUDGET_USD}/mo, 50/90/100%)"
BUDGET_DISPLAY="github-radar gharchive (${PROJECT_ID})"
# Resolve billing account name for budgets API.
BUDGETS_PARENT="billingAccounts/${BILLING_ACCOUNT_ID}"
gcloud billing budgets create \
  --billing-account="${BILLING_ACCOUNT_ID}" \
  --display-name="${BUDGET_DISPLAY}" \
  --budget-amount="${BUDGET_USD}USD" \
  --threshold-rule=percent=0.5 \
  --threshold-rule=percent=0.9 \
  --threshold-rule=percent=1.0 \
  --filter-projects="projects/${PROJECT_ID}" \
  --notifications-rule-monitoring-notification-channels="" 2>&1 || {
    echo "    (budget create failed — may already exist, or notification"
    echo "     channels need to be set up first via:"
    echo "       gcloud beta monitoring channels create --type=email \\"
    echo "         --display-name='Henrik gharchive budget' \\"
    echo "         --channel-labels=email_address=${BUDGET_NOTIFY_EMAIL})"
  }

cat <<EOF

==> Provisioning complete.

Project ID:           ${PROJECT_ID}
Service account:      ${SA_EMAIL}
JSON key:             ${KEY_FILE}
Daily query cap:      ${DAILY_QUERY_QUOTA_GB} GB scanned / user / day
Monthly budget alert: \$${BUDGET_USD} (50/90/100%)

Next steps:
  1. Verify with:
       gcloud auth activate-service-account --key-file=${KEY_FILE}
       bq --project_id=${PROJECT_ID} query --use_legacy_sql=false \\
         'SELECT COUNT(*) FROM \`bigquery-public-data.githubarchive.day.20260420\`'
  2. Add the key to the github-radar repo as a GitHub Actions secret:
       gh secret set GHARCHIVE_GCP_SA_JSON \\
         --repo=henrikrexed/github-radar < ${KEY_FILE}
  3. Drop the same key at ~/.config/github-radar/gcp-key.json on hosts that
     run the daemon directly (path comes from collector.gharchive.credentials_file).
  4. Securely delete the local key copy:
       shred -u ${KEY_FILE}
EOF
