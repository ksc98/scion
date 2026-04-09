# SCION — Personal Knowledge Base

Living doc for running self-hosted scion on my home-lab k8s cluster. Accumulated
debugging/setup knowledge. Kept outside upstream intent — do not commit if
publishing to upstream repo.

---

## Infrastructure

| Piece | Where | Notes |
|---|---|---|
| Container registry | `zot.kych.cloud` (192.168.1.200) | No auth. Reverse-proxy fronted. Nodes' containerd has ambient creds, so no `imagePullSecret` needed in k8s manifests. |
| K8s cluster | `kubectl --context homectl` (k3s) | Mixed-arch: `homectl` amd64, `cachy` amd64, `asahi-studio` **arm64** (Asahi on Apple Silicon), `lima-m4m1/2` arm64 (Lima on Apple Silicon), `zima-*` amd64, `white` NotReady. **Any image deployed must be multi-arch or it'll get QEMU-emulated on arm nodes and SIGTRAP.** |
| Ingress | `Gateway` `kgateway-system/https` (kgateway controller, IP 192.168.1.200) | `allowedRoutes.namespaces.from: All` — cross-namespace HTTPRoutes work without `ReferenceGrant`. |
| TLS | `Certificate` `kgateway-system/kych-cloud-wildcard` → secret `kych-cloud-tls` | Wildcard `*.kych.cloud` issued by `cloudflare-issuer` ClusterIssuer (DNS-01). Works with internal-only DNS since Cloudflare API handles challenges. |
| DNS | Internal-only for `*.kych.cloud` | Public internet can't reach these hostnames. OAuth still works because GitHub/Google only redirect the user's browser, they don't connect back-channel. |
| Postgres | CNPG cluster `postgres-cluster` in `cnpg-system` ns | RW endpoint: `postgres-cluster-rw.cnpg-system.svc.cluster.local:5432`. User `scion` + db `scion` exist. **Noted: cluster sometimes slow to report all instances active — was "3/3 not yet active" during hub setup but actually serving queries.** |
| Other `*.kych.cloud` services | authentik (`auth`), coder, grafana, helm-dashboard, home-assistant, infisical, gatus | All use same kgateway + wildcard cert pattern. |

---

## Scion architecture (quick ref)

Four roles, same `scion` binary, different flags:

1. **Hub** — control plane. State store, auth, task dispatch. `scion server start --enable-hub`. Default port 9810 standalone, or mounted on web port 8080 in combo mode.
2. **Runtime Broker** — executes agents. Registers with a hub. `scion server start --enable-runtime-broker`. Default port 9800. In **combo mode** runs in the same process as the hub.
3. **Agent** — running LLM instance (claude/gemini/codex/opencode) inside a pod/container. Authenticated via one-time JWT from hub.
4. **CLI** — `scion` on a user's machine. Talks to hub via `hub.endpoint` setting. Can also run fully local (no hub).

Agents run inside containers built from the image hierarchy:
```
core-base          (debian slim + Go + Node + Bun + Python + chromium + gcloud + gcsfuse + git-from-source + all unix tools)
 └── scion-base    (+ sciontool + scion binaries, + scion user, + sciontool init entrypoint)
       ├── claude    (+ @anthropic-ai/claude-code, + zsh-in-docker, + git-delta, + init-firewall.sh)
       ├── gemini
       ├── codex
       └── opencode
```

core-base is the slow one — compiles git from source. scion-base recompiles on every scion code change. harness layers are thin.

---

## Image build

### Local multi-arch build script

`/tmp/scion-build.sh` — builds core-base → scion-base → scion-claude for
`linux/amd64,linux/arm64` and pushes to zot. Uses `--push` directly (not
`--load` then `docker push`) because buildkit can't load a multi-arch
manifest list into the local docker daemon.

```
/tmp/scion-build.sh
```

Runs in a tmux window named `scion-build`. Watch with `Ctrl+b <N>`.

### Buildx builder gotcha

Buildkit runs in a container. By default its network can't route to
`zot.kych.cloud` (192.168.1.200) because the buildkit container isn't on
the host network. **Fix:**

```
docker buildx create --name scion-builder \
  --driver docker-container \
  --driver-opt network=host \
  --use
```

### Arm64 via QEMU

Register the qemu binfmt handler so buildx can build arm64 from this amd64
host:

```
docker run --privileged --rm tonistiigi/binfmt --install arm64
```

Then recreate the buildx builder (it only picks up new binfmt at creation time).

**Performance:** core-base's git-from-source compile under QEMU arm64 is the
bottleneck — ~15-25 minutes. Everything else (apt, Go, Node) is much faster.

