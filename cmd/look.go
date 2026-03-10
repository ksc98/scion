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
	"os"
	"strings"
	"time"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	lookPlain    bool
	lookFull     bool
	lookNumLines int
)

// buildLookCmd builds the shell command for tmux capture-pane based on flags.
func buildLookCmd(plain, full bool, numLines int) []string {
	captureArgs := "-p"
	if !plain {
		captureArgs += "e"
	}
	if numLines > 0 {
		captureArgs += fmt.Sprintf("S -%d", numLines)
	} else if full {
		captureArgs += "S -"
	}

	shellCmd := fmt.Sprintf(
		`tmux -S $(find /tmp -name "default" -type s | head -n 1) capture-pane %s -t scion`,
		captureArgs,
	)
	return []string{"/bin/sh", "-c", shellCmd}
}

// lookCmd represents the look command
var lookCmd = &cobra.Command{
	Use:               "look <agent>",
	Short:             "View an agent's current terminal output",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: getAgentNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentName := api.Slugify(args[0])

		execCmd := buildLookCmd(lookPlain, lookFull, lookNumLines)

		// Check if Hub is enabled
		hubCtx, err := CheckHubAvailabilityForAgent(grovePath, agentName, false)
		if err != nil {
			return err
		}

		if hubCtx != nil {
			return lookViaHub(hubCtx, agentName, execCmd)
		}

		effectiveProfile := profile
		if effectiveProfile == "" {
			effectiveProfile = agent.GetSavedRuntime(agentName, grovePath)
		}

		rt := runtime.GetRuntime(grovePath, effectiveProfile)

		output, err := rt.Exec(context.Background(), agentName, execCmd)
		if err != nil {
			return fmt.Errorf("failed to capture terminal output for agent '%s': %w", agentName, err)
		}

		printLookOutput(output)
		return nil
	},
}

// printLookOutput prints the captured terminal output, optionally wrapped
// with top/bottom borders sized to the current terminal width.
func printLookOutput(output string) {
	if IsNonInteractive() || lookPlain {
		fmt.Print(output)
		return
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		// Fallback: no border when we can't determine terminal width.
		fmt.Print(output)
		return
	}

	border := func(ch string) string {
		return strings.Repeat(ch, width)
	}

	fmt.Println(border("⌄"))
	fmt.Println()
	fmt.Print(output)
	fmt.Println()
	fmt.Println()
	fmt.Println(border("^"))
}

func lookViaHub(hubCtx *HubContext, agentName string, execCmd []string) error {
	PrintUsingHub(hubCtx.Endpoint)

	groveID, err := GetGroveID(hubCtx)
	if err != nil {
		return wrapHubError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := hubCtx.Client.GroveAgents(groveID).Exec(ctx, agentName, execCmd, 10)
	if err != nil {
		return wrapHubError(fmt.Errorf("failed to capture terminal output for agent '%s': %w", agentName, err))
	}

	printLookOutput(resp.Output)
	return nil
}

func init() {
	lookCmd.Flags().BoolVar(&lookPlain, "plain", false, "Strip ANSI escape sequences from output")
	lookCmd.Flags().BoolVar(&lookFull, "full", false, "Capture the full scrollback history")
	lookCmd.Flags().IntVarP(&lookNumLines, "num-lines", "n", 0, "Number of scrollback lines to capture (tail)")
	lookCmd.MarkFlagsMutuallyExclusive("full", "num-lines")
	rootCmd.AddCommand(lookCmd)
}
