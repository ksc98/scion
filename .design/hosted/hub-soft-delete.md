# Hub Agent Soft Delete

**Status:** Proposed
**Updated:** 2026-02-22

## 1. Overview

This document specifies the design for soft-deleting agents in the Scion Hub. Currently, `DELETE /api/v1/agents/{id}` performs an immediate hard delete—removing the agent record from the database and dispatching container/filesystem cleanup to the runtime broker. There is no recovery path.

Soft delete introduces a grace period between the delete request and permanent purging. During this grace period, agents are marked as `deleted` but remain in the database, allowing recovery from accidental deletions and forensic investigation of agent history.

### 1.1 Goals

- **Recoverable deletion**: Deleted agents enter a `deleted` state with a configurable retention period before permanent purge.
- **Zero-disruption default**: The default retention is `0` (immediate purge), preserving current behavior for users who don't opt in.
- **Human-friendly configuration**: Retention duration is specified as a human-readable string (e.g., `72h`, `168h` for 7 days).
- **Invisible by default**: Soft-deleted agents are excluded from standard list views unless explicitly requested.
- **Automatic purging**: The Hub server runs a periodic background loop to purge expired soft-deleted agents.
- **Undelete support**: Agents in the `deleted` state can be restored before the retention period expires.

### 1.2 Non-Goals (This Iteration)

- Per-grove or per-agent retention overrides (retention is a single hub-wide setting).
- Retention of runtime artifacts (containers, worktrees). These are cleaned up at delete time as today; only the Hub database record is retained.
- Archival to external storage (e.g., exporting deleted agent records to GCS).
- UI/web frontend changes (CLI and API only in this iteration).

---

## 2. Configuration

### 2.1 Hub Server Setting

Soft-delete retention is configured in the Hub server configuration under a new field on `HubServerConfig`:

```go
// In pkg/config/hub_config.go

type HubServerConfig struct {
    // ... existing fields ...

    // SoftDeleteRetention is the duration to retain deleted agent records
    // before permanent purge. Specified as a Go duration string (e.g., "72h",
    // "168h", "720h"). A value of "0" or "" means immediate deletion (no
    // soft delete). Default: "0".
    SoftDeleteRetention time.Duration `json:"softDeleteRetention" yaml:"softDeleteRetention" koanf:"softDeleteRetention"`
}
```

### 2.2 V1 Settings Format

In the versioned settings file (`settings.yaml`), this maps to:

```yaml
server:
  hub:
    soft_delete_retention: "168h"  # 7 days
```

The `V1ServerHubConfig` struct gains:

```go
// In pkg/config/settings_v1.go

type V1ServerHubConfig struct {
    // ... existing fields ...

    // SoftDeleteRetention is the retention period for soft-deleted agents.
    SoftDeleteRetention string `json:"soft_delete_retention,omitempty" yaml:"soft_delete_retention,omitempty" koanf:"soft_delete_retention"`
}
```

### 2.3 Environment Variable Override

The retention can also be set via environment variable:

```
SCION_SERVER_HUB_SOFT_DELETE_RETENTION=168h
```

### 2.4 ServerConfig Plumbing

The parsed duration is passed through to `hub.ServerConfig` so the Hub server has access at runtime:

```go
// In pkg/hub/server.go

type ServerConfig struct {
    // ... existing fields ...

    // SoftDeleteRetention is the retention period for soft-deleted agents.
    // Zero means immediate hard delete (default behavior).
    SoftDeleteRetention time.Duration
}
```

---

## 3. Agent Status: `deleted`

### 3.1 New Status Constant

A new agent status constant is added to the existing set:

```go
// In pkg/store/models.go

const (
    // ... existing statuses ...
    AgentStatusDeleted = "deleted"
)
```

### 3.2 New Timestamp Field

The `Agent` model gains a `DeletedAt` field to track when the agent was soft-deleted:

```go
// In pkg/store/models.go

type Agent struct {
    // ... existing fields ...

    // DeletedAt is the timestamp when the agent was soft-deleted.
    // Zero value means the agent has not been deleted.
    DeletedAt time.Time `json:"deletedAt,omitempty"`
}
```

This field is also added to `api.AgentInfo`:

```go
// In pkg/api/types.go

type AgentInfo struct {
    // ... existing fields ...

    // DeletedAt is the timestamp when the agent was soft-deleted (zero if not deleted).
    DeletedAt time.Time `json:"deletedAt,omitempty"`
}
```

The `ToAPI()` conversion in `store/models.go` maps `DeletedAt` accordingly.

### 3.3 Database Schema

