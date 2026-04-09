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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/GoogleCloudPlatform/scion/pkg/config"
	"github.com/GoogleCloudPlatform/scion/pkg/harness"
	"github.com/GoogleCloudPlatform/scion/pkg/hubclient"
	"github.com/GoogleCloudPlatform/scion/pkg/hubsync"
	"github.com/GoogleCloudPlatform/scion/pkg/util"
	"github.com/spf13/cobra"
)

// templatesCmd represents the templates command
var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage agent templates",
	Long:  `List and inspect templates used to provision new agents.`,
}

var templatesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE:  runTemplateList,
}

func runTemplateList(cmd *cobra.Command, args []string) error {
	// Get local templates grouped by scope
	localGlobal, localGrove, err := config.ListTemplatesGrouped()
	if err != nil {
		return err
	}

	// Check if Hub is available (suppress errors, just skip Hub if not available)
	var hubCtx *HubContext
	var hubGlobal, hubGrove []hubclient.Template
	hubAvailable := false

	if !noHub {
		hubCtx, _ = CheckHubAvailabilityWithOptions(grovePath, true)
		if hubCtx != nil {
			hubAvailable = true
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Get grove ID for filtering grove-scoped templates
			groveID, _ := GetGroveID(hubCtx)

			// Fetch global templates from Hub
			globalResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{
				Scope:  "global",
				Status: "active",
			})
			if err == nil {
				hubGlobal = globalResp.Templates
			}

			// Fetch grove templates from Hub (if we have a grove ID)
			if groveID != "" {
				groveResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{
					Scope:   "grove",
					GroveID: groveID,
					Status:  "active",
				})
				if err == nil {
					hubGrove = groveResp.Templates
				}
			}

			// Sort hub templates by name for consistent output
			sort.Slice(hubGlobal, func(i, j int) bool {
				return hubGlobal[i].Name < hubGlobal[j].Name
			})
			sort.Slice(hubGrove, func(i, j int) bool {
				return hubGrove[i].Name < hubGrove[j].Name
			})
		}
	}

	if isJSONOutput() {
		type templateEntry struct {
			Name        string `json:"name"`
			Path        string `json:"path,omitempty"`
			ID          string `json:"id,omitempty"`
			ContentHash string `json:"contentHash,omitempty"`
		}
		output := map[string]interface{}{}

		localSection := map[string][]templateEntry{}
		if len(localGlobal) > 0 {
			entries := make([]templateEntry, len(localGlobal))
			for i, t := range localGlobal {
				entries[i] = templateEntry{Name: t.Name, Path: t.Path}
			}
			localSection["global"] = entries
		}
		if len(localGrove) > 0 {
			entries := make([]templateEntry, len(localGrove))
			for i, t := range localGrove {
				entries[i] = templateEntry{Name: t.Name, Path: t.Path}
			}
			localSection["grove"] = entries
		}
		if len(localSection) > 0 {
			output["local"] = localSection
		}

		if hubAvailable {
			hubSection := map[string][]templateEntry{}
			if len(hubGlobal) > 0 {
				entries := make([]templateEntry, len(hubGlobal))
				for i, t := range hubGlobal {
					entries[i] = templateEntry{Name: t.Name, ID: t.ID, ContentHash: t.ContentHash}
				}
				hubSection["global"] = entries
			}
			if len(hubGrove) > 0 {
				entries := make([]templateEntry, len(hubGrove))
				for i, t := range hubGrove {
					entries[i] = templateEntry{Name: t.Name, ID: t.ID, ContentHash: t.ContentHash}
				}
				hubSection["grove"] = entries
			}
			if len(hubSection) > 0 {
				output["hub"] = hubSection
			}
		}

		return outputJSON(output)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	if hubAvailable {
		// Hub mode: group by local/hub, then by global/grove
		printTemplateListHubMode(w, localGlobal, localGrove, hubGlobal, hubGrove)
	} else {
		// Local mode: group by global/grove
		printTemplateListLocalMode(w, localGlobal, localGrove)
	}

	w.Flush()
	return nil
}

func printTemplateListLocalMode(w *tabwriter.Writer, global, grove []*config.Template) {
	hasGlobal := len(global) > 0
	hasGrove := len(grove) > 0

	if !hasGlobal && !hasGrove {
		fmt.Fprintln(w, "No templates found.")
		return
	}

	if hasGlobal {
		fmt.Fprintln(w, "Global Templates:")
		fmt.Fprintln(w, "  NAME\tPATH")
		for _, t := range global {
			fmt.Fprintf(w, "  %s\t%s\n", t.Name, t.Path)
		}
	}

	if hasGrove {
		if hasGlobal {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "Grove Templates:")
		fmt.Fprintln(w, "  NAME\tPATH")
		for _, t := range grove {
			fmt.Fprintf(w, "  %s\t%s\n", t.Name, t.Path)
		}
	}
}

