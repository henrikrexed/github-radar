# Changelog

All notable changes to GitHub Radar are documented here. Format roughly
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Unreleased

### Changed

- **Scanner schema (`scanner.db`): dropped empty `description` and `topics`
  columns** ([ISI-744](./), follow-up to [ISI-743](./)). A live audit of the
  production DB found both columns were empty strings for 100% of 559 active
  repos — the scanner fetched those values at scan time but never persisted
  them in readable form. The classifier now live-fetches description and
  topics from the GitHub API at classification time (`GET /repos/{owner}/{repo}`)
  alongside the README, so the GitHub API remains the single source of truth.
  - A forward-only SQLite migration runs automatically on `database.Open`:
    it issues `ALTER TABLE repos DROP COLUMN description` and
    `ALTER TABLE repos DROP COLUMN topics` on legacy DBs, then bumps
    `metadata.schema_version` from `1` to `2`.
  - `database.RepoRecord.Description` and `database.RepoRecord.Topics`, and the
    `TopicsSlice()` / `SetTopicsFromSlice()` helpers, have been removed. In-repo
    consumers already treated those fields as unreliable.
  - Gold-set builders ([ISI-739](./)) and T2 evaluation samples ([ISI-713](./))
    were already live-fetching, so they are unaffected.
