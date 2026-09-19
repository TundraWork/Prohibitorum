package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestOpenAPIWithoutWebBundle(t *testing.T) {
	distPath, err := filepath.Abs("../../pkg/webui/dist")
	if err != nil {
		t.Fatal(err)
	}

	// Hide build artifacts through a Go overlay without touching the worktree.
	replacements := make(map[string]string)
	err = filepath.WalkDir(distPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && path != filepath.Join(distPath, ".gitkeep") {
			replacements[path] = ""
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	overlay, err := json.Marshal(map[string]any{"Replace": replacements})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", "-overlay", overlayPath, ".", "openapi")
	cmd.Env = append(os.Environ(), "PROHIBITORUM_DATABASE_URL=postgres://invalid.invalid/unavailable")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("openapi without SPA assets: %v\n%s", err, output)
	}
	var document struct {
		OpenAPI string `yaml:"openapi"`
		Paths   map[string]struct {
			Get struct {
				OperationID string `yaml:"operationId"`
			} `yaml:"get"`
		} `yaml:"paths"`
	}
	if err := yaml.Unmarshal(output, &document); err != nil {
		t.Fatalf("decode OpenAPI: %v\n%s", err, output)
	}
	if !strings.HasPrefix(document.OpenAPI, "3.") {
		t.Fatalf("OpenAPI version = %q, want 3.x", document.OpenAPI)
	}
	if operation := document.Paths["/api/prohibitorum/auth/status"].Get.OperationID; operation != "getAuthStatus" {
		t.Fatalf("auth status operation = %q, want getAuthStatus", operation)
	}
}
