# Scion Web Frontend Implementation Milestones

## Overview

This document outlines the implementation milestones for the Scion Web Frontend. Each milestone is designed to be independently testable and builds upon previous work. The milestones follow a bottom-up approach: infrastructure first, then core functionality, then enhanced features.

---

## Milestone 1: Koa Server Foundation

**Goal:** Establish the basic Koa server infrastructure with static asset serving, health endpoints, and development tooling.

### Deliverables

1. **Project scaffolding**
   - TypeScript configuration
   - ESLint/Prettier setup
   - Vite build configuration
   - Package.json with dependencies

2. **Koa application core**
   - Application entry point (`src/server/index.ts`)
   - Middleware stack (logger, error handler, security headers)
   - Static asset serving from `public/`

3. **Health endpoints**
   - `GET /healthz` - liveness probe
   - `GET /readyz` - readiness probe (initially same as liveness)

4. **Development workflow**
   - Hot reload for server changes
   - Vite dev server for client assets
   - npm scripts: `dev`, `build`, `start`

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Server starts | `npm run dev` | Server listens on port 8080 |
| Health check | `curl localhost:8080/healthz` | `{"status":"healthy"}` with 200 |
| Static file | `curl localhost:8080/assets/test.txt` | File contents returned |
| 404 handling | `curl localhost:8080/nonexistent` | 404 with JSON error |
| Security headers | `curl -I localhost:8080/healthz` | CSP, X-Frame-Options present |

### Directory Structure After M1

```
web/
├── src/
│   └── server/
│       ├── index.ts
│       ├── app.ts
│       ├── config.ts
│       └── middleware/
│           ├── error-handler.ts
│           ├── logger.ts
│           └── security.ts
├── public/
│   └── assets/
├── package.json
├── tsconfig.json
└── vite.config.ts
```

---

## Milestone 2: Lit SSR Integration

**Goal:** Integrate @lit-labs/ssr for server-side rendering of Lit components with client-side hydration.

### Deliverables

1. **SSR renderer**
   - HTML shell template with hydration script injection
   - Lit component rendering via `@lit-labs/ssr`
   - Initial data serialization (`__SCION_DATA__` script tag)

2. **Basic Lit components (server + client)**
   - `<scion-app>` - application shell
   - `<scion-page-home>` - simple home page
   - `<scion-page-404>` - not found page

3. **Client hydration**
   - Client entry point (`src/client/main.ts`)
   - Hydration of SSR content
   - Client-side router setup (@vaadin/router)

4. **Page routes**
   - `GET /` - home page (SSR)
   - `GET /*` - catch-all for SPA routing

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| SSR home page | `curl localhost:8080/` | HTML with `<scion-app>` content |
| View source | Browser "View Source" | Complete HTML (not empty shell) |
| Hydration | Browser console | No hydration errors |
| Client navigation | Click internal link | Client-side route change (no reload) |
| Initial data | `document.getElementById('__SCION_DATA__')` | JSON with page data |
| 404 page | `curl localhost:8080/nonexistent-page` | 404 page SSR rendered |

### Key Technical Decisions

- Use declarative shadow DOM for SSR (`<template shadowroot="open">`)
- Serialize initial data as JSON in script tag (not inline in HTML)
- Use @vaadin/router for client-side routing (Lit-compatible)

---

## Milestone 3: Web Awesome Component Library

**Goal:** Integrate Web Awesome component library and establish the UI foundation with theming.

### Deliverables

1. **Web Awesome integration**
   - CDN script/style loading (initial approach)
   - Theme CSS custom properties
   - Component registration verification

2. **Core UI components**
   - `<scion-nav>` - sidebar navigation
   - `<scion-header>` - top header bar
   - `<scion-breadcrumb>` - breadcrumb navigation
   - `<scion-status-badge>` - status indicator

3. **Layout system**
   - Responsive sidebar layout
   - Content area with padding/scrolling
   - Mobile breakpoint handling

4. **Theme configuration**
   - Light/dark mode support
   - CSS custom property overrides
   - Consistent color palette

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Web Awesome loads | Browser console | No 404 for WA assets |
| Components render | Visual inspection | `<wa-button>`, `<wa-card>` display correctly |
| Theme variables | DevTools | CSS custom properties applied |
| Dark mode | Toggle theme | Colors switch appropriately |
| Responsive | Resize to mobile | Sidebar collapses/hides |
| Navigation | Click nav items | Routes change, active state updates |

### Notes

- Initially load Web Awesome from CDN for simplicity
- Future optimization: bundle locally for offline/faster loads
- Ensure SSR output includes Web Awesome component tags (hydrated client-side)

