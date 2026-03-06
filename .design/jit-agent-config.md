# JIT Agent Configuration: Inline Config at Agent Creation Time

**Status:** Draft
**Created:** 2026-03-06
**Related:** [agent-config-flow.md](./agent-config-flow.md), [hosted-templates.md](./hosted/hosted-templates.md)

---

## 1. Overview

### Problem

Today, agent configuration is assembled from a multi-layered composition of templates, harness configs, settings profiles, and CLI flags. To customize an agent beyond the available CLI flags, users must:

1. Create a custom template directory with a `scion-agent.yaml`
2. Optionally create a custom harness-config directory
3. Reference these by name at agent creation time

This works well for reusable configurations but creates friction for one-off or exploratory use cases. Every new tunable that a user might want to set ad-hoc requires either a new CLI flag (leading to flag proliferation — e.g., `--enable-telemetry`, `--model`, `--max-turns`) or a new template.

The Hub web UI amplifies this problem: building a form that exposes all agent options requires either generating templates server-side or adding every field as a discrete API parameter.

### Goal

Allow agents to be started with an **inline configuration object** — a self-contained document that can express the full range of agent configuration without requiring pre-existing template or harness-config artifacts on disk. This is referred to as "JIT (Just-In-Time) Agent Config."

### Design Principles

1. **Additive** — JIT config is a new input path, not a replacement. Templates and harness configs continue to work as before.
2. **Superset** — The JIT config schema is a superset of `ScionConfig` (the current `scion-agent.yaml` format), extended with fields that today live outside the config file (system prompt content, harness-config details).
3. **Explicit over composed** — When a JIT config is provided, its values are authoritative. The multi-layer merge behavior is simplified: JIT config is the "template equivalent," composed only with broker/runtime-level concerns.
4. **Backwards compatible** — Existing `scion start --type my-template` workflows are unaffected.

---

## 2. Current State

### Configuration Sources (Precedence, Low → High)

```
Embedded defaults
  → Global settings (~/.scion/settings.yaml)
    → Grove settings (.scion/settings.yaml)
      → Template chain (scion-agent.yaml, inherited)
        → Harness-config (config.yaml + home/ files)
          → Agent-persisted config (scion-agent.json)
            → CLI flags (--image, --enable-telemetry, etc.)
```

### What Lives Where Today

| Concern | Where It's Defined | Format |
|---------|-------------------|--------|
| Harness type | Template `scion-agent.yaml` | `harness: claude` |
| Container image | Harness-config `config.yaml` or template | `image: ...` |
| Environment vars | Template, harness-config, settings, CLI | `env: {K: V}` |
| System prompt | Template directory file (`system-prompt.md`) | Markdown file |
| Agent instructions | Template directory file (`agents.md`) | Markdown file |
| Model selection | Template or harness-config | `model: claude-opus-4-6` |
| Auth method | Harness-config or settings profile | `auth_selected_type: api-key` |
| Container user | Harness-config `config.yaml` | `user: scion` |
| Volumes | Template, harness-config, settings | `volumes: [...]` |
| Resources | Template or settings profile | `resources: {requests: ...}` |
| Telemetry | Settings, template, or CLI flag | `telemetry: {enabled: true}` |
| Services (sidecars) | Template | `services: [...]` |
| Max turns/duration | Template | `max_turns: 100` |
| Home directory files | Harness-config `home/` directory | Filesystem artifacts |

### Key Observation

The current `ScionConfig` struct already captures most of these concerns. The gaps are:

1. **System prompt and agent instructions** — stored as file references in `ScionConfig` (`system_prompt: system-prompt.md`) but the actual content lives as files alongside the template. The config references a filename, not inline content.
2. **Harness-config details** — the container user, task flag, default CLI args, and auth method come from `HarnessConfigEntry`, not from `ScionConfig`.
3. **Home directory files** — harness-config `home/` directories provide files like `.claude.json`, `.bashrc`, etc. These are filesystem artifacts that can't be expressed inline.

### Existing Partial Precedent

The `AgentConfigOverride` struct in `pkg/hub/handlers.go` already provides a limited inline override:

```go
type AgentConfigOverride struct {
    Image    string            `json:"image,omitempty"`
    Env      map[string]string `json:"env,omitempty"`
    Detached *bool             `json:"detached,omitempty"`
    Model    string            `json:"model,omitempty"`
}
```

And `hubclient.AgentConfig` similarly has `Image`, `HarnessConfig`, `HarnessAuth`, `Env`, `Model`, `Task`. These are narrow override surfaces — the JIT config proposal generalizes this.

