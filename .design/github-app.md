# GitHub App Integration for Scion Agents

**Created:** 2026-03-18
**Status:** Draft / Proposal (Rev 3)
**Related:** `hosted/git-groves.md`, `hosted/secrets-gather.md`, `agent-credentials.md`, `hosted/auth/oauth-setup.md`

---

## 1. Overview

Today, Scion agents authenticate to GitHub using **Personal Access Tokens (PATs)** stored as secrets (`GITHUB_TOKEN`). This works but has significant limitations:

- **PATs are user-scoped**: Tied to a single person's identity. If that person leaves or rotates credentials, all groves using their token break.
- **No automatic rotation**: PATs have fixed expiration. When they expire, agents fail until someone manually updates the secret.
- **Coarse permission model**: Fine-grained PATs can be scoped to repos, but the permissions are static — there's no way to issue narrower tokens per-agent or per-operation.
- **Attribution**: All commits and API calls appear as the PAT owner, not as the agent or the system.
- **Organization governance**: Org admins have limited visibility into which PATs access their repos and no central revocation mechanism.

**GitHub Apps** address all of these issues. This document proposes a design for integrating GitHub App authentication into Scion as a first-class alternative to PATs.

### Goals

1. Support GitHub App installation tokens as a credential source for agent git operations (clone, push) and GitHub API access (PRs, issues).
2. Automatic short-lived token generation — no manual rotation required.
3. Clear ownership model: one GitHub App per Hub, grove owners install it for their repos.
4. Coexist with the existing PAT flow — GitHub App is an alternative, not a replacement.

### Non-Goals

- Webhook-driven agent creation (GitHub App receiving events to trigger agents). Deferred to a future design.
- GitHub App as a Scion Hub user authentication provider (the existing GitHub OAuth flow handles Hub login separately).
- Multi-provider abstraction (GitLab, Bitbucket app equivalents). This design targets GitHub only.
- GitHub App Manifest flow for automated app creation.
- Solo/local mode support. GitHub App is Hub-only; solo mode continues to use PATs.
- User-Brought Apps (BYOA). Each user registering their own GitHub App adds complexity with minimal benefit. May be revisited as a future escape hatch.

---

## 2. GitHub App Primer

### 2.1 What Is a GitHub App?

A GitHub App is a first-class integration registered on GitHub. Unlike OAuth Apps or PATs, a GitHub App:

- Has its **own identity** separate from any user.
- Is **installed** on organizations or user accounts, granting it access to specific repositories.
- Authenticates using a **private key** (RSA) to generate short-lived JWTs, which are exchanged for **installation access tokens**.
- Has **fine-grained permissions** declared at registration time (e.g., Contents: read/write, Pull Requests: read/write, Issues: read/write).
- Can further **restrict tokens to specific repositories** at token creation time.

### 2.2 Authentication Flow

```
                GitHub App (registered)
                     |
                     | Private Key (PEM)
                     v
            ┌─────────────────┐
            │  Generate JWT   │  (signed with private key, 10-min expiry)
            │  (app identity) │
            └────────┬────────┘
                     |
                     v
            ┌─────────────────┐
            │  POST /app/     │  (JWT as Bearer token)
            │  installations/ │
            │  {id}/access_   │
            │  tokens         │
            └────────┬────────┘
                     |
                     v
            ┌─────────────────┐
            │ Installation    │  (scoped to repos, 1-hour expiry)
            │ Access Token    │
            └─────────────────┘
```

1. **JWT Generation**: The app signs a JWT using its private key. The JWT identifies the app (by App ID) and expires in 10 minutes.
2. **Token Request**: The JWT is used to call `POST /app/installations/{installation_id}/access_tokens`, optionally scoping to specific repositories and permissions.
3. **Installation Token**: GitHub returns a token (format `ghs_xxx`) valid for 1 hour. This token is used for git operations and API calls.

### 2.3 Installation Model

A GitHub App can be installed on:

- **An organization account**: Grants access to repos owned by that org. An org admin approves the installation.
- **A user account**: Grants access to repos owned by that user.

Each installation has a unique `installation_id`. A single GitHub App can have many installations across different orgs and users.

The installer chooses which repositories the app can access:
- **All repositories** in the org/account.
- **Selected repositories** — a specific subset.