### Zot quirks we hit

- **Clustering bugs** — zot was originally running 3 replicas with hash-based
  sharding. `core-base` was hashing to `zot-1` / `zot-2` which were crashlooping
  (macOS file-descriptor limit on lima-m4m1 for zot-1; zot-2 stuck Terminating).
  Fix: scale to 1 replica, remove cluster config block. To restore HA later,
  fix the `maxfiles` limit on lima-m4m1 (`sudo launchctl limit maxfiles 200000 200000`).
- **504 / 503** from the reverse proxy in front of zot on large blob HEAD
  requests (chromium layer). Fixed after zot scale-down.
- **No auth** — zot is open on the LAN. Old `~/.docker/config.json` had
  stale basic-auth creds that don't actually matter. Pulls work unauth.

---

## Scion config system

### Settings hierarchy

Loaded via koanf (deep merge) in this priority order (later overrides earlier):

1. Embedded defaults (`pkg/config/embeds/default_settings.yaml`)
2. **Global** settings — `~/.scion/settings.yaml`
3. **Grove** settings — `<project>/.scion/settings.yaml`
4. Env vars (`SCION_*` prefix)

`scion config list` (no `--global`) shows the grove-resolved effective
settings. `scion config list --global` shows only global.

### Git groves use split storage

When the grove is a git repo, scion writes a `grove-id` file in the
in-repo `.scion/` dir and stores the **actual settings** externally at:

```
~/.scion/grove-configs/<slug>__<grove-id-short>/.scion/settings.yaml
```

**Gotcha:** editing `<repo>/.scion/settings.yaml` does nothing for split-storage
groves. Use `scion config set <key> <value>` (without `--global`) to write to
the external file, or edit that file directly.

Agent home dirs and workspaces also live under this external path:
`~/.scion/grove-configs/scion__0a40ffd1/.scion/agents/<name>/home/` (not inside
the repo).

### Profile vs runtime vs runtime-type

These are three different things that collide by name:

- **Runtime type** — the kind of execution backend. Valid values per v1 schema: `docker`, `podman`, `container`, `kubernetes`. Enum at `pkg/config/schemas/settings-v1.schema.json:207`.
- **Runtime** (entry in `runtimes:` map) — a named instance of a runtime type. The map key is used as the type by the legacy-to-v1 migrator unless `type:` is set explicitly. **Gotcha:** my original `runtimes.k8s:` broke v1 migration because `k8s` isn't in the enum. Renamed to `runtimes.kubernetes:`.
- **Profile** (entry in `profiles:` map) — binds a name to a runtime. Has extra fields like `env`, `resources`, `harness_overrides`. `active_profile: <name>` picks which profile is active.

### Saved runtime vs saved profile

`pkg/agent/provision.go` has both `GetSavedRuntime` and `GetSavedProfile`. They
read from `agent-info.json` but return different fields:

- `GetSavedProfile` → `info.Profile` (e.g., `"default"`)
- `GetSavedRuntime` → `info.Runtime` (e.g., `"kubernetes"`)

`runtime.GetRuntime(grovePath, profileName)` expects a **profile name**. If
you pass a runtime type (like `"kubernetes"`) it falls back to the legacy
behavior at `pkg/runtime/factory.go:48` which loses the runtime config —
the resulting k8s client uses the current kubectl context and `default`
namespace, **not** the configured `homectl` / `scion-agents`.

**Bug I fixed:** `cmd/logs.go`, `cmd/look.go`, `cmd/delete.go`, `cmd/attach.go`
were all calling `GetSavedRuntime` and passing the result as a profile name.
Fixed all four to try `GetSavedProfile` first, fall back to `GetSavedRuntime`
for legacy agents.

---

## Bugs found and fixed in scion code

### 1. k8s runtime List injects grove_path into label selector

**Location:** `pkg/runtime/k8s_runtime.go` `KubernetesRuntime.List` (line 1536).

**Bug:** Callers (`cmd/list.go`, `cmd/stop.go`, `cmd/delete.go`, `cmd/message.go`)
inject `scion.grove_path: <absolute path>` into the filter map. The k8s
runtime naively converts every filter key into a label selector `k=v`. k8s
label values can't contain `/`, so this fails at parse time with:

```
values[0][scion.grove_path]: Invalid value: "/home/kchang/dev/scion/.scion":
  a valid label must be ... alphanumeric characters, '-', '_' or '.'
```

**Root cause:** grove_path is stored as a pod **annotation** (correctly), but
the query path treats it as a label. Two layers disagreed.