func printTemplateListHubMode(w *tabwriter.Writer, localGlobal, localGrove []*config.Template, hubGlobal, hubGrove []hubclient.Template) {
	hasLocalGlobal := len(localGlobal) > 0
	hasLocalGrove := len(localGrove) > 0
	hasHubGlobal := len(hubGlobal) > 0
	hasHubGrove := len(hubGrove) > 0

	hasLocal := hasLocalGlobal || hasLocalGrove
	hasHub := hasHubGlobal || hasHubGrove

	if !hasLocal && !hasHub {
		fmt.Fprintln(w, "No templates found.")
		return
	}

	// Local section
	if hasLocal {
		fmt.Fprintln(w, "Local Templates:")
		if hasLocalGlobal {
			fmt.Fprintln(w, "  Global:")
			fmt.Fprintln(w, "    NAME\tPATH")
			for _, t := range localGlobal {
				fmt.Fprintf(w, "    %s\t%s\n", t.Name, t.Path)
			}
		}
		if hasLocalGrove {
			if hasLocalGlobal {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "  Grove:")
			fmt.Fprintln(w, "    NAME\tPATH")
			for _, t := range localGrove {
				fmt.Fprintf(w, "    %s\t%s\n", t.Name, t.Path)
			}
		}
	}

	// Hub section
	if hasHub {
		if hasLocal {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "Hub Templates:")
		if hasHubGlobal {
			fmt.Fprintln(w, "  Global:")
			fmt.Fprintln(w, "    NAME\tID\tHASH")
			for _, t := range hubGlobal {
				fmt.Fprintf(w, "    %s\t%s\t%s\n", t.Name, t.ID, truncateHash(t.ContentHash))
			}
		}
		if hasHubGrove {
			if hasHubGlobal {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, "  Grove:")
			fmt.Fprintln(w, "    NAME\tID\tHASH")
			for _, t := range hubGrove {
				fmt.Fprintf(w, "    %s\t%s\t%s\n", t.Name, t.ID, truncateHash(t.ContentHash))
			}
		}
	}
}

var templatesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show template configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateShow,
}

func runTemplateShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Get flags - handle nil cmd for testing
	var localOnly, hubOnly bool
	if cmd != nil {
		localOnly, _ = cmd.Flags().GetBool("local")
		hubOnly, _ = cmd.Flags().GetBool("hub")
	}

	// Build resolution options
	opts := &ResolveOpts{
		LocalOnly:   localOnly,
		HubOnly:     hubOnly,
		GroveOnly:   false,
		GlobalOnly:  globalMode,
		AutoConfirm: autoConfirm,
	}

	// Check if Hub is available (suppress errors for read operations)
	var hubCtx *HubContext
	if !noHub && !localOnly {
		hubCtx, _ = CheckHubAvailabilityWithOptions(grovePath, true)
	}

	ctx := context.Background()
	match, err := ResolveTemplate(ctx, name, hubCtx, opts, "show")
	if err != nil {
		return err
	}

	// Display based on whether local or Hub template
	if match.IsLocal() {
		// Load and display local template
		tpl := &config.Template{Name: match.Name, Path: match.LocalPath}
		cfg, err := tpl.LoadConfig()
		if err != nil {
			return err
		}

		if isJSONOutput() {
			return outputJSON(map[string]interface{}{
				"name":     tpl.Name,
				"location": string(match.Location),
				"path":     tpl.Path,
				"config":   cfg,
			})
		}

		fmt.Printf("Template: %s\n", tpl.Name)
		fmt.Printf("Location: %s\n", match.DisplayLocation())
		fmt.Printf("Path:     %s\n", tpl.Path)
		fmt.Println("Configuration (scion-agent.json):")

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	}

	// Hub template - display hub info
	t := match.HubTemplate

	if isJSONOutput() {
		output := map[string]interface{}{
			"name":     t.Name,
			"location": string(match.Location),
			"id":       t.ID,
			"harness":  t.Harness,
			"scope":    t.Scope,
			"status":   t.Status,
			"created":  t.Created.Format(time.RFC3339),
			"updated":  t.Updated.Format(time.RFC3339),
		}
		if t.ContentHash != "" {
			output["contentHash"] = t.ContentHash
		}
		if t.Description != "" {
			output["description"] = t.Description
		}
		return outputJSON(output)
	}

	fmt.Printf("Template: %s\n", t.Name)
	fmt.Printf("Location: %s\n", match.DisplayLocation())
	fmt.Printf("ID:       %s\n", t.ID)
	fmt.Printf("Harness:  %s\n", t.Harness)
	fmt.Printf("Scope:    %s\n", t.Scope)
	fmt.Printf("Status:   %s\n", t.Status)
	if t.ContentHash != "" {
		fmt.Printf("Hash:     %s\n", truncateHash(t.ContentHash))
	}
	if t.Description != "" {
		fmt.Printf("Description: %s\n", t.Description)
	}
	fmt.Printf("Created:  %s\n", t.Created.Format(time.RFC3339))
	fmt.Printf("Updated:  %s\n", t.Updated.Format(time.RFC3339))

	return nil
}

var templatesCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")

		err := config.CreateTemplate(name, global)
		if err != nil {
			return err
		}
		if isJSONOutput() {
			return outputJSON(ActionResult{
				Status:  "success",
				Command: "templates create",
				Message: fmt.Sprintf("Template %s created successfully.", name),
				Details: map[string]interface{}{
					"name":   name,
					"global": global,
				},
			})
		}
		fmt.Printf("Template %s created successfully.\n", name)
		return nil
	},
}

