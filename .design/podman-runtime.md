# Podman Runtime Support

## Overview
This document outlines the design for adding Podman as a supported container runtime in Scion. Podman provides a daemonless, rootless alternative to Docker, sharing a largely compatible CLI but offering different architectural benefits, particularly regarding security and process isolation.

## Motivation
- **Security**: Podman's rootless mode is a significant advantage for users who cannot or do not want to run a root-level daemon.
- **Compatibility**: Many developers use Podman as a drop-in replacement for Docker (often aliased). Native support ensures Scion works correctly without relying on aliases.
- **Ecosystem**: Podman is the default container tool in several Linux distributions (e.g., RHEL, Fedora) and is gaining popularity on macOS via Podman Machine.

## Implementation Design

### 1. `PodmanRuntime` Implementation
A new `PodmanRuntime` struct will be added to `pkg/runtime/podman.go`, implementing the `Runtime` interface defined in `pkg/runtime/interface.go`.

```go
type PodmanRuntime struct {
    Command string // defaults to "podman"
    Host    string // optional remote Podman socket
}
```

The struct mirrors `DockerRuntime` with a `Host` field for remote Podman connections (e.g., `podman machine` socket or a remote Podman service).

### 2. Command Compatibility & Code Reuse
Most Podman commands are identical to Docker, allowing `PodmanRuntime` to follow the same structure as `DockerRuntime`. The shared logic in `buildCommonRunArgs()` (`pkg/runtime/common.go`) handles volume construction, environment variables, harness integration, and secret staging — all of which are runtime-agnostic and will work for Podman without modification.

Podman-compatible commands:
- `podman run`: Supports `--memory`, `--cpus`, `-v` (bind mounts), `-l` (labels), `-t` (TTY).
- `podman stop`, `podman rm`, `podman logs`, `podman exec`, `podman attach`, `podman pull`.
- `podman inspect --format '{{json .Mounts}}'`: Same output structure for `GetWorkspacePath()`.

### 3. Key Differences & Challenges

#### JSON Output Format (`podman ps`)
Docker's `docker ps --format '{{json .}}'` returns a stream of newline-separated JSON objects.
Podman's `podman ps --format json` returns a single JSON array of objects.

Furthermore, field names and types differ:
- **Docker**: `ID`, `Names`, `Status`, `Image`, `Labels` (all strings; Labels is CSV).
- **Podman**: `Id`, `Names` (array), `Status`, `Image`, `Labels` (map).

`PodmanRuntime.List()` must implement a custom parser:

```go
type podmanListOutput struct {
    Id     string            `json:"Id"`
    Names  []string          `json:"Names"`
    Status string            `json:"Status"`
    Image  string            `json:"Image"`
    Labels map[string]string `json:"Labels"`
}
```

Parsing is simpler than Docker's since Labels are already a map (no CSV splitting needed) and Names are already an array.

#### Rootless Mode & UID/GID Handling
Scion's existing UID/GID synchronization mechanism in `sciontool init` (`cmd/sciontool/commands/init.go`) handles bind mount permissions as follows:

1. The host passes `SCION_HOST_UID` and `SCION_HOST_GID` as environment variables (set in `pkg/runtime/common.go`).
2. The container starts as root via the `sciontool init` entrypoint.
3. `setupHostUser()` uses `usermod`/`groupmod` to adjust the `scion` user's UID/GID to match the host user.
4. The supervisor drops privileges to the adjusted `scion` user via `syscall.Credential`.

**Rootful Podman**: This flow works identically to Docker — the container starts as real root and `sciontool init` can freely modify users and drop privileges.

**Rootless Podman**: This is where the key difference lies. In rootless mode:
- The container runs inside a user namespace where the invoking user is mapped to UID 0 (root) inside the container.
- `usermod`/`groupmod` operate on `/etc/passwd` and `/etc/group` inside the container's filesystem layer, which still works within the user namespace.
- Bind-mounted files appear owned by the mapped UID. By default, the host user's UID maps to root (UID 0) inside the container.
- The `--userns=keep-id` flag can map the host user's UID to the same UID inside the container instead of to root, but this conflicts with Scion's "start as root, then drop privileges" model.

**Recommended approach**: For rootless Podman, use the default user namespace mapping (host UID → container UID 0) and let `sciontool init` handle the remapping as it does today. The `SCION_HOST_UID`/`SCION_HOST_GID` values passed will be the host user's actual UID/GID. Since `usermod`/`groupmod` modify the in-container user database (which is writable in the overlay), this works even in a user namespace. The bind-mounted files will be accessible because rootless Podman maps the host user to container root, and `sciontool init` runs as that mapped root.

> **Open Question**: Should `PodmanRuntime` detect rootless mode (via `podman info`) and log a diagnostic message or warning if an unusual configuration is detected? This could help users troubleshoot permission issues without adding runtime complexity.

#### Podman Machine (macOS/Windows)
On macOS and Windows, Podman runs inside a Linux virtual machine managed by `podman machine`. The `podman` CLI communicates with the VM.

Key considerations:
- **Volume mounts**: The host path must be accessible inside the VM. Podman Machine exposes the user's home directory (and configurable additional paths) via virtiofs. Paths outside these mounts will silently fail or error.
- **`GetWorkspacePath()`**: Returns the host path to the workspace. Since Scion worktrees are created relative to the project directory (typically under `$HOME`), they should be within the default VM mount. However, this should be validated.
- **Socket path**: On macOS, the Podman socket is typically at `$XDG_RUNTIME_DIR/podman/podman.sock` or managed by `podman machine`. The `Host` field on `PodmanRuntime` can point to a custom socket if needed.

> **Open Question**: Should Scion validate that the workspace path is within a Podman Machine-accessible mount on macOS, and provide a clear error if not?

