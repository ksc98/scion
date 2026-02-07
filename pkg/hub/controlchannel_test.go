package hub

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlChannelManager_OnDisconnectCallback(t *testing.T) {
	mgr := NewControlChannelManager(DefaultControlChannelConfig())

	var mu sync.Mutex
	var receivedBrokerID string
	done := make(chan struct{})

	mgr.SetOnDisconnect(func(brokerID string) {
		mu.Lock()
		defer mu.Unlock()
		receivedBrokerID = brokerID
		close(done)
	})

	// Manually add a connection entry so removeConnection has something to remove
	mgr.mu.Lock()
	mgr.connections["broker-1"] = &BrokerConnection{brokerID: "broker-1"}
	mgr.mu.Unlock()

	mgr.removeConnection("broker-1")

	// Wait for async callback
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onDisconnect callback")
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "broker-1", receivedBrokerID)

	// Verify connection was removed
	require.False(t, mgr.IsConnected("broker-1"))
}

func TestControlChannelManager_OnDisconnectCallback_NilSafe(t *testing.T) {
	mgr := NewControlChannelManager(DefaultControlChannelConfig())

	// Don't set any callback - verify removeConnection doesn't panic
	mgr.mu.Lock()
	mgr.connections["broker-2"] = &BrokerConnection{brokerID: "broker-2"}
	mgr.mu.Unlock()

	// This should not panic
	mgr.removeConnection("broker-2")

	require.False(t, mgr.IsConnected("broker-2"))
}
