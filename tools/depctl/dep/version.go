// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/mod/modfile"
)

// GetVersion retrieves the version of the specified dependency from go.mod
// For tagged versions, returns the version as-is
// For pseudo-versions, extracts and returns the first 8 chars of the hash
func GetVersion(target RepoTarget) (string, error) {
	modulePath, _, err := target.Resolve()
	if err != nil {
		return "", err
	}

	// Read go.mod file
	goModData, err := os.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	// Parse go.mod
	modFile, err := modfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to parse go.mod: %w", err)
	}

	// Find the require directive for the module
	var version string
	for _, req := range modFile.Require {
		if req.Mod.Path == modulePath {
			version = req.Mod.Version
			break
		}
	}

	if version == "" {
		return "", fmt.Errorf("module %q not found in go.mod", modulePath)
	}

	// Check if it's a pseudo-version (format: v0.0.0-YYYYMMDDHHMMSS-abcdefabcdef)
	if isPseudoVersion(version) {
		// Extract the hash suffix (last component after the last hyphen)
		parts := strings.Split(version, "-")
		if len(parts) >= 3 {
			hash := parts[len(parts)-1]
			// Return first 8 chars of hash
			if len(hash) >= 8 {
				return hash[:8], nil
			}
			return hash, nil
		}
	}

	// Return tagged version as-is
	return version, nil
}

// isPseudoVersion checks if a version string is a pseudo-version
// Pseudo-versions have format: vX.Y.Z-YYYYMMDDHHMMSS-hash
func isPseudoVersion(version string) bool {
	parts := strings.Split(version, "-")
	// Pseudo-versions have at least 3 parts: version, timestamp, hash
	if len(parts) < 3 {
		return false
	}
	// Check if second-to-last part looks like a timestamp (14 digits)
	timestamp := parts[len(parts)-2]
	if len(timestamp) != 14 {
		return false
	}
	// Check if all characters in timestamp are digits
	for _, c := range timestamp {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// UpdateVersion updates the version of the specified dependency in go.mod
// If the target is avalanchego, also updates GitHub workflow files to use the full SHA
func UpdateVersion(target RepoTarget, version string) error {
	modulePath, _, err := target.Resolve()
	if err != nil {
		return err
	}

	// Run go get to update the version
	cmd := exec.Command("go", "get", fmt.Sprintf("%s@%s", modulePath, version))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run go get: %w", err)
	}

	// Run go mod tidy
	cmd = exec.Command("go", "mod", "tidy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run go mod tidy: %w", err)
	}

	// Only update workflow files if target is avalanchego
	if target == TargetAvalanchego {
		if err := updateWorkflowFiles(version); err != nil {
			return fmt.Errorf("failed to update workflow files: %w", err)
		}
	}

	return nil
}

// updateWorkflowFiles updates GitHub workflow files to use full SHA for avalanchego actions
func updateWorkflowFiles(version string) error {
	// Get full SHA for the version
	fullSHA, err := getFullSHA(version)
	if err != nil {
		return err
	}

	// Find all workflow files
	workflowFiles, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		return fmt.Errorf("failed to find workflow files: %w", err)
	}

	// Also check for .yaml extension
	yamlFiles, err := filepath.Glob(".github/workflows/*.yaml")
	if err != nil {
		return fmt.Errorf("failed to find workflow yaml files: %w", err)
	}
	workflowFiles = append(workflowFiles, yamlFiles...)

	// Pattern to match: uses: ava-labs/avalanchego/.github/actions/*@<ref>
	pattern := regexp.MustCompile(`(uses:\s+ava-labs/avalanchego/\.github/actions/[^@]+@)([^\s]+)`)

	for _, file := range workflowFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", file, err)
		}

		// Replace all occurrences with the full SHA
		newContent := pattern.ReplaceAllString(string(content), fmt.Sprintf("${1}%s", fullSHA))

		// Only write if content changed
		if newContent != string(content) {
			if err := os.WriteFile(file, []byte(newContent), 0o644); err != nil {
				return fmt.Errorf("failed to write %s: %w", file, err)
			}
		}
	}

	return nil
}

// getFullSHA retrieves the full commit SHA for a given version using GitHub API
func getFullSHA(version string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/ava-labs/avalanchego/commits/%s", version)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Use GITHUB_TOKEN if available to avoid rate limiting
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch commit info from GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d for version %s", resp.StatusCode, version)
	}

	var result struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode GitHub response: %w", err)
	}

	if result.SHA == "" {
		return "", fmt.Errorf("no SHA found for version %s", version)
	}

	return result.SHA, nil
}
