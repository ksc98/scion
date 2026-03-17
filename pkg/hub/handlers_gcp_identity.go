// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/store"
	"github.com/google/uuid"
)

// handleGroveGCPServiceAccounts handles /api/v1/groves/{groveId}/gcp-service-accounts
func (s *Server) handleGroveGCPServiceAccounts(w http.ResponseWriter, r *http.Request, groveID string) {
	switch r.Method {
	case http.MethodGet:
		s.listGCPServiceAccounts(w, r, groveID)
	case http.MethodPost:
		s.createGCPServiceAccount(w, r, groveID)
	default:
		MethodNotAllowed(w)
	}
}

// handleGroveGCPServiceAccountByID handles /api/v1/groves/{groveId}/gcp-service-accounts/{id}[/action]
func (s *Server) handleGroveGCPServiceAccountByID(w http.ResponseWriter, r *http.Request, groveID, saPath string) {
	parts := strings.SplitN(saPath, "/", 2)
	saID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "verify" && r.Method == http.MethodPost {
		s.verifyGCPServiceAccount(w, r, groveID, saID)
		return
	}

	if action != "" {
		NotFound(w, "GCP Service Account action")
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getGCPServiceAccount(w, r, groveID, saID)
	case http.MethodDelete:
		s.deleteGCPServiceAccount(w, r, groveID, saID)
	default:
		MethodNotAllowed(w)
	}
}

type createGCPServiceAccountRequest struct {
	Email       string   `json:"email"`
	ProjectID   string   `json:"project_id"`
	DisplayName string   `json:"display_name"`
	Scopes      []string `json:"default_scopes,omitempty"`
}

