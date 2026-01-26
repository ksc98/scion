package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hubclient"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/util"
	"github.com/ptone/scion-agent/pkg/version"
	"github.com/spf13/cobra"
)

var (
	hubRegisterMode   string
	hubForceRegister  bool
	hubOutputJSON     bool
	hubDeregisterHost bool
)

// hubCmd represents the hub command
var hubCmd = &cobra.Command{
	Use:   "hub",
	Short: "Interact with the Scion Hub",
	Long: `Commands for interacting with a remote Scion Hub.

The Hub provides centralized coordination for groves, agents, and templates
across multiple runtime hosts.

Configure the Hub endpoint via:
  - SCION_HUB_ENDPOINT environment variable
  - hub.endpoint in settings.yaml
  - --hub flag on any command`,
}

// hubStatusCmd shows Hub connection status
var hubStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Hub connection status",
	Long:  `Show the current Hub connection status and configuration.`,
	RunE:  runHubStatus,
}

// hubRegisterCmd registers this host with the Hub
var hubRegisterCmd = &cobra.Command{
	Use:   "register [grove-path]",
	Short: "Register this host with the Hub",
	Long: `Register this host as a runtime contributor for a grove.

If grove-path is not specified, uses the current project grove or global grove.
The host is identified by its hostname to prevent duplicate registrations.

This command will:
1. Create or update the grove in the Hub (matched by git remote or name)
2. Register this host as a contributor to the grove (using hostname as identifier)
3. Save the returned host token for future authentication

Examples:
  # Register the current project grove
  scion hub register

  # Register the global grove
  scion hub register --global`,
	RunE: runHubRegister,
}

// hubDeregisterCmd removes this host from the Hub
var hubDeregisterCmd = &cobra.Command{
	Use:   "deregister",
	Short: "Remove this host from the Hub",
	Long: `Remove this host from the Hub.

This command will:
1. Remove this host from all groves it contributes to
2. Clear the stored host token

Use --host-only to only remove the host record without affecting grove contributions.`,
	RunE: runHubDeregister,
}

// hubGrovesCmd lists groves on the Hub
var hubGrovesCmd = &cobra.Command{
	Use:   "groves",
	Short: "List groves on the Hub",
	Long:  `List groves registered on the Hub that you have access to.`,
	RunE:  runHubGroves,
}

// hubHostsCmd lists runtime hosts on the Hub
var hubHostsCmd = &cobra.Command{
	Use:   "hosts",
	Short: "List runtime hosts on the Hub",
	Long:  `List runtime hosts registered on the Hub.`,
	RunE:  runHubHosts,
}

// hubEnableCmd enables Hub integration
var hubEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable Hub integration",
	Long: `Enable Hub integration for agent operations.

When enabled, agent operations (create, start, delete) will be routed through
the Hub API instead of being performed locally. This allows centralized
coordination of agents across multiple runtime hosts.

The Hub endpoint must be configured before enabling:
  - SCION_HUB_ENDPOINT environment variable
  - hub.endpoint in settings.yaml
  - --hub flag on any command`,
	RunE: runHubEnable,
}

// hubDisableCmd disables Hub integration
var hubDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable Hub integration",
	Long: `Disable Hub integration for agent operations.

When disabled, agent operations are performed locally on this host.
The Hub configuration is preserved and can be re-enabled later.`,
	RunE: runHubDisable,
}

func init() {
	rootCmd.AddCommand(hubCmd)
	hubCmd.AddCommand(hubStatusCmd)
	hubCmd.AddCommand(hubRegisterCmd)
	hubCmd.AddCommand(hubDeregisterCmd)
	hubCmd.AddCommand(hubGrovesCmd)
	hubCmd.AddCommand(hubHostsCmd)
	hubCmd.AddCommand(hubEnableCmd)
	hubCmd.AddCommand(hubDisableCmd)

	// Register flags
	hubRegisterCmd.Flags().StringVar(&hubRegisterMode, "mode", "connected", "Registration mode (connected, read-only)")
	hubRegisterCmd.Flags().BoolVar(&hubForceRegister, "force", false, "Force re-registration even if already registered")

	// Deregister flags
	hubDeregisterCmd.Flags().BoolVar(&hubDeregisterHost, "host-only", false, "Only remove host record, not grove contributions")

	// Common flags
	hubStatusCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
	hubGrovesCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
	hubHostsCmd.Flags().BoolVar(&hubOutputJSON, "json", false, "Output in JSON format")
}

