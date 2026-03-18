---
title: Personal Access Tokens
description: Managing and using Personal Access Tokens (PATs) in Scion.
---

Scion supports Personal Access Tokens (PATs) for programmatic access to the Hub API and for authenticating CLI operations when browser-based OAuth is not feasible.

## Overview

A Personal Access Token is a long-lived credential linked to your user account. It inherits all your permissions, allowing scripts, CI/CD pipelines, or remote tools to interact with the Scion Hub on your behalf.

## Creating a Personal Access Token

You can generate a new PAT using the Scion CLI:

```bash
scion hub token create "My CI/CD Token"
```

This will output the token value. **Store this token securely.** It is only displayed once and cannot be retrieved later.

## Using a Personal Access Token

To authenticate with a PAT, you must set it in your environment using the `SCION_HUB_TOKEN` variable:

```bash
export SCION_HUB_TOKEN="scion_pat_..."
scion list
```

When this environment variable is set, the CLI will bypass the browser-based OAuth flow and use the token for all communication with the Hub.

## Trust Level Separation

It is crucial to understand the distinction between how users authenticate with the Hub and how agents authenticate with the Hub. Scion uses two separate environment variables for this purpose to enforce strict privilege boundaries:

### `SCION_HUB_TOKEN` (User Level)
- **Purpose**: Authenticates a human user or a CI/CD pipeline.
- **Scope**: Grants full access based on the user's permissions.
- **Usage**: Used by the Scion CLI or external scripts calling the Hub API.

### `SCION_AUTH_TOKEN` (Agent Level)
- **Purpose**: Authenticates an agent running within a container.
- **Scope**: Carries a Hub-issued JWT scoped specifically to that agent. It is short-lived, auto-injected by the Runtime Broker, and grants only the specific permissions that agent needs to function (e.g., reporting status, reading its own secrets).
- **Usage**: Automatically used by the `sciontool` binary running inside the agent.

:::danger[Privilege Escalation Risk]
**Never inject a `SCION_HUB_TOKEN` (or a user-level PAT) into an agent container as the `SCION_AUTH_TOKEN`.** 

Injecting a user PAT into an agent means the agent will operate with your full user permissions, rather than its intended, restricted scope. This allows the agent to create other agents, access other groves, or read secrets it shouldn't have access to. The Scion runtime automatically handles agent authentication; you do not need to manually configure agent tokens.
:::

## Managing Tokens

If a token is compromised or no longer needed, you can revoke it:

```bash
scion hub token revoke <token-id>
```

You can list all your active tokens using:

```bash
scion hub token list
```
