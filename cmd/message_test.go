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

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// messageTestState captures and restores package-level vars for test isolation.
type messageTestState struct {
	grovePath string
	noHub     bool
}

func saveMessageTestState() messageTestState {
	return messageTestState{
		grovePath: grovePath,
		noHub:     noHub,
	}
}

func (s messageTestState) restore() {
	grovePath = s.grovePath
	noHub = s.noHub
}

// messageMockServer creates a mock Hub server that handles grove-scoped
// agent message and list requests. Returns the server, a pointer to a slice of
// messages sent (as agent-name strings), and a configurable list of agents
// returned by the list endpoint.
type sentMessage struct {
	AgentName string
	Message   string
	Interrupt bool
}

func newMessageMockHubServer(t *testing.T, groveID string, runningAgents []hubclient.Agent) (*httptest.Server, *[]sentMessage) {
	t.Helper()
	var sent []sentMessage
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})

		case r.Method == http.MethodGet && (r.URL.Path == "/api/v1/groves/"+groveID+"/agents" || r.URL.Path == "/api/v1/agents"):
			// List agents endpoint
			json.NewEncoder(w).Encode(map[string]interface{}{
				"agents": runningAgents,
			})

		case r.Method == http.MethodPost:
			// Extract agent name from path: /api/v1/groves/<groveID>/agents/<name>/message
			// or /api/v1/agents/<name>/message
			var agentName string
			grovePrefix := "/api/v1/groves/" + groveID + "/agents/"
			globalPrefix := "/api/v1/agents/"
			path := r.URL.Path
			if len(path) > len(grovePrefix) && path[:len(grovePrefix)] == grovePrefix {
				rest := path[len(grovePrefix):]
				// rest is "<name>/message"
				agentName = rest[:len(rest)-len("/message")]
			} else if len(path) > len(globalPrefix) && path[:len(globalPrefix)] == globalPrefix {
				rest := path[len(globalPrefix):]
				agentName = rest[:len(rest)-len("/message")]
			}

			var body struct {
				Message   string `json:"message"`
				Interrupt bool   `json:"interrupt"`
			}
			json.NewDecoder(r.Body).Decode(&body)

			mu.Lock()
			sent = append(sent, sentMessage{
				AgentName: agentName,
				Message:   body.Message,
				Interrupt: body.Interrupt,
			})
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return server, &sent
}

func TestSendMessageViaHub_SingleAgent(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-single"
	server, sent := newMessageMockHubServer(t, groveID, nil)
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	err = sendMessageViaHub(hubCtx, "my-agent", "hello world", false, false, false)
	require.NoError(t, err)

	require.Len(t, *sent, 1)
	assert.Equal(t, "my-agent", (*sent)[0].AgentName)
	assert.Equal(t, "hello world", (*sent)[0].Message)
	assert.False(t, (*sent)[0].Interrupt)
}

func TestSendMessageViaHub_SingleAgentInterrupt(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-int"
	server, sent := newMessageMockHubServer(t, groveID, nil)
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	err = sendMessageViaHub(hubCtx, "my-agent", "urgent", true, false, false)
	require.NoError(t, err)

	require.Len(t, *sent, 1)
	assert.Equal(t, "my-agent", (*sent)[0].AgentName)
	assert.True(t, (*sent)[0].Interrupt)
}

func TestSendMessageViaHub_Broadcast(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-broadcast"
	agents := []hubclient.Agent{
		{Name: "agent-1", Status: "running"},
		{Name: "agent-2", Status: "running"},
		{Name: "agent-3", Status: "running"},
	}
	server, sent := newMessageMockHubServer(t, groveID, agents)
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	err = sendMessageViaHub(hubCtx, "", "broadcast msg", false, true, false)
	require.NoError(t, err)

	require.Len(t, *sent, 3)
	names := make([]string, len(*sent))
	for i, s := range *sent {
		names[i] = s.AgentName
		assert.Equal(t, "broadcast msg", s.Message)
	}
	assert.ElementsMatch(t, []string{"agent-1", "agent-2", "agent-3"}, names)
}

