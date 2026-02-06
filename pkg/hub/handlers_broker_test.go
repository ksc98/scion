//go:build !no_sqlite

package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ptone/scion-agent/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentCreate_HostResolution(t *testing.T) {
	srv, s := testServer(t)
	ctx := context.Background()

	// Create a runtime broker
	broker := &store.RuntimeBroker{
		ID:     "broker_id_123",
		Name:   "My Laptop",
		Slug:   "my-laptop",
		Mode:   store.BrokerModeConnected,
		Status: store.BrokerStatusOnline,
	}
	require.NoError(t, s.CreateRuntimeBroker(ctx, broker))

	// Create a grove
	grove := &store.Grove{
		ID:      "grove_1",
		Slug:    "test-grove",
		Name:    "Test Grove",
		Created: time.Now(),
		Updated: time.Now(),
	}
	require.NoError(t, s.CreateGrove(ctx, grove))

	// Register host as contributor
	contrib := &store.GroveContributor{
		GroveID:  grove.ID,
		BrokerID:   broker.ID,
		BrokerName: broker.Name,
		Mode:     broker.Mode,
		Status:   store.BrokerStatusOnline,
	}
	require.NoError(t, s.AddGroveContributor(ctx, contrib))

	t.Run("Resolve by ID", func(t *testing.T) {
		body := map[string]interface{}{
			"name":          "Agent ID",
			"groveId":       grove.ID,
			"runtimeBrokerId": "broker_id_123",
		}
		rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
		assert.Equal(t, http.StatusCreated, rec.Code)
		
		var resp CreateAgentResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.Equal(t, "broker_id_123", resp.Agent.RuntimeBrokerID)
	})

	t.Run("Resolve by Name", func(t *testing.T) {
		body := map[string]interface{}{
			"name":          "Agent Name",
			"groveId":       grove.ID,
			"runtimeBrokerId": "My Laptop",
		}
		rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
		assert.Equal(t, http.StatusCreated, rec.Code)
		
		var resp CreateAgentResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.Equal(t, "broker_id_123", resp.Agent.RuntimeBrokerID)
	})

	t.Run("Resolve by Slug", func(t *testing.T) {
		body := map[string]interface{}{
			"name":          "Agent Slug",
			"groveId":       grove.ID,
			"runtimeBrokerId": "my-laptop",
		}
		rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
		assert.Equal(t, http.StatusCreated, rec.Code)
		
		var resp CreateAgentResponse
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
		assert.Equal(t, "broker_id_123", resp.Agent.RuntimeBrokerID)
	})

	t.Run("Invalid host", func(t *testing.T) {
		body := map[string]interface{}{
			"name":          "Agent Invalid",
			"groveId":       grove.ID,
			"runtimeBrokerId": "non-existent",
		}
		rec := doRequest(t, srv, http.MethodPost, "/api/v1/agents", body)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}