---

## Milestone 4: Authentication Flow

**Goal:** Implement OAuth authentication with session management.

### Deliverables

1. **Session middleware**
   - koa-session configuration
   - Secure cookie settings
   - Session store (in-memory for dev, Redis for prod)

2. **OAuth routes**
   - `GET /auth/login/:provider` - initiate OAuth
   - `GET /auth/callback/:provider` - OAuth callback
   - `POST /auth/logout` - clear session
   - `GET /auth/me` - current user info

3. **OAuth providers**
   - Google OAuth 2.0 integration
   - GitHub OAuth integration
   - Provider abstraction for future additions

4. **Auth middleware**
   - `auth()` middleware for protected routes
   - Redirect to login for unauthenticated requests
   - User context injection into SSR

5. **Login UI**
   - `<scion-login-page>` component
   - Provider selection buttons
   - Error handling/display

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Login redirect | Visit protected route | Redirect to `/auth/login` |
| Google OAuth | Click "Login with Google" | Redirect to Google, then callback |
| Session created | After OAuth callback | Session cookie set |
| User in context | Visit protected route | User info available in page |
| Logout | POST /auth/logout | Session cleared, redirect to login |
| Auth/me API | `curl /auth/me` with session | User JSON returned |
| Invalid session | Expired/tampered cookie | Redirect to login |

### Configuration Required

```bash
# Environment variables for testing
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...
SESSION_SECRET=dev-secret-min-32-chars-long
BASE_URL=http://localhost:8080
```

---

## Milestone 5: Hub API Proxy

**Goal:** Proxy requests to the Hub API with authentication header injection.

### Deliverables

1. **API proxy middleware**
   - Route `/api/*` to Hub API
   - Forward authentication headers
   - Request/response logging
   - Error transformation

2. **Hub client service**
   - Typed API client for server-side calls
   - Request timeout handling
   - Retry logic with backoff

3. **SSR data fetching**
   - Fetch data during SSR for initial render
   - Pass data to Lit components
   - Error boundary for failed fetches

4. **Mock Hub API (for testing)**
   - Standalone mock server
   - Fixtures for common responses
   - Configurable via environment

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Proxy request | `curl /api/groves` (authenticated) | Hub API response |
| Auth forwarding | Check Hub logs | Authorization header present |
| Timeout | Slow Hub response | 504 after timeout |
| Hub down | Stop Hub | 502 Bad Gateway |
| Rate limit headers | Response headers | X-RateLimit-* forwarded |
| SSR with data | Visit `/groves` | Page renders with grove list |
| Mock mode | `HUB_MOCK=true npm run dev` | Mock data returned |

### Mock Server

Create a simple mock for development without a real Hub:

```typescript
// tools/mock-hub/index.ts
// Serves static JSON responses for Hub API endpoints
```

---

## Milestone 6: Grove & Agent Pages

**Goal:** Implement the core pages for viewing and managing groves and agents.

### Deliverables

1. **Grove pages**
   - `<scion-grove-list>` - list all groves with filtering
   - `<scion-grove-detail>` - single grove view with agent list
   - Grove card component with status summary

2. **Agent pages**
   - `<scion-agent-list>` - agents within a grove
   - `<scion-agent-detail>` - single agent view
   - Agent card component with status, actions

3. **Action handlers**
   - Start/stop agent buttons
   - Delete agent with confirmation
   - Create agent dialog (basic)

4. **State management (client)**
   - State manager class
   - Hydration from SSR data
   - Optimistic updates

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Grove list loads | Visit `/groves` | Groves displayed from Hub API |
| Grove detail | Click grove | Navigate to grove detail page |
| Agent list | Visit grove detail | Agents listed for that grove |
| Agent status | View agent card | Correct status badge color |
| Stop agent | Click "Stop" button | API call, status updates |
| Start agent | Click "Start" button | API call, status updates |
| Delete agent | Click "Delete", confirm | Agent removed from list |
| Empty state | Grove with no agents | "No agents" message |
| Loading state | Slow API | Loading spinner shown |
| Error state | API error | Error message displayed |

### Routes

| Route | Page | SSR Data |
|-------|------|----------|
| `/groves` | Grove list | All groves |
| `/groves/:groveId` | Grove detail | Grove + agents |
| `/agents/:agentId` | Agent detail | Agent |

---

## Milestone 7: SSE + NATS Real-Time Updates

**Goal:** Implement the Snapshot + Delta pattern with SSE and NATS for real-time updates.

