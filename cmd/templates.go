package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/ptone/scion-agent/pkg/config"
	"github.com/ptone/scion-agent/pkg/harness"
	"github.com/ptone/scion-agent/pkg/hubclient"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		templates, err := config.ListTemplates()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPATH")
		for _, t := range templates {
			fmt.Fprintf(w, "%s\t%s\n", t.Name, t.Path)
		}
		w.Flush()
		return nil
	},
}

var templatesShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show template configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		tpl, err := config.FindTemplate(name)
		if err != nil {
			return err
		}

		cfg, err := tpl.LoadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Template: %s\n", tpl.Name)
		fmt.Printf("Path:     %s\n", tpl.Path)
		fmt.Println("Configuration (scion-agent.json):")

		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(cfg)
	},
}

var templatesCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		harnessName, _ := cmd.Flags().GetString("harness")
		if harnessName == "" {
			harnessName = "gemini"
		}

		h := harness.New(harnessName)

		err := config.CreateTemplate(name, h, global)
		if err != nil {
			return err
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
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		global, _ := cmd.Flags().GetBool("global")
		err := config.DeleteTemplate(name, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s deleted successfully.\n", name)
		return nil
	},
}

var templatesCloneCmd = &cobra.Command{
	Use:   "clone <src-name> <dest-name>",
	Short: "Clone an existing template",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcName := args[0]
		destName := args[1]
		global, _ := cmd.Flags().GetBool("global")
		err := config.CloneTemplate(srcName, destName, global)
		if err != nil {
			return err
		}
		fmt.Printf("Template %s cloned to %s successfully.\n", srcName, destName)
		return nil
	},
}

var templatesUpdateDefaultCmd = &cobra.Command{
	Use:   "update-default",
	Short: "Update default templates with the latest from the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		global, _ := cmd.Flags().GetBool("global")
		harnesses := harness.All()
		err := config.UpdateDefaultTemplates(global, harnesses)
		if err != nil {
			return err
		}
		fmt.Println("Default templates updated successfully.")
		return nil
	},
}

// templatesSyncCmd creates or updates a template in the Hub (upsert).
var templatesSyncCmd = &cobra.Command{
	Use:   "sync <template>",
	Short: "Create or update a template in the Hub (Hub only)",
	Long: `Sync a local template to the Hub. Creates the template if it doesn't exist,
or updates it if it does. This is an upsert operation.

The harness type is automatically detected from the template's configuration file.
Use the root --global flag to sync to global scope instead of grove scope.

Examples:
  # Sync a local template to the Hub (grove scope by default)
  scion templates sync custom-claude

  # Sync with global scope
  scion --global templates sync custom-claude

  # Sync with a different name on the Hub
  scion templates sync custom-claude --name my-team-claude`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplateSync,
}

// templatesPushCmd is a semantic alias for sync.
var templatesPushCmd = &cobra.Command{
	Use:   "push <template>",
	Short: "Upload local template to Hub (alias for sync)",
	Long: `Push a local template to the Hub. This is a semantic alias for 'sync'.

Examples:
  # Push a local template to the Hub
  scion templates push custom-claude

  # Push with global scope
  scion --global templates push custom-claude`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplateSync,
}

// runTemplateSync implements the shared logic for sync and push commands.
func runTemplateSync(cmd *cobra.Command, args []string) error {
	localTemplateName := args[0]
	hubName, _ := cmd.Flags().GetString("name")

	// Determine scope from root's --global flag
	scope := "grove"
	if globalMode {
		scope = "global"
	}

	// If no explicit Hub name, use the local template name
	if hubName == "" {
		hubName = localTemplateName
	}

	// Find the local template
	tpl, err := config.FindTemplate(localTemplateName)
	if err != nil {
		return fmt.Errorf("template '%s' not found locally: %w", localTemplateName, err)
	}

	// Detect harness type from template config
	harnessType, err := detectHarnessType(tpl)
	if err != nil {
		return fmt.Errorf("failed to detect harness type: %w\n\n"+
			"Ensure the template has a valid scion-agent.json with a 'harness' field", err)
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

	return syncTemplateToHub(hubCtx, hubName, tpl.Path, scope, harnessType)
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
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		toPath, _ := cmd.Flags().GetString("to")

		// Check Hub availability
		hubCtx, err := CheckHubAvailability(grovePath)
		if err != nil {
			return err
		}
		if hubCtx == nil {
			return fmt.Errorf("Hub integration is not enabled. Use 'scion hub enable' first")
		}

		PrintUsingHub(hubCtx.Endpoint)

		return pullTemplateFromHub(hubCtx, name, toPath)
	},
}