func (s *Server) createGCPServiceAccount(w http.ResponseWriter, r *http.Request, groveID string) {
	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	var req createGCPServiceAccountRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid request body: "+err.Error(), nil)
		return
	}

	if req.Email == "" || req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "email and project_id are required", nil)
		return
	}

	// Verify grove exists
	if _, err := s.store.GetGrove(r.Context(), groveID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			NotFound(w, "Grove")
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	sa := &store.GCPServiceAccount{
		ID:            uuid.New().String(),
		Scope:         store.ScopeGrove,
		ScopeID:       groveID,
		Email:         req.Email,
		ProjectID:     req.ProjectID,
		DisplayName:   req.DisplayName,
		DefaultScopes: req.Scopes,
		CreatedBy:     user.ID(),
		CreatedAt:     time.Now(),
	}

	if len(sa.DefaultScopes) == 0 {
		sa.DefaultScopes = []string{"https://www.googleapis.com/auth/cloud-platform"}
	}

	if err := s.store.CreateGCPServiceAccount(r.Context(), sa); err != nil {
		if errors.Is(err, store.ErrAlreadyExists) {
			writeError(w, http.StatusConflict, ErrCodeConflict,
				"a service account with this email already exists for this grove", nil)
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	writeJSON(w, http.StatusCreated, sa)
}

// GCPServiceAccountWithCapabilities wraps a service account with its per-item capabilities.
type GCPServiceAccountWithCapabilities struct {
	store.GCPServiceAccount
	Cap *Capabilities `json:"_capabilities,omitempty"`
}

// ListGCPServiceAccountsResponse is the response for listing GCP service accounts.
type ListGCPServiceAccountsResponse struct {
	Items        []GCPServiceAccountWithCapabilities `json:"items"`
	Capabilities *Capabilities                       `json:"_capabilities,omitempty"`
}

func (s *Server) listGCPServiceAccounts(w http.ResponseWriter, r *http.Request, groveID string) {
	ctx := r.Context()
	sas, err := s.store.ListGCPServiceAccounts(ctx, store.GCPServiceAccountFilter{
		Scope:   store.ScopeGrove,
		ScopeID: groveID,
	})
	if err != nil {
		writeErrorFromErr(w, err, "")
		return
	}
	if sas == nil {
		sas = []store.GCPServiceAccount{}
	}

	identity := GetIdentityFromContext(ctx)

	items := make([]GCPServiceAccountWithCapabilities, len(sas))
	if identity != nil {
		resources := make([]Resource, len(sas))
		for i := range sas {
			resources[i] = gcpServiceAccountResource(&sas[i])
		}
		caps := s.authzService.ComputeCapabilitiesBatch(ctx, identity, resources, "gcp_service_account")
		for i := range sas {
			items[i] = GCPServiceAccountWithCapabilities{GCPServiceAccount: sas[i], Cap: caps[i]}
		}
	} else {
		for i := range sas {
			items[i] = GCPServiceAccountWithCapabilities{GCPServiceAccount: sas[i]}
		}
	}

	var scopeCap *Capabilities
	if identity != nil {
		scopeCap = s.authzService.ComputeScopeCapabilities(ctx, identity, "grove", groveID, "gcp_service_account")
	}

	writeJSON(w, http.StatusOK, ListGCPServiceAccountsResponse{
		Items:        items,
		Capabilities: scopeCap,
	})
}

func (s *Server) getGCPServiceAccount(w http.ResponseWriter, r *http.Request, groveID, saID string) {
	sa, err := s.store.GetGCPServiceAccount(r.Context(), saID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			NotFound(w, "GCP Service Account")
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	if sa.ScopeID != groveID {
		NotFound(w, "GCP Service Account")
		return
	}

	writeJSON(w, http.StatusOK, sa)
}

func (s *Server) deleteGCPServiceAccount(w http.ResponseWriter, r *http.Request, groveID, saID string) {
	user := GetUserIdentityFromContext(r.Context())
	if user == nil {
		Forbidden(w)
		return
	}

	sa, err := s.store.GetGCPServiceAccount(r.Context(), saID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			NotFound(w, "GCP Service Account")
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	if sa.ScopeID != groveID {
		NotFound(w, "GCP Service Account")
		return
	}

	if err := s.store.DeleteGCPServiceAccount(r.Context(), saID); err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) verifyGCPServiceAccount(w http.ResponseWriter, r *http.Request, groveID, saID string) {
	sa, err := s.store.GetGCPServiceAccount(r.Context(), saID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			NotFound(w, "GCP Service Account")
			return
		}
		writeErrorFromErr(w, err, "")
		return
	}

	if sa.ScopeID != groveID {
		NotFound(w, "GCP Service Account")
		return
	}

	// Attempt to verify impersonation via the GCP token generator
	if s.gcpTokenGenerator != nil {
		if err := s.gcpTokenGenerator.VerifyImpersonation(r.Context(), sa.Email); err != nil {
			// Persist the failure status
			sa.Verified = false
			sa.VerificationStatus = "failed"
			sa.VerificationError = err.Error()
			_ = s.store.UpdateGCPServiceAccount(r.Context(), sa)

			details := map[string]interface{}{
				"hubServiceAccountEmail": s.gcpTokenGenerator.ServiceAccountEmail(),
				"targetEmail":            sa.Email,
			}
			writeError(w, http.StatusBadGateway, "gcp_verification_failed",
				"Failed to verify impersonation: "+err.Error(), details)
			return
		}
	}

	sa.Verified = true
	sa.VerifiedAt = time.Now()
	sa.VerificationStatus = "verified"
	sa.VerificationError = ""

	if err := s.store.UpdateGCPServiceAccount(r.Context(), sa); err != nil {
		writeErrorFromErr(w, err, "")
		return
	}

	writeJSON(w, http.StatusOK, sa)
}

// handleAgentGCPToken handles POST /api/v1/agent/gcp-token.
// Called by the metadata sidecar to obtain a GCP access token for the agent's assigned SA.
func (s *Server) handleAgentGCPToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowed(w)
		return
	}

	start := time.Now()

	agent := GetAgentFromContext(r.Context())
	if agent == nil {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "agent authentication required", nil)
		return
	}

	// Rate limit check
	if s.gcpTokenRateLimiter != nil && !s.gcpTokenRateLimiter.Allow(agent.Subject) {
		if s.gcpTokenMetrics != nil {
			s.gcpTokenMetrics.RecordRateLimitRejection()
		}
		writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded for GCP token requests", nil)
		return
	}

	// Look up agent's GCP identity assignment
	agentRecord, err := s.store.GetAgent(r.Context(), agent.Subject)
	if err != nil {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "agent not found", nil)
		return
	}

	if agentRecord.AppliedConfig == nil || agentRecord.AppliedConfig.GCPIdentity == nil ||
		agentRecord.AppliedConfig.GCPIdentity.MetadataMode != store.GCPMetadataModeAssign {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "no GCP identity assigned", nil)
		return
	}

	gcpID := agentRecord.AppliedConfig.GCPIdentity

	// Verify the agent's JWT has the correct scope
	requiredScope := GCPTokenScopeForSA(gcpID.ServiceAccountID)
	if !agent.HasScope(requiredScope) {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "missing required GCP token scope", nil)
		return
	}

	// Parse requested scopes (or default)
	var req gcpTokenRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = []string{"https://www.googleapis.com/auth/cloud-platform"}
	}

	if s.gcpTokenGenerator == nil {
		writeError(w, http.StatusServiceUnavailable, "gcp_not_configured",
			"GCP token generation is not configured on this Hub", nil)
		return
	}

	token, err := s.gcpTokenGenerator.GenerateAccessToken(r.Context(), gcpID.ServiceAccountEmail, scopes)
	if err != nil {
		if s.gcpTokenMetrics != nil {
			s.gcpTokenMetrics.RecordAccessTokenRequest(false, time.Since(start))
		}
		LogGCPTokenGeneration(r.Context(), s.auditLogger, GCPTokenEventAccessToken,
			agent.Subject, agentRecord.GroveID, gcpID.ServiceAccountEmail, gcpID.ServiceAccountID, false, err.Error())
		writeError(w, http.StatusBadGateway, "gcp_token_failed",
			"token generation failed: "+err.Error(), nil)
		return
	}

	if s.gcpTokenMetrics != nil {
		s.gcpTokenMetrics.RecordAccessTokenRequest(true, time.Since(start))
	}
	LogGCPTokenGeneration(r.Context(), s.auditLogger, GCPTokenEventAccessToken,
		agent.Subject, agentRecord.GroveID, gcpID.ServiceAccountEmail, gcpID.ServiceAccountID, true, "")
	writeJSON(w, http.StatusOK, token)
}