### Deliverables

1. **NATS client**
   - Connection management with reconnection
   - Subject subscription/unsubscription
   - Message deserialization

2. **SSE endpoint**
   - `GET /events` - SSE stream
   - `POST /events/subscribe` - subscribe to subjects
   - Connection tracking per user
   - Heartbeat messages

3. **SSE manager**
   - Connection lifecycle management
   - NATS-to-SSE message bridging
   - Subject-based routing
   - Permission filtering

4. **Client SSE handler**
   - EventSource connection
   - Reconnection with backoff
   - Message parsing and dispatch

5. **Reactive component updates**
   - State manager integration with SSE
   - Delta merging into local state
   - Lit reactive property updates

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| SSE connects | Browser Network tab | EventSource connection open |
| Heartbeat | Wait 30s | Heartbeat event received |
| Subscribe | Call subscribe API | Subscription confirmed |
| Agent status update | Change agent status in Hub | UI updates without refresh |
| Agent created | Create agent via CLI | New agent appears in list |
| Agent deleted | Delete agent via CLI | Agent removed from list |
| Reconnection | Kill NATS, restart | SSE reconnects automatically |
| Multiple tabs | Open same page in 2 tabs | Both receive updates |
| Permission check | Subscribe to unauthorized grove | Subscription rejected |

### NATS Testing

```bash
# Start NATS for local development
docker run -p 4222:4222 nats:latest

# Publish test message
nats pub agent.test123.status '{"status":"running"}'
```

---

## Milestone 8: Terminal Component

**Goal:** Implement the xterm.js-based terminal for PTY access to agents.

### Deliverables

1. **Terminal component**
   - `<scion-terminal>` Lit component
   - xterm.js integration with addons (fit, web-links)
   - Theme matching Web Awesome colors

2. **WebSocket connection**
   - Ticket-based authentication
   - PTY WebSocket proxy through Koa
   - Binary data handling (base64)

3. **Terminal features**
   - Auto-resize on container change
   - Connection status indicator
   - Reconnection handling
   - Copy/paste support

4. **Terminal page**
   - Full-screen terminal view
   - Toolbar with agent info and actions
   - Back navigation

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Terminal loads | Visit `/agents/:id/terminal` | Terminal container renders |
| WebSocket connects | Network tab | WS connection established |
| PTY output | Run command in agent | Output displays in terminal |
| Keyboard input | Type in terminal | Input sent to agent |
| Resize | Resize browser window | Terminal adjusts, PTY resizes |
| Connection lost | Kill agent container | "Disconnected" shown |
| Reconnect | Click reconnect button | Terminal reconnects |
| Theme | Toggle dark/light mode | Terminal colors update |
| Copy text | Select and Ctrl+C | Text copied to clipboard |

### WebSocket Proxy

The Koa server proxies WebSocket connections to the Hub API:

```
Browser WS → Koa WS Proxy → Hub API WS → Runtime Host
```

---

## Milestone 9: Agent Creation Workflow

**Goal:** Implement the full agent creation flow with template selection and configuration.

### Deliverables

1. **Create agent dialog**
   - `<scion-create-agent-dialog>` component
   - Template selector
   - Configuration form (name, task, branch)
   - Advanced options (image, env vars)

2. **Template browser**
   - List available templates
   - Template detail view
   - Template preview

3. **Creation flow**
   - Form validation
   - API submission
   - Progress tracking
   - Error handling

4. **Post-creation navigation**
   - Redirect to agent detail
   - Option to open terminal
   - Notification of creation

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Open dialog | Click "New Agent" | Dialog opens |
| Template list | View templates | Templates from Hub API |
| Select template | Click template | Template selected, form updates |
| Validation | Submit empty name | Validation error shown |
| Create agent | Fill form, submit | Agent created, redirect to detail |
| Creation error | Hub returns error | Error message displayed |
| Cancel | Click cancel | Dialog closes, no changes |
| Advanced options | Expand advanced | Additional fields shown |

---

## Milestone 10: Production Hardening

**Goal:** Prepare for production deployment with security, performance, and observability improvements.

### Deliverables

1. **Security hardening**
   - CSRF protection
   - Rate limiting
   - Input sanitization
   - Audit logging

2. **Performance optimization**
   - Asset bundling and minification
   - Gzip/Brotli compression
   - Cache headers configuration
   - Critical CSS inlining

3. **Error handling**
   - Global error boundary
   - User-friendly error pages
   - Error reporting integration (optional)

4. **Logging and monitoring**
   - Structured JSON logging
   - Request ID tracing
   - Metrics endpoint (optional)