---

## 3. Proposed Design

### 3.1 JIT Config Schema

A new `JITAgentConfig` type that is a superset of `ScionConfig`, adding fields that today require separate artifacts:

```go
// JITAgentConfig is a self-contained agent configuration document.
// It extends ScionConfig with fields that normally come from harness-config
// artifacts and template content files.
type JITAgentConfig struct {
    // === All existing ScionConfig fields ===
    Harness            string            `json:"harness,omitempty" yaml:"harness,omitempty"`
    HarnessConfig      string            `json:"harness_config,omitempty" yaml:"harness_config,omitempty"`
    Image              string            `json:"image,omitempty" yaml:"image,omitempty"`
    Env                map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
    Volumes            []VolumeMount     `json:"volumes,omitempty" yaml:"volumes,omitempty"`
    Detached           *bool             `json:"detached,omitempty" yaml:"detached,omitempty"`
    CommandArgs        []string          `json:"command_args,omitempty" yaml:"command_args,omitempty"`
    TaskFlag           string            `json:"task_flag,omitempty" yaml:"task_flag,omitempty"`
    Model              string            `json:"model,omitempty" yaml:"model,omitempty"`
    AuthSelectedType   string            `json:"auth_selected_type,omitempty" yaml:"auth_selected_type,omitempty"`
    Resources          *ResourceSpec     `json:"resources,omitempty" yaml:"resources,omitempty"`
    Kubernetes         *KubernetesConfig `json:"kubernetes,omitempty" yaml:"kubernetes,omitempty"`
    Services           []ServiceSpec     `json:"services,omitempty" yaml:"services,omitempty"`
    MaxTurns           int               `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`
    MaxModelCalls      int               `json:"max_model_calls,omitempty" yaml:"max_model_calls,omitempty"`
    MaxDuration        string            `json:"max_duration,omitempty" yaml:"max_duration,omitempty"`
    Hub                *AgentHubConfig   `json:"hub,omitempty" yaml:"hub,omitempty"`
    Telemetry          *TelemetryConfig  `json:"telemetry,omitempty" yaml:"telemetry,omitempty"`
    Secrets            []RequiredSecret  `json:"secrets,omitempty" yaml:"secrets,omitempty"`

    // === Content fields (inline instead of file references) ===
    // When set, these contain the actual content rather than a filename.
    AgentInstructions  string `json:"agent_instructions,omitempty" yaml:"agent_instructions,omitempty"`
    SystemPrompt       string `json:"system_prompt,omitempty" yaml:"system_prompt,omitempty"`

    // === Harness-config inline fields ===
    // These replace the need for a separate harness-config directory.
    User               string   `json:"user,omitempty" yaml:"user,omitempty"`           // Container unix user

    // === Agent metadata ===
    Task               string `json:"task,omitempty" yaml:"task,omitempty"`
    Branch             string `json:"branch,omitempty" yaml:"branch,omitempty"`
}
```

### 3.2 Relationship to ScionConfig

`JITAgentConfig` is not a new parallel type — it's a presentation format that maps onto the existing `ScionConfig` plus `HarnessConfigEntry` plus content. The conversion is:

```
JITAgentConfig
  ├── maps to ScionConfig (most fields 1:1)
  ├── maps to HarnessConfigEntry (user, task_flag, command_args, auth_selected_type)
  └── provides inline content (system_prompt, agent_instructions → written to files)
```

A `ToScionConfig()` method converts a `JITAgentConfig` into the existing `ScionConfig` struct, and a `ToHarnessConfigEntry()` extracts harness-config-level fields.

### 3.3 Merge Semantics When JIT Config Is Present

When a JIT config is provided:

```
Base template (if --type also specified)
  → JIT config merged over base (JIT wins)
    → CLI flags merged over JIT (flags win)
      → Runtime concerns (auth, env expansion) applied last
```

When a JIT config is provided **without** `--type`:
- The `harness` field in JIT config determines the harness (required in this case)
- A harness-config name can still be specified to pick up `home/` directory files
- If no harness-config is specified, the harness's embedded defaults are used

This means JIT config **replaces** the template layer but still composes with harness-config `home/` files and runtime-level concerns.

### 3.4 CLI Interface

```bash
# From a file
scion start my-agent --config agent-config.yaml

# From stdin (pipe from another tool)
cat config.yaml | scion start my-agent --config -

# Combined with a base template (JIT overrides template)
scion start my-agent --type base-template --config overrides.yaml

# CLI flags still override everything
scion start my-agent --config config.yaml --image custom:latest
```

