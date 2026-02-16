# Installation

## Download Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/henrikrexed/github-radar/releases).

=== "Linux (amd64)"

    ```bash
    curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-linux-amd64
    chmod +x github-radar
    sudo mv github-radar /usr/local/bin/
    ```

=== "Linux (arm64)"

    ```bash
    curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-linux-arm64
    chmod +x github-radar
    sudo mv github-radar /usr/local/bin/
    ```

=== "macOS (Apple Silicon)"

    ```bash
    curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-darwin-arm64
    chmod +x github-radar
    sudo mv github-radar /usr/local/bin/
    ```

=== "macOS (Intel)"

    ```bash
    curl -Lo github-radar https://github.com/henrikrexed/github-radar/releases/latest/download/github-radar-darwin-amd64
    chmod +x github-radar
    sudo mv github-radar /usr/local/bin/
    ```

=== "Windows"

    Download `github-radar-windows-amd64.exe` from the [releases page](https://github.com/henrikrexed/github-radar/releases) and add it to your `PATH`.

## Build from Source

Requires **Go 1.22+**.

```bash
git clone https://github.com/henrikrexed/github-radar.git
cd github-radar
make build
```

The binary is built to `./bin/github-radar`.

## Docker

```bash
# Pull from registry
docker pull ghcr.io/henrikrexed/github-radar:latest

# Or build locally
git clone https://github.com/henrikrexed/github-radar.git
cd github-radar
make docker
```

See the [Daemon Guide](daemon-guide.md) for Docker Compose deployment.

## Verify Installation

```bash
github-radar --help
```

## GitHub Token Setup

GitHub Radar requires a Personal Access Token for the GitHub API. Authenticated requests get 5,000 requests/hour (vs 60/hour without a token).

### Classic Token

1. Go to [github.com/settings/tokens](https://github.com/settings/tokens)
2. Click **"Generate new token"** → **"Generate new token (classic)"**
3. Set a descriptive name (e.g., "GitHub Radar")
4. Select scopes:
      - **`public_repo`** — Required: access public repository data
      - **`read:org`** — Optional: access organization repository data
5. Click **"Generate token"** and copy the token immediately

### Fine-Grained Token

1. Go to [github.com/settings/tokens?type=beta](https://github.com/settings/tokens?type=beta)
2. Click **"Generate new token"**
3. Set token name and expiration
4. Under **Repository access**, select **"Public Repositories (read-only)"**
5. No additional permissions are needed — "Public Repositories (read-only)" already grants read access to metadata, issues, and pull requests for all public repositories
6. Click **"Generate token"**

### Set the Token

```bash
# Set as environment variable
export GITHUB_TOKEN="ghp_your_token_here"

# Or add to your shell profile for persistence
echo 'export GITHUB_TOKEN="ghp_your_token_here"' >> ~/.zshrc
source ~/.zshrc
```

!!! warning "Security"
    Never commit tokens to source control. Always use environment variables or a secrets manager.