func TestSendMessageViaHub_BroadcastNoAgents(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-empty"
	server, sent := newMessageMockHubServer(t, groveID, []hubclient.Agent{})
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	err = sendMessageViaHub(hubCtx, "", "hello", false, true, false)
	require.NoError(t, err)

	// No messages should be sent
	assert.Len(t, *sent, 0)
}

func TestSendMessageViaHub_All(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-all"
	agents := []hubclient.Agent{
		{Name: "grove1-agent", Status: "running", GroveID: "grove-a"},
		{Name: "grove2-agent", Status: "running", GroveID: "grove-b"},
	}
	server, sent := newMessageMockHubServer(t, groveID, agents)
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	// For --all mode, we use global agent service (no grove scoping)
	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
	}

	err = sendMessageViaHub(hubCtx, "", "all msg", false, false, true)
	require.NoError(t, err)

	require.Len(t, *sent, 2)
	for _, s := range *sent {
		assert.Equal(t, "all msg", s.Message)
	}
}

func TestSendMessageViaHub_SingleAgentError(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-err"

	// Server that returns 500 for message requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/healthz" {
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"code":    "internal",
				"message": "internal error",
			},
		})
	}))
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	err = sendMessageViaHub(hubCtx, "my-agent", "hello", false, false, false)
	require.Error(t, err, "single-agent message failure should return an error")
}

func TestScheduleMessageFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		at      string
		broadcast bool
		all     bool
		wantErr string
	}{
		{
			name:    "in and at are mutually exclusive",
			in:      "30m",
			at:      "2030-01-01T00:00:00Z",
			wantErr: "--in and --at are mutually exclusive",
		},
		{
			name:      "in with broadcast not allowed",
			in:        "30m",
			broadcast: true,
			wantErr:   "--in/--at cannot be combined with --broadcast or --all",
		},
		{
			name:    "at with all not allowed",
			at:      "2030-01-01T00:00:00Z",
			all:     true,
			wantErr: "--in/--at cannot be combined with --broadcast or --all",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Save and restore global state
			origIn, origAt := msgIn, msgAt
			origBroadcast, origAll := msgBroadcast, msgAll
			defer func() {
				msgIn, msgAt = origIn, origAt
				msgBroadcast, msgAll = origBroadcast, origAll
			}()

			msgIn = tc.in
			msgAt = tc.at
			msgBroadcast = tc.broadcast
			msgAll = tc.all

			// Build args appropriate for the flag combination
			var args []string
			if tc.broadcast || tc.all {
				args = []string{"hello"}
			} else {
				args = []string{"agent1", "hello"}
			}

			err := messageCmd.RunE(messageCmd, args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSendMessageViaHub_BroadcastPartialFailure(t *testing.T) {
	orig := saveMessageTestState()
	defer orig.restore()

	groveID := "grove-msg-partial"
	agents := []hubclient.Agent{
		{Name: "good-agent", Status: "running"},
		{Name: "bad-agent", Status: "running"},
	}

	var sent []sentMessage
	var mu sync.Mutex
	// Server that succeeds for good-agent but fails for bad-agent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/healthz":
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
		case r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{"agents": agents})
		case r.Method == http.MethodPost:
			prefix := "/api/v1/groves/" + groveID + "/agents/"
			rest := r.URL.Path[len(prefix):]
			agentName := rest[:len(rest)-len("/message")]

			if agentName == "bad-agent" {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"code": "internal", "message": "error"},
				})
				return
			}

			var body struct {
				Message   string `json:"message"`
				Interrupt bool   `json:"interrupt"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			sent = append(sent, sentMessage{AgentName: agentName, Message: body.Message})
			mu.Unlock()

			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := hubclient.New(server.URL)
	require.NoError(t, err)

	hubCtx := &HubContext{
		Client:   client,
		Endpoint: server.URL,
		GroveID:  groveID,
	}

	// Broadcast should not return an error on partial failure
	err = sendMessageViaHub(hubCtx, "", "test", false, true, false)
	require.NoError(t, err)

	// Only the good agent should have received the message
	assert.Len(t, sent, 1)
	assert.Equal(t, "good-agent", sent[0].AgentName)
}
