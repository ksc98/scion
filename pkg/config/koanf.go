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

package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// LoadSettingsKoanf loads settings using Koanf with provider priority:
// 1. Embedded defaults (YAML) with OS-specific runtime adjustment
// 2. Global settings file (~/.scion/settings.yaml or .json)
// 3. Grove settings file (.scion/settings.yaml or .json)
// 4. Environment variables (SCION_ prefix, top-level only)
func LoadSettingsKoanf(grovePath string) (*Settings, error) {
	k := koanf.New(".")

	// 1. Load embedded defaults (YAML with fallback to JSON)
	// GetDefaultSettingsData applies OS-specific runtime adjustments
	if defaultData, err := GetDefaultSettingsData(); err == nil {
		_ = k.Load(rawbytes.Provider(defaultData), json.Parser())
	}

	// 2. Load global settings (~/.scion/settings.yaml or .json)
	globalDir, _ := GetGlobalDir()
	if globalDir != "" {
		if err := loadSettingsFile(k, globalDir); err != nil {
			return nil, err
		}
	}

	// 3. Load grove settings
	effectiveGrovePath := resolveEffectiveGrovePath(grovePath)
	// Only load grove settings if it's different from global (avoid double-loading)
	if effectiveGrovePath != "" && effectiveGrovePath != globalDir {
		if err := loadSettingsFile(k, effectiveGrovePath); err != nil {
			return nil, err
		}
	}

	// 4. Load environment variables (SCION_ prefix, top-level only)
	// Maps: SCION_ACTIVE_PROFILE -> active_profile
	//       SCION_DEFAULT_TEMPLATE -> default_template
	//       SCION_BUCKET_PROVIDER -> bucket.provider
	//       SCION_BUCKET_NAME -> bucket.name
	//       SCION_BUCKET_PREFIX -> bucket.prefix
	//       SCION_HUB_ENDPOINT -> hub.endpoint
	//       SCION_HUB_TOKEN -> hub.token
	//       SCION_HUB_API_KEY -> hub.apiKey
	//       SCION_HUB_BROKER_ID -> hub.brokerId
	//       SCION_HUB_BROKER_TOKEN -> hub.brokerToken
	_ = k.Load(env.Provider("SCION_", ".", func(s string) string {
		key := strings.ToLower(strings.TrimPrefix(s, "SCION_"))
		// Handle nested bucket keys
		if strings.HasPrefix(key, "bucket_") {
			return "bucket." + strings.TrimPrefix(key, "bucket_")
		}
		// Handle nested hub keys
		if strings.HasPrefix(key, "hub_") {
			subkey := strings.TrimPrefix(key, "hub_")
			// Convert snake_case to camelCase for specific keys
			switch subkey {
			case "grove_id":
				// SCION_HUB_GROVE_ID maps to top-level grove_id, not hub.grove_id
				return "grove_id"
			case "api_key":
				return "hub.apiKey"
			case "broker_id":
				return "hub.brokerId"
			case "broker_token":
				return "hub.brokerToken"
			default:
				return "hub." + subkey
			}
		}
		return key
	}), nil)

	// Normalize v1 settings keys to legacy keyspace. See normalizeV1HubKeys
	// for the full mapping table — most importantly, hub.grove_id is fanned
	// out to BOTH top-level grove_id AND hub.groveId so that GetHubGroveID()
	// can read it via the legacy camelCase koanf tag. Without populating
	// hub.groveId, Hub.GroveID stays empty and CompareAgents falls back to
	// Settings.GroveID — which for git groves gets overwritten below with
	// the local deterministic UUID v5, causing the CLI to send the wrong ID
	// to the hub and 404.
	normalizeV1HubKeys(k)

	// For git groves, the grove_id is stored in a grove-id file inside the
	// .scion directory rather than in the settings file. Read it here so that
	// it overrides any grove_id inherited from global settings. The original
	// grovePath points to the .scion directory (before resolveEffectiveGrovePath
	// redirects to the external config dir).
	//
	// NOTE: this only overwrites top-level grove_id (the local deterministic
	// ID); hub.groveId (the hub-side ID set by normalizeV1HubKeys above) is
	// preserved so GetHubGroveID() still returns the hub-side value.
	if grovePath != "" && grovePath != globalDir {
		if groveID, err := ReadGroveID(grovePath); err == nil && groveID != "" {
			_ = k.Load(confmap.Provider(map[string]interface{}{
				"grove_id": groveID,
			}, "."), nil)
		}
	}

	// Unmarshal into Settings struct
	settings := &Settings{
		Runtimes:  make(map[string]RuntimeConfig),
		Harnesses: make(map[string]HarnessConfig),
		Profiles:  make(map[string]ProfileConfig),
	}

	if err := k.Unmarshal("", settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// LoadSettingsFromDir loads settings from a single directory's settings file
// without applying embedded defaults, global settings, or environment variables.
// This is useful when you need to read just one grove's settings file in isolation,
// for example to get the grove's hub.endpoint without the broker's own env vars
// overriding it.
func LoadSettingsFromDir(dir string) (*Settings, error) {
	k := koanf.New(".")
	if err := loadSettingsFile(k, dir); err != nil {
		return nil, err
	}
	// Apply the same v1 → legacy normalization as LoadSettingsKoanf so that
	// callers that read isolated grove settings (e.g. cmd/hub.go's link flow
	// at line 2181) can correctly observe Hub.GroveID. Without this, files
	// written in v1 snake_case format leave Hub.GroveID empty, and callers
	// fall back to Settings.GroveID — which for git groves is the local
	// deterministic UUID v5, not the hub-assigned ID.
	normalizeV1HubKeys(k)
	settings := &Settings{
		Runtimes:  make(map[string]RuntimeConfig),
		Harnesses: make(map[string]HarnessConfig),
		Profiles:  make(map[string]ProfileConfig),
	}
	if err := k.Unmarshal("", settings); err != nil {
		return nil, err
	}
	return settings, nil
}

// normalizeV1HubKeys remaps v1 snake_case hub keys to the legacy camelCase
// keyspace expected by the Settings struct. Specifically:
//   - hub.grove_id   → grove_id (top-level legacy field) AND hub.groveId
//   - server.broker.broker_id      → hub.brokerId (when not already set)
//   - server.broker.broker_token   → hub.brokerToken (when not already set)
//   - server.broker.broker_nickname → hub.brokerNickname (when not already set)
//
// Both LoadSettingsKoanf and LoadSettingsFromDir must apply this so isolated
// grove reads behave consistently with merged-chain reads.
func normalizeV1HubKeys(k *koanf.Koanf) {
	if k.Exists("hub.grove_id") {
		hubGroveID := k.String("hub.grove_id")
		_ = k.Load(confmap.Provider(map[string]interface{}{
			"grove_id":    hubGroveID,
			"hub.groveId": hubGroveID,
		}, "."), nil)
	}
	if k.Exists("server.broker.broker_id") && !k.Exists("hub.brokerId") {
		_ = k.Load(confmap.Provider(map[string]interface{}{
			"hub.brokerId": k.String("server.broker.broker_id"),
		}, "."), nil)
	}
	if k.Exists("server.broker.broker_token") && !k.Exists("hub.brokerToken") {
		_ = k.Load(confmap.Provider(map[string]interface{}{
			"hub.brokerToken": k.String("server.broker.broker_token"),
		}, "."), nil)
	}
	if k.Exists("server.broker.broker_nickname") && !k.Exists("hub.brokerNickname") {
		_ = k.Load(confmap.Provider(map[string]interface{}{
			"hub.brokerNickname": k.String("server.broker.broker_nickname"),
		}, "."), nil)
	}
}

// loadSettingsFile loads settings from a directory, preferring YAML over JSON
func loadSettingsFile(k *koanf.Koanf, dir string) error {
	yamlPath := filepath.Join(dir, "settings.yaml")
	ymlPath := filepath.Join(dir, "settings.yml")
	jsonPath := filepath.Join(dir, "settings.json")

	// Try YAML first (.yaml then .yml)
	if _, err := os.Stat(yamlPath); err == nil {
		return k.Load(file.Provider(yamlPath), yaml.Parser())
	}
	if _, err := os.Stat(ymlPath); err == nil {
		return k.Load(file.Provider(ymlPath), yaml.Parser())
	}
	// Fall back to JSON
	if _, err := os.Stat(jsonPath); err == nil {
		return k.Load(file.Provider(jsonPath), json.Parser())
	}
	return nil
}

// getDefaultSettingsYAMLForRuntime generates the default settings YAML with the
// specified runtime for the local profile. The embedded template defaults to
// "container"; if a different runtime is specified, the template is adjusted.
func getDefaultSettingsYAMLForRuntime(targetRuntime string) ([]byte, error) {
	data, err := EmbedsFS.ReadFile("embeds/default_settings.yaml")
	if err != nil {
		return nil, err
	}

	if targetRuntime != "container" {
		data = bytes.Replace(data,
			[]byte("runtime: container  # Auto-adjusted by OS"),
			[]byte(fmt.Sprintf("runtime: %s  # Auto-detected", targetRuntime)),
			1)
	}

	return data, nil
}

// GetDefaultSettingsDataYAML returns the embedded default settings in YAML format.
// This function adjusts the local profile runtime based on the OS. It is used as
// a fallback default for settings loaders; during init, DetectLocalRuntime is used
// instead for actual runtime probing.
func GetDefaultSettingsDataYAML() ([]byte, error) {
	if goruntime.GOOS != "darwin" {
		return getDefaultSettingsYAMLForRuntime("docker")
	}
	return getDefaultSettingsYAMLForRuntime("container")
}

// GetGroveDefaultSettingsYAML returns the embedded grove-level default settings YAML.
// Unlike the full default settings, grove settings do not include profiles or runtimes;
// those are managed at the global/broker level (~/.scion/settings.yaml).
func GetGroveDefaultSettingsYAML() ([]byte, error) {
	return EmbedsFS.ReadFile("embeds/default_grove_settings.yaml")
}

// GetSettingsPath returns the path to the settings file in a directory,
// preferring YAML over JSON. Returns empty string if no settings file exists.
func GetSettingsPath(dir string) string {
	yamlPath := filepath.Join(dir, "settings.yaml")
	ymlPath := filepath.Join(dir, "settings.yml")
	jsonPath := filepath.Join(dir, "settings.json")

	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath
	}
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath
	}
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath
	}
	return ""
}

// GetScionAgentConfigPath returns the path to the scion-agent config file,
// preferring YAML over JSON. Returns empty string if no config file exists.
func GetScionAgentConfigPath(dir string) string {
	yamlPath := filepath.Join(dir, "scion-agent.yaml")
	ymlPath := filepath.Join(dir, "scion-agent.yml")
	jsonPath := filepath.Join(dir, "scion-agent.json")

	if _, err := os.Stat(yamlPath); err == nil {
		return yamlPath
	}
	if _, err := os.Stat(ymlPath); err == nil {
		return ymlPath
	}
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath
	}
	return ""
}

// SettingsFileExists checks if a settings file exists in a directory (YAML or JSON)
func SettingsFileExists(dir string) bool {
	return GetSettingsPath(dir) != ""
}

// ScionAgentConfigExists checks if a scion-agent config file exists (YAML or JSON)
func ScionAgentConfigExists(dir string) bool {
	return GetScionAgentConfigPath(dir) != ""
}
