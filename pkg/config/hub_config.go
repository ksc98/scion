package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// HubServerConfig holds configuration for the Hub API server.
type HubServerConfig struct {
	Port         int           `json:"port" yaml:"port" koanf:"port"`
	Host         string        `json:"host" yaml:"host" koanf:"host"`
	ReadTimeout  time.Duration `json:"readTimeout" yaml:"readTimeout" koanf:"readTimeout"`
	WriteTimeout time.Duration `json:"writeTimeout" yaml:"writeTimeout" koanf:"writeTimeout"`

	// CORS settings
	CORSEnabled        bool     `json:"corsEnabled" yaml:"corsEnabled" koanf:"corsEnabled"`
	CORSAllowedOrigins []string `json:"corsAllowedOrigins" yaml:"corsAllowedOrigins" koanf:"corsAllowedOrigins"`
	CORSAllowedMethods []string `json:"corsAllowedMethods" yaml:"corsAllowedMethods" koanf:"corsAllowedMethods"`
	CORSAllowedHeaders []string `json:"corsAllowedHeaders" yaml:"corsAllowedHeaders" koanf:"corsAllowedHeaders"`
	CORSMaxAge         int      `json:"corsMaxAge" yaml:"corsMaxAge" koanf:"corsMaxAge"`
}

// RuntimeHostConfig holds configuration for the Runtime Host API server.
type RuntimeHostConfig struct {
	// Enabled indicates whether the Runtime Host API is enabled
	Enabled bool `json:"enabled" yaml:"enabled" koanf:"enabled"`
	// Port is the HTTP port to listen on (default 9800)
	Port int `json:"port" yaml:"port" koanf:"port"`
	// Host is the address to bind to (e.g., "0.0.0.0" or "127.0.0.1")
	Host string `json:"host" yaml:"host" koanf:"host"`
	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout time.Duration `json:"readTimeout" yaml:"readTimeout" koanf:"readTimeout"`
	// WriteTimeout is the maximum duration before timing out writes
	WriteTimeout time.Duration `json:"writeTimeout" yaml:"writeTimeout" koanf:"writeTimeout"`

	// Mode is the operational mode: "connected" (full control) or "read-only" (reporting only)
	Mode string `json:"mode" yaml:"mode" koanf:"mode"`
	// HubEndpoint is the Hub API endpoint for status reporting (when Hub not co-located)
	HubEndpoint string `json:"hubEndpoint" yaml:"hubEndpoint" koanf:"hubEndpoint"`

	// HostID is a unique identifier for this runtime host (auto-generated if empty)
	HostID string `json:"hostId" yaml:"hostId" koanf:"hostId"`
	// HostName is a human-readable name for this runtime host
	HostName string `json:"hostName" yaml:"hostName" koanf:"hostName"`

	// CORS settings
	CORSEnabled        bool     `json:"corsEnabled" yaml:"corsEnabled" koanf:"corsEnabled"`
	CORSAllowedOrigins []string `json:"corsAllowedOrigins" yaml:"corsAllowedOrigins" koanf:"corsAllowedOrigins"`
	CORSAllowedMethods []string `json:"corsAllowedMethods" yaml:"corsAllowedMethods" koanf:"corsAllowedMethods"`
	CORSAllowedHeaders []string `json:"corsAllowedHeaders" yaml:"corsAllowedHeaders" koanf:"corsAllowedHeaders"`
	CORSMaxAge         int      `json:"corsMaxAge" yaml:"corsMaxAge" koanf:"corsMaxAge"`
}

// Runtime Host operational modes
const (
	RuntimeHostModeConnected = "connected"
	RuntimeHostModeReadOnly  = "read-only"
)

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	Driver string `json:"driver" yaml:"driver" koanf:"driver"` // sqlite, postgres
	URL    string `json:"url" yaml:"url" koanf:"url"`          // Connection URL/path
}

// GlobalConfig holds the complete server configuration.
// This is distinct from hub.ServerConfig which only holds HTTP server settings.
type GlobalConfig struct {
	// Hub API server settings
	Hub HubServerConfig `json:"hub" yaml:"hub" koanf:"hub"`

	// Runtime Host API server settings
	RuntimeHost RuntimeHostConfig `json:"runtimeHost" yaml:"runtimeHost" koanf:"runtimeHost"`

	// Database settings
	Database DatabaseConfig `json:"database" yaml:"database" koanf:"database"`

	// Logging settings
	LogLevel  string `json:"logLevel" yaml:"logLevel" koanf:"logLevel"`
	LogFormat string `json:"logFormat" yaml:"logFormat" koanf:"logFormat"` // text, json
}

// DefaultGlobalConfig returns the default global configuration.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		Hub: HubServerConfig{
			Port:         9810,
			Host:         "0.0.0.0",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			CORSEnabled:  true,
			CORSAllowedOrigins: []string{"*"},
			CORSAllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			CORSAllowedHeaders: []string{"Authorization", "Content-Type", "X-Scion-Host-Token", "X-Scion-Agent-Token", "X-API-Key"},
			CORSMaxAge:         3600,
		},
		RuntimeHost: RuntimeHostConfig{
			Enabled:      false,
			Port:         9800,
			Host:         "0.0.0.0",
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 120 * time.Second, // Longer for agent operations
			Mode:         RuntimeHostModeConnected,
			CORSEnabled:  true,
			CORSAllowedOrigins: []string{"*"},
			CORSAllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			CORSAllowedHeaders: []string{"Authorization", "Content-Type", "X-Scion-Host-Token", "X-API-Key"},
			CORSMaxAge:         3600,
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			URL:    "", // Will be set to default path if empty
		},
		LogLevel:  "info",
		LogFormat: "text",
	}
}

