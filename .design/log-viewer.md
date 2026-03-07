# Agent Log Viewer

## Status
**Complete** — Phase 3 complete (2026-03-07)

## Overview

Add a "Logs" tab to the agent detail page that queries Google Cloud Logging for
structured log entries associated with a specific agent. Logs are loaded
on-demand (not on page load), support manual refresh and live streaming, and
render as expandable structured-JSON rows. A `scion logs` CLI implementation
is also provided for hub-connected agents, including `--follow` support.

## Problem / Current State

- `scion logs` only reads local filesystem logs (`agents/<name>/home/agent.log`).
  Hub mode returns: `"logs command is not yet supported when using Hub integration"`.
- The web agent detail page has no log visibility — operators must SSH into
  brokers or use the GCP Console to view Cloud Logging.
- The hub already writes structured logs to Cloud Logging via `CloudHandler`
  (`pkg/util/logging/cloud_handler.go`) with `agent_id` and `grove_id`
  promoted to Cloud Logging labels, making per-agent filtering efficient.
- Agent-side logs (harness output, tool execution) flow to Cloud Logging via
  the sciontool exporter, written to a `scion-agents` log. These also carry
  the `agent_id` label, enabling unified querying across all log sources.

## Design

### 1. Hub API — Log Query Endpoint

#### 1.1 REST Endpoint (Polling / Refresh)

```
GET /api/v1/agents/{agentId}/logs?tail=100&since=<RFC3339>&until=<RFC3339>&severity=INFO&pageToken=<token>
```

| Param       | Type   | Default | Description                                    |
|-------------|--------|---------|------------------------------------------------|
| `tail`      | int    | 200     | Max entries to return (cap: 1000)              |
| `since`     | string | —       | RFC3339 lower bound (inclusive)                |
| `until`     | string | —       | RFC3339 upper bound (exclusive)                |
| `severity`  | string | —       | Minimum severity filter (DEBUG, INFO, WARNING, ERROR, CRITICAL) |
| `pageToken` | string | —       | Opaque cursor for forward pagination           |

**Response:**

```json
{
  "entries": [
    {
      "timestamp": "2026-03-07T10:15:32.123456Z",
      "severity": "INFO",
      "message": "Agent started processing task",
      "labels": { "agent_id": "abc123", "grove_id": "my-grove", "component": "scion-broker" },
      "resource": { "type": "gce_instance", "labels": { ... } },
      "jsonPayload": { "subsystem": "harness.claude", "duration_ms": 142, ... },
      "insertId": "abc123xyz",
      "sourceLocation": { "file": "pkg/harness/claude.go", "line": "215", "function": "..." }
    }
  ],
  "nextPageToken": "...",
  "hasMore": true
}
```

Entries are returned **newest-first** (descending timestamp).

**Authorization:** Access is gated on grove-level read permissions. Any user
with read access to the grove can view logs for agents in that grove. The hub
itself and hub admins have access to all logs. This is enforced at the hub API
layer.

**Graceful degradation:** If Cloud Logging is not configured (no GCP project
available), the endpoint returns `501 Not Implemented` with a clear message:
`{"error": "Cloud Logging is not configured"}`.

#### 1.2 SSE Endpoint (Streaming)

```
GET /api/v1/agents/{agentId}/logs/stream?severity=INFO
```

Returns `text/event-stream` with:

```
event: log
data: {"timestamp":"...","severity":"INFO","message":"...","jsonPayload":{...}}

event: log
data: {"timestamp":"...","severity":"ERROR","message":"...","jsonPayload":{...}}

:heartbeat
```

The server holds open a Cloud Logging tail session and forwards matching entries
as SSE events. Connection lifecycle:

- Client connects → server starts a `logadmin` tail or polling loop
- Client disconnects (tab switch, browser close) → server cleans up
- Heartbeat every 15 seconds to keep connection alive
- Server-side timeout: 10 minutes (client can reconnect)

### 2. Hub Server Implementation

#### 2.1 Cloud Logging Query Service

New file: `pkg/hub/logquery.go`

