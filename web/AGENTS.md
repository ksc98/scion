# Scion Web Frontend - Agent Instructions

This document provides instructions for AI agents working on the Scion Web Frontend.

## Design Documents

Before making changes, review the relevant design documentation:

- **[Web Frontend Design](../.design/hosted/web-frontend-design.md)** - Architecture, technology stack, component patterns

## Architecture Overview

The web frontend is a **client-side SPA** built with Lit web components. There is no Node.js server at runtime. The Go `scion` binary serves the compiled client assets and handles all server-side concerns (OAuth, sessions, SSE real-time events, API routing) via `pkg/hub/web.go` and `pkg/hub/events.go`.

Node.js and npm are used **only at build time** to compile and bundle client assets via Vite.

## Development Workflow

### Building and Running

```bash
cd web
npm install    # First time only, or after package.json changes

# Build client assets
npm run build

# Run the Go server with dev auth (from repository root)
scion server start --enable-hub --enable-web --dev-auth \
  --web-assets-dir ./web/dist/client
```

Dev auth bypasses OAuth and auto-creates a session with admin privileges. The `--web-assets-dir` flag loads assets from disk so you can rebuild and refresh without restarting the server.

### Using Vite Dev Server

For client-side development with hot module reload:

```bash
npm run dev
```

Note: The Vite dev server only serves client assets. API calls and SSE require the Go server to be running.

### Common Commands

| Command | Purpose |
|---------|---------|
| `npm run dev` | Start Vite dev server with hot reload |
| `npm run build` | Build client assets for production |
| `npm run build:dev` | Build client assets in development mode |
| `npm run lint` | Check for linting errors |
| `npm run lint:fix` | Auto-fix linting errors |
| `npm run format` | Format code with Prettier |
| `npm run typecheck` | Run TypeScript type checking |

### Verifying Changes

After making changes, verify:

1. **Type checking passes:** `npm run typecheck`
2. **Linting passes:** `npm run lint`
3. **Client builds:** `npm run build`

## Project Structure

```
web/
├── src/
│   ├── client/              # Browser-side code
│   │   ├── main.ts          # Client entry point, routing setup
│   │   ├── state.ts         # State manager with SSE subscriptions
│   │   └── sse-client.ts    # SSE client for real-time updates
│   ├── components/          # Lit web components
│   │   ├── index.ts         # Component exports
│   │   ├── app-shell.ts     # Main application shell (sidebar, header, content)
│   │   ├── shared/          # Reusable UI components
│   │   │   ├── index.ts         # Shared component exports
│   │   │   ├── nav.ts           # Sidebar navigation
│   │   │   ├── header.ts       # Top header bar with user menu
│   │   │   ├── breadcrumb.ts   # Breadcrumb navigation
│   │   │   ├── debug-panel.ts  # Debug panel component
│   │   │   └── status-badge.ts # Status indicator badges
│   │   └── pages/           # Page components
│   │       ├── home.ts          # Dashboard page
│   │       ├── login.ts         # OAuth login page
│   │       ├── agents.ts       # Agents list page
│   │       ├── agent-detail.ts # Agent details page
│   │       ├── groves.ts       # Groves list page
│   │       ├── grove-detail.ts # Grove details page
│   │       ├── terminal.ts     # Terminal/session page (xterm.js)
│   │       ├── unauthorized.ts # 401/403 page
│   │       └── not-found.ts    # 404 page
│   ├── styles/              # CSS theme and utilities
│   │   ├── theme.css        # CSS custom properties, light/dark mode
│   │   └── utilities.css    # Utility classes
│   └── shared/              # Shared types between components
│       └── types.ts         # Type definitions (User, Grove, Agent, etc.)
├── public/                  # Static assets
│   └── assets/              # Built client assets (CSS, JS)
├── dist/                    # Build output (gitignored)
├── vite.config.ts           # Vite build configuration
├── tsconfig.json            # TypeScript configuration
└── package.json
```

## Technology Stack

- **Components:** Lit 3.x with TypeScript decorators
- **UI Library:** Shoelace 2.x
- **Build:** Vite for client-side bundling
- **Routing:** Client-side via History API (click interception in `main.ts`)
- **Terminal:** xterm.js for terminal sessions
- **Server:** Go (`scion` binary with `--enable-web`)

## Key Patterns

### Creating Lit Components

Components use standard Lit patterns with TypeScript decorators:

```typescript
import { LitElement, html, css } from 'lit';
import { customElement, property } from 'lit/decorators.js';

@customElement('my-component')
export class MyComponent extends LitElement {
  @property({ type: String })
  myProp = 'default';

  static override styles = css`
    :host { display: block; }
  `;

  override render() {
    return html`<div>${this.myProp}</div>`;
  }
}
```

### Using Shoelace Components

