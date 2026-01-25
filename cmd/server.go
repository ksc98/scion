package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ptone/scion-agent/pkg/agent"
	"github.com/ptone/scion-agent/pkg/api"
	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/hub"
	"github.com/ptone/scion-agent/pkg/runtime"
	"github.com/ptone/scion-agent/pkg/runtimehost"
	"github.com/ptone/scion-agent/pkg/store"
	"github.com/ptone/scion-agent/pkg/store/sqlite"
	"github.com/spf13/cobra"
)

var (
	serverConfigPath  string
	hubPort           int
	hubHost           string
	enableHub         bool
	enableRuntimeHost bool
	runtimeHostPort   int
	runtimeHostMode   string
	dbURL             string
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the Scion server components",
	Long: `Commands for managing the Scion server components.

The server provides:
- Hub API: Central registry for groves, agents, and templates (port 9810)
- Runtime Host API: Agent lifecycle management on compute nodes (port 9800)
- Web Frontend: Browser-based UI (coming soon, port 9820)`,
}

// serverStartCmd represents the server start command
var serverStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Scion server components",
	Long: `Start one or more Scion server components.

Server Components:
- Hub API (--enable-hub): Central coordination for groves, agents, templates
- Runtime Host API (--enable-runtime-host): Agent lifecycle on this compute node

Configuration can be provided via:
- Config file (--config flag or ~/.scion/server.yaml)
- Environment variables (SCION_SERVER_* prefix)
- Command-line flags

Examples:
  # Start Hub API only
  scion server start --enable-hub

  # Start Runtime Host API only
  scion server start --enable-runtime-host

  # Start both Hub and Runtime Host
  scion server start --enable-hub --enable-runtime-host

  # Start Runtime Host with custom port
  scion server start --enable-runtime-host --runtime-host-port 9800`,
	RunE: runServerStart,
}

