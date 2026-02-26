/**
 * Copyright 2026 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * Agent detail page component
 *
 * Displays detailed information about a single agent
 */

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import type { PageData, Agent, Grove } from '../../shared/types.js';
import { can } from '../../shared/types.js';
import { apiFetch } from '../../client/api.js';
import { stateManager } from '../../client/state.js';
import '../shared/status-badge.js';

@customElement('scion-page-agent-detail')
export class ScionPageAgentDetail extends LitElement {
  /**
   * Page data from SSR
   */
  @property({ type: Object })
  pageData: PageData | null = null;

  /**
   * Agent ID from URL
   */
  @property({ type: String })
  agentId = '';

  /**
   * Loading state
   */
  @state()
  private loading = true;

  /**
   * Agent data
   */
  @state()
  private agent: Agent | null = null;

  /**
   * Parent grove data
   */
  @state()
  private grove: Grove | null = null;

  /**
   * Error message if loading failed
   */
  @state()
  private error: string | null = null;

  /**
   * Action in progress
   */
  @state()
  private actionLoading = false;

  static override styles = css`
    :host {
      display: block;
    }

    .back-link {
      display: inline-flex;
      align-items: center;
      gap: 0.5rem;
      color: var(--scion-text-muted, #64748b);
      text-decoration: none;
      font-size: 0.875rem;
      margin-bottom: 1rem;
    }

    .back-link:hover {
      color: var(--scion-primary, #3b82f6);
    }

    .header {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      margin-bottom: 1.5rem;
      gap: 1rem;
    }

    .header-info {
      flex: 1;
    }

    .header-title {
      display: flex;
      align-items: center;
      gap: 0.75rem;
      margin-bottom: 0.5rem;
    }

    .header-title sl-icon {
      color: var(--scion-primary, #3b82f6);
      font-size: 1.5rem;
    }

    .header h1 {
      font-size: 1.5rem;
      font-weight: 700;
      color: var(--scion-text, #1e293b);
      margin: 0;
    }

    .header-meta {
      display: flex;
      align-items: center;
      gap: 1rem;
      margin-top: 0.5rem;
    }

    .template-badge {
      display: inline-flex;
      align-items: center;
      gap: 0.25rem;
      padding: 0.25rem 0.75rem;
      background: var(--scion-bg-subtle, #f1f5f9);
      border-radius: var(--scion-radius, 0.5rem);
      font-size: 0.875rem;
      color: var(--scion-text-muted, #64748b);
    }

    .grove-link {
      display: inline-flex;
      align-items: center;
      gap: 0.25rem;
      color: var(--scion-text-muted, #64748b);
      text-decoration: none;
      font-size: 0.875rem;
    }

    .grove-link:hover {
      color: var(--scion-primary, #3b82f6);
    }

    .header-actions {
      display: flex;
      gap: 0.5rem;
      flex-shrink: 0;
    }

    .card {
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
      padding: 1.5rem;
      margin-bottom: 1.5rem;
    }

    .card-title {
      font-size: 1rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 1rem 0;
      padding-bottom: 0.75rem;
      border-bottom: 1px solid var(--scion-border, #e2e8f0);
    }

    .info-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
      gap: 1.5rem;
    }

    .info-item {
      display: flex;
      flex-direction: column;
    }

    .info-label {
      font-size: 0.75rem;
      color: var(--scion-text-muted, #64748b);
      text-transform: uppercase;
      letter-spacing: 0.05em;
      margin-bottom: 0.25rem;
    }

    .info-value {
      font-size: 1rem;
      color: var(--scion-text, #1e293b);
    }

    .info-value.mono {
      font-family: var(--scion-font-mono, monospace);
      font-size: 0.875rem;
    }

    .task-summary {
      font-size: 1rem;
      color: var(--scion-text, #1e293b);
      padding: 1rem;
      background: var(--scion-bg-subtle, #f1f5f9);
      border-radius: var(--scion-radius, 0.5rem);
      margin-top: 1rem;
      white-space: pre-wrap;
      line-height: 1.5;
    }

    .status-timeline {
      display: flex;
      flex-direction: column;
      gap: 0.75rem;
    }

    .timeline-item {
      display: flex;
      align-items: flex-start;
      gap: 0.75rem;
    }

    .timeline-dot {
      width: 10px;
      height: 10px;
      border-radius: 50%;
      background: var(--scion-border, #e2e8f0);
      margin-top: 0.35rem;
      flex-shrink: 0;
    }

    .timeline-dot.active {
      background: var(--sl-color-success-500, #22c55e);
    }

    .timeline-content {
      flex: 1;
    }

    .timeline-title {
      font-weight: 500;
      color: var(--scion-text, #1e293b);
    }

    .timeline-time {
      font-size: 0.75rem;
      color: var(--scion-text-muted, #64748b);
    }

    .quick-actions {
      display: flex;
      gap: 1rem;
    }

    .quick-action {
      display: flex;
      flex-direction: column;
      align-items: center;
      padding: 1.5rem;
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--scion-border, #e2e8f0);
      border-radius: var(--scion-radius-lg, 0.75rem);
      cursor: pointer;
      transition: all var(--scion-transition-fast, 150ms ease);
      text-decoration: none;
      color: inherit;
      flex: 1;
    }

    .quick-action:hover:not([disabled]) {
      border-color: var(--scion-primary, #3b82f6);
      box-shadow: var(--scion-shadow-md, 0 4px 6px -1px rgba(0, 0, 0, 0.1));
    }

    .quick-action[disabled] {
      opacity: 0.5;
      cursor: not-allowed;
    }

    .quick-action sl-icon {
      font-size: 2rem;
      color: var(--scion-primary, #3b82f6);
      margin-bottom: 0.5rem;
    }

    .quick-action span {
      font-weight: 500;
      color: var(--scion-text, #1e293b);
    }

    .loading-state {
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      padding: 4rem 2rem;
      color: var(--scion-text-muted, #64748b);
    }

    .loading-state sl-spinner {
      font-size: 2rem;
      margin-bottom: 1rem;
    }

    .error-state {
      text-align: center;
      padding: 3rem 2rem;
      background: var(--scion-surface, #ffffff);
      border: 1px solid var(--sl-color-danger-200, #fecaca);
      border-radius: var(--scion-radius-lg, 0.75rem);
    }

    .error-state sl-icon {
      font-size: 3rem;
      color: var(--sl-color-danger-500, #ef4444);
      margin-bottom: 1rem;
    }

    .error-state h2 {
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--scion-text, #1e293b);
      margin: 0 0 0.5rem 0;
    }

    .error-state p {
      color: var(--scion-text-muted, #64748b);
      margin: 0 0 1rem 0;
    }

    .error-details {
      font-family: var(--scion-font-mono, monospace);
      font-size: 0.875rem;
      background: var(--scion-bg-subtle, #f1f5f9);
      padding: 0.75rem 1rem;
      border-radius: var(--scion-radius, 0.5rem);
      color: var(--sl-color-danger-700, #b91c1c);
      margin-bottom: 1rem;
    }

    .agent-error-banner {
      background: var(--sl-color-danger-50, #fef2f2);
      border: 1px solid var(--sl-color-danger-200, #fecaca);
      border-radius: var(--scion-radius-lg, 0.75rem);
      padding: 1rem 1.25rem;
      margin-bottom: 1.5rem;
      display: flex;
      align-items: flex-start;
      gap: 0.75rem;
    }

    .agent-error-banner sl-icon {
      color: var(--sl-color-danger-500, #ef4444);
      font-size: 1.25rem;
      flex-shrink: 0;
      margin-top: 0.125rem;
    }

    .agent-error-banner .error-content {
      flex: 1;
      min-width: 0;
    }

    .agent-error-banner .error-title {
      font-weight: 600;
      color: var(--sl-color-danger-700, #b91c1c);
      margin-bottom: 0.25rem;
    }

    .agent-error-banner .error-message {
      font-size: 0.875rem;
      color: var(--sl-color-danger-600, #dc2626);
      font-family: var(--scion-font-mono, monospace);
      white-space: pre-wrap;
      word-break: break-word;
    }
  `;