var templatesDeleteCmd = &cobra.Command{
	Use:     "delete <name>",
	Aliases: []string{"rm"},
	Short:   "Delete a template",
	Args:    cobra.ExactArgs(1),
	RunE:    runTemplateDelete,
}

// runTemplateDelete implements the delete command with confirmation prompts.
// It searches all 4 locations for the template, then prompts for which to delete.
func runTemplateDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Get flags - handle nil cmd for testing
	var localOnly, hubOnly bool
	if cmd != nil {
		localOnly, _ = cmd.Flags().GetBool("local")
		hubOnly, _ = cmd.Flags().GetBool("hub")
	}

	// Build resolution options
	opts := &ResolveOpts{
		LocalOnly:   localOnly,
		HubOnly:     hubOnly,
		GroveOnly:   false,
		GlobalOnly:  globalMode,
		AutoConfirm: autoConfirm,
	}

	// Check if Hub is available (suppress errors for delete operations)
	var hubCtx *HubContext
	if !noHub && !localOnly {
		hubCtx, _ = CheckHubAvailabilityWithOptions(grovePath, true)
	}

	ctx := context.Background()
	matches, err := ResolveTemplateForDelete(ctx, name, hubCtx, opts)
	if err != nil {
		return err
	}

	// Delete each selected template
	for _, match := range matches {
		if err := deleteTemplateMatch(ctx, &match, hubCtx); err != nil {
			return err
		}
	}

	return nil
}

// deleteTemplateMatch deletes a single template match after confirmation.
func deleteTemplateMatch(ctx context.Context, match *TemplateMatch, hubCtx *HubContext) error {
	// Confirm deletion
	prompt := fmt.Sprintf("Delete template '%s' from %s?", match.Name, match.DisplayLocation())
	if !hubsync.ConfirmAction(prompt, true, autoConfirm) {
		fmt.Println("Skipped.")
		return nil
	}

	if match.IsLocal() {
		// Delete local template
		isGlobal := match.Location == LocationLocalGlobal
		if err := config.DeleteTemplate(match.Name, isGlobal); err != nil {
			return fmt.Errorf("failed to delete local template: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(ActionResult{
				Status:  "success",
				Command: "templates delete",
				Message: fmt.Sprintf("Local template '%s' deleted successfully.", match.Name),
				Details: map[string]interface{}{
					"name":     match.Name,
					"location": string(match.Location),
				},
			})
		}
		fmt.Printf("Local template '%s' deleted successfully.\n", match.Name)
	} else {
		// Delete hub template
		if hubCtx == nil {
			return fmt.Errorf("Hub context not available for deleting Hub template")
		}
		deleteCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := hubCtx.Client.Templates().Delete(deleteCtx, match.HubTemplate.ID); err != nil {
			return fmt.Errorf("failed to delete Hub template: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(ActionResult{
				Status:  "success",
				Command: "templates delete",
				Message: fmt.Sprintf("Hub template '%s' deleted successfully.", match.Name),
				Details: map[string]interface{}{
					"name":     match.Name,
					"location": string(match.Location),
					"id":       match.HubTemplate.ID,
				},
			})
		}
		fmt.Printf("Hub template '%s' deleted successfully.\n", match.Name)
	}

	return nil
}

var templatesCloneCmd = &cobra.Command{
	Use:   "clone <src-name> <dest-name>",
	Short: "Clone an existing template",
	Args:  cobra.ExactArgs(2),
	RunE:  runTemplateClone,
}

func runTemplateClone(cmd *cobra.Command, args []string) error {
	srcName := args[0]
	destName := args[1]

	// Get flags - handle nil cmd for testing
	var localOnly, hubOnly bool
	if cmd != nil {
		localOnly, _ = cmd.Flags().GetBool("local")
		hubOnly, _ = cmd.Flags().GetBool("hub")
	}

	// Destination scope from root's --global flag
	destGlobal := globalMode

	// Build resolution options
	opts := &ResolveOpts{
		LocalOnly:   localOnly,
		HubOnly:     hubOnly,
		AutoConfirm: autoConfirm,
	}

	// Check if Hub is available for cloning from Hub templates
	var hubCtx *HubContext
	if !noHub && !localOnly {
		hubCtx, _ = CheckHubAvailabilityWithOptions(grovePath, true)
	}

	ctx := context.Background()
	match, err := ResolveTemplate(ctx, srcName, hubCtx, opts, "clone")
	if err != nil {
		return err
	}

	// If source is a Hub template, we need to pull it first then clone
	if match.IsHub() {
		// Pull the Hub template to a temp location, then clone
		return cloneFromHubTemplate(hubCtx, match, destName, destGlobal)
	}

	// Local source - use existing clone function with the resolved path
	// We need to use the resolved path directly
	if err := cloneLocalTemplate(match.LocalPath, destName, destGlobal); err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(ActionResult{
			Status:  "success",
			Command: "templates clone",
			Message: fmt.Sprintf("Template '%s' cloned to '%s' successfully.", srcName, destName),
			Details: map[string]interface{}{
				"source":      srcName,
				"destination": destName,
				"global":      destGlobal,
			},
		})
	}
	fmt.Printf("Template '%s' cloned to '%s' successfully.\n", srcName, destName)
	return nil
}

