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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoogleCloudPlatform/scion/pkg/config"
)

func TestHandleAdminServerConfig_NonAdmin(t *testing.T) {
	srv := &Server{}

	member := NewAuthenticatedUser("u1", "member@example.com", "Member", "member", "cli")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-config", nil)
	req = req.WithContext(contextWithIdentity(req.Context(), member))
	rr := httptest.NewRecorder()
	srv.handleAdminServerConfig(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHandleAdminServerConfig_Unauthenticated(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-config", nil)
	rr := httptest.NewRecorder()
	srv.handleAdminServerConfig(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHandleAdminServerConfig_MethodNotAllowed(t *testing.T) {
	srv := &Server{}

	admin := NewAuthenticatedUser("u1", "admin@example.com", "Admin", "admin", "cli")
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/server-config", nil)
	req = req.WithContext(contextWithIdentity(req.Context(), admin))
	rr := httptest.NewRecorder()
	srv.handleAdminServerConfig(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestHandleAdminServerConfig_Get(t *testing.T) {
	srv := &Server{}

	admin := NewAuthenticatedUser("u1", "admin@example.com", "Admin", "admin", "cli")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-config", nil)
	req = req.WithContext(contextWithIdentity(req.Context(), admin))
	rr := httptest.NewRecorder()
	srv.handleAdminServerConfig(rr, req)

	// Should return 200 with at least schema_version, even if settings.yaml doesn't exist
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body ServerConfigResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.SchemaVersion == "" {
		t.Error("expected non-empty schema_version")
	}
}

func TestMaskSensitiveFields(t *testing.T) {
	sc := serverConfigForMaskTest()
	resp := &ServerConfigResponse{
		Server: &sc,
	}

	maskSensitiveFields(resp)

	if resp.Server.Auth.DevToken != "********" {
		t.Errorf("expected masked dev token, got %s", resp.Server.Auth.DevToken)
	}
	if resp.Server.Broker.BrokerToken != "********" {
		t.Errorf("expected masked broker token, got %s", resp.Server.Broker.BrokerToken)
	}
	if resp.Server.Database.URL != "********" {
		t.Errorf("expected masked db URL, got %s", resp.Server.Database.URL)
	}
}

func serverConfigForMaskTest() config.V1ServerConfig {
	return config.V1ServerConfig{
		Auth: &config.V1AuthConfig{
			DevToken: "secret-token-123",
		},
		Broker: &config.V1BrokerConfig{
			BrokerToken: "broker-secret-456",
		},
		Database: &config.V1DatabaseConfig{
			Driver: "sqlite",
			URL:    "/path/to/db",
		},
	}
}