```go
type LogQueryService struct {
    client    *logadmin.Client  // from cloud.google.com/go/logging/logadmin
    projectID string
}

type LogEntry struct {
    Timestamp      time.Time              `json:"timestamp"`
    Severity       string                 `json:"severity"`
    Message        string                 `json:"message"`
    Labels         map[string]string      `json:"labels,omitempty"`
    Resource       map[string]interface{} `json:"resource,omitempty"`
    JSONPayload    map[string]interface{} `json:"jsonPayload,omitempty"`
    InsertID       string                 `json:"insertId"`
    SourceLocation *SourceLocation        `json:"sourceLocation,omitempty"`
}

type SourceLocation struct {
    File     string `json:"file,omitempty"`
    Line     string `json:"line,omitempty"`
    Function string `json:"function,omitempty"`
}

type LogQueryOptions struct {
    AgentID   string
    GroveID   string
    Tail      int
    Since     time.Time
    Until     time.Time
    Severity  string
    PageToken string
}

// Query builds a Cloud Logging filter and returns matching entries.
func (s *LogQueryService) Query(ctx context.Context, opts LogQueryOptions) ([]LogEntry, string, error)

// Tail opens a streaming session for new log entries.
func (s *LogQueryService) Tail(ctx context.Context, opts LogQueryOptions) (<-chan LogEntry, func(), error)
```

**Cloud Logging filter construction:**

```
labels.agent_id = "{agentId}"
AND timestamp >= "{since}"
AND timestamp < "{until}"
AND severity >= {severity}
```

The query matches across **all logs** in the project with the `agent_id` label —
it is not scoped to a specific log ID. This ensures both hub-side logs (from
`CloudHandler`) and agent-side logs (from the sciontool exporter's
`scion-agents` log) are returned in a unified view.

The `labels.agent_id` filter leverages the label promotion already done by
`CloudHandler` and `GCPHandler` — this is the most efficient query path.

> **Note:** The `broker_id` should be considered as a candidate for promotion
> to a primary Cloud Logging label. This would allow filtering by broker in
> future iterations and provide clear provenance for entries from multi-broker
> environments.

#### 2.2 Handler Registration

In `pkg/hub/server.go` `registerRoutes()`, add:

```go
s.mux.HandleFunc("/api/v1/agents/{agentId}/logs", s.handleAgentLogs)
s.mux.HandleFunc("/api/v1/agents/{agentId}/logs/stream", s.handleAgentLogsStream)
```

Note: Since the mux uses prefix matching, these need to be handled via the
existing `handleAgentByID` dispatcher (similar to how
`/api/v1/agents/{id}/exec` routes work today) or via explicit path registration.

#### 2.3 Configuration

The `LogQueryService` is initialized only when Cloud Logging is available:

| Env Var                    | Purpose                          | Required |
|----------------------------|----------------------------------|----------|
| `SCION_GCP_PROJECT_ID`     | GCP project for log queries      | Yes (or `GOOGLE_CLOUD_PROJECT`) |
| `SCION_CLOUD_LOGGING`      | Enables Cloud Logging features   | No (log query can work independently) |

Uses Application Default Credentials (ADC), consistent with the existing
`CloudHandler` pattern. The `logadmin.Client` needs the
`logging.viewer` IAM role.

### 3. Web Frontend — Logs Tab

#### 3.1 Tab Addition

In `web/src/components/pages/agent-detail.ts`, add a third tab to the existing
`sl-tab-group`:

```html
<sl-tab slot="nav" panel="logs">Logs</sl-tab>
<sl-tab-panel name="logs">${this.renderLogsTab()}</sl-tab-panel>
```