The `--config` flag accepts a path to a YAML or JSON file. A value of `-` reads from stdin.

### 3.5 Hub API Interface

The existing `CreateAgentRequest` is extended:

```go
// In pkg/hub/handlers.go
type CreateAgentRequest struct {
    Name          string               `json:"name"`
    GroveID       string               `json:"groveId"`
    Template      string               `json:"template,omitempty"`
    // ... existing fields ...

    // JITConfig provides a complete inline agent configuration.
    // When set, this replaces the template as the primary config source.
    // If Template is also set, JITConfig is merged over the template config.
    JITConfig     *JITAgentConfig      `json:"jitConfig,omitempty"`
}
```

The Hub handler treats `JITConfig` as a template-equivalent: it extracts the relevant fields and passes them through to the broker in the `RemoteCreateAgentRequest`.

### 3.6 Web UI Integration

With JIT config, the Hub web UI can present a form with all agent options:

```
┌─────────────────────────────────────────┐
│  Create Agent                           │
├─────────────────────────────────────────┤
│  Name: [________________]               │
│  Grove: [dropdown________]              │
│                                         │
│  ── Configuration ──                    │
│  Base Template: [optional dropdown]     │
│  Harness: [claude ▼]                    │
│  Model: [________________]              │
│  Image: [________________]              │
│                                         │
│  ── Limits ──                           │
│  Max Turns: [____]  Max Duration: [___] │
│                                         │
│  ── Environment ──                      │
│  [KEY] = [VALUE]        [+ Add]         │
│                                         │
│  ── System Prompt ──                    │
│  [multiline editor........................│
│  .........................................│
│                                         │
│  ── Task ──                             │
│  [multiline editor........................│
│  .........................................│
│                                         │
│  [Advanced: Resources, Telemetry, ...]  │
│                                         │
│  [Create Agent]                         │
└─────────────────────────────────────────┘
```

The form serializes to a `JITAgentConfig` JSON object and sends it as `jitConfig` in the create request. No template creation needed.

---

## 4. Implementation Approach

### Phase 1: CLI `--config` Flag (Local/Solo Mode)

**Scope:** Add `--config <path>` to `scion start` and `scion create`. Parse the file into `JITAgentConfig`, convert to `ScionConfig`, and inject into the existing provisioning flow.

**Changes:**
- `pkg/api/types.go` — Add `JITAgentConfig` struct and conversion methods
- `cmd/start.go` / `cmd/common.go` — Add `--config` flag, load file, convert
- `pkg/agent/provision.go` — Accept optional `JITAgentConfig` in provisioning; when present, use it as the template-equivalent layer
- `pkg/agent/provision.go` — Handle inline `system_prompt` and `agent_instructions` by writing them to the agent home directory (same as template content resolution, but from inline strings)

**Key detail:** When `--config` is provided without `--type`, the provisioning path skips template loading and uses the JIT config as the base. The harness-config `home/` directory is still applied (based on the `harness_config` field in JIT config or the harness default).

**Validation:**
- If neither `--type` nor `--config` is provided, existing behavior (default template)
- If `--config` is provided, `harness` must be specified either in the config or via a base template
- If both `--type` and `--config`, merge config over template

### Phase 2: Hub API Support

**Scope:** Extend Hub create-agent API to accept `jitConfig`. The Hub resolves the JIT config and passes relevant fields to the broker.

**Changes:**
- `pkg/hub/handlers.go` — Accept `jitConfig` in `CreateAgentRequest`; merge with template if both provided; pass through to dispatcher
- `pkg/hub/httpdispatcher.go` — Include JIT config fields in `RemoteCreateAgentRequest`
- `pkg/runtimebroker/handlers.go` — Accept and apply JIT config fields during agent provisioning
- `pkg/runtimebroker/types.go` — Extend `CreateAgentConfig` with JIT config fields

**Design decision:** The Hub can either:
- (A) Pass the entire `JITAgentConfig` to the broker, letting the broker do all resolution
- (B) Resolve the JIT config into the existing `RemoteAgentConfig` fields, keeping the broker interface stable

Option (B) is preferred — it keeps the broker interface unchanged and centralizes JIT-to-standard conversion in the Hub. The broker doesn't need to know whether config came from a template or JIT.

### Phase 3: Web UI Form