**Fix:** Extract `scion.grove_path` from the filter before building the
selector, and do in-memory filtering on `pod.Annotations["scion.grove_path"]`
after fetching. Label fallback preserved for legacy pods.

### 2. CLI commands use wrong "saved" lookup

See "Profile vs runtime vs runtime-type" above. Fixed in `cmd/logs.go`,
`cmd/look.go`, `cmd/delete.go`, `cmd/attach.go`.

### 3. `cmd/logs.go` used wrong log path for split-storage groves

**Bug:** built the log path from `a.GrovePath` (the in-repo `.scion/`) +
`agents/<name>/home/agent.log`. For git groves with split storage, the real
agent home lives at `~/.scion/grove-configs/<slug>/.scion/agents/<name>/home/`.

**Fix:** use `config.GetAgentHomePath(a.GrovePath, agentName)` which handles
split storage correctly.

### 4. `scion server start` wrongly required image_registry

**Location:** `cmd/root.go` PersistentPreRunE, exemption list (~line 86).

**Bug:** The hub pod crashloop'd at startup with `image_registry is not
configured`. Root cause: the exemption list only matched `cmdName == "server"`,
but for `scion server start` cobra sets `cmdName == "start"` and
`parentName == "server"`. No exemption matched, so `RequireImageRegistry` ran
and failed (pod has no settings file → empty resolved settings).

**Why the env-var workaround also didn't work:** previous doc revisions
suggested setting `SCION_IMAGE_REGISTRY=zot.kych.cloud` on the pod. That's a
dead end — the hub pod has no `settings.yaml` anywhere, so
`LoadEffectiveSettings` takes the **legacy** `LoadSettingsKoanf` path. The
legacy `Settings` struct has no `ImageRegistry` field (only `VersionedSettings`
does at `pkg/config/settings_v1.go:235`), so the env var is read by koanf and
silently dropped during unmarshal. `SCION_IMAGE_REGISTRY` only works in
contexts where a versioned settings file is already on disk.

**Fix:** added `parentName == "server"` to the `requiresGrove = false` branch
in `cmd/root.go`. Since `requiresRegistry := requiresGrove` a few lines down,
this exempts both the grove-required check and the registry check for all
`scion server <anything>` invocations. Server subcommands run the hub/broker
themselves — they never spawn agent containers, so they have no reason to
care about `image_registry`.

### 5. `scion server start` in a pod fails on `DetectLocalRuntime`

**Location:** `cmd/server_foreground.go:86` → `config.InitGlobal` → `InitMachine`
at `pkg/config/init.go:550` → `DetectLocalRuntime` at `pkg/config/runtime_detect.go`.

**Bug:** When the hub pod has no pre-existing `~/.scion/` directory,
`server_foreground.go` calls `InitGlobal` which calls `DetectLocalRuntime`.
That probes for `podman`/`docker`/`container` binaries on PATH — none of
which exist in the scion-base image. Fails with
`no supported container runtime found: install podman or docker`.

**Observation — not a code bug per se, but a bad interaction:** the init
path is designed for interactive user setup on a workstation and doesn't
know about "I'm a hub running in a pod and I'll never spawn local containers."

**Workaround:** pre-seed `~/.scion/settings.yaml` via a ConfigMap +
initContainer + emptyDir so `os.Stat(globalDir)` returns non-error and
`InitGlobal` is skipped entirely. See "Hub deployment" below.

**Long-term fix idea (not yet done):** make `InitMachine` tolerate a
`DetectLocalRuntime` failure when running in `--production` / non-interactive
mode, or add a "server context" flag that skips local runtime seeding
altogether. Filed mentally, not yet implemented.

### 6. Postgres is advertised in settings schema but not implemented

**Location:** `cmd/server_foreground.go:560` `initStore` switch statement.

**Bug:** `pkg/config/hub_config.go:130` has `Driver string` with a comment
`// sqlite, postgres`, and the JSON schema accepts both. But `initStore`
has only a `case "sqlite":` arm — everything else falls into
`default: return nil, fmt.Errorf("unsupported database driver: %s", ...)`.

There IS an `OpenPostgres` function at `pkg/ent/entc/client.go:56`, but
nothing in `cmd/server_foreground.go` calls it. Postgres is a hanging
wire: ent adapter exists, config schema advertises it, but the store
initialization path only knows sqlite. Older doc revisions of this file
assumed postgres worked — that was wrong.

**Implication for hub deployment:** can't use CNPG. Must either use
sqlite-on-PVC or plumb the postgres driver through `initStore`. Sqlite
on a PVC is the path of least resistance for now.

---

### 7. `hub.grove_id` (v1 snake_case) never reaches `Hub.GroveID` (legacy camelCase)