  private boundOnAgentsUpdated = this.onAgentsUpdated.bind(this);
  private boundOnGrovesUpdated = this.onGrovesUpdated.bind(this);
  private relativeTimeInterval: ReturnType<typeof setInterval> | null = null;

  override connectedCallback(): void {
    super.connectedCallback();
    // SSR property bindings (.agentId=) aren't restored during client-side
    // hydration for top-level page components. Fall back to URL parsing.
    if (!this.agentId && typeof window !== 'undefined') {
      const match = window.location.pathname.match(/\/agents\/([^/]+)/);
      if (match) {
        this.agentId = match[1];
      }
    }
    void this.loadData();

    // Listen for real-time updates (scope is set after loadData resolves with groveId)
    stateManager.addEventListener('agents-updated', this.boundOnAgentsUpdated as EventListener);
    stateManager.addEventListener('groves-updated', this.boundOnGrovesUpdated as EventListener);

    // Periodically re-render to keep relative timestamps fresh
    this.relativeTimeInterval = setInterval(() => this.requestUpdate(), 15000);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    stateManager.removeEventListener('agents-updated', this.boundOnAgentsUpdated as EventListener);
    stateManager.removeEventListener('groves-updated', this.boundOnGrovesUpdated as EventListener);
    if (this.relativeTimeInterval) {
      clearInterval(this.relativeTimeInterval);
      this.relativeTimeInterval = null;
    }
  }