func runServerStart(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.LoadGlobalConfig(serverConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Override with command-line flags if specified
	if cmd.Flags().Changed("port") {
		cfg.Hub.Port = hubPort
	}
	if cmd.Flags().Changed("host") {
		cfg.Hub.Host = hubHost
	}
	if cmd.Flags().Changed("db") {
		cfg.Database.URL = dbURL
	}
	if cmd.Flags().Changed("enable-hub") {
		// If explicitly set, use the flag value
		// (enableHub is the variable, it's already set by cobra)
	}
	if cmd.Flags().Changed("enable-runtime-host") {
		cfg.RuntimeHost.Enabled = enableRuntimeHost
	}
	if cmd.Flags().Changed("runtime-host-port") {
		cfg.RuntimeHost.Port = runtimeHostPort
	}
	if cmd.Flags().Changed("runtime-host-mode") {
		cfg.RuntimeHost.Mode = runtimeHostMode
	}

	// Check if at least one server is enabled
	if !enableHub && !cfg.RuntimeHost.Enabled {
		return fmt.Errorf("no server components enabled; use --enable-hub or --enable-runtime-host")
	}

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Start Hub API if enabled
	if enableHub {
		// Initialize store
		var s store.Store

		switch cfg.Database.Driver {
		case "sqlite":
			sqliteStore, err := sqlite.New(cfg.Database.URL)
			if err != nil {
				return fmt.Errorf("failed to open database: %w", err)
			}
			s = sqliteStore
			defer s.Close()

			// Run migrations
			if err := s.Migrate(context.Background()); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}
		default:
			return fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
		}

		// Verify database connectivity
		if err := s.Ping(context.Background()); err != nil {
			return fmt.Errorf("database ping failed: %w", err)
		}

		// Create Hub server configuration
		hubCfg := hub.ServerConfig{
			Port:               cfg.Hub.Port,
			Host:               cfg.Hub.Host,
			ReadTimeout:        cfg.Hub.ReadTimeout,
			WriteTimeout:       cfg.Hub.WriteTimeout,
			CORSEnabled:        cfg.Hub.CORSEnabled,
			CORSAllowedOrigins: cfg.Hub.CORSAllowedOrigins,
			CORSAllowedMethods: cfg.Hub.CORSAllowedMethods,
			CORSAllowedHeaders: cfg.Hub.CORSAllowedHeaders,
			CORSMaxAge:         cfg.Hub.CORSMaxAge,
		}

		// Create Hub server
		hubSrv := hub.New(hubCfg, s)

		log.Printf("Starting Hub API server on %s:%d", cfg.Hub.Host, cfg.Hub.Port)
		log.Printf("Database: %s (%s)", cfg.Database.Driver, cfg.Database.URL)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := hubSrv.Start(ctx); err != nil {
				errCh <- fmt.Errorf("hub server error: %w", err)
			}
		}()
	}

	// Start Runtime Host API if enabled
	if cfg.RuntimeHost.Enabled {
		// Initialize runtime (auto-detect based on environment)
		rt := runtime.GetRuntime("", "")

		// Create agent manager
		mgr := agent.NewManager(rt)

		// Generate host ID if not set
		hostID := cfg.RuntimeHost.HostID
		if hostID == "" {
			hostID = api.NewUUID()
		}

		// Create Runtime Host server configuration
		rhCfg := runtimehost.ServerConfig{
			Port:               cfg.RuntimeHost.Port,
			Host:               cfg.RuntimeHost.Host,
			ReadTimeout:        cfg.RuntimeHost.ReadTimeout,
			WriteTimeout:       cfg.RuntimeHost.WriteTimeout,
			Mode:               cfg.RuntimeHost.Mode,
			HubEndpoint:        cfg.RuntimeHost.HubEndpoint,
			HostID:             hostID,
			HostName:           cfg.RuntimeHost.HostName,
			CORSEnabled:        cfg.RuntimeHost.CORSEnabled,
			CORSAllowedOrigins: cfg.RuntimeHost.CORSAllowedOrigins,
			CORSAllowedMethods: cfg.RuntimeHost.CORSAllowedMethods,
			CORSAllowedHeaders: cfg.RuntimeHost.CORSAllowedHeaders,
			CORSMaxAge:         cfg.RuntimeHost.CORSMaxAge,
		}

		// Create Runtime Host server
		rhSrv := runtimehost.New(rhCfg, mgr, rt)

		log.Printf("Starting Runtime Host API server on %s:%d (mode: %s)",
			cfg.RuntimeHost.Host, cfg.RuntimeHost.Port, cfg.RuntimeHost.Mode)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := rhSrv.Start(ctx); err != nil {
				errCh <- fmt.Errorf("runtime host server error: %w", err)
			}
		}()
	}

	// Wait for either an error or context cancellation
	select {
	case err := <-errCh:
		cancel() // Stop other servers
		return err
	case <-ctx.Done():
		// Wait for all servers to shutdown
		wg.Wait()
		return nil
	}
}

func init() {
	rootCmd.AddCommand(serverCmd)
	serverCmd.AddCommand(serverStartCmd)

	// Server start flags
	serverStartCmd.Flags().StringVarP(&serverConfigPath, "config", "c", "", "Path to server configuration file")

	// Hub API flags
	serverStartCmd.Flags().BoolVar(&enableHub, "enable-hub", false, "Enable the Hub API")
	serverStartCmd.Flags().IntVar(&hubPort, "port", 9810, "Hub API port")
	serverStartCmd.Flags().StringVar(&hubHost, "host", "0.0.0.0", "Hub API host to bind")
	serverStartCmd.Flags().StringVar(&dbURL, "db", "", "Database URL/path")

	// Runtime Host API flags
	serverStartCmd.Flags().BoolVar(&enableRuntimeHost, "enable-runtime-host", false, "Enable the Runtime Host API")
	serverStartCmd.Flags().IntVar(&runtimeHostPort, "runtime-host-port", 9800, "Runtime Host API port")
	serverStartCmd.Flags().StringVar(&runtimeHostMode, "runtime-host-mode", "connected", "Runtime Host mode (connected, read-only)")
}