**Location:** `pkg/config/koanf.go` `LoadSettingsKoanf` normalize step.

**Bug:** v1 settings store the hub-side grove ID as snake_case
`hub.grove_id`, but the legacy `HubClientConfig.GroveID` field uses
koanf tag `groveId` (camelCase). The original normalize remap copied
`hub.grove_id` to top-level `grove_id` only — and then `ReadGroveID`'s
marker-file override clobbered top-level `grove_id` with the local UUID
v5, leaving `Hub.GroveID` empty via the camelCase mismatch.

**Symptom:** `GetHubGroveID()` returned `""`, `CompareAgents` fell back
to `Settings.GroveID` (= the local UUID), and the hub returned
`404 Grove not found` because it indexes by its own assigned ID, not
the deterministic v5. The CLI printed `Linked to existing grove (ID:
346565df-...)` from `registerGrove` and then immediately `failed to
list Hub agents: not_found` from the next call — *the displayed ID and
the dispatched ID were different*.

**Fix:** fan `hub.grove_id` out to BOTH `grove_id` (preserves existing
behavior) AND `hub.groveId` (so the legacy `Hub.GroveID` field actually
gets populated). Factored into shared `normalizeV1HubKeys()` so
`LoadSettingsFromDir` applies it too — previously isolated grove reads
silently dropped the hub ID for the same reason.

**Test gotcha while debugging this:** `LoadSettingsKoanf` redirects
settings.yaml lookups via `GetGroveConfigDir` to
`~/.scion/grove-configs/<name>__<id>/.scion/`, so a regression test
that writes settings.yaml to the *passed* grove path silently never
loads it. Write to the redirected `GetGroveConfigDir(grovePath)` path
instead. See `TestLoadSettingsKoanfV1HubGroveIDSurvivesGitGroveMarker`.

---

### 8. Env-type secrets dropped in dispatcher merge when `Target == ""`

**Location:** `pkg/hub/httpdispatcher.go` `buildCreateRequest`,
environment-type secret merge into `req.ResolvedEnv`.

**Bug:** the merge filtered on `s.Target != ""`. Secrets created via
the default `scion hub secret set FOO bar` form land with `Name=FOO`,
`Target=""` — so every CLI-created secret got skipped. The broker
never received `CLAUDE_CODE_OAUTH_TOKEN` even when it was sitting in
the hub secret store; auto-detection then defaulted to `api-key` and
agent registration failed with `ANTHROPIC_API_KEY missing`.

**Fix:** fall back to `Name` when `Target` is empty. Also added
defense-in-depth in `pkg/runtimebroker/handlers.go`
`extractRequiredEnvKeys` so the broker's auto-detect step also merges
env-type secrets before calling `DetectAuthTypeFromEnvVars`.

---

### 9. `gitCloneWorkspace` "dubious ownership" when sciontool runs as root

**Location:** `cmd/sciontool/commands/init.go` `gitCloneWorkspace`.

**Bug:** the chown to fix `/workspace` ownership was gated on `uid > 0`.
In some k8s setups the scion user gets remapped to UID=0 (no host UID
to preserve), so the chown was skipped — but the workspace bind mount
was created by the kubelet with a different SecurityContext UID. Git
running as root then refused to operate on the foreign-owned dir with
`fatal: detected dubious ownership in repository at '/workspace'` and
the agent failed before the harness even started.

**Fix:** chown unconditionally (uid==0 still safe — chown to root just
re-asserts ownership), and pass `-c safe.directory=*` to every `git`
command in the clone path as belt-and-suspenders. The `-c` is in-memory
so it doesn't persist anything outside this single process tree.

---

### 10. `SCION_SERVER_HUB_ENDPOINT` confused for in-pod broker dial

**Location:** `pkg/hub/server.go:1319` `dispatcher.SetHubEndpoint`,
plus the hub deployment manifest at `/tmp/scion-hub.yaml`.

**Bug:** the manifest set `SCION_SERVER_HUB_ENDPOINT=http://localhost:8080`
on the hub container under the assumption that it was the broker's
"where to dial the hub" address. It is *not* — that's
`SCION_SERVER_BROKER_HUB_ENDPOINT`. `SCION_SERVER_HUB_ENDPOINT` flows
through `s.config.HubEndpoint` → `dispatcher.SetHubEndpoint()` →
gets injected into agent containers as `SCION_HUB_ENDPOINT`. Agents
then dial `localhost:8080` *inside their own pod network namespace*
and get `connection refused`, which silently masks the *real* startup
error in the agent (the hub status update is the only place it could
have surfaced).