### 4. Integration Plan

#### Factory Registration
Update `pkg/runtime/factory.go` to register "podman" in the runtime type switch:

```go
case "podman":
    pr := NewPodmanRuntime()
    if rtConfig.Host != "" {
        pr.Host = rtConfig.Host
    }
    return pr
```

Also add "podman" to the known-type fallback check (the condition around line 48 that recognizes bare type names like "docker", "kubernetes", etc.).

#### Auto-Detection
Update the "auto"/"local" detection logic in `GetRuntime()`. On Linux, if `podman` is found on `$PATH` but `docker` is not, prefer Podman. If both are available, prefer Docker for backward compatibility (or make this configurable).

> **Open Question**: When both `docker` and `podman` are available, which should "auto" prefer? Docker is the safer default for backward compatibility, but on RHEL/Fedora systems where Podman is the native tool, users may expect Podman. Options:
> 1. Always prefer Docker when available (simplest, most compatible).
> 2. Prefer Podman on systems where it is the native package manager default.
> 3. Require explicit configuration; never auto-select Podman.

#### Default Settings
Add a `podman` entry to the embedded default settings at `pkg/config/embeds/default_settings.yaml` for discoverability:

```yaml
runtimes:
  docker:
    type: docker
    host: ""
  podman:
    type: podman
    host: ""
  container:
    type: container
    tmux: true
  kubernetes:
    type: kubernetes
    context: ""
    namespace: ""
```

Users can then reference it from a profile:
```yaml
profiles:
  local:
    runtime: podman
```

The OS-specific default adjustment logic in `pkg/config/koanf.go` (`GetDefaultSettingsDataYAML()`) and `pkg/config/init.go` (`GetDefaultSettingsData()`) should remain as-is — the `local` profile defaults to `docker` on Linux and `container` on macOS. Users who want Podman configure it explicitly.

#### API Updates
Add `"podman"` to any runtime type enumerations or documentation in `pkg/api/types.go`.

### 5. Method-by-Method Implementation Notes

| Method | Approach |
|---|---|
| `Name()` | Return `"podman"` |
| `Run()` | Same as Docker: call `buildCommonRunArgs()`, prepend `podman run -t`, add resource flags, execute. |
| `Stop()` | `podman stop <id>` — identical to Docker. |
| `Delete()` | `podman rm -f <id>` — identical to Docker. Podman supports `-f` unlike Apple container. |
| `List()` | Custom parser for Podman's JSON array format (see section 3). |
| `GetLogs()` | `podman logs <id>` — identical to Docker. |
| `Attach()` | Same logic as Docker: find container, check status, use `podman attach` or `podman exec -it` for tmux. |
| `ImageExists()` | `podman image inspect <image>` — identical to Docker. |
| `PullImage()` | `podman pull <image>` — identical to Docker. |
| `Sync()` | Same as Docker: bind mounts are automatic, GCS sync handled by existing `gcp` package. |
| `Exec()` | `podman exec <id> <cmd>` — identical to Docker. |
| `GetWorkspacePath()` | `podman inspect --format '{{json .Mounts}}'` — identical to Docker. |

### 6. Shared Code Consideration

Given that most methods are identical to Docker, consider one of these approaches:

**Option A: Independent implementation** — Copy the `DockerRuntime` structure, change the command to "podman", and implement `List()` differently. Simple, no coupling, easy to diverge later.

**Option B: Embedded struct with override** — Create a shared base or embed `DockerRuntime` in `PodmanRuntime`, overriding only `List()` and `Name()`. Reduces duplication but creates coupling.

> **Open Question**: Which approach is preferred? Option A is recommended given the project's alpha status — the duplication is minimal (most logic is in `buildCommonRunArgs()` already) and avoids premature abstraction.

## Testing Strategy

### Unit Tests
Create `pkg/runtime/podman_test.go` with:
- **JSON parsing tests**: Verify `List()` correctly parses Podman's JSON array format, including edge cases (empty array, containers with no labels, containers with multiple names).
- **Argument construction tests**: Verify `Run()` produces correct `podman run` arguments using a mockable command runner (same pattern as Docker tests).
- **Label filtering tests**: Ensure label-based filtering works with Podman's native map format.

### Integration Tests
- Verify end-to-end lifecycle: create, start, attach, exec, logs, stop, delete.
- Verify bind mount permissions work correctly (UID/GID sync via `sciontool init`).
- Test with both rootful and rootless Podman.

### Cross-Platform
- **Linux (native rootful)**: Standard Podman installation.
- **Linux (native rootless)**: Rootless Podman with user namespace — validate UID/GID handling.
- **macOS (Podman Machine)**: Verify volume mounts pass through the VM correctly and `GetWorkspacePath()` returns usable host paths.

### CI Considerations
If CI runs in a container or restricted environment, rootless Podman may not be available (requires `newuidmap`/`newgidmap` and `/etc/subuid`/`/etc/subgid` configuration). Tests may need to be conditional or run in a dedicated environment.

## Open Questions Summary

1. **Auto-detection priority**: When both Docker and Podman are on `$PATH`, which should "auto" select? (Recommendation: Docker for backward compatibility.)
2. **Rootless diagnostics**: Should `PodmanRuntime` detect rootless mode and surface diagnostic info for troubleshooting?
3. **Podman Machine mount validation**: Should Scion check that workspace paths are within Podman Machine's virtiofs mounts on macOS?
4. **Code structure**: Independent implementation vs. shared base with Docker? (Recommendation: Independent, per Option A.)
5. **`podman compose` / pods**: Should the design account for future multi-container agent scenarios using Podman pods? (Not needed now, but worth noting for extensibility.)
