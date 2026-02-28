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
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
)

var msgInterrupt bool
var msgBroadcast bool
var msgAll bool
var msgIn string
var msgAt string

// messageCmd represents the message command
var messageCmd = &cobra.Command{
	Use:     "message [agent] <message>",
	Aliases: []string{"msg"},
	Short:   "Send a message to an agent's harness",
	Long: `Sends a message to a running agent's harness by enqueuing it into the tmux session.
If --broadcast is used, the agent name can be omitted and the message will be sent to all running agents.`,
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: getAgentNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		var agentName string
		var message string

		if msgBroadcast || msgAll {
			message = strings.Join(args, " ")
		} else {
			if len(args) < 2 {
				return fmt.Errorf("agent name and message are required unless --broadcast is used")
			}
			agentName = args[0]
			message = strings.Join(args[1:], " ")
		}

		// Validate scheduling flags
		if msgIn != "" && msgAt != "" {
			return fmt.Errorf("--in and --at are mutually exclusive")
		}
		if (msgIn != "" || msgAt != "") && (msgBroadcast || msgAll) {
			return fmt.Errorf("--in/--at cannot be combined with --broadcast or --all")
		}

		// Check if Hub should be used
		var hubCtx *HubContext
		var err error
		if msgAll {
			// Cross-grove operation: skip sync
			hubCtx, err = CheckHubAvailabilityWithOptions(grovePath, true)
		} else if msgBroadcast {
			// Grove-scoped broadcast: no specific agent
			hubCtx, err = CheckHubAvailability(grovePath)
		} else {
			// Single agent: exclude target from sync requirements
			hubCtx, err = CheckHubAvailabilityForAgent(grovePath, agentName, false)
		}
		if err != nil {
			return err
		}

		// Handle scheduled messages
		if msgIn != "" || msgAt != "" {
			if hubCtx == nil {
				return fmt.Errorf("scheduled messages require Hub mode (use 'scion hub enable' first)")
			}
			return scheduleMessageViaHub(hubCtx, agentName, message, msgInterrupt)
		}

		if hubCtx != nil {
			return sendMessageViaHub(hubCtx, agentName, message, msgInterrupt, msgBroadcast, msgAll)
		}

		// Local mode
		ctx := context.Background()

		effectiveProfile := profile
		if !(msgBroadcast || msgAll) && effectiveProfile == "" {
			effectiveProfile = agent.GetSavedProfile(agentName, grovePath)
		}

		rt := runtime.GetRuntime(grovePath, effectiveProfile)
		mgr := agent.NewManager(rt)

		var targets []string
		if msgBroadcast || msgAll {
			filters := map[string]string{
				"scion.agent": "true",
			}

			if !msgAll {
				projectDir, _ := config.GetResolvedProjectDir(grovePath)
				if projectDir != "" {
					filters["scion.grove_path"] = projectDir
					filters["scion.grove"] = config.GetGroveName(projectDir)
				}
			}

			agents, err := mgr.List(ctx, filters)
			if err != nil {
				return err
			}
			for _, a := range agents {
				status := strings.ToLower(a.ContainerStatus)
				if strings.HasPrefix(status, "up") || status == "running" {
					targets = append(targets, a.Name)
				}
			}
		} else {
			targets = []string{agentName}
		}

		if len(targets) == 0 {
			if msgBroadcast || msgAll {
				fmt.Println("No running agents found to broadcast to.")
				return nil
			}
			return fmt.Errorf("agent '%s' not found or not running", agentName)
		}

		if len(targets) > 1 {
			fmt.Printf("Broadcasting message to %d agents...\n", len(targets))
			var wg sync.WaitGroup
			for _, target := range targets {
				wg.Add(1)
				go func(name string) {
					defer wg.Done()
					if err := mgr.Message(ctx, name, message, msgInterrupt); err != nil {
						fmt.Printf("Warning: failed to send message to agent '%s': %s\n", name, err)
						return
					}
					fmt.Printf("Message delivered to agent '%s'.\n", name)
				}(target)
			}
			wg.Wait()
		} else {
			for _, target := range targets {
				fmt.Printf("Sending message to agent '%s'...\n", target)
				if err := mgr.Message(ctx, target, message, msgInterrupt); err != nil {
					if msgBroadcast || msgAll {
						fmt.Printf("Warning: failed to send message to agent '%s': %s\n", target, err)
						continue
					}
					return err
				}
			}
		}

		return nil
	},
}