**Two env vars, two meanings:**
| Var | Used by | Should point to |
|---|---|---|
| `SCION_SERVER_BROKER_HUB_ENDPOINT` | broker → hub dial *inside the same pod* | `http://localhost:8080` ✓ |
| `SCION_SERVER_HUB_ENDPOINT` | injected into agent containers as `SCION_HUB_ENDPOINT` | reachable from *agent* pods, NOT localhost |

**Fix:** in the manifest, set `SCION_SERVER_HUB_ENDPOINT` to the
in-cluster service URL `http://scion-hub.scion-agents.svc.cluster.local:8080`.
The cluster DNS resolves it without hitting the public hub.kych.cloud
hairpin, and the agent container can actually reach it. Long comment
in `/tmp/scion-hub.yaml` documents the trap.

---

## Hub deployment

### Namespace / naming

Everything in `scion-agents`. Secrets (postgres is NOT supported — see bug #6,
so `scion-hub-db` is gone, sqlite lives on a PVC instead):

- `scion-hub-oauth` — OAuth client credentials for both WEB (browser login)
  and CLI (loopback login) flows:
  - `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID` + `_CLIENTSECRET` (GitHub web flow)
  - `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID` + `_CLIENTSECRET` (Google CLI flow — the
    CLI defaults to the `google` provider at `pkg/hub/handlers_auth.go:758-760`, so
    you need a Google OAuth client in GCP Console even if your web login uses GitHub)
- `scion-hub-session` — `SESSION_SECRET` (random hex, signs cookies)
- ~~`scion-hub-db`~~ — deleted; scion's `initStore` only supports sqlite, DB lives on PVC

Resources in `/tmp/scion-hub.yaml`: `ServiceAccount` + `Role` + `RoleBinding` +
`ConfigMap` + `PersistentVolumeClaim` + `Deployment` + `Service` + `HTTPRoute`.

### Persistent `~/.scion/` via PVC + ConfigMap seed

Required because:
- **Bug #5**: if `~/.scion/` doesn't exist in the pod, `server_foreground.go`
  runs `InitGlobal` → `DetectLocalRuntime` and crashes. Something must exist
  at that path before the main container starts.
- **Persistence**: sqlite (`hub.db`) lives at `/home/scion/.scion/hub.db` per
  default from `pkg/config/hub_config.go:370`. Without a PVC, every pod restart
  wipes users, groves, agent history, scheduled events, github integrations,
  broker HMAC keys. See SCION.md "Is the hub stateless?" — no, it isn't.

Pattern:

1. `PVC/scion-hub-home` — 5Gi on `ceph-block` (explicit, not the default:
   cluster has two default StorageClasses and `local-path` would pin the pod
   to a single node). `ReadWriteOnce` is fine since the Deployment uses
   `Recreate` strategy.
2. `ConfigMap/scion-hub-settings` — carries the minimal seed `settings.yaml`.
3. `initContainer` mounts **both** the PVC (at `/home/scion/.scion`) and the
   ConfigMap (at `/config` read-only), then **copy-if-missing**:
   ```sh
   if [ ! -f /home/scion/.scion/settings.yaml ]; then
     cp /config/settings.yaml /home/scion/.scion/settings.yaml
   fi
   chown -R 1000:1000 /home/scion/.scion
   ```
   Copy-if-missing (not always-copy) so the hub's runtime writes to
   `settings.yaml` persist — notably the `hub.brokerId` written by
   `cmd/server_foreground.go:1188`. To force a re-seed after editing the
   ConfigMap, `kubectl exec` into the pod and delete the file, or recreate
   the PVC.
