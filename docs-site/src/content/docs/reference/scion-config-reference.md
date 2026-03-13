---
title: Configuration Overview
description: Guide to the Scion configuration file ecosystem.
---

Scion uses a multi-layered configuration system to manage orchestrator behavior, agent execution, and server operations.

## Key Configuration Files

| File | Purpose | Scope | Reference |
| :--- | :--- | :--- | :--- |
| `settings.yaml` | **Orchestrator Settings**. Defines profiles, runtimes, and harness configurations. | Global (`~`) or Project (`.scion`) | [Orchestrator Settings](../reference/orchestrator-settings/) |
| `scion-agent.yaml` | **Agent Blueprint**. Defines the configuration for a specific agent or template. | Template or Agent | [Agent Configuration](../reference/agent-config/) |
| `state.yaml` | **Runtime State**. Tracks system state like sync timestamps. | Project (`.scion`) | N/A (Managed by Scion) |

## Server Configuration

Server configuration (for Hub and Runtime Broker) is now integrated into `settings.yaml` under the `server` key.

- [Server Configuration Reference](../reference/server-config/)

## Telemetry Configuration

Telemetry settings control agent observability — trace collection, cloud forwarding, privacy filtering, and debug output. These are configured via the `telemetry` block in `settings.yaml` and can be overridden per-template or per-agent in `scion-agent.yaml`.

- [Orchestrator Settings — Telemetry](../reference/orchestrator-settings/#telemetry-configuration-telemetry)
- [Metrics & OpenTelemetry Guide](../hub-admin/metrics/)

## Configuration Hierarchy

Scion resolves settings in the following order (highest priority first):

1.  **CLI Flags**: (e.g., `scion start --profile remote`)
2.  **Environment Variables**: `SCION_*` overrides.
3.  **Grove Settings**: `.scion/settings.yaml` (Project level).
4.  **Global Settings**: `~/.scion/settings.yaml` (User level).
5.  **Defaults**: System built-ins.

## Migration

To migrate legacy configuration files to the new schema v1 format:

```bash
# Migrate general settings
scion config migrate

# Migrate server.yaml to settings.yaml
scion config migrate --server
```