func getHubClient(settings *config.Settings) (hubclient.Client, error) {
	endpoint := GetHubEndpoint(settings)
	if endpoint == "" {
		return nil, fmt.Errorf("Hub endpoint not configured. Set SCION_HUB_ENDPOINT or use --hub flag")
	}

	debug := os.Getenv("SCION_DEBUG") != ""

	var opts []hubclient.Option

	// Add authentication - check in priority order
	// Note: HostToken is intentionally NOT used here. HostTokens are for host-level
	// operations (registration, heartbeats) and are NOT user authentication tokens.
	// For user operations (listing groves, agents, etc.), we use user tokens, API keys,
	// or dev auth.
	authConfigured := false
	authMethod := ""
	if settings.Hub != nil {
		if settings.Hub.Token != "" {
			opts = append(opts, hubclient.WithBearerToken(settings.Hub.Token))
			authConfigured = true
			authMethod = "bearer token from settings"
		} else if settings.Hub.APIKey != "" {
			opts = append(opts, hubclient.WithAPIKey(settings.Hub.APIKey))
			authConfigured = true
			authMethod = "API key from settings"
		}
	}

	// Fallback to auto dev auth if no explicit auth configured
	// This checks SCION_DEV_TOKEN env var and ~/.scion/dev-token file
	if !authConfigured {
		opts = append(opts, hubclient.WithAutoDevAuth())
		authMethod = "dev auth (auto-detected)"
	}

	if debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] Hub client auth: %s\n", authMethod)
		fmt.Fprintf(os.Stderr, "[DEBUG] Hub endpoint: %s\n", endpoint)
	}

	opts = append(opts, hubclient.WithTimeout(30*time.Second))

	return hubclient.New(endpoint, opts...)
}