5. **Configuration management**
   - Environment-based config
   - Secret handling
   - Feature flags

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| CSRF protection | POST without token | 403 Forbidden |
| Rate limiting | Exceed limit | 429 Too Many Requests |
| Asset compression | Check Content-Encoding | gzip or br |
| Cache headers | Check static assets | Cache-Control set |
| Error page | Trigger 500 error | Friendly error page |
| Structured logs | Check stdout | JSON log entries |
| Request tracing | Check logs | X-Request-ID present |

---

## Milestone 11: Cloud Run Deployment

**Goal:** Deploy the web frontend to Cloud Run with full CI/CD pipeline.

### Deliverables

1. **Container image**
   - Multi-stage Dockerfile
   - Minimal production image
   - Non-root user

2. **Cloud Run configuration**
   - Service definition (cloudrun.yaml)
   - Environment variables
   - Secret references
   - Resource limits

3. **CI/CD pipeline**
   - Build on push to main
   - Run tests
   - Build and push image
   - Deploy to Cloud Run

4. **Infrastructure**
   - Secret Manager setup
   - IAM configuration
   - VPC connector (for Hub access)
   - Custom domain (optional)

5. **Monitoring**
   - Cloud Run metrics
   - Error reporting
   - Uptime checks

### Test Criteria

| Test | Method | Expected Result |
|------|--------|-----------------|
| Container builds | `docker build .` | Image builds successfully |
| Container runs | `docker run ...` | Server starts, health check passes |
| Deploy to staging | Push to staging branch | Deploys to staging environment |
| Health check | Cloud Run console | Instance healthy |
| Cold start | Scale to 0, then request | Response within 5s |
| Secrets loaded | Check app behavior | OAuth works, session works |
| Hub connectivity | Create agent | Agent created successfully |
| Custom domain | Visit domain | SSL works, site loads |

### Deployment Commands

```bash
# Build and push
gcloud builds submit --tag gcr.io/PROJECT/scion-web

# Deploy
gcloud run deploy scion-web \
  --image gcr.io/PROJECT/scion-web \
  --platform managed \
  --region us-central1 \
  --allow-unauthenticated
```

---

## Milestone Dependencies

```
M1 ──► M2 ──► M3 ──┬──► M4 ──► M5 ──► M6 ──┬──► M7 ──► M8
                   │                        │
                   │                        └──► M9
                   │
                   └──────────────────────────────────► M10 ──► M11
```

| Milestone | Depends On | Can Parallelize With |
|-----------|------------|----------------------|
| M1: Koa Foundation | - | - |
| M2: Lit SSR | M1 | - |
| M3: Web Awesome | M2 | - |
| M4: Authentication | M3 | - |
| M5: Hub API Proxy | M4 | - |
| M6: Grove & Agent Pages | M5 | - |
| M7: SSE + NATS | M6 | M9 |
| M8: Terminal | M7 | - |
| M9: Agent Creation | M6 | M7, M8 |
| M10: Production Hardening | M3+ | M7-M9 |
| M11: Cloud Run Deployment | M10 | - |

---

## Estimated Complexity

| Milestone | Complexity | Key Risks |
|-----------|------------|-----------|
| M1: Koa Foundation | Low | None |
| M2: Lit SSR | Medium | @lit-labs/ssr edge cases |
| M3: Web Awesome | Low | Version compatibility |
| M4: Authentication | Medium | OAuth provider config |
| M5: Hub API Proxy | Low | None |
| M6: Grove & Agent Pages | Medium | UI/UX decisions |
| M7: SSE + NATS | High | Connection management, race conditions |
| M8: Terminal | Medium | xterm.js SSR compatibility |
| M9: Agent Creation | Medium | Form complexity |
| M10: Production Hardening | Medium | Security review |
| M11: Cloud Run Deployment | Medium | Infrastructure setup |

---

## Testing Strategy

### Unit Tests
- Component rendering tests (Lit)
- Middleware tests (Koa)
- Service tests (Hub client, NATS client)

### Integration Tests
- API proxy end-to-end
- SSE subscription flow
- OAuth flow with mock provider

### E2E Tests
- Full user flows (login → create agent → terminal)
- Playwright or Cypress
- Run against staging environment

### Manual Testing
- Cross-browser compatibility
- Mobile responsiveness
- Accessibility audit (WCAG 2.1 AA)

---

## References

- **Web Frontend Design:** `web-frontend-design.md`
- **Hub API:** `hub-api.md`
- **Server Implementation:** `server-implementation-design.md`