### 2.4 Key Properties for Scion

| Property | PAT | GitHub App |
|----------|-----|------------|
| **Identity** | Personal user | App (machine identity) |
| **Token lifetime** | User-configured (max 1 year) | 1 hour (auto-generated) |
| **Rotation** | Manual | Automatic |
| **Repo scoping** | At PAT creation time (static) | Per-token request (dynamic) |
| **Permission scoping** | At PAT creation time (static) | Per-token request (dynamic, up to app max) |
| **Org visibility** | Limited (admin audit log) | Full (installed apps page, permissions visible) |
| **Rate limits** | User-level (5000/hr shared) | App-level (5000/hr per installation, separate from user) |
| **Revocation** | Per-token | Per-installation or per-app |
| **Commit attribution** | PAT owner | App identity (configurable) |

---

## 3. Ownership Model

**One GitHub App per Scion Hub deployment. The Hub admin creates it. Grove owners install it.**

```
Scion Hub (1:1 with GitHub App)
  └── GitHub App (registered by Hub admin)
        │
        │  Hub stores: App ID, Private Key, Webhook Secret
        │
        ├── Installation: org-acme (installation_id: 12345)
        │     ├── Grove: acme-widgets → repo: acme/widgets
        │     └── Grove: acme-api → repo: acme/api
        │
        ├── Installation: org-beta (installation_id: 67890)
        │     └── Grove: beta-platform → repo: beta/platform
        │
        └── Installation: user-alice (installation_id: 11111)
              └── Grove: alice-dotfiles → repo: alice/dotfiles
```

### 3.1 Roles and Responsibilities

| Actor | Action |
|-------|--------|
| **Hub Admin** | Registers the GitHub App on GitHub. Configures the Hub with App ID, private key, and setup URL. This is a one-time operation per Hub deployment. |
| **Grove Owner** | Installs the GitHub App on their GitHub org or user account for the grove's repo. GitHub's post-installation callback notifies the Hub, which auto-associates the installation with the matching grove. |

### 3.2 Installation Flow

```
Grove Owner                GitHub.com                   Scion Hub
    |                          |                            |
    |-- "Install App" ------->|                            |
    |   (from grove settings  |                            |
    |    or Hub admin page)   |                            |
    |                          |                            |
    |   GitHub shows app      |                            |
    |   install page:         |                            |
    |   - select org/user     |                            |
    |   - select repo(s)      |                            |
    |                          |                            |
    |-- Approve install ----->|                            |
    |                          |                            |
    |                          |-- POST webhook:            |
    |                          |   installation.created --->|
    |                          |                            |-- Record installation
    |                          |                            |-- Match to grove(s)
    |                          |                            |   by repo URL
    |                          |                            |-- Update grove settings
    |                          |                            |
    |                          |-- Redirect to setup URL -->|
    |                          |   ?installation_id=12345   |
    |                          |                            |
    |<-- Hub shows confirmation page -----------------------|
    |   "App installed for org 'acme'.                     |
    |    Grove 'acme-widgets' now uses GitHub App auth."   |
```

The **setup URL** is configured when registering the GitHub App on GitHub:
```
https://{hub_external_url}/github-app/setup
```

GitHub appends `installation_id` and `setup_action` query parameters. The Hub uses this to:
1. Look up the installation via the GitHub API (repos, permissions).
2. Match the installation's repos against existing groves.
3. Auto-associate matching groves with the installation.
4. Redirect the user to a confirmation page.

The **webhook** (`installation.created`) also fires, providing a server-to-server confirmation. Both mechanisms (setup URL redirect + webhook) are handled idempotently — either one alone is sufficient, both together provide redundancy.

### 3.3 Credential Resolution

When an agent starts, the Hub resolves credentials in this order:

```
1. Grove-scoped GITHUB_TOKEN secret (explicit PAT override)
2. GitHub App installation token (if grove has an associated installation)
3. User-scoped GITHUB_TOKEN secret (user's PAT)
4. Hub-level GITHUB_TOKEN secret (shared PAT, if any)
```