  private onAgentsUpdated(): void {
    const updatedAgent = stateManager.getAgent(this.agentId);
    if (updatedAgent && this.agent) {
      this.agent = { ...this.agent, ...updatedAgent };
    }
  }

  private onGrovesUpdated(): void {
    if (this.agent?.groveId) {
      const updatedGrove = stateManager.getGrove(this.agent.groveId);
      if (updatedGrove && this.grove) {
        this.grove = { ...this.grove, ...updatedGrove };
      }
    }
  }

  private async loadData(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const agentResponse = await apiFetch(`/api/v1/agents/${this.agentId}`);

      if (!agentResponse.ok) {
        const errorData = (await agentResponse.json().catch(() => ({}))) as { message?: string };
        throw new Error(
          errorData.message || `HTTP ${agentResponse.status}: ${agentResponse.statusText}`
        );
      }

      this.agent = (await agentResponse.json()) as Agent;

      // Set SSE scope now that we have the groveId
      if (this.agent.groveId) {
        stateManager.setScope({
          type: 'agent-detail',
          groveId: this.agent.groveId,
          agentId: this.agentId,
        });
      }

      // Try to load grove info
      if (this.agent.groveId) {
        try {
          const groveResponse = await apiFetch(`/api/v1/groves/${this.agent.groveId}`);
          if (groveResponse.ok) {
            this.grove = (await groveResponse.json()) as Grove;
          }
        } catch {
          // Grove loading is optional, don't fail if it doesn't work
        }
      }
    } catch (err) {
      console.error('Failed to load agent:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load agent';
    } finally {
      this.loading = false;
    }
  }

  private getStatusVariant(status: string): 'success' | 'warning' | 'danger' | 'neutral' {
    switch (status) {
      case 'running':
        return 'success';
      case 'stopped':
        return 'neutral';
      case 'provisioning':
      case 'cloning':
        return 'warning';
      case 'error':
        return 'danger';
      default:
        return 'neutral';
    }
  }

  private formatDate(dateString: string): string {
    try {
      const date = new Date(dateString);
      return new Intl.DateTimeFormat('en', {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      }).format(date);
    } catch {
      return dateString;
    }
  }

  private formatRelativeTime(dateString: string): string {
    try {
      const date = new Date(dateString);
      const diffMs = Date.now() - date.getTime();
      const diffSeconds = Math.round(diffMs / 1000);
      const diffMinutes = Math.round(diffMs / (1000 * 60));
      const diffHours = Math.round(diffMs / (1000 * 60 * 60));
      const diffDays = Math.round(diffMs / (1000 * 60 * 60 * 24));

      const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });

      if (Math.abs(diffSeconds) < 60) {
        return rtf.format(-diffSeconds, 'second');
      } else if (Math.abs(diffMinutes) < 60) {
        return rtf.format(-diffMinutes, 'minute');
      } else if (Math.abs(diffHours) < 24) {
        return rtf.format(-diffHours, 'hour');
      } else {
        return rtf.format(-diffDays, 'day');
      }
    } catch {
      return dateString;
    }
  }

  private async handleAction(action: 'start' | 'stop' | 'delete'): Promise<void> {
    if (!this.agent) return;

    this.actionLoading = true;

    try {
      let response: Response;

      switch (action) {
        case 'start':
          response = await apiFetch(`/api/v1/agents/${this.agentId}/start`, {
            method: 'POST',
          });
          break;
        case 'stop':
          response = await apiFetch(`/api/v1/agents/${this.agentId}/stop`, {
            method: 'POST',
          });
          break;
        case 'delete':
          if (!confirm('Are you sure you want to delete this agent?')) {
            this.actionLoading = false;
            return;
          }
          response = await apiFetch(`/api/v1/agents/${this.agentId}`, {
            method: 'DELETE',
          });
          break;
      }

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to ${action} agent`);
      }

      if (action === 'delete') {
        // Navigate back to agents list
        window.location.href = '/agents';
      } else {
        // Reload data to reflect changes
        await this.loadData();
      }
    } catch (err) {
      console.error(`Failed to ${action} agent:`, err);
      alert(err instanceof Error ? err.message : `Failed to ${action} agent`);
    } finally {
      this.actionLoading = false;
    }
  }

  override render() {
    if (this.loading) {
      return this.renderLoading();
    }

    if (this.error || !this.agent) {
      return this.renderError();
    }

    return html`
      <a href="/agents" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Agents
      </a>

      <div class="header">
        <div class="header-info">
          <div class="header-title">
            <sl-icon name="cpu"></sl-icon>
            <h1>${this.agent.name}</h1>
            <scion-status-badge
              status=${this.getStatusVariant(this.agent.status)}
              label=${this.agent.status}
            ></scion-status-badge>
          </div>
          <div class="header-meta">
            <span class="template-badge">
              <sl-icon name="code-square"></sl-icon>
              ${this.agent.template}
            </span>
            ${this.grove
              ? html`
                  <a href="/groves/${this.grove.id}" class="grove-link">
                    <sl-icon name="folder"></sl-icon>
                    ${this.grove.name}
                  </a>
                `
              : ''}
          </div>
        </div>
        <div class="header-actions">
          ${this.agent.status === 'running'
            ? can(this.agent._capabilities, 'stop') ? html`
                <sl-button
                  variant="danger"
                  size="small"
                  outline
                  ?loading=${this.actionLoading}
                  ?disabled=${this.actionLoading}
                  @click=${() => this.handleAction('stop')}
                >
                  <sl-icon slot="prefix" name="stop-circle"></sl-icon>
                  Stop
                </sl-button>
              ` : nothing
            : can(this.agent._capabilities, 'start') ? html`
                <sl-button
                  variant="success"
                  size="small"
                  ?loading=${this.actionLoading}
                  ?disabled=${this.actionLoading}
                  @click=${() => this.handleAction('start')}
                >
                  <sl-icon slot="prefix" name="play-circle"></sl-icon>
                  Start
                </sl-button>
              ` : nothing}
          ${can(this.agent._capabilities, 'delete') ? html`
            <sl-button
              variant="danger"
              size="small"
              ?loading=${this.actionLoading}
              ?disabled=${this.actionLoading}
              @click=${() => this.handleAction('delete')}
            >
              <sl-icon slot="prefix" name="trash"></sl-icon>
              Delete
            </sl-button>
          ` : nothing}
        </div>
      </div>

      ${this.agent.status === 'error' && this.agent.message
        ? html`
            <div class="agent-error-banner">
              <sl-icon name="exclamation-octagon"></sl-icon>
              <div class="error-content">
                <div class="error-title">Agent Error</div>
                <div class="error-message">${this.agent.message}</div>
              </div>
            </div>
          `
        : ''}

      <!-- Quick Actions -->
      <div class="quick-actions" style="margin-bottom: 1.5rem;">
        ${can(this.agent._capabilities, 'attach') ? html`
          <a
            class="quick-action"
            href="/agents/${this.agentId}/terminal"
            ?disabled=${this.agent.status !== 'running'}
          >
            <sl-icon name="terminal"></sl-icon>
            <span>Open Terminal</span>
          </a>
        ` : nothing}
        <div class="quick-action" disabled>
          <sl-icon name="file-text"></sl-icon>
          <span>View Logs</span>
        </div>
        <div class="quick-action" disabled>
          <sl-icon name="gear"></sl-icon>
          <span>Settings</span>
        </div>
      </div>

      <!-- Agent Info -->
      <div class="card">
        <h3 class="card-title">Agent Information</h3>
        <div class="info-grid">
          <div class="info-item">
            <span class="info-label">Agent ID</span>
            <span class="info-value mono">${this.agent.id}</span>
          </div>
          <div class="info-item">
            <span class="info-label">Template</span>
            <span class="info-value">${this.agent.template}</span>
          </div>
          <div class="info-item">
            <span class="info-label">Grove</span>
            <span class="info-value">${this.grove?.name || this.agent.groveId}</span>
          </div>
          <div class="info-item">
            <span class="info-label">Status</span>
            <span class="info-value">${this.agent.status || 'Unknown'}</span>
          </div>
          <div class="info-item">
            <span class="info-label">Created</span>
            <span class="info-value">${this.formatDate(this.agent.createdAt)}</span>
          </div>
          <div class="info-item">
            <span class="info-label">Last Seen</span>
            <span class="info-value"
              >${this.agent.lastSeen
                ? this.formatRelativeTime(this.agent.lastSeen)
                : this.formatRelativeTime(this.agent.updatedAt)}</span
            >
          </div>
        </div>

        ${this.agent.taskSummary
          ? html`
              <h4
                style="margin-top: 1.5rem; margin-bottom: 0.5rem; font-size: 0.875rem; font-weight: 600;"
              >
                Current Task
              </h4>
              <div class="task-summary">${this.agent.taskSummary}</div>
            `
          : ''}
      </div>

      <!-- Status Timeline -->
      <div class="card">
        <h3 class="card-title">Status</h3>
        <div class="status-timeline">
          <div class="timeline-item">
            <div class="timeline-dot ${this.agent.status === 'running' ? 'active' : ''}"></div>
            <div class="timeline-content">
              <div class="timeline-title">
                ${this.agent.status.charAt(0).toUpperCase() + this.agent.status.slice(1)}
              </div>
              <div class="timeline-time">
                Last updated:
                ${this.agent.lastSeen
                  ? this.formatRelativeTime(this.agent.lastSeen)
                  : this.formatRelativeTime(this.agent.updatedAt)}
              </div>
            </div>
          </div>
          <div class="timeline-item">
            <div class="timeline-dot"></div>
            <div class="timeline-content">
              <div class="timeline-title">Created</div>
              <div class="timeline-time">${this.formatDate(this.agent.createdAt)}</div>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  private renderLoading() {
    return html`
      <div class="loading-state">
        <sl-spinner></sl-spinner>
        <p>Loading agent...</p>
      </div>
    `;
  }

  private renderError() {
    return html`
      <a href="/agents" class="back-link">
        <sl-icon name="arrow-left"></sl-icon>
        Back to Agents
      </a>

      <div class="error-state">
        <sl-icon name="exclamation-triangle"></sl-icon>
        <h2>Failed to Load Agent</h2>
        <p>There was a problem loading this agent.</p>
        <div class="error-details">${this.error || 'Agent not found'}</div>
        <sl-button variant="primary" @click=${() => this.loadData()}>
          <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
          Retry
        </sl-button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-page-agent-detail': ScionPageAgentDetail;
  }
}