4. Main container mounts only the PVC at `/home/scion/.scion`.
5. Env var `HOME=/home/scion` set explicitly so scion always resolves
   `~/.scion` to the mount regardless of the runtime UID (the scion base
   image's `useradd` sets home, but being explicit avoids surprises).

ConfigMap contents (note what is **intentionally absent**):

```yaml
schema_version: "1"
active_profile: default
runtimes:
  kubernetes:
    type: kubernetes
    namespace: scion-agents
image_registry: zot.kych.cloud
profiles:
  default:
    runtime: kubernetes
```

- **No `context:` under `runtimes.kubernetes`.** `pkg/runtime/factory.go:115`
  passes `rtConfig.Context` to `k8s.NewClientWithContext`. In the in-cluster
  auth path (PR #62, `pkg/k8s/client.go:90`), the fallback to
  `rest.InClusterConfig()` **only** kicks in when both `kubeconfigPath == ""`
  AND `contextName == ""`. Leave `context: homectl` set and clientcmd will
  try to resolve that context from a nonexistent `~/.kube/config` and die
  with a dual-error message. Drop the field.
- **No `server.broker.broker_id`.** `cmd/server_foreground.go:1176` generates
  a UUID if none is set and persists it via `config.UpdateSetting(globalDir,
  "hub.brokerId", ...)`. With the PVC-backed settings.yaml, the written ID
  is stable across pod restarts — verified: same broker_id survives rolling
  updates.

### Key config via env vars

| Env var | Purpose |
|---|---|
| `SCION_SERVER_HUB_ENDPOINT=http://localhost:8080` | Internal "where does the hub live" resolver. Set to localhost because in combo mode the broker and hub are the same process — no reason to hairpin out through the ingress. **Does not** drive OAuth callback URLs (that's `--base-url`, see Flags). |
| `SCION_SERVER_BROKER_HUB_ENDPOINT=http://localhost:8080` | Broker's explicit "dial the hub" endpoint. Takes priority over the previous var at `cmd/server_foreground.go:1222` (`cfg.RuntimeBroker.HubEndpoint`). Belt-and-suspenders — either one alone would work, both set is fine. |
| `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID/SECRET` | From `scion-hub-oauth` secret. Drives GitHub web login. |
| `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID/SECRET` | From `scion-hub-oauth` secret. Drives CLI loopback login (`scion hub auth login`). The CLI defaults to `google` as the provider, hence the name. |
| `SESSION_SECRET` | Consumed by `--session-secret=$(SESSION_SECRET)` arg expansion. Verified working with `envFrom` via empirical pod test. |
| `HOME=/home/scion` | Pins `~/.scion` to the PVC mount so resolution is stable regardless of container UID. |

**`SCION_IMAGE_REGISTRY` is no longer needed on the pod** — the ConfigMap
settings.yaml sets `image_registry` at the top level, and the cmd/root.go
fix (bug #4) means server start doesn't validate it anyway.

**No database env vars needed either.** `cfg.Database.Driver` defaults to
`sqlite` and `cfg.Database.URL` defaults to `$HOME/.scion/hub.db` per
`pkg/config/hub_config.go:370` — which lands on the PVC.

### The hub dialing itself via `hub.kych.cloud` problem

Symptom: `Control channel connection failed: websocket dial failed: dial tcp:
lookup hub.kych.cloud on 10.43.0.10:53: no such host`.

Cause: cluster CoreDNS (`10.43.0.10`) doesn't know about `*.kych.cloud` —
that's on the LAN resolver only (SCION.md line 17). When
`resolveHubEndpointForBroker` at `cmd/server_foreground.go:1221` falls
through to the public hub endpoint, the broker tries to dial
`https://hub.kych.cloud` from inside the pod and fails.

Fix: set `SCION_SERVER_BROKER_HUB_ENDPOINT=http://localhost:8080`. The
broker dials its own in-process hub, skipping DNS, TLS, and the ingress
hairpin entirely. OAuth callback URLs are driven by the `--base-url` flag,
not this env var, so external browser logins still use `https://hub.kych.cloud`.

Alternatives rejected: `hostAliases → 127.0.0.1` (port/scheme mismatch:
dial is `:443` but pod listens `:8080`), `hostAliases → Service ClusterIP`
(same mismatch), `hostAliases → kgateway IP 192.168.1.200` (works but
hairpins through the ingress for no reason), `hostNetwork: true` (loses
in-cluster SA auth).

### Flags

```
scion --global server start \
  --foreground --production \
  --enable-hub --enable-runtime-broker --enable-web \
  --web-port=8080 \
  --base-url=https://hub.kych.cloud \
  --session-secret=$(SESSION_SECRET) \
  --auto-provide
```

All flag names verified against `cmd/server.go:230-269`.

### Health endpoint

**`/healthz`** only — registered on web mux at `pkg/hub/web.go:592`, exempted
from auth at `pkg/hub/auth.go:289`. `/readyz` is registered on the separate
hub-API mux (`pkg/hub/server.go:1944`) which is not mounted on the web server
in combo mode.

**Weak probe warning:** `/healthz` unconditionally returns 200 (it's a status
aggregator, not a gate). Won't detect DB outage. Fine as a liveness smoke
probe, but don't rely on it for correctness.

### Deploy order

1. Wait for multi-arch build to finish (arm nodes in the cluster otherwise SIGTRAP).
2. Add DNS `hub.kych.cloud → 192.168.1.200` (internal resolver).
3. `kubectl apply -f /tmp/scion-hub.yaml` (creates SA, Role, RoleBinding,
   ConfigMap, PVC, Deployment, Service, HTTPRoute in one shot).
4. Browse to `https://hub.kych.cloud`, log in via GitHub web flow.
5. From your laptop: `scion hub auth login` (uses Google CLI flow — see below).

### RBAC

ServiceAccount `scion-hub` in `scion-agents` with a namespace-scoped `Role`
covering: `pods` (+ `exec`/`attach`/`log` subresources), `secrets`,
`persistentvolumeclaims`, `events`, `configmaps`. Bound via `RoleBinding`
`scion-hub`. The Deployment references this SA via `serviceAccountName:
scion-hub`.

### Known TODOs

- **GitHub OAuth web client secret in chat history** — regenerate at
  github.com/settings/developers after initial setup.
- **Google OAuth CLI client secret in chat history** — regenerate at GCP
  Console → APIs & Services → Credentials after initial setup.
- **imagePullPolicy: Always + :latest** — fine while iterating, swap to a
  versioned tag or digest pin once things stabilize.

---

## Bugs found and fixed — kata runtime / template sync

### 11. Template sync broken for remote hubs with local storage

**Location:** `pkg/transfer/client.go:99` (`uploadToFile`), `cmd/templates.go` (`syncTemplateToHub`).

**Bug:** `scion templates sync` asks the hub for signed upload/download URLs.
`LocalStorage` returns `file://` paths pointing inside the hub pod's filesystem.
The CLI then tries to `os.Create` those paths on the local machine — fails with
`mkdir /home/scion: permission denied` because `/home/scion` doesn't exist locally.

**Root cause:** the signed-URL abstraction only works for cloud storage backends
(GCS/S3) that return real HTTP URLs. `LocalStorage` shoehorned `file://` URLs
into the same interface, which only works when CLI and hub share a filesystem.

**Fix:** the hub already had direct file endpoints at
`POST|PUT|GET|DELETE /api/v1/templates/{id}/files[/{path}]`
(`pkg/hub/template_file_handlers.go`) that call `stor.Upload()`/`stor.Download()`
internally. Added `ListFiles`, `ReadFile`, `WriteFile`, `UploadFiles` methods to
`pkg/hubclient/templates.go` and rewrote `syncTemplateToHub` and
`pullTemplateFromHubMatch` in `cmd/templates.go` to use them instead of signed URLs.

### 12. `store.KubernetesConfig` missing most fields

**Location:** `pkg/store/models.go:438`.

**Bug:** the hub-side `KubernetesConfig` struct only had `Resources` and
`NodeSelector`. `RuntimeClassName`, `ImagePullPolicy`, `ServiceAccountName`,
`Tolerations`, etc. were all missing — so even if the template config was
populated, those fields would be silently dropped on JSON round-trip.

**Fix:** expanded `store.KubernetesConfig` to include all fields from
`api.KubernetesConfig`.

### 13. `populateAgentConfig` never merged template kubernetes config

**Location:** `pkg/hub/handlers.go:7823` (`populateAgentConfig`).

**Bug:** the function merged `Image`, `Model`, `Env`, `Telemetry` from the
resolved template into `AppliedConfig.InlineConfig`, but completely skipped
`Kubernetes`. So `runtimeClassName`, `imagePullPolicy`, etc. from the template
never reached the agent's config → never reached the pod spec.

**Fix:** added kubernetes config merge block in `populateAgentConfig()`, same
pattern as the existing telemetry merge (template defaults, explicit agent
values take precedence).

### 14. Hub never parsed `scion-agent.yaml` into template DB config

**Location:** `pkg/hub/template_file_handlers.go`, `pkg/hub/template_handlers.go`.

**Bug:** when template files were uploaded, they were stored as opaque blobs.
Nobody parsed `scion-agent.yaml` to extract the `kubernetes:` block (or any
other config fields) into the template's `Config` column in the database.
So `resolvedTemplate.Config.Kubernetes` was always nil even though the file
on disk had `runtimeClassName: kata`.

**Fix:** added `updateTemplateConfigFromAgentYAML()` which parses the YAML
and populates `template.Config`. Called from `handleTemplateFileWrite`,
`handleTemplateFileUpload` (multipart), and `handleTemplateFinalize`.

### 15. Hub path didn't use `default_template` from settings

**Location:** `cmd/common.go:590`, `cmd/create.go:166`.

**Bug:** when creating an agent via the hub without `-t <template>`, the CLI
sent an empty template reference. The hub only resolves templates when
`req.Template != ""`. The local provisioning path reads `default_template`
from settings and falls back to `"default"`, but the hub path didn't.

**Fix:** in `startAgentViaHub` and `createAgentViaHub`, read
`vs.DefaultTemplate` from `LoadEffectiveSettings` when `templateName` is
empty.

---

## OAuth app registration (for reference)

The hub uses **two separate** OAuth apps — one for web browser login (GitHub)
and one for CLI loopback login (Google). They correspond to scion's
`OAuthClientTypeWEB` and `OAuthClientTypeCLI` at `pkg/hub/handlers_auth.go`.

The CLI defaults to the `google` provider at `pkg/hub/handlers_auth.go:758-760`
if none is specified, which is why we use Google specifically for CLI —
it's the path of least resistance. You could override with
`scion hub auth login --provider=github`, but that requires a second GitHub
OAuth app because GitHub OAuth apps only allow one callback URL per app and
the CLI needs a loopback URL.

### Web — GitHub OAuth app

Register at github.com/settings/developers:

| Field | Value |
|---|---|
| Application name | `Scion` |
| Homepage URL | `https://hub.kych.cloud` |
| Authorization callback URL | `https://hub.kych.cloud/auth/callback/github` |
| Enable Device Flow | off |

Env vars: `SCION_SERVER_OAUTH_WEB_GITHUB_CLIENTID` + `_CLIENTSECRET` in
the `scion-hub-oauth` secret.

### CLI — Google OAuth client

Register at console.cloud.google.com → APIs & Services → Credentials →
Create OAuth client ID. Either client type works:

- **Web application** client: add `http://127.0.0.1:18271/callback` to
  "Authorized redirect URIs". Google is strict about `127.0.0.1` vs
  `localhost` — they're treated as different origins for OAuth, and the
  CLI binds to `127.0.0.1` specifically.
- **Desktop app** client: loopback redirects are implicitly allowed, no
  redirect URIs to configure.

The CLI loopback port is hardcoded at `pkg/hub/auth/localhost_server.go:31`
(`CallbackPort = 18271`, `CallbackPath = "/callback"`).

Env vars: `SCION_SERVER_OAUTH_CLI_GOOGLE_CLIENTID` + `_CLIENTSECRET` in
the same `scion-hub-oauth` secret. After updating the secret, restart the
hub pod (`kubectl rollout restart deployment/scion-hub -n scion-agents`)
because the env vars come in via `envFrom` and only get re-read on pod
start.

### CLI login flow (what happens when you run `scion hub auth login`)

1. CLI (`cmd/hub_auth.go:runBrowserAuthFlow`) starts a local HTTP server
   on `127.0.0.1:18271`.
2. CLI calls the hub's `/api/v1/auth/cli/authorize` endpoint with
   `callbackUrl=http://127.0.0.1:18271/callback` and a state nonce.
3. Hub (`pkg/hub/handlers_auth.go:740`) uses the CLI Google OAuth client
   to generate a Google authorization URL with that callback URL and
   returns it.
4. CLI opens your browser to the Google URL. You sign in.
5. Google redirects to `http://127.0.0.1:18271/callback?code=...&state=...`
   which hits the CLI's local server.
6. CLI sends the code back to the hub's `/api/v1/auth/cli/exchange`
   endpoint.
7. Hub exchanges the code with Google, receives user info, issues its
   own JWT, and returns it to the CLI.
8. CLI stores the JWT in the local credentials store. Subsequent
   `scion hub ...` commands send it as `Authorization: Bearer`.

---

## Useful commands

```bash
# See effective scion settings for current grove
scion config list

# See effective global settings only
scion config list --global

# Set grove-level config (writes to split-storage location if git grove)
scion config set <key> <value>

# Set global config
scion config set --global <key> <value>

# Diagnose runtime connectivity
scion doctor

# Debug scion with full trace
SCION_DEBUG=1 scion <cmd>

# Check which buildkit platforms are available
docker buildx inspect scion-builder

# Watch build in tmux
tmux select-window -t scion-build

# Verify image exists in zot
curl -s https://zot.kych.cloud/v2/<repo>/tags/list
```

---

## Knowledge gaps / open questions

- Hub+broker combo mode RBAC — still need to write the ServiceAccount/Role/RoleBinding manifests.
- How cluster-internal DNS handles `postgres-cluster-rw.cnpg-system.svc.cluster.local` during cnpg failover — haven't tested.
- Whether the `/readyz` endpoint is actually reachable on port 8080 in combo
  mode (it's registered on the hub mux, not web mux — they may or may not be
  merged at `--enable-web` time). Would need to hit it from inside the pod once
  deployed to confirm.
- Whether scion's runtime-broker in the hub pod will auto-discover the
  in-cluster service account, or needs explicit kubeconfig mounting.