If a grove has both a `GITHUB_TOKEN` secret and an associated installation, the explicit secret wins. This allows per-grove PAT override (e.g., for permissions the app doesn't have).

---

## 4. Data Model

### 4.1 GitHub App Configuration (Hub-Level)

The Hub server gains a new configuration section for the GitHub App:

```yaml
# Hub server config (e.g., hub.yaml or server flags)
github_app:
  app_id: 123456
  private_key_path: /etc/scion/github-app-key.pem
  # OR inline:
  # private_key: |
  #   -----BEGIN RSA PRIVATE KEY-----
  #   ...
  webhook_secret: "whsec_..."     # For validating incoming webhooks
  api_base_url: https://api.github.com  # default; override for GHES
```

In Go:

```go
type GitHubAppConfig struct {
    AppID          int64  `json:"app_id" yaml:"app_id" koanf:"app_id"`
    PrivateKeyPath string `json:"private_key_path,omitempty" yaml:"private_key_path,omitempty" koanf:"private_key_path"`
    PrivateKey     string `json:"private_key,omitempty" yaml:"private_key,omitempty" koanf:"private_key"`
    WebhookSecret  string `json:"webhook_secret,omitempty" yaml:"webhook_secret,omitempty" koanf:"webhook_secret"`
    APIBaseURL     string `json:"api_base_url,omitempty" yaml:"api_base_url,omitempty" koanf:"api_base_url"`
}
```

**Settings Schema Note:** All fields must be tracked in the Hub settings schema for validation and UI rendering. The `api_base_url` field enables GitHub Enterprise Server support.

### 4.2 Installation Registration

Each GitHub App installation is registered as a Hub resource. Installations are created automatically when the grove owner installs the app (via webhook or setup URL callback):

```go
type GitHubInstallation struct {
    InstallationID int64     `json:"installation_id"`
    AccountLogin   string    `json:"account_login"`   // GitHub org or user login
    AccountType    string    `json:"account_type"`     // "Organization" or "User"
    AppID          int64     `json:"app_id"`           // Always matches Hub's app
    Repositories   []string  `json:"repositories"`     // Repos granted access to
    Status         string    `json:"status"`           // "active", "suspended", "deleted"
    CreatedAt      time.Time `json:"created_at"`
}
```

### 4.3 Grove-to-Installation Mapping

A grove references a GitHub App installation for its credential source:

```go
// Existing Grove model, extended:
type Grove struct {
    // ... existing fields ...

    // GitHubInstallationID links this grove to a GitHub App installation.
    // When set, agents use installation tokens instead of PATs.
    // Set automatically by the setup URL callback or webhook handler.
    GitHubInstallationID *int64 `json:"github_installation_id,omitempty"`

    // GitHubPermissions specifies the permissions to request when minting
    // installation tokens for this grove. If nil, the default set is used.
    GitHubPermissions *GitHubTokenPermissions `json:"github_permissions,omitempty"`
}

type GitHubTokenPermissions struct {
    Contents     string `json:"contents,omitempty"`      // "read" or "write"
    PullRequests string `json:"pull_requests,omitempty"` // "read" or "write"
    Issues       string `json:"issues,omitempty"`        // "read" or "write"
    Metadata     string `json:"metadata,omitempty"`      // "read"
    Checks       string `json:"checks,omitempty"`        // "read" or "write"
    Actions      string `json:"actions,omitempty"`        // "read"
}
```

Since groves are 1:1 with a repository, the installation token is always scoped to exactly one repo. The Hub automatically restricts the token to the grove's target repository regardless of whether the installation grants broader access.

---

## 5. Token Lifecycle

### 5.1 Token Minting

The Hub is the sole authority for minting installation tokens. The private key never leaves the Hub.

```
Agent Start                   Hub                          GitHub API
    |                          |                              |
    |-- CreateAgent ---------->|                              |
    |                          |-- Resolve grove              |
    |                          |   (has installation_id?)     |
    |                          |                              |
    |                          |-- Generate JWT (app key) --->|
    |                          |                              |
    |                          |-- POST /installations/       |
    |                          |   {id}/access_tokens ------->|
    |                          |   { repositories: [repo],    |
    |                          |     permissions: (from grove |
    |                          |       settings or defaults)  |
    |                          |   }                          |
    |                          |                              |
    |                          |<-- token: ghs_xxx (1hr) -----|
    |                          |                              |
    |<-- GITHUB_TOKEN=ghs_xxx-|                              |
    |    (in resolved env)     |                              |
```

The minted token is injected as `GITHUB_TOKEN` in the agent's environment — **the agent doesn't know or care whether the token came from a PAT or a GitHub App**. This is key: the credential source is transparent to the agent and harness.

### 5.2 Token Refresh — Blended Approach

Installation tokens expire after 1 hour. Agents that run longer than 1 hour need token refresh. The design uses a **blended approach** that combines a credential helper (for git) with a background refresh loop (for `gh` CLI and other API consumers).

#### Component 1: Credential Helper (Git Operations)

The `sciontool` credential helper intercepts git credential requests and returns fresh tokens on demand:

```bash
# Git credential helper (configured during clone):
git config credential.helper '!sciontool credential-helper'

# sciontool credential-helper:
#   1. Check cached token age
#   2. If fresh (< 50 min): return cached token
#   3. If stale: call Hub refresh endpoint, cache new token, return
```

This provides the most native git integration — git operations transparently receive fresh tokens without any polling or background processes.

#### Component 2: Background Refresh Loop (API/CLI Operations)

`sciontool` runs a background goroutine that proactively refreshes the token before expiry, ensuring the on-disk token file stays current for non-git consumers like the `gh` CLI:

```
sciontool init
  └── tokenRefreshLoop():
        every 50 minutes:
          1. POST to Hub: /api/v1/agents/{id}/refresh-token
          2. Hub mints new installation token
          3. Hub returns token
          4. sciontool updates:
             - writes to /tmp/.github-token (for running processes to read)
             - updates git credential helper cache
```

The `gh` CLI is wrapped by a lightweight script that reads the current token from the token file before delegating to the real `gh` binary, ensuring it always uses a fresh token.

#### Why Both?

| Consumer | Mechanism | Rationale |
|----------|-----------|-----------|
| `git clone/push` | Credential helper | Native git integration; lazy refresh only when needed |
| `gh` CLI | Background loop + wrapper | `gh` reads token at invocation; wrapper reads fresh file |
| Custom scripts | Background loop | Any process reading the token file gets a fresh value |

### 5.3 Environment Variables

The following environment variables control GitHub App token behavior inside the agent container:

| Variable | Purpose |
|----------|---------|
| `GITHUB_TOKEN` | Initial token (set at agent start) |
| `SCION_GITHUB_APP_ENABLED` | `true` when credential source is GitHub App (enables refresh) |
| `SCION_GITHUB_TOKEN_EXPIRY` | ISO 8601 timestamp of initial token expiry |
| `SCION_GITHUB_TOKEN_PATH` | Path to refreshable token file (`/tmp/.github-token`) |

---

## 6. Installation Lifecycle

### 6.1 App Installation (Primary Flow)

The primary way groves get associated with the GitHub App is through the **installation callback flow** described in §3.2. The grove owner installs the app from a link in the Hub UI (grove settings or Hub admin page), GitHub handles the authorization UI, and the Hub auto-associates the installation with matching groves.

### 6.2 Manual Association (Fallback)

If the callback flow doesn't match correctly (e.g., grove was created after installation), a manual association is available:

```bash
scion hub grove set acme-widgets --github-installation 12345
```

The Hub validates that the installation exists and includes the grove's target repo.

### 6.3 Auto-Discovery (Fallback)

When a grove is created from a GitHub URL and the Hub has a GitHub App configured, the Hub can discover matching installations:

```
1. Hub generates JWT (app identity)
2. Hub calls GET /app/installations (lists all installations)
3. For each installation, calls GET /installation/repositories
4. Finds installation(s) that include the grove's target repo
5. If exactly one match: auto-associate
6. If no match: grove uses PAT, grove settings show "Install GitHub App" link
```

This auto-discovery runs during `scion hub grove create`.

### 6.4 Webhooks

GitHub sends webhooks for installation lifecycle events. The Hub's webhook endpoint handles them idempotently:

| Event | Hub Action |
|-------|------------|
| `installation.created` | Record installation, match to groves by repo |
| `installation.deleted` | Mark installation as `deleted`, notify affected groves |
| `installation.suspend` | Mark as `suspended`, affected groves fall back to PAT |
| `installation.unsuspend` | Mark as `active`, groves resume using app tokens |
| `installation_repositories.added` | Update installation's repo list, check for new grove matches |
| `installation_repositories.removed` | Update repo list, disassociate affected groves |

**Public-Facing Requirement:** Webhooks require the Hub to be publicly reachable. The Hub config includes a flag:

```yaml
github_app:
  webhooks_enabled: true  # admin asserts Hub is publicly reachable
```

When `webhooks_enabled` is false, the Hub falls back to auto-discovery and manual association. A validation step during setup can optionally verify reachability by registering a test webhook and checking for the ping event.

**Revocation Handling:** When an installation is revoked:
1. The Hub marks the installation as `deleted` (via webhook or 403 during token minting).
2. Running agents with valid tokens continue until their token expires (up to 1 hour).
3. Token refresh attempts fail; `sciontool` logs: "GitHub App installation revoked for org 'acme'."
4. Affected groves fall back to PAT if one is configured, or surface an error status.
5. The Hub notifies the grove owner.

---

## 7. Hub API Changes

### 7.1 New Endpoints

```
# GitHub App configuration (admin only)
GET    /api/v1/github-app                          → App config (app ID, status, not the key)
PUT    /api/v1/github-app                          → Update app config

# Installations (auto-managed, read-mostly)
GET    /api/v1/github-app/installations             → List known installations
POST   /api/v1/github-app/installations/discover    → Trigger discovery from GitHub API
GET    /api/v1/github-app/installations/{id}        → Get installation details

# Grove GitHub settings (in grove settings tab)
PUT    /api/v1/groves/{id}/github-installation      → Set/override installation for grove
DELETE /api/v1/groves/{id}/github-installation      → Remove (fall back to PAT)
PUT    /api/v1/groves/{id}/github-permissions        → Set per-grove token permissions
GET    /api/v1/groves/{id}/github-permissions        → Get current permission config
DELETE /api/v1/groves/{id}/github-permissions        → Reset to defaults

# Token refresh (called by sciontool inside agent container)
POST   /api/v1/agents/{id}/refresh-token            → Mint fresh installation token

# Callbacks and webhooks
GET    /github-app/setup                            → Post-installation callback (browser redirect)
POST   /api/v1/webhooks/github                      → Receive GitHub webhook events
```

### 7.2 Modified Endpoints

The existing agent creation flow (`POST /api/v1/groves/{id}/agents` and the Hub→Broker dispatch) is modified to:

1. Check if the grove has a `github_installation_id`.
2. If yes: mint an installation token (with grove-specific permissions if configured, otherwise defaults) and include it as `GITHUB_TOKEN` in resolved environment.
3. If no: fall through to existing PAT secret resolution.

This is transparent to the Broker and agent — they always receive a `GITHUB_TOKEN` env var regardless of source.

---

## 8. Permission Model

### 8.1 App-Level Permissions (Set at Registration)

The GitHub App should be registered with the **maximum permissions** any agent might need:

| Permission | Access | Purpose |
|------------|--------|---------|
| Contents | Read and write | Clone, commit, push |
| Metadata | Read | Repository info |
| Pull requests | Read and write | Create/update PRs |
| Issues | Read and write | Create/comment on issues |
| Checks | Read and write | Report CI status (future) |
| Actions | Read | Read workflow status (future) |

### 8.2 Per-Token Permission Restriction

When minting an installation token, the Hub requests a **subset** of the app's registered permissions. The token is always scoped to the single repo the grove targets.

```go
// Token request body
{
    "repositories": ["widgets"],
    "permissions": {
        "contents": "write",
        "pull_requests": "write",
        "metadata": "read"
    }
}
```

### 8.3 Grove-Level Permission Settings

Each grove can declare the permissions its agents need. This is configured in grove settings and stored as part of the grove model (see `GitHubTokenPermissions` in §4.3).

**CLI configuration:**

```bash
# Set grove-specific permissions
scion hub grove set acme-widgets --github-permissions contents:write,pull_requests:write,metadata:read

# View current permissions
scion hub grove get acme-widgets --show-github-permissions
```

**Template-driven defaults:**

```yaml
# In scion-agent.yaml template
github_permissions:
  contents: write
  pull_requests: write
  metadata: read
```

If a grove does not have explicit permissions configured, the **default permission set** is used: `Contents: write, Pull Requests: write, Metadata: read`.

**Validation:** The Hub validates that requested grove-level permissions do not exceed the app's registered permissions. If a grove requests `checks: write` but the app was not registered with Checks permission, the configuration is rejected with a clear error.

**Web UI:** Grove-level permission settings are managed in the **Grove Settings tab**.

---

## 9. Integration with Existing Systems

### 9.1 Agent Transparency

The agent and harness code requires **zero changes**. The credential arrives as `GITHUB_TOKEN` regardless of source. The git credential helper configured by `sciontool` works identically with both PATs and installation tokens. The `gh` CLI also uses `GITHUB_TOKEN` natively.

### 9.2 sciontool Changes

`sciontool` gains:

1. **Token refresh credential helper**: When `SCION_GITHUB_APP_ENABLED=true` is set, the credential helper calls the Hub to refresh tokens instead of returning a static value.
2. **Background token refresh loop**: Proactively refreshes the token every 50 minutes, writing the fresh token to `SCION_GITHUB_TOKEN_PATH` for non-git consumers.
3. **gh wrapper**: A lightweight script at `/usr/local/bin/gh` that reads the current token from the token file before delegating to the real `gh` binary.

### 9.3 Web UI

**Hub Admin Page:**
- GitHub App configuration (App ID, setup URL to give to GitHub, webhook status).
- "Install App" link that directs to the GitHub App's public installation page.
- Installation list with status indicators (active/suspended/deleted).
- Webhook connectivity indicator.

**Grove Settings Tab** (grove-level items):
- Credential source indicator (PAT vs GitHub App) with health status.
- "Install GitHub App" button if no installation is associated (links to GitHub).
- GitHub token permission configuration.
- Token refresh status for active agents.

**Grove Creation Flow:**
- Auto-discovery of existing installations for the repo.
- Prompt to install the app if no installation found.

---

## 10. Commit Attribution

Agent commits can be attributed in three configurable ways:

### 10.1 Option A: App Bot Identity (Default)

Commits from `scion-app[bot]@users.noreply.github.com`. Clear automated provenance.

### 10.2 Option B: Custom Identity

Groves or templates specify `git user.name` and `git user.email`. The installation token authenticates the push, but the commit author is the configured identity. Already supported — custom templates use standard Scion environment variable injection for `GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL`, etc.

### 10.3 Option C: Co-authored-by Trailers

Use the bot identity but add `Co-authored-by: Alice <alice@example.com>` trailers linking to the Scion user who started the agent.

### 10.4 Configuration

Attribution mode is configurable at the grove level (in grove settings) and template level:

```yaml
# In grove settings or template
git_identity:
  mode: bot          # "bot" (default), "custom", "co-authored"
  name: "My Agent"   # Used when mode is "custom"
  email: "agent@example.com"
```

The default is **bot identity** (Option A). Templates that already set git user identity via Scion env vars continue to work.

---

## 11. Rate Limiting

GitHub App installation tokens have their own rate limit (5000 req/hr per installation). With many agents on the same grove (same installation), rate limits could potentially be exhausted.

**Strategy:**
1. **Monitor:** The Hub logs rate limit headers (`X-RateLimit-Remaining`, `X-RateLimit-Reset`) from GitHub API responses during token minting.
2. **Surface:** Rate limit status is included in agent health checks and visible in the Web UI.
3. **Warn:** When remaining rate limit drops below a threshold (e.g., 20%), the Hub surfaces a warning on affected groves.

---

## 12. GitHub Enterprise Server

The design supports GitHub Enterprise Server (GHES) from the start via the `api_base_url` configuration field:

```yaml
github_app:
  app_id: 123
  private_key_path: /path/to/key.pem
  api_base_url: https://github.mycompany.com/api/v3  # default: https://api.github.com
```

**Settings schema tracking:** The `api_base_url` field is registered in the Hub settings schema for validation and UI rendering. For GHES instances on the same network as the Hub, webhook reachability is likely simpler (both behind the same firewall).

---

## 13. Private Key Rotation

The GitHub App private key can be rotated using GitHub's multi-key support:

**Procedure:**
1. Generate a new private key on GitHub (GitHub App settings → Generate a private key).
2. Update the key on the Hub: update the config file or secret manager entry.
3. Restart the Hub server or trigger a config reload.
4. Verify token minting works with the new key.
5. Delete the old key on GitHub.

During steps 2-3, both keys are valid on GitHub's side, so there is no downtime window.

A runbook for key rotation should be included in the operations guide.

---

## 14. Alternatives Considered

### 14.1 GitHub OAuth User Tokens for Git Operations

**Why rejected:** OAuth user tokens inherit the user's full access — no repo restriction, no automatic refresh, commits attributed to user, conflates Hub auth with agent auth.

### 14.2 GitHub App as Sole Auth Method (Replace PATs)

**Why rejected:** PATs are simpler for solo/local mode. Not all users can install apps. Backward compatibility with existing deployments.

### 14.3 Per-Agent GitHub App

**Why rejected:** GitHub limits on app creation. Massive operational overhead. No benefit over installation-scoped tokens.

### 14.4 User-Brought App (BYOA)

**Why rejected as primary:** Unreasonable UX burden — every user must understand GitHub App registration. Multiple apps on the same org creates clutter. The Hub-level app covers the majority case cleanly. May be revisited as a future escape hatch for multi-tenant deployments.

### 14.5 Proxy All Git Operations Through Hub

**Why rejected:** Massive bandwidth/latency implications. Breaks standard git tooling. Over-engineered.

---

## 15. Security Considerations

### 15.1 Private Key Protection

The GitHub App private key is the most sensitive credential in this system. It can mint tokens for any installation of the app.

- **At rest**: Stored on the Hub server's filesystem or in a cloud secret manager (GCP SM, AWS SM). Never in the database.
- **In transit**: Never leaves the Hub. Brokers and agents receive only installation tokens.
- **Access**: Only the Hub server process reads the key. Filesystem permissions: `0600`, owned by the Hub service user.
- **Rotation**: Supported via GitHub's multi-key feature (see §13).

### 15.2 Installation Token Scope

Installation tokens are always scoped to the **minimum necessary**:
- **Repositories**: Scoped to the grove's target repository (single repo, since groves are 1:1 with repos).
- **Permissions**: Grove-level if configured (§8.3), otherwise default set (Contents: write, Pull Requests: write, Metadata: read).

Even if an installation grants access to "all repositories" in an org, the minted token only gets access to the specific repo the grove targets.

### 15.3 Token Exposure

Installation tokens are treated identically to PATs in the security model:
- Injected as environment variables (same as today).
- Never logged by `sciontool` (existing sanitization applies). The token file at `SCION_GITHUB_TOKEN_PATH` has permissions `0600`.
- 1-hour expiry limits blast radius of token theft.

### 15.4 Webhook Security

The webhook endpoint (`/api/v1/webhooks/github`) validates all incoming payloads:
- **Signature verification**: Using the `webhook_secret` from Hub config (`X-Hub-Signature-256` header).
- **Event filtering**: Only processes `installation` and `installation_repositories` events; ignores all others.
- **Rate limiting**: The webhook endpoint has its own rate limit to prevent abuse.

### 15.5 Trust Boundary

The Hub is the trust anchor. Organizations installing the GitHub App are trusting:
1. The Hub operator (who holds the private key).
2. The Scion platform (to mint correctly scoped tokens).
3. Their own installation scope (which repos the app can access).

This is comparable to installing any third-party GitHub App (CI systems, code review tools, etc.).

---

## 16. Implementation Phases

### Phase 1: Hub-Level App Configuration and Token Minting

1. Add `GitHubAppConfig` to Hub server configuration (including `api_base_url`, `webhook_secret`).
2. Register all fields in Hub settings schema.
3. Implement JWT generation from private key (`pkg/hub/githubapp/`).
4. Implement installation token minting via GitHub API.
5. Add Hub API: `GET /api/v1/github-app`, `PUT /api/v1/github-app`.
6. Add `GitHubInstallation` model and store operations.
7. Add Hub API: `GET/POST /api/v1/github-app/installations`.
8. Unit tests for JWT generation and token exchange.

### Phase 2: Installation Callback, Grove Association, and Secret Resolution

1. Implement setup URL callback handler (`GET /github-app/setup`).
2. Implement webhook endpoint (`POST /api/v1/webhooks/github`) with signature verification.
3. Auto-match installations to groves by repo URL in both callback and webhook handlers.
4. Add `github_installation_id` and `github_permissions` to Grove model.
5. Add Hub API: grove GitHub installation and permissions endpoints.
6. Implement auto-discovery fallback for `scion hub grove create`.
7. Integrate into secret resolution: when grove has installation, mint token with grove-specific permissions (or defaults).
8. Transparent injection as `GITHUB_TOKEN` in agent environment.
9. Integration tests: app install callback → grove association → agent start → git clone.

### Phase 3: Token Refresh (Blended)

1. Add Hub API: `POST /api/v1/agents/{id}/refresh-token`.
2. Extend `sciontool` credential helper for on-demand token refresh (git operations).
3. Add `sciontool` background token refresh loop (gh CLI / API operations).
4. Add `gh` wrapper script for fresh token injection.
5. Add `SCION_GITHUB_APP_ENABLED`, `SCION_GITHUB_TOKEN_EXPIRY`, and `SCION_GITHUB_TOKEN_PATH` env vars.
6. Test long-running agents with token refresh across git and gh CLI.

### Phase 4: Web UI and Polish

1. Hub admin page: GitHub App configuration, install link, installation list.
2. Grove settings tab: credential source, permissions, "Install App" button.
3. Grove creation flow with auto-discovery.
4. Commit attribution configuration (bot/custom/co-authored).
5. Rate limit monitoring and warning system.
6. Documentation and key rotation runbook.

---

## 17. Open Questions

### 17.1 Token File Security in Shared Containers

**Question:** The background refresh loop writes fresh tokens to `/tmp/.github-token`. Is this an acceptable trade-off vs environment-variable-only tokens?

**Consideration:** The file has `0600` permissions and the token expires in 1 hour — same security posture as `GITHUB_TOKEN` in the environment. `sciontool` should clean up the token file on agent exit.

### 17.2 Webhook Reachability Validation

**Question:** Beyond the admin-asserted `webhooks_enabled` flag, should the Hub validate webhook reachability during setup?

**Leaning:** Yes — register a test webhook with GitHub during app configuration in the Web UI and check for the ping event. This provides a concrete validation step without relying on external probing.

### 17.3 Setup URL vs Webhook Race Condition

**Question:** The setup URL redirect and the `installation.created` webhook may arrive in any order (or one may fail). Both attempt to register the installation and match groves.

**Consideration:** Both handlers must be idempotent. The installation record uses `installation_id` as a natural key — creating an already-existing installation is a no-op. Grove matching is also idempotent. This should be safe but needs explicit testing.

### 17.4 Token Permissions Drift

**Question:** What happens when the GitHub App's registered permissions are reduced, but groves still request those permissions?

**Consideration:** GitHub rejects the token request. The Hub should detect this failure, surface a clear error, and periodically sync the app's current permissions from `GET /app` to validate grove configurations proactively.

### 17.5 Installation Repo Changes After Setup

**Question:** An org admin can modify which repos the GitHub App has access to at any time (via GitHub settings). If they remove a repo that a grove targets, token minting will fail.

**Consideration:** The `installation_repositories.removed` webhook event handles this if webhooks are enabled. Without webhooks, the failure surfaces at token minting time. The Hub should surface a clear error: "Repository 'acme/widgets' is no longer accessible to the GitHub App installation."

---

## 18. References

- **GitHub Docs**: [About GitHub Apps](https://docs.github.com/en/apps/overview)
- **GitHub Docs**: [Authenticating as a GitHub App](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/about-authentication-with-a-github-app)
- **GitHub Docs**: [Creating an installation access token](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app)
- **GitHub Docs**: [GitHub App setup URL](https://docs.github.com/en/apps/creating-github-apps/setting-up-a-github-app/about-the-setup-url)
- **Scion Design**: `.design/hosted/git-groves.md` — Current PAT-based git authentication
- **Scion Design**: `.design/hosted/secrets-gather.md` — Secret provisioning and resolution
- **Scion Design**: `.design/agent-credentials.md` — Agent credential management
- **Scion Design**: `.design/hosted/auth/oauth-setup.md` — Hub OAuth configuration (user auth, separate from this)