// handleAgentGCPIdentityToken handles POST /api/v1/agent/gcp-identity-token.
// Called by the metadata sidecar to obtain a GCP OIDC identity token.
func (s *Server) handleAgentGCPIdentityToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		MethodNotAllowed(w)
		return
	}

	start := time.Now()

	agent := GetAgentFromContext(r.Context())
	if agent == nil {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "agent authentication required", nil)
		return
	}

	// Rate limit check
	if s.gcpTokenRateLimiter != nil && !s.gcpTokenRateLimiter.Allow(agent.Subject) {
		if s.gcpTokenMetrics != nil {
			s.gcpTokenMetrics.RecordRateLimitRejection()
		}
		writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded for GCP token requests", nil)
		return
	}

	agentRecord, err := s.store.GetAgent(r.Context(), agent.Subject)
	if err != nil {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "agent not found", nil)
		return
	}

	if agentRecord.AppliedConfig == nil || agentRecord.AppliedConfig.GCPIdentity == nil ||
		agentRecord.AppliedConfig.GCPIdentity.MetadataMode != store.GCPMetadataModeAssign {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "no GCP identity assigned", nil)
		return
	}

	gcpID := agentRecord.AppliedConfig.GCPIdentity
	requiredScope := GCPTokenScopeForSA(gcpID.ServiceAccountID)
	if !agent.HasScope(requiredScope) {
		writeError(w, http.StatusForbidden, ErrCodeForbidden, "missing required GCP token scope", nil)
		return
	}

	var req gcpIdentityTokenRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "invalid request body: "+err.Error(), nil)
		return
	}
	if req.Audience == "" {
		writeError(w, http.StatusBadRequest, ErrCodeInvalidRequest, "audience is required", nil)
		return
	}

	if s.gcpTokenGenerator == nil {
		writeError(w, http.StatusServiceUnavailable, "gcp_not_configured",
			"GCP token generation is not configured on this Hub", nil)
		return
	}

	token, err := s.gcpTokenGenerator.GenerateIDToken(r.Context(), gcpID.ServiceAccountEmail, req.Audience)
	if err != nil {
		if s.gcpTokenMetrics != nil {
			s.gcpTokenMetrics.RecordIDTokenRequest(false, time.Since(start))
		}
		LogGCPTokenGeneration(r.Context(), s.auditLogger, GCPTokenEventIdentityToken,
			agent.Subject, agentRecord.GroveID, gcpID.ServiceAccountEmail, gcpID.ServiceAccountID, false, err.Error())
		writeError(w, http.StatusBadGateway, "gcp_token_failed",
			"identity token generation failed: "+err.Error(), nil)
		return
	}

	if s.gcpTokenMetrics != nil {
		s.gcpTokenMetrics.RecordIDTokenRequest(true, time.Since(start))
	}
	LogGCPTokenGeneration(r.Context(), s.auditLogger, GCPTokenEventIdentityToken,
		agent.Subject, agentRecord.GroveID, gcpID.ServiceAccountEmail, gcpID.ServiceAccountID, true, "")
	writeJSON(w, http.StatusOK, token)
}

type gcpTokenRequest struct {
	Scopes []string `json:"scopes,omitempty"`
}

type gcpIdentityTokenRequest struct {
	Audience string `json:"audience"`
}