// cloneLocalTemplate clones from a local path to a destination.
func cloneLocalTemplate(srcPath, destName string, destGlobal bool) error {
	var destTemplatesDir string
	var err error

	if destGlobal {
		destTemplatesDir, err = config.GetGlobalTemplatesDir()
	} else {
		destTemplatesDir, err = config.GetProjectTemplatesDir()
	}
	if err != nil {
		return err
	}

	destPath := filepath.Join(destTemplatesDir, destName)
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("template %s already exists at %s", destName, destPath)
	}

	return util.CopyDir(srcPath, destPath)
}

// cloneFromHubTemplate pulls a Hub template and clones it locally.
func cloneFromHubTemplate(hubCtx *HubContext, match *TemplateMatch, destName string, destGlobal bool) error {
	if hubCtx == nil {
		return fmt.Errorf("Hub context not available for cloning Hub template")
	}

	// Determine destination path
	var destTemplatesDir string
	var err error

	if destGlobal {
		destTemplatesDir, err = config.GetGlobalTemplatesDir()
	} else {
		destTemplatesDir, err = config.GetProjectTemplatesDir()
	}
	if err != nil {
		return err
	}

	destPath := filepath.Join(destTemplatesDir, destName)
	if _, err := os.Stat(destPath); err == nil {
		return fmt.Errorf("template %s already exists at %s", destName, destPath)
	}

	// Pull directly to destination path
	fmt.Printf("Cloning Hub template '%s' to local '%s'...\n", match.Name, destName)
	return pullTemplateFromHubMatch(hubCtx, match, destPath)
}