// syncTemplateToHub creates or updates a template in the Hub.
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

	// Build file upload request
	fileReqs := make([]hubclient.FileUploadRequest, len(files))
	for i, f := range files {
		fileReqs[i] = hubclient.FileUploadRequest{
			Path: f.Path,
			Size: f.Size,
		}
	}

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

	// Track which files need to be uploaded
	var filesToUpload []hubclient.FileUploadRequest

	if existingTemplate != nil {
		templateID = existingTemplate.ID

		// Fetch existing file manifest to compare hashes
		fmt.Printf("Checking for changes in template '%s'...\n", name)
		downloadResp, err := hubCtx.Client.Templates().RequestDownloadURLs(ctx, templateID)
		if err != nil {
			return fmt.Errorf("failed to get existing template manifest: %w", err)
		}

		// Build map of remote file hashes
		remoteHashes := make(map[string]string)
		for _, f := range downloadResp.Files {
			remoteHashes[f.Path] = f.Hash
		}

		// Compare local vs remote - find changed/new files
		for _, localFile := range files {
			remoteHash, exists := remoteHashes[localFile.Path]
			if !exists || remoteHash != localFile.Hash {
				filesToUpload = append(filesToUpload, hubclient.FileUploadRequest{
					Path: localFile.Path,
					Size: localFile.Size,
				})
			}
		}

		// Check if anything changed
		if len(filesToUpload) == 0 {
			fmt.Printf("Template '%s' is already up to date.\n", name)
			fmt.Printf("  ID: %s\n", templateID)
			fmt.Printf("  Content Hash: %s\n", existingTemplate.ContentHash)
			return nil
		}

		fmt.Printf("Found %d changed file(s), updating template...\n", len(filesToUpload))
	} else {
		// Create new template - upload all files
		fmt.Printf("Creating template '%s' in Hub...\n", name)
		createReq := &hubclient.CreateTemplateRequest{
			Name:    name,
			Harness: harnessType,
			Scope:   scope,
			GroveID: groveID,
		}

		resp, err := hubCtx.Client.Templates().Create(ctx, createReq)
		if err != nil {
			return fmt.Errorf("failed to create template: %w", err)
		}

		templateID = resp.Template.ID
		fmt.Printf("Template created with ID: %s\n", templateID)

		// All files need to be uploaded for new templates
		filesToUpload = fileReqs
	}

	// Request upload URLs only for files that need uploading
	fmt.Printf("Requesting upload URLs for %d file(s)...\n", len(filesToUpload))
	uploadResp, err := hubCtx.Client.Templates().RequestUploadURLs(ctx, templateID, filesToUpload)
	if err != nil {
		return fmt.Errorf("failed to get upload URLs: %w", err)
	}

	// Upload files
	fmt.Printf("Uploading %d file(s)...\n", len(uploadResp.UploadURLs))
	for _, urlInfo := range uploadResp.UploadURLs {
		fileInfo := localFileMap[urlInfo.Path]
		if fileInfo == nil {
			fmt.Printf("  Warning: no matching file for %s\n", urlInfo.Path)
			continue
		}

		// Open and upload file
		f, err := os.Open(fileInfo.FullPath)
		if err != nil {
			return fmt.Errorf("failed to open %s: %w", fileInfo.Path, err)
		}

		err = hubCtx.Client.Templates().UploadFile(ctx, urlInfo.URL, urlInfo.Method, urlInfo.Headers, f)
		f.Close()
		if err != nil {
			return fmt.Errorf("failed to upload %s: %w", fileInfo.Path, err)
		}
		fmt.Printf("  Uploaded: %s\n", fileInfo.Path)
	}

	// Build manifest
	manifest := &hubclient.TemplateManifest{
		Version: "1.0",
		Harness: harnessType,
		Files:   make([]hubclient.TemplateFile, len(files)),
	}
	for i, f := range files {
		manifest.Files[i] = hubclient.TemplateFile{
			Path: f.Path,
			Size: f.Size,
			Hash: f.Hash,
			Mode: f.Mode,
		}
	}

	// Finalize template
	fmt.Println("Finalizing template...")
	template, err := hubCtx.Client.Templates().Finalize(ctx, templateID, manifest)
	if err != nil {
		return fmt.Errorf("failed to finalize template: %w", err)
	}

	fmt.Printf("Template '%s' synced successfully!\n", name)
	fmt.Printf("  ID: %s\n", template.ID)
	fmt.Printf("  Status: %s\n", template.Status)
	fmt.Printf("  Content Hash: %s\n", template.ContentHash)

	return nil
}

