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
 * GCP Service Account List Component
 *
 * CRUD component for managing GCP service accounts at the grove level.
 * Follows the same patterns as scion-secret-list.
 */

import { LitElement, html, css, nothing } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';

import type {
  GCPServiceAccount,
  GCPVerificationStatus,
  Capabilities,
} from '../../shared/types.js';
import { can } from '../../shared/types.js';
import { apiFetch } from '../../client/api.js';
import { resourceStyles } from './resource-styles.js';

@customElement('scion-gcp-service-account-list')
export class ScionGCPServiceAccountList extends LitElement {
  @property() groveId = '';
  @property({ type: Boolean }) compact = false;

  @state() private accounts: GCPServiceAccount[] = [];
  @state() private loading = true;
  @state() private error: string | null = null;
  @state() private listCapabilities: Capabilities | undefined;

  // Add dialog state
  @state() private dialogOpen = false;
  @state() private dialogEmail = '';
  @state() private dialogProjectId = '';
  @state() private dialogDisplayName = '';
  @state() private dialogLoading = false;
  @state() private dialogError: string | null = null;

  // Action state
  @state() private verifyingId: string | null = null;
  @state() private deletingId: string | null = null;

  static override styles = [
    resourceStyles,
    css`
      .status-cell-inline {
        display: inline-flex;
        align-items: center;
        gap: 0.25rem;
      }
    `,
  ];

  override connectedCallback(): void {
    super.connectedCallback();
    void this.loadAccounts();
  }

  private async loadAccounts(): Promise<void> {
    this.loading = true;
    this.error = null;

    try {
      const response = await apiFetch(`/api/v1/groves/${this.groveId}/gcp-service-accounts`);

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      const data = (await response.json()) as
        | {
            items?: GCPServiceAccount[];
            _capabilities?: Capabilities;
          }
        | GCPServiceAccount[];

      if (Array.isArray(data)) {
        this.accounts = data;
      } else {
        this.accounts = data.items || [];
        this.listCapabilities = data._capabilities;
      }
    } catch (err) {
      console.error('Failed to load GCP service accounts:', err);
      this.error = err instanceof Error ? err.message : 'Failed to load service accounts';
    } finally {
      this.loading = false;
    }
  }

  private openAddDialog(): void {
    this.dialogEmail = '';
    this.dialogProjectId = '';
    this.dialogDisplayName = '';
    this.dialogError = null;
    this.dialogOpen = true;
  }

  private closeDialog(): void {
    this.dialogOpen = false;
  }