**Scope:** Add agent creation form to the Hub web UI that generates `JITAgentConfig`.

**Changes:**
- `web/src/client/` — Agent creation form component
- `web/src/server/` — API pass-through (already handled by Hub API)

This phase is purely frontend work once Phase 2 is complete.

### Phase 4: Config Export and Sharing

**Scope:** Allow exporting an existing agent's resolved config as a JIT config file, enabling config sharing and reproduction.

```bash
# Export current agent config as a reusable JIT config file
scion config export my-agent > agent-config.yaml

# Start a new agent with the same config
scion start new-agent --config agent-config.yaml
```

**Changes:**
- `cmd/config.go` — Add `config export` subcommand
- `pkg/agent/` — Read agent's `scion-agent.json` + content files, produce `JITAgentConfig`

---

## 5. Alternative Approaches Considered

### A: Extend `ScionConfig` Directly (Flatten Everything)

Instead of a separate `JITAgentConfig` type, add the missing fields (`user`, inline content) directly to `ScionConfig`.

**Pros:**
- Single type, no conversion logic
- `scion-agent.yaml` files in templates immediately gain inline content support

**Cons:**
- `ScionConfig` is serialized to `scion-agent.json` in every agent directory — adding `user` and large inline content bloats this file
- Conceptual muddling: `ScionConfig` is the agent's resolved config, not a user-facing input format
- Some JIT fields (like `task`, `branch`) are operational parameters, not configuration

**Verdict:** Rejected as primary approach, but the `system_prompt` and `agent_instructions` fields already exist on `ScionConfig` and can accept inline content in Phase 1 by extending the content resolution logic (check if value is a filename, fall back to treating it as inline content).

### B: Templates-as-JSON via API (Ephemeral Templates)

Instead of a new config format, the Hub could create ephemeral/anonymous templates from the web UI form, then reference them normally.

**Pros:**
- No new config path — reuses existing template machinery
- Broker doesn't change at all

**Cons:**
- Creates invisible template artifacts that need lifecycle management
- Ephemeral templates need garbage collection
- Adds latency (create template → start agent, two-step)
- Doesn't solve the CLI use case

**Verdict:** Rejected. Adds complexity without solving the core problem.

### C: Flag Proliferation (Status Quo Extended)

Continue adding CLI flags for each new option (`--model`, `--max-turns`, `--system-prompt`, etc.).

**Pros:**
- Simple, incremental
- No new concepts

**Cons:**
- Doesn't scale — `ScionConfig` has 20+ fields, many with nested structure
- Each new field requires changes to `cmd/`, `StartOptions`, and all the plumbing
- Can't express complex structures (telemetry config, services) via flags
- Web UI still needs a different solution

**Verdict:** Rejected as a strategy. Individual high-use flags (`--model`) may still be added for convenience alongside `--config`.

### D: JIT Config as Complete Override (No Template Merge)

When `--config` is provided, completely ignore templates — no merge, no composition.

**Pros:**
- Simpler mental model: "config file = everything"
- No ambiguity about precedence

**Cons:**
- Users can't use a template as a base and override a few fields
- Forces duplication if you want "template X but with a different model"
- Loses the composability that makes the current system flexible

**Verdict:** Rejected as default behavior, but could be offered as an opt-in mode (`--config-only` or a field in the config itself: `standalone: true`).

---

## 6. Open Questions

### Q1: Should JIT config support inline home directory files?

Today, harness-config `home/` directories provide files like `.claude.json` and `.bashrc`. Should JIT config support declaring these inline?

```yaml
home_files:
  ".claude.json": |
    {"permissions": {"allow": ["Bash", "Read"]}}
  ".bashrc": |
    export PS1="$ "
```

**Considerations:**
- Powerful but complex — files can be binary (images, compiled configs)
- Significantly increases the JIT config surface area
- Alternative: reference a harness-config by name for `home/` files, only override config values inline

**Recommendation:** Defer to a later phase. For Phase 1, the `harness_config` field in JIT config can reference an existing harness-config for `home/` files. Inline file support can be added later if needed.

### Q2: Type placement — `pkg/api` or new package?

`JITAgentConfig` could live in:
- `pkg/api/types.go` alongside `ScionConfig` (simple, everything in one place)
- A new `pkg/api/jitconfig.go` file (still `api` package, just organized)
- `pkg/config/` (closer to the template/settings resolution logic)

**Recommendation:** `pkg/api/jitconfig.go` — same package as `ScionConfig` for easy conversion, separate file for clarity.

