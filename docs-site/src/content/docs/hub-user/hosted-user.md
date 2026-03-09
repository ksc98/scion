---
title: Team Workflow
description: Connecting to a Scion Hub for team collaboration.
---

**Target Persona**: Team Developer (Hub User)
**What you will learn**: How to connect your local CLI to your organization's Scion Hub, dispatch agents remotely, use the Web Dashboard, and collaborate with your team.

Scion's "Hosted" mode allows teams to share state, infrastructure, and agent configurations by connecting to a central Scion Hub.

## Connecting to a Hub

To connect your local CLI to a team Hub, you configure the `hub` section in your `settings.yaml`.

### Configuration

Edit `~/.scion/settings.yaml` (or use `scion config set`):

```yaml
hub:
  enabled: true
  endpoint: "https://scion.yourcompany.com"
  local_only: false
```

### Authentication

Once the endpoint is configured, authenticate your CLI:

```bash
scion hub auth login
```

This will open your browser to complete the OAuth flow.

## Project Linking (Groves)

In a team environment, a **Grove** represents a shared project. You link your local directory to a Grove on the Hub to share context with your team.

```bash
# Link the current directory to the Hub
scion hub link
```

If the project is already registered (matched by Git remote), Scion will link it automatically. If not, it will prompt you to register a new Grove.

### Grove Configuration

When linked, your `.scion/settings.yaml` will include the Grove ID:

```yaml
hub:
  grove_id: "uuid-of-the-grove"
```

## Using Remote Infrastructure

With the Hub connected, you can dispatch agents to **Runtime Brokers** managed by your team, rather than running them on your local laptop.

### Selecting a Broker
The Hub automatically routes tasks to available brokers. You can tag agents to request specific capabilities (e.g., `gpu-capable`).

### Local Fallback
If you want to temporarily run agents locally even while connected to the Hub, you can use the `--local` flag or set `hub.local_only: true` in your settings.

## Shared Secrets & Environment

Teams should manage configuration and secrets centrally on the Hub instead of sharing `.env` files or hardcoding credentials.

```bash
# Set an environment variable for the project
scion hub env set --grove API_URL=https://api.staging.example.com

# Set a secret for the project
scion hub secret set --grove OPENAI_API_KEY=sk-...
```

Secrets are encrypted and never returned via the API; they are securely injected into agents at runtime by the Runtime Broker.

See the [Secret & Environment Management guide](/hub-user/secrets) for details on scoping and projection modes.

## Remote & Hub-Native Groves

Instead of linking a local directory, you can create groves directly on the Hub. This decouples agent execution from your local machine, allowing for remote-only development.

### Hub-Native Groves
Hub-Native groves allow you to create project workspaces without any external Git repository. The Hub manages the workspace files directly, and you can download or ZIP the workspace via the Web Dashboard.

```bash
# Target a Hub-Native grove remotely by its slug:
scion start my-agent --grove my-hub-native-slug "do some work"
```

### Git Groves
You can also create a grove directly from a git repository URL. The agent's container will clone the repository at startup.

#### Creating a Grove from a Git URL

```bash
scion hub grove create https://github.com/org/my-project.git \
  --name "My Project" \
  --slug my-project \
  --branch develop
```

#### Setting Up Authentication

For private repositories, set a `GITHUB_TOKEN` secret on the grove. The token needs at minimum **Contents: Read** permission.

```bash
scion hub secret set --grove my-project GITHUB_TOKEN=ghp_xxxxxxxxxxxx
```

#### Starting Agents Remotely

Once the grove is created, you can start agents targeting the remote grove directly using the `--grove` flag with the slug or git URL:

```bash
scion start my-agent --grove my-project "implement feature X"
```

The agent's container will clone the repository at startup, create a `scion/<agent-name>` branch, and begin working.

### End-to-End Example

```bash
# 1. Create the grove from a git URL
scion hub grove create https://github.com/acme/backend.git --name "Acme Backend"

# 2. Set the GitHub token for private repo access
scion hub secret set --grove acme-backend GITHUB_TOKEN=ghp_xxxxxxxxxxxx

# 3. Start an agent remotely on the grove
scion start my-agent --grove acme-backend "add user authentication"

# 4. Monitor the agent
scion list --grove acme-backend
```

## Collaboration

- **Web Dashboard**: Use the Hub's web interface to view running agents, logs, and status.
- **Remote Attach**: You can attach to a remote agent's terminal session using `scion attach`, tunneling through the Hub.