```typescript
render() {
  return html`
    <sl-button variant="primary" @click=${() => this.handleClick()}>
      <sl-icon slot="prefix" name="plus-lg"></sl-icon>
      Create Agent
    </sl-button>

    <sl-badge variant="success">Running</sl-badge>
  `;
}
```

### Theme Variables

Use CSS custom properties with the `--scion-` prefix for consistent theming:

```css
:host {
  background: var(--scion-surface);
  color: var(--scion-text);
  border: 1px solid var(--scion-border);
  border-radius: var(--scion-radius);
}
```

### Dark Mode

Dark mode is handled automatically via CSS custom properties. The theme toggle in the navigation saves the preference to localStorage. Components should use the semantic color variables (e.g., `--scion-surface`, `--scion-text`) which automatically adjust for dark mode.

## Testing Real-Time (SSE) Events

Test scripts for validating real-time event delivery are in `web/test-scripts/`. These were used during the initial validation of the SSE pipeline and remain useful for regression testing.

| Script | Purpose |
|--------|---------|
| `sse-curl-test.sh` | Server-side SSE validation with curl (no browser) |
| `realtime-lifecycle-test.js` | Full browser test with Playwright screenshots |
| `screenshot-debug.js` | Debug tool for blank/broken pages |

### Quick SSE smoke test (no browser needed)

```bash
TOKEN=<dev-token> GROVE_ID=<uuid> ./web/test-scripts/sse-curl-test.sh
```

### Full browser lifecycle test

```bash
GROVE_ID=<uuid> TOKEN=<dev-token> node web/test-scripts/realtime-lifecycle-test.js
```

## Containerized / Sandboxed Environments

When working in a containerized or sandboxed agent environment (e.g., scion agents), keep these points in mind:

- **Vite dev server is available.** You can run `npm run dev` to start the Vite dev server for client-side development and visual inspection. API calls and SSE will not work without the Go backend.
- **Use `--dev-auth` for local testing.** When a Go server is available, `--dev-auth` bypasses OAuth and auto-creates a dev session, which is the simplest way to test the frontend end-to-end. See the README for details.
- **Go server** the golang server can be started as a background process, but OAuth flows cannot be used in a container.

## Tips for End-to-End Web Validation

These tips were collected during validation work and are useful for agents debugging or testing the web frontend against the Go backend.

### Server startup

- **Combined mode** runs the Hub API on the web port (default 8080). When `--enable-hub` and `--enable-web` are both set, there is no separate listener on port 9810. All API routes are at `http://localhost:8080/api/v1/`.
- **Runtime broker** must be enabled (`--enable-runtime-broker`) and linked to the grove as a provider before agents can be created. With the co-located broker, use `POST /api/v1/groves/{id}/providers` with `{"brokerId":"<id>"}` to link.
- **Dev token** is printed in the server startup logs. Use it as `Authorization: Bearer scion_dev_...` for API calls.
- **`--web-assets-dir`** loads assets from disk so you can rebuild the frontend (`npm run build`) and refresh the browser without restarting the Go server.

### API gotchas

- **Agent status updates** use `POST /api/v1/agents/{id}/status`, not `PATCH`. The handler only accepts POST.
- **Agent creation response** wraps the agent under an `"agent"` key: `{ "agent": { "id": "...", ... }, "warnings": [...] }`.
- **SSE endpoint** (`/events?sub=...`) requires a session cookie, not a Bearer token. To get a session cookie via curl: `curl -c - -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/groves` then pass the `scion_sess` cookie with `-b`.

### SSE event format

The SSE stream uses a single event type `update` with the subject embedded in the data payload:

```
event: update
data: {"subject":"grove.xxx.agent.created","data":{"agentId":"...","groveId":"..."}}
```

The client `SSEClient` listens for `event: update` and the `StateManager` parses the `subject` field to route events. If the event type is changed or the subject is used as the SSE event type directly, the client will silently drop events.

### Browser testing with Playwright

- Use `waitUntil: 'domcontentloaded'` instead of `'networkidle'` — the SSE connection keeps the network perpetually active, so `networkidle` will time out.
- Chromium needs `--no-sandbox --disable-setuid-sandbox` flags in containerized environments.
- Console logging via `page.on('console', ...)` is essential for debugging SSE connection state — the `[SSE] Connected` log confirms the EventSource opened.
- To validate real-time updates: take a screenshot, make an API call, wait 2-3 seconds, take another screenshot. Compare visually.

### Common failure modes

- **Blank page**: Check that web assets are built (`npm run build`) and the `--web-assets-dir` flag points to `web/dist/client`. Use `screenshot-debug.js` to see console errors and 404s.
- **SSE events not updating UI**: Check the SSE event type. The client only listens for `event: update`. If the server sends events with the subject as the type, they are silently dropped.
- **Agent delete not reflected**: The `onAgentsUpdated()` handler in `grove-detail.ts` must run even when the state manager's agent map is empty (after the last agent is deleted).