### Q3: How should `--config` interact with `--type` for content fields?

If a template has `agents.md` and the JIT config also has `agent_instructions`, the JIT config wins (standard merge). But what about partial specification?

```yaml
# JIT config sets system_prompt but not agent_instructions
system_prompt: "You are a careful code reviewer."
# Should agent_instructions come from the base template?
```

**Recommendation:** Yes — standard merge semantics. JIT config fields override template fields when set, template fields are preserved when the JIT config field is empty. This matches existing `MergeScionConfig` behavior.

### Q4: Should the JIT config schema version independently?

Templates have `schema_version: "1"`. Should JIT config have its own version?

**Recommendation:** Yes, include a `schema_version` field. Start at `"1"`. This allows the JIT config format to evolve independently of the template format, though they'll likely stay in sync.

### Q5: Validation strictness

Should JIT config be validated more strictly than template config? For example:
- Require `harness` to be set (templates can inherit it)
- Require `image` to be set (harness-configs provide defaults)

**Recommendation:** When used standalone (no `--type`), require `harness`. Other fields fall back to harness-config or embedded defaults, same as templates today. The point is to be explicit, not necessarily exhaustive.

### Q6: Should CLI `--config` support URL references?

```bash
scion start my-agent --config https://example.com/configs/reviewer.yaml
```

**Considerations:**
- Templates already support remote URIs (GitHub, archives)
- Security: downloading arbitrary YAML from the internet
- Convenience for shared team configs

**Recommendation:** Defer. File path and stdin are sufficient for Phase 1. URL support can be added alongside template URI support patterns.

### Q7: How does JIT config interact with the env-gather flow?

The env-gather flow (`GatherEnv: true`) evaluates whether required environment variables are present and prompts the user to supply missing ones. JIT config might declare `secrets` that trigger this flow.

**Recommendation:** JIT config's `secrets` field feeds into the same env-gather pipeline. No special handling needed — the broker evaluates completeness the same way regardless of config source.

---

## 7. Migration and Compatibility

### No Breaking Changes

- Existing `scion start --type <template>` continues to work identically
- Existing Hub API `CreateAgentRequest` without `jitConfig` is unchanged
- Existing `scion-agent.yaml` template format is unchanged
- `AgentConfigOverride` in Hub continues to work but becomes a subset of JIT config

### Deprecation Path for `AgentConfigOverride`

Once JIT config is available on the Hub API, `AgentConfigOverride` (`config` field in Hub's `CreateAgentRequest`) becomes redundant — it's a strict subset of `jitConfig`. It can be:
1. Kept for backwards compatibility (simple overrides don't need the full JIT schema)
2. Deprecated in favor of `jitConfig` with a compatibility shim
3. Internally converted to a partial `JITAgentConfig`

**Recommendation:** Keep both for now. `config` (overrides) is for simple cases, `jitConfig` is for complete specifications. Document that `jitConfig` takes precedence if both are set.

### Similarly for `hubclient.AgentConfig`

The client-side `AgentConfig` struct (`Image`, `HarnessConfig`, `HarnessAuth`, `Env`, `Model`, `Task`) is also a subset. Same approach: keep both, document precedence.

---

## 8. Example JIT Config Files

### Minimal: Just override model and add telemetry

```yaml
schema_version: "1"
harness: claude
model: claude-sonnet-4-6
telemetry:
  enabled: true
```

### Full-featured: Code reviewer agent

```yaml
schema_version: "1"
harness: claude
model: claude-opus-4-6
image: us-central1-docker.pkg.dev/my-project/scion/scion-claude:latest

system_prompt: |
  You are a meticulous code reviewer. Focus on:
  - Security vulnerabilities
  - Performance issues
  - API contract violations
  Review only the files that changed. Be concise.

agent_instructions: |
  Review the current branch against main.
  Use `git diff main...HEAD` to see changes.
  Write your review as comments in a new file: REVIEW.md

env:
  REVIEW_STRICTNESS: high
  MAX_FILE_SIZE: "10000"

max_turns: 50
max_duration: 30m

resources:
  requests:
    cpu: "2"
    memory: 4Gi

task: "Review the latest changes on this branch"
```

### Template-based with overrides

```yaml
# Used with: scion start reviewer --type code-review --config this-file.yaml
schema_version: "1"
model: claude-sonnet-4-6  # Override the template's default model
env:
  REVIEW_STRICTNESS: low  # Override one env var, template's others preserved
max_turns: 20             # Shorter review
```