var templatesUpdateDefaultCmd = &cobra.Command{
	Use:   "update-default",
	Short: "Update the global default template with the latest from the binary",
	Long: `Updates the default template in the global grove (~/.scion/templates/default)
with the latest embedded defaults from the scion binary.

If the default template already exists, a warning is printed and no changes
are made. Use --force to overwrite the existing default template.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		harnesses := harness.All()
		err := config.UpdateDefaultTemplates(force, harnesses)
		if err != nil {
			return err
		}
		if isJSONOutput() {
			return outputJSON(ActionResult{
				Status:  "success",
				Command: "templates update-default",
				Message: "Default templates updated successfully.",
			})
		}
		fmt.Println("Default templates updated successfully.")
		return nil
	},
}

// templatesSyncCmd creates or updates a template in the Hub (upsert).
var templatesSyncCmd = &cobra.Command{
	Use:   "sync [template]",
	Short: "Create or update a template in the Hub (Hub only)",
	Long: `Sync a local template to the Hub. Creates the template if it doesn't exist,
or updates it with any changed files if it does.

The harness type is automatically detected from the template's configuration file.
Use the root --global flag to sync to global scope instead of grove scope.

Use --all to sync all grove-scoped local templates to the Hub at once.

Examples:
  # Sync a local template to the Hub (grove scope by default)
  scion templates sync custom-claude

  # Sync all grove templates to Hub
  scion templates sync --all

  # Sync with global scope
  scion --global templates sync custom-claude

  # Sync with a different name on the Hub
  scion templates sync custom-claude --name my-team-claude`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTemplateSync,
}

// templatesPushCmd is a semantic alias for sync.
var templatesPushCmd = &cobra.Command{
	Use:   "push [template]",
	Short: "Upload local template to Hub (alias for sync)",
	Long: `Push a local template to the Hub. This is a semantic alias for 'sync'.

Examples:
  # Push a local template to the Hub
  scion templates push custom-claude

  # Push all grove templates to Hub
  scion templates push --all

  # Push with global scope
  scion --global templates push custom-claude`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTemplateSync,
}

// runTemplateSync implements the shared logic for sync and push commands.
func runTemplateSync(cmd *cobra.Command, args []string) error {
	// Get flags - handle nil cmd for testing
	var hubName string
	var syncAll bool
	if cmd != nil {
		hubName, _ = cmd.Flags().GetString("name")
		syncAll, _ = cmd.Flags().GetBool("all")
	}

	// Validate args: either --all or a template name is required
	if !syncAll && len(args) == 0 {
		return fmt.Errorf("requires a template name argument or --all flag")
	}
	if syncAll && len(args) > 0 {
		return fmt.Errorf("cannot specify both a template name and --all")
	}
	if syncAll && hubName != "" {
		return fmt.Errorf("cannot use --name with --all")
	}

	// Check Hub availability first (we need it for sync anyway)
	hubCtx, err := CheckHubAvailability(grovePath)
	if err != nil {
		return err
	}
	if hubCtx == nil {
		return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
	}

	PrintUsingHub(hubCtx.Endpoint)

	// Determine destination scope from root's --global flag
	destScope := "grove"
	if globalMode {
		destScope = "global"
	}

	if syncAll {
		return syncAllTemplatesToHub(hubCtx, destScope)
	}

	localTemplateName := args[0]

	// If no explicit Hub name, use the local template name
	if hubName == "" {
		hubName = localTemplateName
	}

	// Build resolution options - local only for source, since we're syncing TO hub
	opts := &ResolveOpts{
		LocalOnly:   true,
		AutoConfirm: autoConfirm,
	}

	ctx := context.Background()
	match, err := ResolveTemplate(ctx, localTemplateName, nil, opts, "sync")
	if err != nil {
		return fmt.Errorf("template '%s' not found locally: %w", localTemplateName, err)
	}

	if !match.IsLocal() {
		return fmt.Errorf("internal error: expected local template but got hub")
	}

	// Create template object for harness detection
	tpl := &config.Template{Name: match.Name, Path: match.LocalPath}

	// Detect harness type from template config (optional - can be resolved during agent provisioning)
	harnessType, err := detectHarnessType(tpl)
	if err != nil {
		return fmt.Errorf("failed to read template config: %w", err)
	}

	return syncTemplateToHub(hubCtx, hubName, tpl.Path, destScope, harnessType)
}

// syncAllTemplatesToHub syncs all local grove templates to the Hub.
func syncAllTemplatesToHub(hubCtx *HubContext, scope string) error {
	// Get local templates based on scope
	localGlobal, localGrove, err := config.ListTemplatesGrouped()
	if err != nil {
		return fmt.Errorf("failed to list local templates: %w", err)
	}

	var templates []*config.Template
	if scope == "global" {
		templates = localGlobal
	} else {
		templates = localGrove
	}

	if len(templates) == 0 {
		fmt.Printf("No local %s templates found to sync.\n", scope)
		return nil
	}

	fmt.Printf("Syncing %d %s template(s) to Hub...\n", len(templates), scope)

	var synced, failed int
	for _, tpl := range templates {
		harnessType, err := detectHarnessType(tpl)
		if err != nil {
			fmt.Printf("  %s: failed to detect harness type: %v\n", tpl.Name, err)
			failed++
			continue
		}

		err = syncTemplateToHub(hubCtx, tpl.Name, tpl.Path, scope, harnessType)
		if err != nil {
			fmt.Printf("  %s: failed: %v\n", tpl.Name, err)
			failed++
			continue
		}
		synced++
	}

	fmt.Printf("\n%d template(s) synced", synced)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println()

	return nil
}

// templatesPullCmd downloads a template from the Hub.
var templatesPullCmd = &cobra.Command{
	Use:   "pull <name>",
	Short: "Download a template from Hub to local cache (Hub only)",
	Long: `Pull a template from the Hub to the local filesystem.

Examples:
  # Pull a template from Hub
  scion template pull custom-claude

  # Pull to a specific location
  scion template pull custom-claude --to .scion/templates/custom`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplatePull,
}

func runTemplatePull(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Get flags - handle nil cmd for testing
	var toPath string
	if cmd != nil {
		toPath, _ = cmd.Flags().GetString("to")
	}

	// Check Hub availability
	hubCtx, err := CheckHubAvailability(grovePath)
	if err != nil {
		return err
	}
	if hubCtx == nil {
		return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
	}

	PrintUsingHub(hubCtx.Endpoint)

	// Build resolution options - Hub only for pull
	opts := &ResolveOpts{
		HubOnly:     true,
		GroveOnly:   false,
		GlobalOnly:  globalMode,
		AutoConfirm: autoConfirm,
	}

	ctx := context.Background()
	match, err := ResolveTemplate(ctx, name, hubCtx, opts, "pull")
	if err != nil {
		return err
	}

	if !match.IsHub() {
		return fmt.Errorf("internal error: expected Hub template but got local")
	}

	return pullTemplateFromHubMatch(hubCtx, match, toPath)
}

// pullTemplateFromHubMatch downloads a template from a resolved Hub match.
func pullTemplateFromHubMatch(hubCtx *HubContext, match *TemplateMatch, toPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	template := match.HubTemplate
	name := template.Name

	// Determine destination path
	destPath := toPath
	if destPath == "" {
		// Default to project templates directory
		projectTemplatesDir, err := config.GetProjectTemplatesDir()
		if err != nil {
			return fmt.Errorf("failed to get templates directory: %w", err)
		}
		destPath = filepath.Join(projectTemplatesDir, name)
	} else {
		var err error
		destPath, err = filepath.Abs(toPath)
		if err != nil {
			return fmt.Errorf("failed to resolve path: %w", err)
		}
	}

	// Create destination directory
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// List files in template
	fmt.Printf("Fetching file list for template '%s'...\n", name)
	listResp, err := hubCtx.Client.Templates().ListFiles(ctx, template.ID)
	if err != nil {
		return fmt.Errorf("failed to list template files: %w", err)
	}

	// Download files directly from hub
	fmt.Printf("Downloading %d files to %s...\n", len(listResp.Files), destPath)
	for _, entry := range listResp.Files {
		filePath := filepath.Join(destPath, filepath.FromSlash(entry.Path))

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", entry.Path, err)
		}

		// Read file content directly from hub
		fileResp, readErr := hubCtx.Client.Templates().ReadFile(ctx, template.ID, entry.Path)
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", entry.Path, readErr)
		}

		// Write file
		if err := os.WriteFile(filePath, []byte(fileResp.Content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", entry.Path, err)
		}
		fmt.Printf("  Downloaded: %s\n", entry.Path)
	}

	if isJSONOutput() {
		return outputJSON(ActionResult{
			Status:  "success",
			Command: "templates pull",
			Message: fmt.Sprintf("Template '%s' pulled successfully.", name),
			Details: map[string]interface{}{
				"name":        name,
				"id":          template.ID,
				"destination": destPath,
				"filesCount":  len(listResp.Files),
			},
		})
	}

	fmt.Printf("Template '%s' pulled successfully to %s\n", name, destPath)

	return nil
}

// syncTemplateToHub creates or updates a template in the Hub.
// If a template with the same name already exists, only changed files are uploaded.
func syncTemplateToHub(hubCtx *HubContext, name, localPath, scope, harnessType string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Default scope
	if scope == "" {
		scope = "grove"
	}

	// Collect local files
	fmt.Printf("Scanning template files in %s...\n", localPath)
	files, err := hubclient.CollectFiles(localPath, nil)
	if err != nil {
		return fmt.Errorf("failed to scan template files: %w", err)
	}
	fmt.Printf("Found %d files\n", len(files))

	// Get grove ID for grove scope
	var groveID string
	if scope == "grove" {
		groveID, err = GetGroveID(hubCtx)
		if err != nil {
			return err
		}
	}

	// Check if a template with this name already exists in the same scope
	var templateID string
	existingResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{
		Name:    name,
		Scope:   scope,
		GroveID: groveID,
		Status:  "active",
	})
	if err != nil {
		return fmt.Errorf("failed to check for existing template: %w", err)
	}

	// Find exact name match
	var existingTemplate *hubclient.Template
	for i := range existingResp.Templates {
		if existingResp.Templates[i].Name == name {
			existingTemplate = &existingResp.Templates[i]
			break
		}
	}

	// Build a map of local files by path for easy lookup
	localFileMap := make(map[string]*hubclient.FileInfo)
	for i := range files {
		localFileMap[files[i].Path] = &files[i]
	}

	// Determine which files need uploading
	var filesToUpload []*hubclient.FileInfo

	if existingTemplate != nil {
		templateID = existingTemplate.ID

		// Fetch existing file manifest to compare hashes
		fmt.Printf("Checking for changes in template '%s'...\n", name)
		listResp, listErr := hubCtx.Client.Templates().ListFiles(ctx, templateID)

		uploadAll := false
		if listErr != nil {
			// Template exists but has no files — upload everything
			if strings.Contains(listErr.Error(), "template has no files") || (listResp != nil && listResp.TotalCount == 0) {
				fmt.Printf("Template '%s' exists but has no files. Uploading all files...\n", name)
				uploadAll = true
			} else {
				return fmt.Errorf("failed to list template files: %w", listErr)
			}
		}

		if uploadAll {
			for i := range files {
				filesToUpload = append(filesToUpload, &files[i])
			}
		} else {
			// Build map of remote file hashes from the manifest list
			remoteHashes := make(map[string]string)
			if listResp != nil {
				for _, f := range listResp.Files {
					// ListFiles doesn't return hashes directly; use ReadFile for
					// hash comparison. But we can compare by reading the existing
					// template's file list from the Get endpoint which includes hashes.
					remoteHashes[f.Path] = "" // placeholder
				}
			}

			// Get template with full file hashes for diffing
			tmpl, getErr := hubCtx.Client.Templates().Get(ctx, templateID)
			if getErr == nil && tmpl != nil {
				for _, f := range tmpl.Files {
					remoteHashes[f.Path] = f.Hash
				}
			}

			// Compare local vs remote
			for i := range files {
				remoteHash, exists := remoteHashes[files[i].Path]
				if !exists || remoteHash != files[i].Hash {
					filesToUpload = append(filesToUpload, &files[i])
				}
			}

			if len(filesToUpload) == 0 {
				fmt.Printf("Template '%s' is already up to date.\n", name)
				fmt.Printf("  ID: %s\n", templateID)
				fmt.Printf("  Content Hash: %s\n", truncateHash(existingTemplate.ContentHash))
				return nil
			}

			fmt.Printf("Found %d changed file(s), updating template...\n", len(filesToUpload))
		}
	} else {
		// Create new template
		fmt.Printf("Creating template '%s' in Hub...\n", name)
		createReq := &hubclient.CreateTemplateRequest{
			Name:    name,
			Harness: harnessType,
			Scope:   scope,
			GroveID: groveID,
		}

		resp, createErr := hubCtx.Client.Templates().Create(ctx, createReq)
		if createErr != nil {
			return fmt.Errorf("failed to create template: %w", createErr)
		}

		templateID = resp.Template.ID
		fmt.Printf("Template created with ID: %s\n", templateID)

		// All files need to be uploaded for new templates
		for i := range files {
			filesToUpload = append(filesToUpload, &files[i])
		}
	}

	// Upload files directly to hub (no signed URLs needed)
	fmt.Printf("Uploading %d file(s)...\n", len(filesToUpload))
	for _, fileInfo := range filesToUpload {
		content, readErr := os.ReadFile(fileInfo.FullPath)
		if readErr != nil {
			return fmt.Errorf("failed to read %s: %w", fileInfo.Path, readErr)
		}

		_, writeErr := hubCtx.Client.Templates().WriteFile(ctx, templateID, fileInfo.Path, string(content))
		if writeErr != nil {
			return fmt.Errorf("failed to upload %s: %w", fileInfo.Path, writeErr)
		}
		fmt.Printf("  Uploaded: %s\n", fileInfo.Path)
	}

	// Get final template state (WriteFile updates manifest + hash atomically)
	template, err := hubCtx.Client.Templates().Get(ctx, templateID)
	if err != nil {
		return fmt.Errorf("failed to get template after upload: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(ActionResult{
			Status:  "success",
			Command: "templates sync",
			Message: fmt.Sprintf("Template '%s' synced successfully.", name),
			Details: map[string]interface{}{
				"id":            template.ID,
				"name":          name,
				"status":        template.Status,
				"contentHash":   template.ContentHash,
				"scope":         scope,
				"filesUploaded": len(filesToUpload),
			},
		})
	}

	fmt.Printf("Template '%s' synced successfully!\n", name)
	fmt.Printf("  ID: %s\n", template.ID)
	fmt.Printf("  Status: %s\n", template.Status)
	fmt.Printf("  Content Hash: %s\n", truncateHash(template.ContentHash))

	return nil
}

// templatesStatusCmd shows the sync state of templates between local and Hub.
var templatesStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show template sync status between local and Hub",
	Long: `Show the sync status of templates between the local filesystem and the Hub.

Compares local templates with Hub templates to determine which are synced,
out of date, or only present in one location.

Examples:
  # Show sync status for grove templates
  scion templates status

  # Show sync status for global templates
  scion --global templates status`,
	RunE: runTemplateStatus,
}

func runTemplateStatus(cmd *cobra.Command, args []string) error {
	// Get local templates grouped by scope
	localGlobal, localGrove, err := config.ListTemplatesGrouped()
	if err != nil {
		return fmt.Errorf("failed to list local templates: %w", err)
	}

	// Check Hub availability
	hubCtx, err := CheckHubAvailability(grovePath)
	if err != nil {
		return err
	}
	if hubCtx == nil {
		return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
	}

	PrintUsingHub(hubCtx.Endpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	groveID, _ := GetGroveID(hubCtx)

	// Fetch hub templates
	var hubGrove, hubGlobal []hubclient.Template
	if groveID != "" {
		resp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{
			Scope:   "grove",
			GroveID: groveID,
			Status:  "active",
		})
		if err == nil {
			hubGrove = resp.Templates
		}
	}
	globalResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{
		Scope:  "global",
		Status: "active",
	})
	if err == nil {
		hubGlobal = globalResp.Templates
	}

	// Determine which scope to show
	var localTemplates []*config.Template
	var hubTemplates []hubclient.Template
	scopeLabel := "grove"
	if globalMode {
		localTemplates = localGlobal
		hubTemplates = hubGlobal
		scopeLabel = "global"
	} else {
		localTemplates = localGrove
		hubTemplates = hubGrove
	}

	// Build status entries
	type statusEntry struct {
		Name      string `json:"name"`
		Local     bool   `json:"local"`
		Hub       bool   `json:"hub"`
		Status    string `json:"status"`
		LocalHash string `json:"localHash,omitempty"`
		HubHash   string `json:"hubHash,omitempty"`
	}

	// Build lookup maps
	localMap := make(map[string]*config.Template)
	for _, t := range localTemplates {
		localMap[t.Name] = t
	}
	hubMap := make(map[string]*hubclient.Template)
	for i := range hubTemplates {
		hubMap[hubTemplates[i].Name] = &hubTemplates[i]
	}

	// Collect all template names
	nameSet := make(map[string]bool)
	for _, t := range localTemplates {
		nameSet[t.Name] = true
	}
	for _, t := range hubTemplates {
		nameSet[t.Name] = true
	}

	var names []string
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	var entries []statusEntry
	for _, name := range names {
		local := localMap[name]
		hub := hubMap[name]

		entry := statusEntry{
			Name:  name,
			Local: local != nil,
			Hub:   hub != nil,
		}

		if local != nil && hub != nil {
			// Both exist - compare hashes
			files, err := hubclient.CollectFiles(local.Path, nil)
			if err == nil {
				localHash := computeLocalContentHash(files)
				entry.LocalHash = localHash
				entry.HubHash = hub.ContentHash
				if localHash == hub.ContentHash {
					entry.Status = "synced"
				} else {
					entry.Status = "out of date"
				}
			} else {
				entry.Status = "unknown (hash error)"
			}
		} else if local != nil {
			entry.Status = "local only"
		} else {
			entry.Status = "hub only"
		}

		entries = append(entries, entry)
	}

	if isJSONOutput() {
		return outputJSON(map[string]interface{}{
			"scope":     scopeLabel,
			"groveId":   groveID,
			"templates": entries,
		})
	}

	groveName := ""
	if groveID != "" {
		groveName = groveID
	}
	fmt.Printf("Grove: %s\n\n", groveName)

	if len(entries) == 0 {
		fmt.Println("No templates found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TEMPLATE\tLOCAL\tHUB\tSTATUS")
	for _, e := range entries {
		localStr := "no"
		if e.Local {
			localStr = "yes"
		}
		hubStr := "no"
		if e.Hub {
			hubStr = "yes"
		}
		status := e.Status
		if e.Status == "synced" {
			status = "synced (hash match)"
		} else if e.Status == "out of date" {
			status = "out of date (local differs)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name, localStr, hubStr, status)
	}
	w.Flush()

	return nil
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesShowCmd)
	templatesCmd.AddCommand(templatesCreateCmd)
	templatesCmd.AddCommand(templatesCloneCmd)
	templatesCmd.AddCommand(templatesDeleteCmd)
	templatesCmd.AddCommand(templatesUpdateDefaultCmd)

	// Import command
	templatesCmd.AddCommand(templatesImportCmd)

	// Hub-only commands
	templatesCmd.AddCommand(templatesSyncCmd)
	templatesCmd.AddCommand(templatesPushCmd)
	templatesCmd.AddCommand(templatesPullCmd)
	templatesCmd.AddCommand(templatesStatusCmd)

	// Flags for update-default command
	templatesUpdateDefaultCmd.Flags().Bool("force", false, "Overwrite the existing default template")

	// Flags for show command
	templatesShowCmd.Flags().Bool("local", false, "Only search local filesystem")
	templatesShowCmd.Flags().Bool("hub", false, "Only search Hub")

	// Flags for delete command
	templatesDeleteCmd.Flags().Bool("local", false, "Only search local filesystem")
	templatesDeleteCmd.Flags().Bool("hub", false, "Only search Hub")

	// Flags for clone command
	templatesCloneCmd.Flags().Bool("local", false, "Only search local filesystem for source")
	templatesCloneCmd.Flags().Bool("hub", false, "Only search Hub for source")

	// Flags for import command
	templatesImportCmd.Flags().StringP("harness", "H", "", "Force harness type (claude, gemini)")
	templatesImportCmd.Flags().String("name", "", "Override template name")
	templatesImportCmd.Flags().Bool("force", false, "Overwrite existing templates")
	templatesImportCmd.Flags().Bool("dry-run", false, "Preview import without writing files")
	templatesImportCmd.Flags().Bool("all", false, "Import all discovered agents")

	// Flags for sync command (--global is inherited from root)
	templatesSyncCmd.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	templatesSyncCmd.Flags().Bool("all", false, "Sync all local templates to the Hub")

	// Flags for push command (same as sync, since push is an alias)
	templatesPushCmd.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	templatesPushCmd.Flags().Bool("all", false, "Push all local templates to the Hub")

	// Flags for pull command
	templatesPullCmd.Flags().String("to", "", "Destination path for downloaded template")

	// Also add a 'template' alias (singular) for convenience
	templateCmd := &cobra.Command{
		Use:   "template",
		Short: "Manage agent templates (alias for 'templates')",
		Long:  `List and inspect templates used to provision new agents.`,
	}
	rootCmd.AddCommand(templateCmd)
	templateCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available templates",
		RunE:  runTemplateList,
	})
	showAlias := &cobra.Command{
		Use:   "show <name>",
		Short: "Show template configuration",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateShow,
	}
	showAlias.Flags().Bool("local", false, "Only search local filesystem")
	showAlias.Flags().Bool("hub", false, "Only search Hub")
	templateCmd.AddCommand(showAlias)

	deleteAlias := &cobra.Command{
		Use:     "delete <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a template",
		Args:    cobra.ExactArgs(1),
		RunE:    runTemplateDelete,
	}
	deleteAlias.Flags().Bool("local", false, "Only search local filesystem")
	deleteAlias.Flags().Bool("hub", false, "Only search Hub")
	templateCmd.AddCommand(deleteAlias)

	cloneAlias := &cobra.Command{
		Use:   "clone <src-name> <dest-name>",
		Short: "Clone an existing template",
		Args:  cobra.ExactArgs(2),
		RunE:  runTemplateClone,
	}
	cloneAlias.Flags().Bool("local", false, "Only search local filesystem for source")
	cloneAlias.Flags().Bool("hub", false, "Only search Hub for source")
	templateCmd.AddCommand(cloneAlias)

	// Add sync, push, pull, status to singular alias (--global is inherited from root)
	syncAlias := &cobra.Command{
		Use:   "sync [template]",
		Short: "Create or update a template in the Hub (Hub only)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTemplateSync,
	}
	syncAlias.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	syncAlias.Flags().Bool("all", false, "Sync all local templates to the Hub")
	templateCmd.AddCommand(syncAlias)

	pushAlias := &cobra.Command{
		Use:   "push [template]",
		Short: "Upload local template to Hub (alias for sync)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTemplateSync,
	}
	pushAlias.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	pushAlias.Flags().Bool("all", false, "Push all local templates to the Hub")
	templateCmd.AddCommand(pushAlias)

	statusAlias := &cobra.Command{
		Use:   "status",
		Short: "Show template sync status between local and Hub",
		RunE:  runTemplateStatus,
	}
	templateCmd.AddCommand(statusAlias)

	pullAlias := &cobra.Command{
		Use:   "pull <name>",
		Short: "Download a template from Hub to local cache (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplatePull,
	}
	pullAlias.Flags().String("to", "", "Destination path for downloaded template")
	templateCmd.AddCommand(pullAlias)

	importAlias := &cobra.Command{
		Use:   "import <source>",
		Short: "Import agent definitions as scion templates",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateImport,
	}
	importAlias.Flags().StringP("harness", "H", "", "Force harness type (claude, gemini)")
	importAlias.Flags().String("name", "", "Override template name")
	importAlias.Flags().Bool("force", false, "Overwrite existing templates")
	importAlias.Flags().Bool("dry-run", false, "Preview import without writing files")
	importAlias.Flags().Bool("all", false, "Import all discovered agents")
	templateCmd.AddCommand(importAlias)
}
