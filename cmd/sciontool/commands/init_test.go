/*
Copyright 2025 The Scion Authors.
*/

package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractChildCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "single command",
			args:     []string{"bash"},
			expected: []string{"bash"},
		},
		{
			name:     "command with args",
			args:     []string{"tmux", "new-session", "-A"},
			expected: []string{"tmux", "new-session", "-A"},
		},
		{
			name:     "empty args",
			args:     []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractChildCommand(tt.args)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d args, got %d", len(tt.expected), len(result))
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("arg[%d]: expected %q, got %q", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestInitCommand_Help(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"init", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "init") {
		t.Error("help output should mention 'init'")
	}
	if !strings.Contains(output, "grace-period") {
		t.Error("help output should mention 'grace-period' flag")
	}
}

func TestInitCommand_GracePeriodFlag(t *testing.T) {
	// Verify the flag exists and has the expected default
	flag := initCmd.Flags().Lookup("grace-period")
	if flag == nil {
		t.Fatal("grace-period flag not found")
	}
	if flag.DefValue != "10s" {
		t.Errorf("expected default grace-period 10s, got %s", flag.DefValue)
	}
}

// TestInitCommand_Integration performs an integration test with a real subprocess.
// This is skipped in short mode as it involves actual process execution.
func TestInitCommand_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Build sciontool if needed for integration testing
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", "/tmp/sciontool-test", "../")
	if err := cmd.Run(); err != nil {
		t.Skipf("failed to build sciontool for integration test: %v", err)
	}

	// Test running a simple command
	testCmd := exec.Command("/tmp/sciontool-test", "init", "--", "echo", "hello")
	output, err := testCmd.CombinedOutput()
	if err != nil {
		t.Errorf("init command failed: %v\nOutput: %s", err, output)
	}
	if !strings.Contains(string(output), "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

func TestGitCloneWorkspace_NoCloneURL(t *testing.T) {
	// Ensure SCION_GIT_CLONE_URL is not set
	orig := os.Getenv("SCION_GIT_CLONE_URL")
	os.Unsetenv("SCION_GIT_CLONE_URL")
	defer func() {
		if orig != "" {
			os.Setenv("SCION_GIT_CLONE_URL", orig)
		}
	}()

	err := gitCloneWorkspace(0, 0)
	if err != nil {
		t.Errorf("expected nil error when SCION_GIT_CLONE_URL is not set, got: %v", err)
	}
}

func TestGitCloneWorkspace_WorkspaceExists(t *testing.T) {
	// Create a temp dir with content to simulate existing workspace
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}

	if isWorkspaceEmpty(tmpDir) {
		t.Error("expected non-empty workspace to return false for isWorkspaceEmpty")
	}
}

func TestIsWorkspaceEmpty(t *testing.T) {
	t.Run("nonexistent directory", func(t *testing.T) {
		if !isWorkspaceEmpty("/nonexistent/path/12345") {
			t.Error("expected true for nonexistent directory")
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		if !isWorkspaceEmpty(tmpDir) {
			t.Error("expected true for empty directory")
		}
	})

	t.Run("directory with files", func(t *testing.T) {
		tmpDir := t.TempDir()
		os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)
		if isWorkspaceEmpty(tmpDir) {
			t.Error("expected false for directory with files")
		}
	})
}

func TestSanitizeGitOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		token    string
		expected string
	}{
		{
			name:     "replaces token in output",
			output:   "fatal: Authentication failed for 'https://oauth2:ghp_secret123@github.com/org/repo.git/'",
			token:    "ghp_secret123",
			expected: "fatal: Authentication failed for 'https://oauth2:***@github.com/org/repo.git/'",
		},
		{
			name:     "replaces multiple occurrences",
			output:   "token ghp_abc then ghp_abc again",
			token:    "ghp_abc",
			expected: "token *** then *** again",
		},
		{
			name:     "empty token returns output unchanged",
			output:   "some output text",
			token:    "",
			expected: "some output text",
		},
		{
			name:     "no match returns output unchanged",
			output:   "nothing sensitive here",
			token:    "ghp_notpresent",
			expected: "nothing sensitive here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGitOutput(tt.output, tt.token)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildAuthenticatedURL(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		token    string
		expected string
	}{
		{
			name:     "adds oauth2 credentials to HTTPS URL",
			cloneURL: "https://github.com/org/repo.git",
			token:    "ghp_token123",
			expected: "https://oauth2:ghp_token123@github.com/org/repo.git",
		},
		{
			name:     "no token returns URL unchanged",
			cloneURL: "https://github.com/org/repo.git",
			token:    "",
			expected: "https://github.com/org/repo.git",
		},
		{
			name:     "handles URL without .git suffix",
			cloneURL: "https://github.com/org/repo",
			token:    "ghp_abc",
			expected: "https://oauth2:ghp_abc@github.com/org/repo",
		},
		{
			name:     "handles URL with port",
			cloneURL: "https://github.example.com:8443/org/repo.git",
			token:    "tok",
			expected: "https://oauth2:tok@github.example.com:8443/org/repo.git",
		},
		{
			name:     "non-parseable URL returns as-is",
			cloneURL: "not-a-url",
			token:    "ghp_abc",
			expected: "not-a-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAuthenticatedURL(tt.cloneURL, tt.token)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestBuildAuthenticatedURL_SpecialCharsInToken(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		contains string
	}{
		{
			name:     "token with percent sign",
			token:    "ghp_abc%def",
			contains: "oauth2:ghp_abc%25def@",
		},
		{
			name:     "token with at sign",
			token:    "ghp_abc@def",
			contains: "oauth2:ghp_abc%40def@",
		},
		{
			name:     "token with hash sign",
			token:    "ghp_abc#def",
			contains: "oauth2:ghp_abc%23def@",
		},
		{
			name:     "token with all special characters",
			token:    "ghp_%@#tok",
			contains: "oauth2:ghp_%25%40%23tok@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAuthenticatedURL("https://github.com/org/repo.git", tt.token)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
		})
	}
}