  private async handleAdd(e: Event): Promise<void> {
    e.preventDefault();

    const email = this.dialogEmail.trim();
    if (!email) {
      this.dialogError = 'Service account email is required';
      return;
    }

    const projectId = this.dialogProjectId.trim();
    if (!projectId) {
      this.dialogError = 'GCP project ID is required';
      return;
    }

    this.dialogLoading = true;
    this.dialogError = null;

    try {
      const body: Record<string, unknown> = {
        email,
        projectId,
      };
      if (this.dialogDisplayName.trim()) {
        body.displayName = this.dialogDisplayName.trim();
      }

      const response = await apiFetch(`/api/v1/groves/${this.groveId}/gcp-service-accounts`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `HTTP ${response.status}: ${response.statusText}`);
      }

      this.closeDialog();
      await this.loadAccounts();
    } catch (err) {
      console.error('Failed to add service account:', err);
      this.dialogError = err instanceof Error ? err.message : 'Failed to add service account';
    } finally {
      this.dialogLoading = false;
    }
  }

  private async handleVerify(account: GCPServiceAccount): Promise<void> {
    this.verifyingId = account.id;

    try {
      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/gcp-service-accounts/${account.id}/verify`,
        { method: 'POST' }
      );

      if (!response.ok) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(
          errorData.message ||
            'Verification failed. Ensure the Hub service account has serviceAccountTokenCreator role on this SA.'
        );
      }

      await this.loadAccounts();
    } catch (err) {
      console.error('Failed to verify service account:', err);
      alert(err instanceof Error ? err.message : 'Verification failed');
    } finally {
      this.verifyingId = null;
    }
  }

  private async handleDelete(account: GCPServiceAccount, event?: MouseEvent): Promise<void> {
    if (
      !event?.altKey &&
      !confirm(`Delete service account "${account.email}"? This cannot be undone.`)
    ) {
      return;
    }

    this.deletingId = account.id;

    try {
      const response = await apiFetch(
        `/api/v1/groves/${this.groveId}/gcp-service-accounts/${account.id}`,
        { method: 'DELETE' }
      );

      if (!response.ok && response.status !== 204) {
        const errorData = (await response.json().catch(() => ({}))) as { message?: string };
        throw new Error(errorData.message || `Failed to delete (HTTP ${response.status})`);
      }

      await this.loadAccounts();
    } catch (err) {
      console.error('Failed to delete service account:', err);
      alert(err instanceof Error ? err.message : 'Failed to delete');
    } finally {
      this.deletingId = null;
    }
  }

  private getVerificationStatus(account: GCPServiceAccount): GCPVerificationStatus {
    if (account.verificationStatus) return account.verificationStatus;
    return account.verified ? 'verified' : 'unverified';
  }

  private formatRelativeTime(dateString: string): string {
    try {
      const date = new Date(dateString);
      if (isNaN(date.getTime())) return dateString;
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

  // ── Rendering ────────────────────────────────────────────────────────

  override render() {
    if (this.compact) {
      return this.renderCompact();
    }
    return this.renderFull();
  }

  private renderFull() {
    if (this.loading) {
      return html`
        <div class="loading-state">
          <sl-spinner></sl-spinner>
          <p>Loading service accounts...</p>
        </div>
      `;
    }

    if (this.error) {
      return html`
        <div class="error-state">
          <sl-icon name="exclamation-triangle"></sl-icon>
          <h2>Failed to Load</h2>
          <p>There was a problem loading GCP service accounts.</p>
          <div class="error-details">${this.error}</div>
          <sl-button variant="primary" @click=${() => this.loadAccounts()}>
            <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
            Retry
          </sl-button>
        </div>
      `;
    }

    return html`
      ${can(this.listCapabilities, 'create')
        ? html`
            <div class="list-header">
              <sl-button variant="primary" @click=${this.openAddDialog}>
                <sl-icon slot="prefix" name="plus-lg"></sl-icon>
                Add Service Account
              </sl-button>
            </div>
          `
        : ''}
      ${this.accounts.length === 0 ? this.renderEmpty() : this.renderTable()} ${this.renderDialog()}
    `;
  }

  private renderCompact() {
    return html`
      <div class="section compact">
        <div class="section-header">
          <div class="section-header-info">
            <h2>GCP Service Accounts</h2>
            <p>Manage GCP service accounts for agent identity assignment in this grove.</p>
          </div>
          ${can(this.listCapabilities, 'create')
            ? html`
                <sl-button variant="primary" size="small" @click=${this.openAddDialog}>
                  <sl-icon slot="prefix" name="plus-lg"></sl-icon>
                  Add Service Account
                </sl-button>
              `
            : ''}
        </div>

        ${this.loading
          ? html`<div class="section-loading">
              <sl-spinner></sl-spinner> Loading service accounts...
            </div>`
          : this.error
            ? html`<div class="section-error">
                <span>${this.error}</span>
                <sl-button size="small" @click=${() => this.loadAccounts()}>
                  <sl-icon slot="prefix" name="arrow-clockwise"></sl-icon>
                  Retry
                </sl-button>
              </div>`
            : this.accounts.length === 0
              ? this.renderEmpty()
              : this.renderTable()}
      </div>
      ${this.renderDialog()}
    `;
  }

  private renderTable() {
    return html`
      <div class="table-container">
        <table>
          <thead>
            <tr>
              <th>Email</th>
              <th class="hide-mobile">Project</th>
              <th class="hide-mobile">Name</th>
              <th>Status</th>
              <th class="actions-cell"></th>
            </tr>
          </thead>
          <tbody>
            ${this.accounts.map((account) => this.renderRow(account))}
          </tbody>
        </table>
      </div>
    `;
  }

  private renderRow(account: GCPServiceAccount) {
    const isDeleting = this.deletingId === account.id;
    const isVerifying = this.verifyingId === account.id;

    return html`
      <tr>
        <td class="key-cell">
          <div class="key-info">
            <div
              class="key-icon"
              style="background: var(--sl-color-primary-100, #dbeafe); color: var(--sl-color-primary-600, #2563eb);"
            >
              <sl-icon name="shield-lock"></sl-icon>
            </div>
            ${account.email}
          </div>
        </td>
        <td class="hide-mobile">
          <span class="meta-text">${account.projectId}</span>
        </td>
        <td class="hide-mobile">
          <span class="meta-text">${account.displayName || '\u2014'}</span>
        </td>
        <td>${this.renderStatus(account, isVerifying, isDeleting)}</td>
        <td class="actions-cell">
          ${can(account._capabilities, 'delete')
            ? html`
                <sl-icon-button
                  name="trash"
                  label="Delete"
                  ?disabled=${isDeleting || isVerifying}
                  @click=${(e: MouseEvent) => this.handleDelete(account, e)}
                ></sl-icon-button>
              `
            : ''}
        </td>
      </tr>
    `;
  }

  private renderStatus(
    account: GCPServiceAccount,
    isVerifying: boolean,
    isDeleting: boolean
  ) {
    const status = this.getVerificationStatus(account);

    const badge =
      status === 'verified'
        ? html`<sl-badge variant="success">
            Verified
            ${account.verifiedAt
              ? html`<sl-tooltip
                  content="Verified ${this.formatRelativeTime(account.verifiedAt)}"
                  ><span>\u2713</span></sl-tooltip
                >`
              : ''}
          </sl-badge>`
        : status === 'failed'
          ? html`<sl-tooltip
              content=${account.verificationError ||
              'Hub service account lacks serviceAccountTokenCreator role on this SA.'}
            >
              <sl-badge variant="danger">Failed</sl-badge>
            </sl-tooltip>`
          : html`<sl-badge variant="warning">Unverified</sl-badge>`;

    const canVerify = can(account._capabilities, 'verify');

    return html`
      <div class="status-cell-inline">
        ${badge}
        ${canVerify
          ? html`
              <sl-icon-button
                name="arrow-clockwise"
                label="Re-check verification"
                style="font-size: 0.875rem;"
                ?disabled=${isVerifying || isDeleting}
                @click=${() => this.handleVerify(account)}
              ></sl-icon-button>
            `
          : ''}
        ${isVerifying ? html`<sl-spinner style="font-size: 0.75rem;"></sl-spinner>` : ''}
      </div>
    `;
  }

  private renderEmpty() {
    return html`
      <div class="empty-state">
        <sl-icon name="shield-lock"></sl-icon>
        <h3>No GCP Service Accounts</h3>
        <p>Register GCP service accounts to assign cloud identities to agents in this grove.</p>
        ${can(this.listCapabilities, 'create')
          ? html`
              <sl-button variant="primary" size="small" @click=${this.openAddDialog}>
                <sl-icon slot="prefix" name="plus-lg"></sl-icon>
                Add Service Account
              </sl-button>
            `
          : ''}
      </div>
    `;
  }

  private renderDialog() {
    return html`
      <sl-dialog
        label="Add GCP Service Account"
        ?open=${this.dialogOpen}
        @sl-request-close=${this.closeDialog}
      >
        <form class="dialog-form" @submit=${this.handleAdd}>
          <sl-input
            label="Service Account Email"
            placeholder="e.g. agent-worker@project.iam.gserviceaccount.com"
            value=${this.dialogEmail}
            @sl-input=${(e: Event) => {
              this.dialogEmail = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="GCP Project ID"
            placeholder="e.g. my-project-123"
            value=${this.dialogProjectId}
            @sl-input=${(e: Event) => {
              this.dialogProjectId = (e.target as HTMLInputElement).value;
            }}
            required
          ></sl-input>

          <sl-input
            label="Display Name"
            placeholder="Optional human-friendly label"
            value=${this.dialogDisplayName}
            @sl-input=${(e: Event) => {
              this.dialogDisplayName = (e.target as HTMLInputElement).value;
            }}
          ></sl-input>

          <div class="dialog-hint">
            <sl-icon name="info-circle"></sl-icon>
            The Hub will automatically attempt to verify the service account after creation.
          </div>

          ${this.dialogError ? html`<div class="dialog-error">${this.dialogError}</div>` : nothing}
        </form>

        <sl-button
          slot="footer"
          variant="default"
          @click=${this.closeDialog}
          ?disabled=${this.dialogLoading}
        >
          Cancel
        </sl-button>
        <sl-button
          slot="footer"
          variant="primary"
          ?loading=${this.dialogLoading}
          ?disabled=${this.dialogLoading}
          @click=${this.handleAdd}
        >
          Add
        </sl-button>
      </sl-dialog>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'scion-gcp-service-account-list': ScionGCPServiceAccountList;
  }
}
