# Deploy

GitHub Radar is shipped as a multi-arch container image at
`ghcr.io/henrikrexed/github-radar`. The runtime host pulls the image; it does
not build from source.

## Image tags

| Tag | When it moves | Use for |
| --- | --- | --- |
| `:main` | Every push to `main` (CI workflow) | Steady-state hosts that follow `main` |
| `:main-<sha7>` | Immutable, one per merge commit | Pinned/canary rollouts and rollback |
| `:latest` | On `v*` tag releases | Stable hosts that follow tagged releases |
| `:<version>` (e.g. `2.0.0`) | On a specific `v*` tag | Pinned production deploys |

`:main` is a floating tag — it advances as soon as a commit lands on `main` and
the CI `docker-main` job finishes. For any host where you need deterministic
rollback, pin to a `main-<sha7>` tag instead.

## Steady-state update (host running docker compose)

The runtime host is expected to have:

- a checkout of this repo (or just the `docker-compose.yml` and `configs/`),
- Docker + Compose v2,
- the required environment variables (`GITHUB_TOKEN`, `DT_OTLP_ENDPOINT`,
  `DT_API_TOKEN`).

To pick up a new build, run two commands:

```bash
docker compose pull
docker compose up -d
```

Because `docker-compose.yml` sets `pull_policy: always` and references the
`:main` floating tag, `docker compose pull` fetches whatever CI pushed last and
`up -d` recreates the `github-radar` container against the new digest while
leaving `otel-collector` untouched if its image is unchanged.

## Rollback

If `:main` is bad, roll back to a previous immutable tag.

1. Find the previous good `main-<sha7>` from the CI run history or the package
   page at
   <https://github.com/henrikrexed/github-radar/pkgs/container/github-radar>.
2. Override the image tag for that single host:

   ```bash
   GITHUB_RADAR_IMAGE=ghcr.io/henrikrexed/github-radar:main-<sha7> \
   docker compose up -d
   ```

   …or edit `docker-compose.yml` on that host to pin the `image:` line, then
   `docker compose up -d`.
3. Open a fix PR (or revert) on `main` so CI republishes a healthy `:main` and
   the host can be returned to the floating tag.

> **Why prefer the immutable tag for rollback?** `:main` will move forward
> again as soon as the next commit lands. Pinning to `main-<sha7>` gives you a
> stable target until the fix is merged.

## Verifying a deploy

After `docker compose up -d`, confirm the container picked up the new image:

```bash
docker compose ps
docker inspect github-radar --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}'
```

The `revision` label should match the commit you expect on `main`. The CI
workflow sets it via `docker/build-push-action` labels.

For end-to-end signal verification, the daemon's `--interval` (default `24h`)
means the first scan after restart can lag. Trigger an immediate scan or use
the `/health` endpoint to confirm the process is up:

```bash
curl -fsS http://localhost:8080/health
```

## Related

- CI workflow: [`ci.yml`](https://github.com/henrikrexed/github-radar/blob/main/.github/workflows/ci.yml)
  (`docker-main` job pushes `:main` on every merge to `main`).
- Release workflow: [`release.yml`](https://github.com/henrikrexed/github-radar/blob/main/.github/workflows/release.yml)
  (`v*` tags publish `:latest` and `:<version>`).
- Daemon configuration: see [Daemon Guide](../daemon-guide.md).
