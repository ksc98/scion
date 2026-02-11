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

//go:build !no_sqlite

package hub

import (
	"context"
	"testing"

	"github.com/ptone/scion-agent/pkg/store"
)

func TestResolveSecrets(t *testing.T) {
	memStore := createTestStore(t)
	ctx := context.Background()

	// Create test secrets across multiple scopes
	userSecret := &store.Secret{
		ID:             "s1",
		Key:            "API_KEY",
		EncryptedValue: "user-api-key",
		SecretType:     store.SecretTypeEnvironment,
		Target:         "API_KEY",
		Scope:          store.ScopeUser,
		ScopeID:        "user-1",
	}
	groveSecret := &store.Secret{
		ID:             "s2",
		Key:            "DB_PASS",
		EncryptedValue: "grove-db-pass",
		SecretType:     store.SecretTypeEnvironment,
		Target:         "DATABASE_PASSWORD",
		Scope:          store.ScopeGrove,
		ScopeID:        "grove-1",
	}
	// Grove-level override of user API_KEY
	groveOverride := &store.Secret{
		ID:             "s3",
		Key:            "API_KEY",
		EncryptedValue: "grove-api-key",
		SecretType:     store.SecretTypeEnvironment,
		Target:         "API_KEY",
		Scope:          store.ScopeGrove,
		ScopeID:        "grove-1",
	}
	fileSecret := &store.Secret{
		ID:             "s4",
		Key:            "TLS_CERT",
		EncryptedValue: "cert-data",
		SecretType:     store.SecretTypeFile,
		Target:         "/etc/ssl/cert.pem",
		Scope:          store.ScopeUser,
		ScopeID:        "user-1",
	}
	varSecret := &store.Secret{
		ID:             "s5",
		Key:            "CONFIG",
		EncryptedValue: `{"key":"val"}`,
		SecretType:     store.SecretTypeVariable,
		Target:         "config",
		Scope:          store.ScopeUser,
		ScopeID:        "user-1",
	}

	for _, s := range []*store.Secret{userSecret, groveSecret, groveOverride, fileSecret, varSecret} {
		if err := memStore.CreateSecret(ctx, s); err != nil {
			t.Fatalf("failed to create test secret %s: %v", s.Key, err)
		}
	}

	// Create dispatcher
	mockClient := &mockRuntimeBrokerClient{}
	dispatcher := NewHTTPAgentDispatcherWithClient(memStore, mockClient, false)

	agent := &store.Agent{
		ID:      "agent-1",
		Name:    "test-agent",
		OwnerID: "user-1",
		GroveID: "grove-1",
	}

	resolved, err := dispatcher.resolveSecrets(ctx, agent)
	if err != nil {
		t.Fatalf("resolveSecrets failed: %v", err)
	}

	// Build a map for easier assertions
	byName := make(map[string]ResolvedSecret)
	for _, rs := range resolved {
		byName[rs.Name] = rs
	}

	// API_KEY should be overridden by grove scope
	apiKey, ok := byName["API_KEY"]
	if !ok {
		t.Fatal("expected API_KEY in resolved secrets")
	}
	if apiKey.Value != "grove-api-key" {
		t.Errorf("expected API_KEY value from grove scope %q, got %q", "grove-api-key", apiKey.Value)
	}
	if apiKey.Source != store.ScopeGrove {
		t.Errorf("expected API_KEY source %q, got %q", store.ScopeGrove, apiKey.Source)
	}

	// DB_PASS should come from grove scope
	dbPass, ok := byName["DB_PASS"]
	if !ok {
		t.Fatal("expected DB_PASS in resolved secrets")
	}
	if dbPass.Value != "grove-db-pass" {
		t.Errorf("expected DB_PASS value %q, got %q", "grove-db-pass", dbPass.Value)
	}
	if dbPass.Target != "DATABASE_PASSWORD" {
		t.Errorf("expected DB_PASS target %q, got %q", "DATABASE_PASSWORD", dbPass.Target)
	}

	// TLS_CERT should be a file type from user scope
	cert, ok := byName["TLS_CERT"]
	if !ok {
		t.Fatal("expected TLS_CERT in resolved secrets")
	}
	if cert.Type != store.SecretTypeFile {
		t.Errorf("expected TLS_CERT type %q, got %q", store.SecretTypeFile, cert.Type)
	}
	if cert.Target != "/etc/ssl/cert.pem" {
		t.Errorf("expected TLS_CERT target %q, got %q", "/etc/ssl/cert.pem", cert.Target)
	}

	// CONFIG should be a variable type
	config, ok := byName["CONFIG"]
	if !ok {
		t.Fatal("expected CONFIG in resolved secrets")
	}
	if config.Type != store.SecretTypeVariable {
		t.Errorf("expected CONFIG type %q, got %q", store.SecretTypeVariable, config.Type)
	}

	// Total count: API_KEY, DB_PASS, TLS_CERT, CONFIG = 4
	if len(resolved) != 4 {
		t.Errorf("expected 4 resolved secrets, got %d", len(resolved))
	}
}

func TestResolveSecrets_NoOwner(t *testing.T) {
	memStore := createTestStore(t)
	ctx := context.Background()

	mockClient := &mockRuntimeBrokerClient{}
	dispatcher := NewHTTPAgentDispatcherWithClient(memStore, mockClient, false)

	agent := &store.Agent{
		ID:   "agent-1",
		Name: "test-agent",
	}

	resolved, err := dispatcher.resolveSecrets(ctx, agent)
	if err != nil {
		t.Fatalf("resolveSecrets failed: %v", err)
	}

	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved secrets for agent with no owner, got %d", len(resolved))
	}
}