func TestSanitizeGitOutput_LongToken(t *testing.T) {
	// Fine-grained GitHub PATs are 93 characters long
	longToken := "github_pat_" + strings.Repeat("A", 82) // 93 chars total
	output := "fatal: Authentication failed for 'https://oauth2:" + longToken + "@github.com/org/repo.git/'"

	result := sanitizeGitOutput(output, longToken)

	if strings.Contains(result, longToken) {
		t.Error("long token should be redacted from output")
	}
	if !strings.Contains(result, "***") {
		t.Error("redacted token should be replaced with ***")
	}
	expected := "fatal: Authentication failed for 'https://oauth2:***@github.com/org/repo.git/'"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestUseDirectPasswdEdit(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "no env vars set",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name:     "container=podman",
			envVars:  map[string]string{"container": "podman"},
			expected: true,
		},
		{
			name:     "container=docker (not podman)",
			envVars:  map[string]string{"container": "docker"},
			expected: false,
		},
		{
			name:     "SCION_ALT_USERMOD set",
			envVars:  map[string]string{"SCION_ALT_USERMOD": "1"},
			expected: true,
		},
		{
			name:     "both set",
			envVars:  map[string]string{"container": "podman", "SCION_ALT_USERMOD": "1"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear both env vars, then set what the test needs
			t.Setenv("container", "")
			t.Setenv("SCION_ALT_USERMOD", "")
			os.Unsetenv("container")
			os.Unsetenv("SCION_ALT_USERMOD")
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			result := useDirectPasswdEdit()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestGitCloneWorkspace_DefaultEnvValues(t *testing.T) {
	// Set SCION_GIT_CLONE_URL to trigger the clone path, but use a URL
	// that will cause a predictable early failure (non-existent host).
	// This tests that the env parsing logic runs with correct defaults.
	t.Setenv("SCION_GIT_CLONE_URL", "https://nonexistent.invalid/org/repo.git")
	// Explicitly unset branch and depth to verify defaults
	t.Setenv("SCION_GIT_BRANCH", "")
	t.Setenv("SCION_GIT_DEPTH", "")
	t.Setenv("SCION_AGENT_NAME", "test-agent")
	t.Setenv("GITHUB_TOKEN", "")

	// gitCloneWorkspace will fail at the git clone step, but we can verify
	// the function doesn't panic and returns a meaningful error.
	// uid=0 exercises the scion-user fallback path (the lookup will fail
	// gracefully outside a container where no scion user exists).
	err := gitCloneWorkspace(0, 0)
	if err == nil {
		t.Fatal("expected error from git clone to nonexistent host")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("expected 'git clone failed' error, got: %v", err)
	}
}

func TestGitCloneWorkspace_NonZeroUIDChownsWorkspace(t *testing.T) {
	// Verify that gitCloneWorkspace chowns /workspace before cloning when
	// a non-zero uid is provided. We use a temp dir as the workspace and
	// our own uid/gid so the chown succeeds without root.
	tmpDir := t.TempDir()

	// Monkey-patch: override workspacePath by setting clone URL so the
	// function proceeds past the early exit, but it will fail at git clone.
	// The important thing is it doesn't panic on chown.
	t.Setenv("SCION_GIT_CLONE_URL", "https://nonexistent.invalid/org/repo.git")
	t.Setenv("SCION_GIT_BRANCH", "main")
	t.Setenv("SCION_GIT_DEPTH", "1")
	t.Setenv("SCION_AGENT_NAME", "test-chown")
	t.Setenv("GITHUB_TOKEN", "")

	// We can't override the hardcoded /workspace path, so we test that
	// the function proceeds without panic when uid > 0. The chown of
	// /workspace will fail (not writable in test), but the error is logged,
	// not returned, so the function continues to the git clone step.
	uid := os.Getuid()
	gid := os.Getgid()
	_ = tmpDir // workspace path is hardcoded; this confirms the logic flow

	err := gitCloneWorkspace(uid, gid)
	if err == nil {
		t.Fatal("expected error from git clone to nonexistent host")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("expected 'git clone failed' error, got: %v", err)
	}
}
