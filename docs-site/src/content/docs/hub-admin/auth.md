---
title: Authentication & Identity
description: Configuring authentication flows for Scion.
---

Scion implements a unified authentication system designed to secure communication between all components: the CLI, the Web Dashboard, the Hub, and individual Agents.

## Identity Types

Scion recognizes four primary identity types:

1.  **Users**: Humans interacting via the CLI or Web Dashboard. Authenticated via OAuth or Development tokens.
2.  **Agents**: Running LLM instances. Authenticated via short-lived JWTs issued by the Hub during provisioning.
3.  **Runtime Brokers**: Infrastructure nodes that execute agents. Authenticated via Broker tokens.
4.  **Development User**: A special identity used for local development and zero-config testing.

## Authentication Methods

Scion supports multiple authentication methods for different use cases:

- **OAuth (Google/GitHub)**: For production web and CLI authentication.
- **Development Auth**: For local development and testing.
- **API Keys**: For programmatic access and CI/CD pipelines.

## OAuth Authentication

Scion supports OAuth authentication via Google and GitHub. OAuth credentials are configured separately for web and CLI clients due to different redirect URI requirements.

### Web OAuth Setup

Configure web OAuth with these environment variables:

```bash
export SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTID="your-client-id"
export SCION_SERVER_OAUTH_WEB_GOOGLE_CLIENTSECRET="your-client-secret"
export SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID="your-client-id"
export SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTSECRET="your-client-secret"
```

### CLI OAuth Setup

Configure CLI OAuth with these environment variables:

```bash
export SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID="your-client-id"
export SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTSECRET="your-client-secret"
export SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTID="your-client-id"
export SCION_SERVER_OAUTH_CLI_GITHUB_CLIENTSECRET="your-client-secret"
```

## Domain Authorization

You can restrict authentication to specific email domains using the `SCION_AUTHORIZED_DOMAINS` setting. This provides an additional layer of access control beyond OAuth authentication.

### Configuration

Set the environment variable with a comma-separated list of allowed domains:

```bash
# Allow only users from these domains
export SCION_AUTHORIZED_DOMAINS="example.com,mycompany.org"
```

Or configure in `server.yaml`:

```yaml
auth:
  authorizedDomains:
    - example.com
    - mycompany.org
```

### Behavior

- **Empty list (default)**: All email domains are allowed.
- **Non-empty list**: Only emails from listed domains can authenticate.
- **Case insensitive**: `Example.COM` matches `example.com`.
- **Exact match**: Subdomains must be listed explicitly.

## Development Authentication (Dev Auth)

To minimize friction during local setup, Scion includes a "Dev Auth" mode. When enabled, the Hub auto-generates a token and creates a "Development User" identity.

### Enabling Dev Auth
Start the server with the `--dev-auth` flag or set it in your `server.yaml`:

```yaml
auth:
  devMode: true
```

Or via environment variable:
```bash
export SCION_SERVER_AUTH_DEVMODE=true
```

### Using the Dev Token
When the Hub starts with `devMode: true`, it writes the token to `~/.scion/dev-token`.
- **CLI**: The `scion` CLI automatically looks for this file.
- **Web**: The Web Dashboard automatically uses this token for the "Development User" login when `SCION_DEV_AUTH_ENABLED=true` is set.

Alternatively, you can set the token in your environment:
```bash
export SCION_DEV_TOKEN=scion_dev_...
```

## Runtime Broker Security

Runtime Brokers use a robust security model to ensure that only authorized Hubs can dispatch commands and that agents remain isolated.

### HMAC-Based Authentication

Communication between the Hub and a Runtime Broker (in both directions) is secured using **HMAC-SHA256 request signing**. This provides several security benefits:
- **Mutual Authentication**: Both parties prove they possess the shared secret.
- **Payload Integrity**: The request body is included in the signature, preventing tampering.
- **Replay Protection**: Every request includes a timestamp and a unique nonce.

A shared secret is established during the `scion broker register` flow and is stored locally in `~/.scion/broker-credentials.json`.

### Provider Authorization

The Hub enforces a "Provider" model for authorization. Even if a broker is authenticated, it will only receive agent dispatch requests for **Groves** that it has been explicitly registered to provide for. This prevents a compromised broker from accessing projects it shouldn't have access to.

### Secret Management

Brokers never store agent secrets (like API keys) on disk.
1. The Hub resolves secrets from all applicable scopes (user, grove, broker) via the configured secrets backend (e.g., GCP Secret Manager).
2. The Hub includes the resolved secrets in the `CreateAgent` command sent to the Broker over the TLS-secured control channel.
3. The Broker projects secrets into the agent container based on their type (environment variable, JSON file, or filesystem path).
4. When the agent is deleted, the secrets are purged from the host.

For details on configuring and managing secrets, see [Secret Management](../hub-user/secrets/).

## CLI Authentication

Users can authenticate the CLI against a Scion Hub using the following flow:

1.  **Login**: `scion hub auth login` opens a browser to the dashboard login page.
2.  **Exchange**: After successful login, the dashboard provides a token (or the CLI exchanges a code).
3.  **Storage**: The token is stored in `~/.scion/config.json`.

## Agent Authentication

Agents are automatically authenticated. When the Hub dispatches an agent to a Runtime Broker, it includes a one-time-use **Agent Token**.
- The agent uses this token for all calls back to the Hub (e.g., updating status, streaming logs).
- Tokens are scoped to the specific agent and its grove.
- Tokens have a default expiration (typically 24 hours), but Scion implements an automated token refresh mechanism to ensure long-running agents maintain valid authorization throughout extended tasks.

## API Keys

For programmatic access (e.g., CI/CD pipelines), the Hub supports API Keys.
- Keys can be generated via the Web Dashboard or CLI.
- Keys are prefixed with `sk_live_` or `sk_test_`.
- Use the `Authorization: Bearer <key>` header or `X-API-Key` header in your requests.