func runHubStatus(cmd *cobra.Command, args []string) error {
	// Resolve grove path to find project settings
	resolvedPath, _, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	endpoint := GetHubEndpoint(settings)

	hubEnabled := settings.IsHubEnabled()

	if hubOutputJSON {
		status := map[string]interface{}{
			"enabled":       hubEnabled,
			"cliOverride":   noHub,
			"endpoint":      endpoint,
			"configured":    settings.IsHubConfigured(),
		}
		if settings.Hub != nil {
			status["groveId"] = settings.Hub.GroveID
			status["hostId"] = settings.Hub.HostID
			status["hasToken"] = settings.Hub.Token != ""
			status["hasApiKey"] = settings.Hub.APIKey != ""
			status["hasHostToken"] = settings.Hub.HostToken != ""
		}

		// Try to connect and get health
		if endpoint != "" && !noHub {
			client, err := getHubClient(settings)
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if health, err := client.Health(ctx); err == nil {
					status["connected"] = true
					status["hubVersion"] = health.Version
					status["hubStatus"] = health.Status
				} else {
					status["connected"] = false
					status["error"] = err.Error()
				}
			}
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	// Text output
	fmt.Println("Hub Integration Status")
	fmt.Println("======================")
	fmt.Printf("Enabled:    %v\n", hubEnabled)
	if noHub {
		fmt.Printf("            (overridden by --no-hub flag)\n")
	}
	fmt.Printf("Endpoint:   %s\n", valueOrNone(endpoint))
	fmt.Printf("Configured: %v\n", settings.IsHubConfigured())

	if settings.Hub != nil {
		fmt.Printf("Grove ID:   %s\n", valueOrNone(settings.Hub.GroveID))
		fmt.Printf("Host ID:    %s\n", valueOrNone(settings.Hub.HostID))
		fmt.Printf("Has Token:  %v\n", settings.Hub.Token != "")
		fmt.Printf("Has API Key: %v\n", settings.Hub.APIKey != "")
		fmt.Printf("Has Host Token: %v\n", settings.Hub.HostToken != "")
	}

	// Try to connect
	if endpoint != "" && !noHub {
		client, err := getHubClient(settings)
		if err != nil {
			fmt.Printf("\nConnection: failed (%s)\n", err)
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		health, err := client.Health(ctx)
		if err != nil {
			fmt.Printf("\nConnection: failed (%s)\n", err)
		} else {
			fmt.Printf("\nConnection: ok\n")
			fmt.Printf("Hub Version: %s\n", health.Version)
			fmt.Printf("Hub Status:  %s\n", health.Status)
		}
	}

	return nil
}

func runHubRegister(cmd *cobra.Command, args []string) error {
	// Determine grove path
	gp := grovePath
	if len(args) > 0 {
		gp = args[0]
	}
	if gp == "" && globalMode {
		gp = "global"
	}

	// Resolve grove path
	resolvedPath, isGlobal, err := config.ResolveGrovePath(gp)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	// Get grove info
	var groveName string
	var gitRemote string

	if isGlobal {
		groveName = "global"
	} else {
		// Get git remote
		gitRemote = util.GetGitRemote()
		if gitRemote == "" {
			return fmt.Errorf("could not determine git remote for this project")
		}
		// Get project name from git remote
		groveName = util.ExtractRepoName(gitRemote)
	}

	// Get hostname (always use system hostname to prevent duplicates)
	hostName, err := os.Hostname()
	if err != nil {
		hostName = "local-host"
	}

	// Detect runtime
	rt := runtime.GetRuntime("", "")
	runtimeType := "docker"
	if rt != nil {
		runtimeType = rt.Name()
	}

	// Get supported harnesses
	allHarnesses := harness.All()
	supportedHarnesses := make([]string, 0, len(allHarnesses))
	for _, h := range allHarnesses {
		supportedHarnesses = append(supportedHarnesses, h.Name())
	}

	// Get existing host ID if available
	var existingHostID string
	if settings.Hub != nil {
		existingHostID = settings.Hub.HostID
	}

	// Build registration request
	req := &hubclient.RegisterGroveRequest{
		Name:      groveName,
		GitRemote: util.NormalizeGitRemote(gitRemote),
		Path:      resolvedPath,
		Mode:      hubRegisterMode,
		Host: &hubclient.HostInfo{
			ID:      existingHostID, // May be empty
			Name:    hostName,
			Version: version.Version,
			Capabilities: &hubclient.HostCapabilities{
				WebPTY: false, // Not implemented yet
				Sync:   true,
				Attach: true,
			},
			Runtimes: []hubclient.HostRuntime{
				{Type: runtimeType, Available: true},
			},
			SupportedHarnesses: supportedHarnesses,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Groves().Register(ctx, req)
	if err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Save the grove ID
	if resp.Grove != nil && resp.Grove.ID != "" {
		if err := config.UpdateSetting(resolvedPath, "hub.groveId", resp.Grove.ID, isGlobal); err != nil {
			fmt.Printf("Warning: failed to save grove ID: %v\n", err)
		}
	}

	// Save the host token
	if resp.HostToken != "" {
		if settings.Hub == nil {
			settings.Hub = &config.HubClientConfig{}
		}
		settings.Hub.HostToken = resp.HostToken
		settings.Hub.HostID = resp.Host.ID

		if err := config.UpdateSetting(resolvedPath, "hub.hostToken", resp.HostToken, isGlobal); err != nil {
			fmt.Printf("Warning: failed to save host token: %v\n", err)
		}
		if err := config.UpdateSetting(resolvedPath, "hub.hostId", resp.Host.ID, isGlobal); err != nil {
			fmt.Printf("Warning: failed to save host ID: %v\n", err)
		}
	}

	if resp.Created {
		fmt.Printf("Created new grove: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
	} else {
		fmt.Printf("Linked to existing grove: %s (ID: %s)\n", resp.Grove.Name, resp.Grove.ID)
	}
	fmt.Printf("Host registered: %s (ID: %s)\n", resp.Host.Name, resp.Host.ID)

	return nil
}

func runHubDeregister(cmd *cobra.Command, args []string) error {
	// Resolve grove path to find project settings
	resolvedPath, isGlobal, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	if settings.Hub == nil || settings.Hub.HostID == "" {
		return fmt.Errorf("no host registration found")
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hostID := settings.Hub.HostID

	if err := client.RuntimeHosts().Delete(ctx, hostID); err != nil {
		return fmt.Errorf("deregistration failed: %w", err)
	}

	// Clear the stored credentials
	_ = config.UpdateSetting(resolvedPath, "hub.hostToken", "", isGlobal)
	_ = config.UpdateSetting(resolvedPath, "hub.hostId", "", isGlobal)

	fmt.Printf("Host %s deregistered from Hub\n", hostID)
	return nil
}

func runHubGroves(cmd *cobra.Command, args []string) error {
	// Resolve grove path to find project settings
	resolvedPath, _, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.Groves().List(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list groves: %w", err)
	}

	if hubOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Groves)
	}

	if len(resp.Groves) == 0 {
		fmt.Println("No groves found")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %-10s  %s\n", "ID", "NAME", "AGENTS", "GIT REMOTE")
	fmt.Printf("%-36s  %-20s  %-10s  %s\n", "------------------------------------", "--------------------", "----------", "----------")
	for _, g := range resp.Groves {
		gitRemote := g.GitRemote
		if len(gitRemote) > 40 {
			gitRemote = gitRemote[:37] + "..."
		}
		fmt.Printf("%-36s  %-20s  %-10d  %s\n", g.ID, truncate(g.Name, 20), g.AgentCount, gitRemote)
	}

	return nil
}

func runHubHosts(cmd *cobra.Command, args []string) error {
	// Resolve grove path to find project settings
	resolvedPath, _, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	client, err := getHubClient(settings)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := client.RuntimeHosts().List(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to list hosts: %w", err)
	}

	if hubOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp.Hosts)
	}

	if len(resp.Hosts) == 0 {
		fmt.Println("No runtime hosts found")
		return nil
	}

	fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", "ID", "NAME", "TYPE", "STATUS", "MODE")
	fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", "------------------------------------", "--------------------", "----------", "----------", "----------")
	for _, h := range resp.Hosts {
		fmt.Printf("%-36s  %-20s  %-10s  %-10s  %s\n", h.ID, truncate(h.Name, 20), h.Type, h.Status, h.Mode)
	}

	return nil
}

func valueOrNone(s string) string {
	if s == "" {
		return "(not configured)"
	}
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func runHubEnable(cmd *cobra.Command, args []string) error {
	// Resolve grove path
	resolvedPath, isGlobal, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	endpoint := GetHubEndpoint(settings)
	if endpoint == "" {
		return fmt.Errorf("Hub endpoint not configured.\n\nConfigure the Hub endpoint via:\n  - SCION_HUB_ENDPOINT environment variable\n  - hub.endpoint in settings.yaml\n  - --hub flag on any command\n\nExample: scion config set hub.endpoint https://hub.scion.dev --global")
	}

	// Try to connect and verify Hub is healthy before enabling
	client, err := getHubClient(settings)
	if err != nil {
		return fmt.Errorf("failed to create Hub client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := client.Health(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Hub at %s: %w\n\nVerify the Hub endpoint is correct and the Hub is running.", endpoint, err)
	}

	// Save the enabled setting
	if err := config.UpdateSetting(resolvedPath, "hub.enabled", "true", isGlobal); err != nil {
		return fmt.Errorf("failed to save setting: %w", err)
	}

	// If the endpoint was provided via --hub flag, persist it to settings
	if hubEndpoint != "" {
		if err := config.UpdateSetting(resolvedPath, "hub.endpoint", hubEndpoint, isGlobal); err != nil {
			return fmt.Errorf("failed to save endpoint: %w", err)
		}
	}

	fmt.Printf("Hub integration enabled.\n")
	fmt.Printf("Endpoint: %s\n", endpoint)
	fmt.Printf("Hub Status: %s (version %s)\n", health.Status, health.Version)
	fmt.Println("\nAgent operations (create, start, delete) will now be routed through the Hub.")
	fmt.Println("Use 'scion hub disable' to switch back to local-only mode.")

	return nil
}

func runHubDisable(cmd *cobra.Command, args []string) error {
	// Resolve grove path
	resolvedPath, isGlobal, err := config.ResolveGrovePath(grovePath)
	if err != nil {
		return fmt.Errorf("failed to resolve grove path: %w", err)
	}

	settings, err := config.LoadSettings(resolvedPath)
	if err != nil {
		return fmt.Errorf("failed to load settings: %w", err)
	}

	if !settings.IsHubEnabled() {
		fmt.Println("Hub integration is already disabled.")
		return nil
	}

	// Save the disabled setting
	if err := config.UpdateSetting(resolvedPath, "hub.enabled", "false", isGlobal); err != nil {
		return fmt.Errorf("failed to save setting: %w", err)
	}

	fmt.Println("Hub integration disabled.")
	fmt.Println("Agent operations will now be performed locally.")
	fmt.Println("\nHub configuration is preserved. Use 'scion hub enable' to re-enable.")

	return nil
}