The agents table requires:
- A new `deleted_at` column (`TIMESTAMP`, nullable, default `NULL`).
- The existing `status` column already supports arbitrary string values, so `"deleted"` requires no schema change.

For SQLite (the current store implementation), a migration adds:

```sql
ALTER TABLE agents ADD COLUMN deleted_at TIMESTAMP;
CREATE INDEX idx_agents_deleted_at ON agents(deleted_at) WHERE deleted_at IS NOT NULL;
```

---

## 4. Deletion Flow

### 4.1 Delete Handler Changes

The `deleteAgent` handler in `pkg/hub/handlers.go` changes behavior based on the configured retention:

```
DELETE /api/v1/agents/{id}?deleteFiles=true&removeBranch=true
```

**When retention is 0 (default):** Behavior is unchanged—immediate hard delete with runtime dispatch.

**When retention > 0:**

1. Verify broker availability (unchanged).
2. Dispatch container/filesystem cleanup to the runtime broker (unchanged—runtime artifacts are always cleaned up immediately).
3. Instead of `store.DeleteAgent(id)`, call `store.UpdateAgent()` to set:
   - `Status` → `"deleted"`
   - `DeletedAt` → `time.Now()`
4. Publish `AgentDeleted` event (unchanged—the agent is effectively gone from the user's perspective).
5. Return `204 No Content` (unchanged).

```go
func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request, id string) {
    // ... existing broker check and dispatch logic ...

    if s.config.SoftDeleteRetention > 0 {
        // Soft delete: mark as deleted, retain record
        agent.Status = store.AgentStatusDeleted
        agent.DeletedAt = time.Now()
        if err := s.store.UpdateAgent(ctx, agent); err != nil {
            writeErrorFromErr(w, err, "")
            return
        }
    } else {
        // Hard delete: remove record immediately (current behavior)
        if err := s.store.DeleteAgent(ctx, id); err != nil {
            writeErrorFromErr(w, err, "")
            return
        }
    }

    s.events.PublishAgentDeleted(ctx, agent.ID, agent.GroveID)
    w.WriteHeader(http.StatusNoContent)
}
```

### 4.2 Force Delete

A `force=true` query parameter bypasses soft delete and performs immediate hard deletion regardless of the retention setting:

```
DELETE /api/v1/agents/{id}?force=true
```

This is useful for operators who want to immediately purge a specific agent.

---

## 5. Listing and Filtering

### 5.1 AgentFilter Changes

The `AgentFilter` struct gains a field to control inclusion of deleted agents:

```go
// In pkg/store/store.go

type AgentFilter struct {
    GroveID         string
    RuntimeBrokerID string
    Status          string
    OwnerID         string
    IncludeDeleted  bool   // When false (default), exclude agents with status "deleted"
}
```

### 5.2 Store Implementation

The `ListAgents` query adds a default exclusion:

```sql
-- When IncludeDeleted is false (default):
WHERE status != 'deleted'
-- Combined with any other status filter
```

When `Status` is explicitly set to `"deleted"`, only deleted agents are returned (overrides `IncludeDeleted`).

When `IncludeDeleted` is `true`, all agents are returned regardless of deleted status.

### 5.3 API Query Parameter

The list agents endpoint accepts a new query parameter:

```
GET /api/v1/agents?includeDeleted=true
```

Handler change in `listAgents`:

```go
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
    query := r.URL.Query()

    filter := store.AgentFilter{
        GroveID:         query.Get("groveId"),
        RuntimeBrokerID: query.Get("runtimeBrokerId"),
        Status:          query.Get("status"),
        IncludeDeleted:  query.Get("includeDeleted") == "true",
    }
    // ...
}
```

### 5.4 CLI List Command

The `scion list` command gains a `--deleted` flag that sets `includeDeleted=true` (or `status=deleted` to show only deleted agents). By default, deleted agents are hidden.

---

## 6. Undelete (Restore)

### 6.1 API Endpoint

A new action restores a soft-deleted agent:

```
POST /api/v1/agents/{id}/restore
```

This action:
1. Verifies the agent exists and is in `deleted` status.
2. Sets `Status` back to `stopped` (the agent's runtime artifacts are gone, so it cannot be `running`).
3. Clears `DeletedAt` to zero value.
4. Publishes an `AgentCreated` event to notify subscribers.
5. Returns `200 OK` with the restored agent record.

```go
func (s *Server) restoreAgent(w http.ResponseWriter, r *http.Request, id string) {
    ctx := r.Context()

    agent, err := s.store.GetAgent(ctx, id)
    if err != nil {
        writeErrorFromErr(w, err, "")
        return
    }

    if agent.Status != store.AgentStatusDeleted {
        BadRequest(w, "Agent is not in deleted state")
        return
    }

    agent.Status = store.AgentStatusStopped
    agent.DeletedAt = time.Time{}
    if err := s.store.UpdateAgent(ctx, agent); err != nil {
        writeErrorFromErr(w, err, "")
        return
    }

    s.events.PublishAgentCreated(ctx, agent)
    writeJSON(w, http.StatusOK, agent)
}
```

### 6.2 CLI Command

```
scion restore <agent-name-or-id>
```

Calls the restore endpoint. Requires the agent to be in `deleted` status.

---

## 7. Automatic Purge Loop

### 7.1 Background Goroutine

When `SoftDeleteRetention > 0`, the Hub server starts a background goroutine during `Start()` that periodically purges expired soft-deleted agents:

```go
func (s *Server) startPurgeLoop(ctx context.Context) {
    if s.config.SoftDeleteRetention <= 0 {
        return
    }

    ticker := time.NewTicker(1 * time.Hour)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                s.purgeExpiredAgents(ctx)
            }
        }
    }()
}
```

### 7.2 Purge Logic

```go
func (s *Server) purgeExpiredAgents(ctx context.Context) {
    cutoff := time.Now().Add(-s.config.SoftDeleteRetention)

    purged, err := s.store.PurgeDeletedAgents(ctx, cutoff)
    if err != nil {
        slog.Error("Failed to purge deleted agents", "error", err)
        return
    }

    if purged > 0 {
        slog.Info("Purged expired soft-deleted agents", "count", purged, "cutoff", cutoff)
    }
}
```

### 7.3 New Store Method

```go
// In pkg/store/store.go

type AgentStore interface {
    // ... existing methods ...

    // PurgeDeletedAgents permanently removes all agents with status "deleted"
    // whose DeletedAt is before the given cutoff time.
    // Returns the number of agents purged.
    PurgeDeletedAgents(ctx context.Context, cutoff time.Time) (int, error)
}
```

SQL implementation:

```sql
DELETE FROM agents WHERE status = 'deleted' AND deleted_at < ?
```

### 7.4 Lifecycle Integration

- `startPurgeLoop` is called from `Server.Start()`.
- The goroutine exits when the server's context is cancelled during `Shutdown()`.
- The purge loop logs each cycle's results at INFO level.

---

## 8. GetAgent Behavior

`GetAgent` and `GetAgentBySlug` continue to return agents in `deleted` status. Callers that need to exclude deleted agents should check the status field. This ensures:

- The restore endpoint can find deleted agents.
- The purge loop can query for expired agents.
- The delete handler's idempotency is preserved (deleting an already-deleted agent is a no-op 204).

---

## 9. Impact on Agent Counts

The `AgentCount` computed field on `Grove` (populated during listing) should exclude `deleted` agents by default. The store query for grove agent counts should filter on `status != 'deleted'`.

---

## 10. Summary of Changes

| Component | File(s) | Change |
|-----------|---------|--------|
| Agent model | `pkg/store/models.go` | Add `AgentStatusDeleted` constant, `DeletedAt` field |
| API types | `pkg/api/types.go` | Add `DeletedAt` field to `AgentInfo` |
| Store interface | `pkg/store/store.go` | Add `IncludeDeleted` to `AgentFilter`, add `PurgeDeletedAgents` method |
| Store implementation | `pkg/store/sqlite.go` (or equivalent) | Implement filter exclusion, purge query, migration |
| Hub config | `pkg/config/hub_config.go` | Add `SoftDeleteRetention` to `HubServerConfig` |
| V1 settings | `pkg/config/settings_v1.go` | Add `SoftDeleteRetention` to `V1ServerHubConfig`, conversion logic |
| Hub server config | `pkg/hub/server.go` | Add `SoftDeleteRetention` to `ServerConfig` |
| Delete handler | `pkg/hub/handlers.go` | Conditional soft vs hard delete, `force` parameter |
| List handler | `pkg/hub/handlers.go` | Pass `includeDeleted` query param to filter |
| Restore handler | `pkg/hub/handlers.go` | New `restore` action on agent |
| Purge loop | `pkg/hub/server.go` | Background goroutine for periodic purge |
| CLI delete | `cmd/delete.go` | No change needed (backend handles soft delete transparently) |
| CLI list | `cmd/list.go` | Add `--deleted` flag |
| CLI restore | `cmd/restore.go` | New command to restore soft-deleted agents |
| Tests | Various `_test.go` | Test soft delete, restore, purge, filter exclusion, force delete |