// LoadGlobalConfig loads global configuration using Koanf with priority:
// 1. Embedded defaults
// 2. Global config file (~/.scion/server.yaml)
// 3. Local config file (./server.yaml or specified path)
// 4. Environment variables (SCION_SERVER_ prefix)
func LoadGlobalConfig(configPath string) (*GlobalConfig, error) {
	k := koanf.New(".")

	// 1. Load embedded defaults
	defaults := DefaultGlobalConfig()
	if err := k.Load(confmap.Provider(map[string]interface{}{
		"hub.port":               defaults.Hub.Port,
		"hub.host":               defaults.Hub.Host,
		"hub.readTimeout":        defaults.Hub.ReadTimeout,
		"hub.writeTimeout":       defaults.Hub.WriteTimeout,
		"hub.corsEnabled":        defaults.Hub.CORSEnabled,
		"hub.corsAllowedOrigins": defaults.Hub.CORSAllowedOrigins,
		"hub.corsAllowedMethods": defaults.Hub.CORSAllowedMethods,
		"hub.corsAllowedHeaders": defaults.Hub.CORSAllowedHeaders,
		"hub.corsMaxAge":         defaults.Hub.CORSMaxAge,
		// RuntimeHost defaults
		"runtimeHost.enabled":            defaults.RuntimeHost.Enabled,
		"runtimeHost.port":               defaults.RuntimeHost.Port,
		"runtimeHost.host":               defaults.RuntimeHost.Host,
		"runtimeHost.readTimeout":        defaults.RuntimeHost.ReadTimeout,
		"runtimeHost.writeTimeout":       defaults.RuntimeHost.WriteTimeout,
		"runtimeHost.mode":               defaults.RuntimeHost.Mode,
		"runtimeHost.corsEnabled":        defaults.RuntimeHost.CORSEnabled,
		"runtimeHost.corsAllowedOrigins": defaults.RuntimeHost.CORSAllowedOrigins,
		"runtimeHost.corsAllowedMethods": defaults.RuntimeHost.CORSAllowedMethods,
		"runtimeHost.corsAllowedHeaders": defaults.RuntimeHost.CORSAllowedHeaders,
		"runtimeHost.corsMaxAge":         defaults.RuntimeHost.CORSMaxAge,
		// Database defaults
		"database.driver": defaults.Database.Driver,
		"database.url":    defaults.Database.URL,
		"logLevel":        defaults.LogLevel,
		"logFormat":       defaults.LogFormat,
	}, "."), nil); err != nil {
		return nil, err
	}

	// 2. Load global config (~/.scion/server.yaml)
	if globalDir, err := GetGlobalDir(); err == nil {
		loadServerConfigFile(k, globalDir)
	}

	// 3. Load local config
	if configPath != "" {
		// Check if configPath is a file or directory
		info, err := os.Stat(configPath)
		if err == nil {
			if info.IsDir() {
				loadServerConfigFile(k, configPath)
			} else {
				_ = k.Load(file.Provider(configPath), yaml.Parser())
			}
		}
	} else {
		// Try current directory
		loadServerConfigFile(k, ".")
	}

	// 4. Load environment variables (SCION_SERVER_ prefix)
	// Maps: SCION_SERVER_HUB_PORT -> hub.port
	//       SCION_SERVER_DATABASE_DRIVER -> database.driver
	//       SCION_SERVER_LOG_LEVEL -> logLevel
	_ = k.Load(env.Provider("SCION_SERVER_", ".", func(s string) string {
		key := strings.ToLower(strings.TrimPrefix(s, "SCION_SERVER_"))
		// Replace underscores with dots for nested keys
		key = strings.Replace(key, "_", ".", -1)
		return key
	}), nil)

	// Unmarshal into GlobalConfig struct
	config := &GlobalConfig{
		Hub: HubServerConfig{
			CORSAllowedOrigins: make([]string, 0),
			CORSAllowedMethods: make([]string, 0),
			CORSAllowedHeaders: make([]string, 0),
		},
		RuntimeHost: RuntimeHostConfig{
			CORSAllowedOrigins: make([]string, 0),
			CORSAllowedMethods: make([]string, 0),
			CORSAllowedHeaders: make([]string, 0),
		},
	}

	if err := k.Unmarshal("", config); err != nil {
		return nil, err
	}

	// Apply defaults for database path if not set
	if config.Database.URL == "" && config.Database.Driver == "sqlite" {
		if globalDir, err := GetGlobalDir(); err == nil {
			config.Database.URL = filepath.Join(globalDir, "hub.db")
		} else {
			config.Database.URL = "hub.db"
		}
	}

	return config, nil
}

// loadServerConfigFile loads server config from a directory
func loadServerConfigFile(k *koanf.Koanf, dir string) {
	yamlPath := filepath.Join(dir, "server.yaml")
	ymlPath := filepath.Join(dir, "server.yml")

	if _, err := os.Stat(yamlPath); err == nil {
		_ = k.Load(file.Provider(yamlPath), yaml.Parser())
		return
	}
	if _, err := os.Stat(ymlPath); err == nil {
		_ = k.Load(file.Provider(ymlPath), yaml.Parser())
	}
}
