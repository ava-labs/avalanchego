// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// CloneOptions contains options for the Clone operation
type CloneOptions struct {
	Target  RepoTarget
	Path    string
	Version string
	Shallow bool
}

// Clone clones or updates a repository and sets up go mod replace directives
func Clone(opts CloneOptions) error {
	// Default path to repo name if not specified
	if opts.Path == "" {
		opts.Path = string(opts.Target)
	}

	// If no version provided, get it from go.mod
	version := opts.Version
	if version == "" {
		var err error
		version, err = GetVersion(opts.Target)
		if err != nil {
			return fmt.Errorf("failed to get version from go.mod: %w", err)
		}
	}

	// Resolve target to get clone URL
	_, cloneURL, err := opts.Target.Resolve()
	if err != nil {
		return err
	}

	// Convert path to absolute for consistency
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	// Clone or update the repository
	if err := cloneOrUpdate(absPath, cloneURL, version, opts.Shallow); err != nil {
		return err
	}

	// Set up go mod replace directives
	if err := setupModReplace(opts.Target, absPath); err != nil {
		return err
	}

	return nil
}

// cloneOrUpdate clones a repository if it doesn't exist, or updates it if it does
func cloneOrUpdate(path, cloneURL, version string, shallow bool) error {
	// Check if directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Clone the repository
		args := []string{"clone"}
		if shallow {
			args = append(args, "--depth", "1")
		}
		args = append(args, cloneURL, path)
		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	}

	// Change to the repository directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(path); err != nil {
		return fmt.Errorf("failed to change to repository directory: %w", err)
	}

	// Fetch latest changes
	cmd := exec.Command("git", "fetch")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	// Create branch and checkout version
	branchName := fmt.Sprintf("test-%s", version)
	cmd = exec.Command("git", "checkout", "-B", branchName, version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout version %s: %w", version, err)
	}

	return nil
}

// setupModReplace sets up go mod replace directives based on current module and clone target
func setupModReplace(target RepoTarget, clonedPath string) error {
	// Get current module name
	currentModule, err := getCurrentModuleName()
	if err != nil {
		return err
	}

	// Determine which go.mod to modify and what replacements to make
	var modDir string
	var replacements []replacement

	switch {
	case strings.HasPrefix(currentModule, "github.com/ava-labs/coreth") && target == TargetAvalanchego:
		// coreth cloning avalanchego: replace coreth in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, ".")
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	case strings.HasPrefix(currentModule, "github.com/ava-labs/firewood-go-ethhash/ffi") && target == TargetAvalanchego:
		// firewood cloning avalanchego: replace firewood in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, ".")
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetFirewood:
		// avalanchego cloning firewood: replace firewood in current go.mod
		modDir = "."
		// Firewood module is in /ffi subdirectory
		ffiPath := filepath.Join(clonedPath, "ffi")
		relPath, err := filepath.Rel(".", ffiPath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetCoreth:
		// avalanchego cloning coreth: replace coreth in current go.mod
		modDir = "."
		relPath, err := filepath.Rel(".", clonedPath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	default:
		// No replacement needed for this combination
		return nil
	}

	// Apply replacements
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	if err := os.Chdir(modDir); err != nil {
		return fmt.Errorf("failed to change to %s: %w", modDir, err)
	}

	for _, repl := range replacements {
		cmd := exec.Command("go", "mod", "edit", fmt.Sprintf("-replace=%s=%s", repl.old, repl.new))
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to run go mod edit: %w", err)
		}
	}

	// Run go mod tidy
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run go mod tidy: %w", err)
	}

	return nil
}

// replacement represents a go mod replace directive
type replacement struct {
	old string
	new string
}

// getCurrentModuleName reads the module name from go.mod in the current directory
func getCurrentModuleName() (string, error) {
	goModData, err := os.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse("go.mod", goModData, nil)
	if err != nil {
		return "", fmt.Errorf("failed to parse go.mod: %w", err)
	}

	if modFile.Module == nil {
		return "", fmt.Errorf("no module declaration found in go.mod")
	}

	return modFile.Module.Mod.Path, nil
}