// pullTemplateFromHub downloads a template from the Hub.
func pullTemplateFromHub(hubCtx *HubContext, name, toPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Find the template in Hub
	fmt.Printf("Looking up template '%s' in Hub...\n", name)

	listResp, err := hubCtx.Client.Templates().List(ctx, &hubclient.ListTemplatesOptions{})
	if err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}

	var template *hubclient.Template
	for i := range listResp.Templates {
		if listResp.Templates[i].Name == name || listResp.Templates[i].Slug == name {
			template = &listResp.Templates[i]
			break
		}
	}

	if template == nil {
		return fmt.Errorf("template '%s' not found in Hub", name)
	}

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

	// Request download URLs
	fmt.Printf("Requesting download URLs for template '%s'...\n", name)
	downloadResp, err := hubCtx.Client.Templates().RequestDownloadURLs(ctx, template.ID)
	if err != nil {
		return fmt.Errorf("failed to get download URLs: %w", err)
	}

	// Download files
	fmt.Printf("Downloading %d files to %s...\n", len(downloadResp.Files), destPath)
	for _, fileInfo := range downloadResp.Files {
		filePath := filepath.Join(destPath, filepath.FromSlash(fileInfo.Path))

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", fileInfo.Path, err)
		}

		// Download file content
		content, err := hubCtx.Client.Templates().DownloadFile(ctx, fileInfo.URL)
		if err != nil {
			return fmt.Errorf("failed to download %s: %w", fileInfo.Path, err)
		}

		// Write file
		if err := os.WriteFile(filePath, content, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", fileInfo.Path, err)
		}
		fmt.Printf("  Downloaded: %s\n", fileInfo.Path)
	}

	fmt.Printf("Template '%s' pulled successfully to %s\n", name, destPath)

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

	// Hub-only commands
	templatesCmd.AddCommand(templatesSyncCmd)
	templatesCmd.AddCommand(templatesPushCmd)
	templatesCmd.AddCommand(templatesPullCmd)

	// Flags for create command
	templatesCreateCmd.Flags().StringP("harness", "H", "", "Harness type (e.g. gemini, claude)")

	// Flags for sync command (--global is inherited from root)
	templatesSyncCmd.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")

	// Flags for push command (same as sync, since push is an alias)
	templatesPushCmd.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")

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
		RunE:  templatesListCmd.RunE,
	})
	templateCmd.AddCommand(&cobra.Command{
		Use:   "show <name>",
		Short: "Show template configuration",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesShowCmd.RunE,
	})
	// Add sync, push, pull to singular alias (--global is inherited from root)
	syncAlias := &cobra.Command{
		Use:   "sync <template>",
		Short: "Create or update a template in the Hub (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateSync,
	}
	syncAlias.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	templateCmd.AddCommand(syncAlias)

	pushAlias := &cobra.Command{
		Use:   "push <template>",
		Short: "Upload local template to Hub (alias for sync)",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateSync,
	}
	pushAlias.Flags().String("name", "", "Name for the template on the Hub (defaults to local template name)")
	templateCmd.AddCommand(pushAlias)

	pullAlias := &cobra.Command{
		Use:   "pull <name>",
		Short: "Download a template from Hub to local cache (Hub only)",
		Args:  cobra.ExactArgs(1),
		RunE:  templatesPullCmd.RunE,
	}
	pullAlias.Flags().String("to", "", "Destination path for downloaded template")
	templateCmd.AddCommand(pullAlias)
}
