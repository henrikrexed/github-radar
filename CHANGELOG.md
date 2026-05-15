# Changelog

All notable changes to github-radar are documented in this file. Release
binaries and container images are published via the GitHub
[Releases](https://github.com/henrikrexed/github-radar/releases) workflow;
this file captures user-visible behaviour changes between releases.

## Unreleased

### Removed

- **OTel: `category_legacy` attribute on `github.radar.*` metrics.** The
  pre-migration flat-slug attribute that ISI-786 emitted alongside the v3
  `(category, subcategory)` split is no longer written. Dashboards migrated
  to the split labels in ISI-718; the dependency gate that authorized this
  removal closed on 2026-05-11 with the v26 deploy report. Consumers that
  still need the flat slug can read `primary_category_legacy` from the
  scanner DB or the `repos_legacy_v1` view — the column itself is preserved
  as a snapshot. (ISI-989)