func sendMessageViaHub(hubCtx *HubContext, agentName string, message string, interrupt bool, broadcast bool, all bool) error {
	if !isJSONOutput() {
		PrintUsingHub(hubCtx.Endpoint)
	}

	// Resolve the agent service once: grove-scoped or global depending on mode.
	var agentSvc hubclient.AgentService
	if all {
		agentSvc = hubCtx.Client.Agents()
	} else {
		groveID, err := GetGroveID(hubCtx)
		if err != nil {
			return wrapHubError(err)
		}
		agentSvc = hubCtx.Client.GroveAgents(groveID)
	}

	var targets []string

	if broadcast || all {
		// List running agents from Hub
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		opts := &hubclient.ListAgentsOptions{
			Status: "running",
		}

		resp, err := agentSvc.List(ctx, opts)
		if err != nil {
			return wrapHubError(fmt.Errorf("failed to list agents via Hub: %w", err))
		}

		for _, a := range resp.Agents {
			targets = append(targets, a.Name)
		}

		if len(targets) == 0 {
			fmt.Println("No running agents found to broadcast to.")
			return nil
		}
	} else {
		targets = []string{agentName}
	}

	if len(targets) > 1 {
		if !isJSONOutput() {
			fmt.Printf("Broadcasting message to %d agents...\n", len(targets))
		}
		var wg sync.WaitGroup
		for _, target := range targets {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := agentSvc.SendMessage(ctx, name, message, interrupt); err != nil {
					fmt.Printf("Warning: failed to send message to agent '%s' via Hub: %s\n", name, err)
					return
				}
				if !isJSONOutput() {
					fmt.Printf("Message delivered to agent '%s' via Hub.\n", name)
				}
			}(target)
		}
		wg.Wait()
	} else {
		for _, target := range targets {
			if !isJSONOutput() {
				fmt.Printf("Sending message to agent '%s'...\n", target)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

			if err := agentSvc.SendMessage(ctx, target, message, interrupt); err != nil {
				cancel()
				if broadcast || all {
					fmt.Printf("Warning: failed to send message to agent '%s' via Hub: %s\n", target, err)
					continue
				}
				return wrapHubError(fmt.Errorf("failed to send message to agent '%s' via Hub: %w", target, err))
			}
			cancel()

			if !isJSONOutput() {
				fmt.Printf("Message sent to agent '%s' via Hub.\n", target)
			}
		}
	}

	return nil
}

func scheduleMessageViaHub(hubCtx *HubContext, agentName string, message string, interrupt bool) error {
	if !isJSONOutput() {
		PrintUsingHub(hubCtx.Endpoint)
	}

	groveID, err := GetGroveID(hubCtx)
	if err != nil {
		return wrapHubError(err)
	}

	req := &hubclient.CreateScheduledEventRequest{
		EventType: "message",
		AgentName: agentName,
		Message:   message,
		Interrupt: interrupt,
	}

	if msgIn != "" {
		req.FireIn = msgIn
	} else {
		req.FireAt = msgAt
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	evt, err := hubCtx.Client.ScheduledEvents(groveID).Create(ctx, req)
	if err != nil {
		return wrapHubError(fmt.Errorf("failed to schedule message: %w", err))
	}

	if !isJSONOutput() {
		fmt.Printf("Message to agent '%s' scheduled for %s\n", agentName, evt.FireAt.Format(time.RFC3339))
	}

	return nil
}

func init() {
	messageCmd.Flags().BoolVarP(&msgInterrupt, "interrupt", "i", false, "Interrupt the harness before sending the message")
	messageCmd.Flags().BoolVarP(&msgBroadcast, "broadcast", "b", false, "Send the message to all running agents in the current grove")
	messageCmd.Flags().BoolVarP(&msgAll, "all", "a", false, "Send the message to all running agents across all groves")
	messageCmd.Flags().StringVar(&msgIn, "in", "", "Schedule message delivery after a duration (e.g. 30m, 1h)")
	messageCmd.Flags().StringVar(&msgAt, "at", "", "Schedule message delivery at an absolute time (ISO 8601, e.g. 2026-02-28T14:00:00Z)")
	rootCmd.AddCommand(messageCmd)
}