The Logs tab is **only rendered** when the hub reports that Cloud Logging is
configured. If Cloud Logging is unavailable, the tab is hidden entirely (no
error message or disabled state — the feature simply isn't present).

#### 3.2 Lazy Loading

Logs are **not** fetched on page load. The tab panel renders a placeholder until
activated. On first activation (Shoelace `sl-tab-show` event), a fetch is
triggered. This avoids unnecessary Cloud Logging API calls for users who only
view status/configuration.

```typescript
@state() private logsLoaded = false;
@state() private logsEntries: LogEntry[] = [];
@state() private logsStreaming = false;
@state() private logsLoading = false;

private handleTabShow(e: CustomEvent) {
  if (e.detail.name === 'logs' && !this.logsLoaded) {
    this.fetchLogs();
  }
  if (e.detail.name !== 'logs' && this.logsStreaming) {
    this.stopLogStream();
  }
}
```

#### 3.3 Toolbar

Top-right of the logs tab panel:

```
[ Refresh ]  [ Stream: OFF/ON toggle ]
```

- **Refresh** button: calls `GET /api/v1/agents/{id}/logs` and prepends new
  entries to the buffer (deduped by `insertId`). Disabled when streaming is on.
- **Stream toggle**: `sl-switch` component. When enabled, opens an `EventSource`
  to the SSE endpoint. New entries are prepended to the buffer in real-time.
  Toggle off or tab switch disconnects the stream.

#### 3.4 Log Entry Row — Compact View

Each entry renders as a clickable row showing key fields:

```
┌──────────────────────────────────────────────────────────────────────┐
│ 10:15:32.123  INFO   harness.claude  Agent started processing task  │
│ 10:15:31.456  ERROR  hub.dispatch    Failed to route message: ...   │
│ 10:15:30.789  INFO   broker.agent    Container health check passed  │
└──────────────────────────────────────────────────────────────────────┘
```

Compact row fields:
- **Timestamp** — `HH:mm:ss.SSS` format (date shown as a section divider)
- **Severity** — color-coded badge (DEBUG=gray, INFO=blue, WARNING=amber, ERROR=red, CRITICAL=red-bold)
- **Subsystem** — from `jsonPayload.subsystem` or `labels.component`
- **Message** — truncated to single line

#### 3.5 Log Entry Row — Expanded View (JSON Browser)

Clicking a row expands it into a structured JSON browser with progressive
disclosure:

```
┌──────────────────────────────────────────────────────────────────────┐
│ v 10:15:31.456  ERROR  hub.dispatch  Failed to route message: ...   │
│   ┌──────────────────────────────────────────────────────────────┐   │
│   │ timestamp: "2026-03-07T10:15:31.456789Z"                    │   │
│   │ severity: "ERROR"                                           │   │
│   │ message: "Failed to route message: connection refused"      │   │
│   │ v labels:                                                   │   │
│   │     agent_id: "abc123"                                      │   │
│   │     grove_id: "my-grove"                                    │   │
│   │     component: "scion-hub"                                  │   │
│   │ v jsonPayload:                                              │   │
│   │     subsystem: "hub.dispatch"                               │   │
│   │     error: "connection refused"                             │   │
│   │     v target:                                               │   │
│   │         broker_id: "broker-west-1"                          │   │
│   │         endpoint: "10.0.1.5:8080"                           │   │
│   │ v sourceLocation:                                           │   │
│   │     file: "pkg/hub/dispatch.go"                             │   │
│   │     line: "342"                                             │   │
│   └──────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────┘
```

Implementation approach: a recursive Lit template function that renders
key-value pairs, with objects/arrays collapsed by default and expandable on
click. Each level is indented. Primitive values use syntax coloring (strings in
green, numbers in blue, booleans in purple, null in gray).

This should be implemented as a reusable `<scion-json-browser>` component since
structured data browsing may be useful elsewhere (e.g., notification payloads,
agent metadata).

#### 3.6 Buffer Management

- Entries stored in a `Map<string, LogEntry>` keyed by `insertId` for dedup
- Sorted array derived from the map (descending timestamp) for rendering
- Buffer capped at 2000 entries (oldest evicted on overflow)
- Refresh fetches with `since` set to the newest entry's timestamp, merges results
- Streaming appends prepend to the buffer in real-time

### 4. CLI — `scion logs` Hub Support

#### 4.1 Basic Implementation

Update `cmd/logs.go` to support hub mode:

```go
if hubCtx != nil {
    opts := &hubclient.GetLogsOptions{
        Tail:  tailLines,
        Since: sinceFlag,
    }
    result, err := hubCtx.Client.GroveAgents(hubCtx.GroveID).GetCloudLogs(ctx, agentName, opts)
    if err != nil {
        return err
    }
    for _, entry := range result.Entries {
        fmt.Fprintf(os.Stdout, "%s  %s  %s\n", entry.Timestamp.Format(time.RFC3339Nano), entry.Severity, entry.Message)
    }
    return nil
}
```

#### 4.2 Follow Mode

The `--follow` flag opens an HTTP connection to the hub's SSE stream endpoint
and prints entries as they arrive:

```go
if followFlag {
    return hubCtx.Client.GroveAgents(hubCtx.GroveID).StreamCloudLogs(ctx, agentName, opts, func(entry CloudLogEntry) {
        fmt.Fprintf(os.Stdout, "%s  %s  %s\n", entry.Timestamp.Format(time.RFC3339Nano), entry.Severity, entry.Message)
    })
}
```

The SSE connection is held open until interrupted (Ctrl+C) or the server
disconnects. On server timeout (10 minutes), the client automatically
reconnects.

#### 4.3 Flags

| Flag        | Short | Default | Description                      |
|-------------|-------|---------|----------------------------------|
| `--tail`    | `-n`  | 100     | Number of lines from end         |
| `--since`   | —     | —       | Show logs since timestamp/duration (e.g., `1h`, `2026-03-07T10:00:00Z`) |
| `--follow`  | `-f`  | false   | Stream logs in real-time         |
| `--severity`| —     | —       | Minimum severity level           |
| `--json`    | —     | false   | Output full JSON entries         |

#### 4.4 Hub Client Extension

Add to `AgentService` interface in `pkg/hubclient/agents.go`:

```go
// GetCloudLogs retrieves structured log entries from Cloud Logging.
GetCloudLogs(ctx context.Context, agentID string, opts *GetCloudLogsOptions) (*CloudLogsResponse, error)

// StreamCloudLogs opens an SSE connection for streaming log entries.
StreamCloudLogs(ctx context.Context, agentID string, opts *GetCloudLogsOptions, handler func(CloudLogEntry)) error
```

```go
type GetCloudLogsOptions struct {
    Tail     int
    Since    string
    Until    string
    Severity string
}

type CloudLogsResponse struct {
    Entries       []CloudLogEntry `json:"entries"`
    NextPageToken string          `json:"nextPageToken,omitempty"`
    HasMore       bool            `json:"hasMore"`
}

type CloudLogEntry struct {
    Timestamp      time.Time              `json:"timestamp"`
    Severity       string                 `json:"severity"`
    Message        string                 `json:"message"`
    Labels         map[string]string      `json:"labels,omitempty"`
    JSONPayload    map[string]interface{} `json:"jsonPayload,omitempty"`
    InsertID       string                 `json:"insertId"`
    SourceLocation *SourceLocation        `json:"sourceLocation,omitempty"`
}
```

### 5. Alternative Approaches Considered

#### 5.1 Log Source: Container Logs vs Cloud Logging

| Approach | Pros | Cons |
|----------|------|------|
| **Cloud Logging (chosen)** | Persists after container stop; labels enable efficient filtering; structured data; works across brokers; unified view of hub + agent logs | Requires GCP; query latency (~1-3s); costs |
| **Container runtime logs** (`docker logs`) | No GCP dependency; real-time; works locally | Lost on container delete; broker-specific; unstructured |
| **Broker-proxied file read** | Simple; works offline | Requires broker connectivity; file access patterns vary |

**Decision:** Cloud Logging is the primary source for hub-connected agents. Both
hub-side logs (via `CloudHandler`) and agent-side logs (via sciontool exporter)
carry the `agent_id` label, enabling a unified query across all log sources. The
existing local `scion logs` (filesystem read) is preserved as fallback for
non-hub/local-only usage.

#### 5.2 Streaming: SSE vs WebSocket vs Polling

| Approach | Pros | Cons |
|----------|------|------|
| **SSE (chosen)** | Existing infra (`SSEClient`); auto-reconnect; simple server impl | Unidirectional; limited browser connections |
| **WebSocket** | Bidirectional; existing PTY infra | Overkill for log streaming; more complex |
| **Long-polling** | Universal compat | Higher latency; more requests |

**Decision:** SSE aligns with existing patterns (the app already has SSE
infrastructure for state updates). A dedicated `/logs/stream` endpoint is
cleaner than multiplexing log data through the existing state SSE channel
(which would pollute the state management model).

#### 5.3 Streaming Backend: Cloud Logging Tail API vs Polling

| Approach | Pros | Cons |
|----------|------|------|
| **`logadmin` Tail API** | True streaming; low latency | Requires `logging.tailLogEntries` permission; may have availability constraints |
| **Polling loop (chosen for v1)** | Simpler; standard `logging.viewer` permissions | Higher latency (2-5s); more API calls |

**Decision:** Start with a server-side polling loop (query every 3 seconds with
`since` cursor). The Tail API can be adopted later as an optimization — the SSE
contract to the client remains identical.

#### 5.4 JSON Browser: Custom vs Library

| Approach | Pros | Cons |
|----------|------|------|
| **Custom Lit component (chosen)** | Full control; matches design system; no dependency | More code to write |
| **`react-json-view` or similar** | Feature-rich | React dependency; doesn't fit Lit ecosystem |
| **`<pre>` with JSON.stringify** | Trivial | Poor UX; no progressive disclosure |

**Decision:** Custom `<scion-json-browser>` Lit component with recursive
expansion. Keeps the stack consistent and allows tailored UX (e.g., highlighting
known scion fields like `agent_id`, `grove_id`).

### 6. Implementation Plan

#### Phase 1 — Hub API + CLI (Backend) ✅ Complete
1. ✅ Add `logadmin` client initialization to hub `Server` (gated on project ID availability)
2. ✅ Implement `LogQueryService` with `Query()` method (`pkg/hub/logquery.go`)
3. ✅ Add `GET /api/v1/agents/{id}/cloud-logs` handler (`pkg/hub/handlers_logs.go`)
4. ✅ Add `GET /api/v1/agents/{id}/cloud-logs/stream` SSE handler with server-side polling loop
5. ✅ Extend `hubclient.AgentService` with `GetCloudLogs()` and `StreamCloudLogs()`
6. ✅ Update `cmd/logs.go` to call hub API when hub is available
7. ✅ Add flags: `--tail`, `--since`, `--severity`, `--json`, `--follow`
8. ✅ Implement `--follow` via SSE stream consumption
9. ✅ Tests for filter construction, entry conversion, handler routing, CLI flag parsing

**Implementation notes:**
- Endpoint paths use `/cloud-logs` (not `/logs`) to avoid conflicting with any
  existing broker-proxied log endpoint.
- Both `/api/v1/agents/{id}/cloud-logs` and grove-scoped
  `/api/v1/groves/{groveId}/agents/{agentId}/cloud-logs` routes are supported.
- The streaming backend uses a 3-second polling loop (not the Tail API) as
  decided in §5.3. The SSE contract is identical either way.
- `LogQueryService` is initialized in `server.go:New()` using `ResolveProjectID()`
  from the logging package. When no GCP project is available, the service stays
  nil and endpoints return 501.

#### Phase 2 — Web Logs Tab (Frontend) ✅ Complete
1. ✅ Add capability flag from hub indicating Cloud Logging availability
2. ✅ Conditionally render "Logs" tab in `agent-detail.ts` (hidden when not configured)
3. ✅ Implement log fetch and buffer management with lazy loading
4. ✅ Build compact log row rendering
5. ✅ Add refresh button and loading states
6. ✅ Build `<scion-json-browser>` component
7. ✅ Wire expanded row view
8. ✅ Add stream toggle with SSE connection lifecycle
9. ✅ Disable refresh button during streaming

**Implementation notes:**
- `AgentWithCapabilities` response includes `cloudLogging: true` when the hub's
  `LogQueryService` is initialized, allowing the frontend to conditionally render
  the Logs tab.
- The `<scion-agent-log-viewer>` component handles all log viewing logic: fetch,
  buffer management (Map keyed by insertId, capped at 2000), SSE streaming, and
  lazy loading (triggered on first tab activation via `sl-tab-show` event).
- The `<scion-json-browser>` component provides recursive expandable key-value
  rendering with syntax coloring for primitive types. Built as a reusable Lit
  component in `web/src/components/shared/json-browser.ts`.
- Log rows show timestamp (HH:mm:ss.SSS), severity badge (color-coded), subsystem
  (from jsonPayload.subsystem or labels.component), and message. Clicking a row
  expands to show the full structured entry via the JSON browser.
- SSE streaming connects directly to `/api/v1/agents/{id}/cloud-logs/stream`
  using the browser's native `EventSource`. The stream toggle disables the
  refresh button to prevent conflicting fetches.

#### Phase 3 — Optimizations ✅ Complete
1. ✅ Upgrade streaming backend to Cloud Logging Tail API
2. ✅ Add `broker_id` as a primary Cloud Logging label for provenance
3. ✅ Add broker-based filtering to the UI and API

**Implementation notes:**
- Streaming backend now uses the `apiv2.Client.TailLogEntries()` gRPC streaming
  API instead of a 3-second polling loop. This provides true real-time streaming
  with a 2-second buffer window, reducing latency and API call volume.
- `broker_id` is promoted to a Cloud Logging label via `promoteAttrToLabels()`
  in `CloudHandler`, alongside the existing `agent_id` and `grove_id` labels.
  The server enriches the default logger with `broker_id` once the broker ID
  is resolved during startup.
- `BrokerID` added to `LogQueryOptions`, `BuildLogFilter()` generates
  `labels.broker_id = "..."` filter clause, and both `/cloud-logs` and
  `/cloud-logs/stream` endpoints accept a `broker_id` query parameter.
- Hub client `GetCloudLogsOptions` includes `BrokerID` field, and the CLI
  adds a `--broker` flag for filtering by runtime broker.
- Web log viewer auto-discovers broker IDs from log entry labels and renders
  a broker filter dropdown in the toolbar. The agent's own broker is seeded
  from the agent detail response (`runtimeBrokerId`/`runtimeBrokerName`).

## Decisions

Resolved from review feedback:

1. **Log scope:** All cloud logs related to the agent are in scope. Agent-side
   logs flow to Cloud Logging via the sciontool exporter, so querying all logs
   with a matching `agent_id` label provides a unified view of both hub-side
   and agent-side activity.

2. **Log ID scoping:** Queries are **not** scoped to a specific log ID. All
   logs in the project with a matching `agent_id` label are returned, ensuring
   entries from both `scion-server` and `scion-agents` logs are included.

3. **Authorization:** Grove-level read access. Any user who can view the agent
   detail page can view its logs. The hub API enforces this check. Hub itself
   and hub admins have unrestricted log access.

4. **Graceful degradation:** When Cloud Logging is not configured, the Logs tab
   is hidden entirely in the web UI. The API returns `501 Not Implemented`.
   No fallback to container logs or broker-proxied file reads.

5. **Multi-broker aggregation:** Not a concern — Cloud Logging naturally
   aggregates across brokers. The `broker_id` should be promoted to a primary
   Cloud Logging label in a future iteration to provide per-entry provenance.

## Related Files

| File | Relevance |
|------|-----------|
| `pkg/util/logging/cloud_handler.go` | Existing Cloud Logging write path; label promotion |
| `pkg/hub/server.go:1149-1227` | Route registration |
| `pkg/hub/handlers.go` | Handler patterns |
| `pkg/hub/web.go:786-846` | Existing SSE handler |
| `pkg/hubclient/agents.go` | Hub client agent service |
| `cmd/logs.go` | Existing CLI logs command |
| `web/src/components/pages/agent-detail.ts` | Agent detail page (tabs) |
| `web/src/client/sse-client.ts` | SSE client infrastructure |
| `web/src/client/api.ts` | API fetch wrapper |
| `.design/hosted/agent-detail-layout.md` | Agent detail page design spec |
| `.design/hosted/logging-components.md` | Logging architecture |
| `web/src/components/shared/json-browser.ts` | Reusable JSON browser component |
| `web/src/components/shared/agent-log-viewer.ts` | Cloud log viewer component |
| `pkg/hub/response_types.go` | AgentWithCapabilities (cloudLogging flag) |
