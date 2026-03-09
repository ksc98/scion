---
title: Runtime Broker
description: Understanding the role, administration, and configuration of Scion Runtime Brokers.
---

The **Runtime Broker** is a fundamental component of the Scion architecture. It serves as the execution host for agents, bridging the gap between the centralized **Scion Hub** (control plane) and the actual compute resources (containers or VMs).

## Role and Purpose

In a distributed Scion deployment, the Runtime Broker is responsible for the heavy lifting of agent execution. While the Hub manages metadata, identity, and coordination, the Broker manages the local lifecycle of agents.

Key responsibilities include:

*   **Agent Lifecycle Management**: Creating, starting, stopping, and deleting agent containers or pods.
*   **Workspace Isolation**: Creating dedicated git worktrees and mounting them into agent containers to ensure no two agents interfere with each other.
*   **Template Hydration**: Fetching agent templates from the Hub or cloud storage and caching them locally.
*   **NAT Traversal**: Establishing a persistent WebSocket "Control Channel" to the Hub, allowing the Hub to send commands to brokers even when they are behind firewalls or NAT.
*   **Observability**: Collecting logs and metrics from running agents and reporting them to the Hub.

## Architecture

A Runtime Broker typically runs as a background daemon on a compute node (e.g., a developer's laptop, a cloud VM, or a node in a Kubernetes cluster).

```d2
direction: right
User -> Hub: "Start Agent"
Hub -> Broker: "CreateAgent (via WS Tunnel)"
Broker -> Docker/Podman/K8s: "Run Container"
Broker -> Storage: "Fetch Template"
Agent -> Hub: "Status: RUNNING"
```

### Deployment Modes

*   **Solo Mode**: The Scion CLI runs a local, ephemeral Runtime Broker automatically when you start an agent.
*   **Hosted Mode**: A dedicated Runtime Broker process registers with a Scion Hub and waits for instructions. This allows teams to share powerful compute resources or run agents in specific network environments.
*   **Combo Mode (Co-located)**: The Broker runs in the same process as the Hub (the default for `scion server start --workstation`). 
*   **Multi-Hub Connectivity**: A single Runtime Broker can maintain simultaneous connections to multiple different Hubs. It manages a multi-credential store and per-connection heartbeats, allowing a broker to be co-located with one Hub (Combo Mode) while serving other remote Hubs concurrently.

## Administration

Managing a Runtime Broker involves starting the server, registering it with a Hub, and assigning it to specific projects (Groves).

### Starting the Broker

To start the broker as a background daemon:

```bash
scion broker start
```

Use the `--foreground` flag if you want to run it in your current terminal session for debugging.

### Registration

Before a broker can receive commands from a Hub, it must be registered. This establishes a trust relationship and generates authentication credentials.

```bash
scion broker register
```

This command will:
1. Verify the local broker server is running.
2. Link the host with your Hub account.
3. Securely exchange a shared secret for [HMAC-based authentication](/guides/auth/#runtime-broker-security).

### Managing Grove Providers

A broker only executes agents for the **Groves** it is a "provider" for. You can add a broker to a grove using the `provide` command:

```bash
# Add this local broker to the current project
scion broker provide
```

To see which groves a broker is currently serving:

```bash
scion broker status
```

## Configuration

The Runtime Broker is configured in `settings.yaml` under the `server` section, or via environment variables.

### Key Settings

| Setting | Env Var | Description |
|---------|---------|-------------|
| `server.broker.port` | `SCION_SERVER_BROKER_PORT` | Port for the Broker API (default: 9800). |
| `server.broker.hub_endpoint` | `SCION_SERVER_BROKER_HUB_ENDPOINT` | URL of the Scion Hub. |
| `server.broker.container_hub_endpoint` | `SCION_SERVER_BROKER_CONTAINERHUBENDPOINT` | Overrides `hub_endpoint` for agent containers (e.g. `http://host.containers.internal:8080`). |
| `server.broker.broker_id` | `SCION_SERVER_BROKER_BROKER_ID` | Unique ID for the broker (assigned during registration). |
| `server.broker.auto_provide` | `SCION_SERVER_BROKER_AUTOPROVIDE` | Automatically add as provider for new groves. |

### Resource Limits

When running in multi-tenant environments, you can configure the broker to limit the resources available to agents.

```yaml
# settings.yaml
server:
  broker:
    resources:
      max_concurrent_agents: 10
      default_cpu_request: "500m"
      default_memory_request: "1Gi"
```

## Security

Security is paramount for Runtime Brokers, as they have the authority to create processes and access source code.

*   **Mutual Authentication**: All communication between the Hub and the Broker is authenticated using HMAC-SHA256 signatures.
*   **Isolation**: Every agent runs in its own container and its own git worktree.
*   **Secret Injection**: Sensitive environment variables (like API keys) are resolved at the Hub and injected directly into the agent's environment by the Broker at startup, never touching the Broker's persistent storage.

For a deep dive into the security protocols, see the [Runtime Broker Security](/guides/auth/#runtime-broker-security) section in the Authentication guide.

## Observability

The Runtime Broker supports structured logging and can forward its internal logs and traces to an OpenTelemetry-compatible backend. This allows administrators to monitor the health of the broker and correlate its actions with agent operations.

To enable log forwarding, set `SCION_OTEL_LOG_ENABLED=true` and `SCION_OTEL_ENDPOINT`. See the [Observability Guide](/hub-admin/observability) for setup instructions.
