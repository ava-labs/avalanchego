// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dep

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
	"golang.org/x/mod/modfile"
)

// CloneOptions contains options for the Clone operation
type CloneOptions struct {
	Target  RepoTarget
	Path    string
	Version string
	Shallow bool
	Logger  logging.Logger
}

// CloneResult contains information about a completed clone operation
type CloneResult struct {
	VersionInfo VersionInfo
	Path        string
}

// Clone clones or updates a repository and sets up go mod replace directives
func Clone(opts CloneOptions) (CloneResult, error) {
	// Default path to repo name if not specified
	if opts.Path == "" {
		opts.Path = string(opts.Target)
		opts.Logger.Debug("Using default path",
			zap.String("path", opts.Path),
		)
	}

	// If no version provided, get it from go.mod
	version := opts.Version
	var versionInfo VersionInfo
	if version == "" {
		opts.Logger.Debug("Getting version from go.mod",
			zap.String("target", string(opts.Target)),
		)
		var err error
		versionInfo, err = GetVersion(opts.Target)
		if err != nil {
			return CloneResult{}, fmt.Errorf("failed to get version from go.mod: %w", err)
		}
		version = versionInfo.Version
		opts.Logger.Debug("Got version from go.mod",
			zap.String("version", version),
			zap.Bool("isDefault", versionInfo.IsDefault),
		)
	} else {
		// Version was explicitly provided
		versionInfo = VersionInfo{Version: version, IsDefault: false}
		opts.Logger.Debug("Using explicitly provided version",
			zap.String("version", version),
		)
	}

	// Resolve target to get clone URL
	_, cloneURL, err := opts.Target.Resolve()
	if err != nil {
		return CloneResult{}, err
	}
	opts.Logger.Debug("Resolved clone URL",
		zap.String("url", cloneURL),
	)

	// Convert path to absolute for consistency
	absPath, err := filepath.Abs(opts.Path)
	if err != nil {
		return CloneResult{}, fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	opts.Logger.Debug("Resolved absolute path",
		zap.String("relativePath", opts.Path),
		zap.String("absolutePath", absPath),
	)

	// Clone or update the repository
	if err := cloneOrUpdate(absPath, cloneURL, version, opts.Shallow, opts.Logger); err != nil {
		return CloneResult{}, err
	}

	// Set up go mod replace directives
	if err := setupModReplace(opts.Target, absPath, opts.Logger); err != nil {
		return CloneResult{}, err
	}

	return CloneResult{
		VersionInfo: versionInfo,
		Path:        opts.Path,
	}, nil
}

// cloneOrUpdate clones a repository if it doesn't exist, or updates it if it does
func cloneOrUpdate(path, cloneURL, version string, shallow bool, log logging.Logger) error {
	// Check if directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Debug("Directory does not exist, cloning repository",
			zap.String("path", path),
		)
		// Clone the repository at the specific version
		args := []string{"-c", "advice.detachedHead=false", "clone", "--quiet"}
		if shallow {
			// For shallow clones, use --branch to clone at specific ref
			args = append(args, "--depth", "1", "--branch", version)
		}
		args = append(args, cloneURL, path)
		log.Debug("Executing git clone",
			zap.Strings("args", args),
		)
		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			// If shallow clone with --branch failed, fall back to full clone
			if shallow {
				log.Debug("Shallow clone failed, falling back to full clone",
					zap.Error(err),
				)
				// Try again without shallow clone
				args = []string{"-c", "advice.detachedHead=false", "clone", "--quiet", cloneURL, path}
				log.Debug("Executing full git clone",
					zap.Strings("args", args),
				)
				cmd = exec.Command("git", args...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					return fmt.Errorf("failed to clone repository: %w", err)
				}
			} else {
				return fmt.Errorf("failed to clone repository: %w", err)
			}
		}
	} else {
		log.Debug("Directory already exists, will update",
			zap.String("path", path),
		)
	}

	// Change to the repository directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	log.Debug("Changing to repository directory",
		zap.String("from", originalDir),
		zap.String("to", path),
	)
	if err := os.Chdir(path); err != nil {
		return fmt.Errorf("failed to change to repository directory: %w", err)
	}

	// Fetch updates (if the directory already existed)
	log.Debug("Fetching updates from remote")
	cmd := exec.Command("git", "fetch", "--quiet")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run() // Ignore errors - might not need to fetch if just cloned

	// Resolve version to full commit SHA if it's a short ref
	resolvedVersion := version
	log.Debug("Resolving version to commit SHA",
		zap.String("version", version),
	)
	resolveCmd := exec.Command("git", "rev-parse", "--verify", version+"^{commit}")
	if output, err := resolveCmd.Output(); err == nil {
		resolvedVersion = strings.TrimSpace(string(output))
		log.Debug("Resolved version (with ^{commit})",
			zap.String("resolvedVersion", resolvedVersion),
		)
	} else {
		// Try without ^{commit} for branches/commit SHAs
		resolveCmd = exec.Command("git", "rev-parse", "--verify", version)
		if output, err := resolveCmd.Output(); err == nil {
			resolvedVersion = strings.TrimSpace(string(output))
			log.Debug("Resolved version (without ^{commit})",
				zap.String("resolvedVersion", resolvedVersion),
			)
		} else {
			log.Debug("Could not resolve version, will use as-is",
				zap.Error(err),
			)
		}
		// If resolution fails, use original version and let checkout fail with clear error
	}

	// Create branch and checkout version
	branchName := fmt.Sprintf("local/%s", version)
	log.Debug("Checking out version to branch",
		zap.String("version", resolvedVersion),
		zap.String("branch", branchName),
	)
	cmd = exec.Command("git", "checkout", "-q", "-B", branchName, resolvedVersion)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout version %s: %w", version, err)
	}

	// Verify that we're at the correct version
	if err := verifyCloneVersion(version); err != nil {
		return err
	}

	// Print success message
	log.Info("Checked out version to branch",
		zap.String("version", version),
		zap.String("branch", branchName),
	)

	return nil
}

// verifyCloneVersion verifies that the current HEAD matches the expected version
func verifyCloneVersion(version string) error {
	// Get current HEAD commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	headOutput, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get HEAD commit: %w", err)
	}
	headCommit := strings.TrimSpace(string(headOutput))

	// Get commit for expected version (dereference if it's an annotated tag)
	cmd = exec.Command("git", "rev-parse", version+"^{commit}")
	versionOutput, err := cmd.Output()
	if err != nil {
		// Try without ^{commit} in case it's a lightweight tag or branch
		cmd = exec.Command("git", "rev-parse", version)
		versionOutput, err = cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to resolve version %s: %w", version, err)
		}
	}
	versionCommit := strings.TrimSpace(string(versionOutput))

	// Compare commits
	if headCommit != versionCommit {
		return fmt.Errorf("verification failed: HEAD commit %s does not match version %s commit %s", headCommit, version, versionCommit)
	}

	return nil
}

// setupModReplace sets up go mod replace directives based on current module and clone target
func setupModReplace(target RepoTarget, clonedPath string, log logging.Logger) error {
	// Get current working directory (where we started)
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	log.Debug("Setting up go mod replace directives",
		zap.String("originalDir", originalDir),
		zap.String("clonedPath", clonedPath),
	)

	// Get current module name
	currentModule, err := getCurrentModuleName()
	if err != nil {
		return err
	}
	log.Debug("Current module",
		zap.String("module", currentModule),
	)

	// Determine which go.mod to modify and what replacements to make
	var modDir string
	var replacements []replacement

	switch {
	case strings.HasPrefix(currentModule, "github.com/ava-labs/coreth") && target == TargetAvalanchego:
		// coreth cloning avalanchego: replace coreth in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, originalDir)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	case strings.HasPrefix(currentModule, "github.com/ava-labs/firewood-go-ethhash/ffi") && target == TargetAvalanchego:
		// firewood cloning avalanchego: replace firewood in avalanchego's go.mod
		modDir = clonedPath
		relPath, err := filepath.Rel(clonedPath, originalDir)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetFirewood:
		// avalanchego cloning firewood: replace firewood in current go.mod
		modDir = originalDir
		// Firewood module is in /ffi subdirectory
		ffiPath := filepath.Join(clonedPath, "ffi")
		relPath, err := filepath.Rel(originalDir, ffiPath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/firewood-go-ethhash/ffi", new: relPath},
		}

	case currentModule == "github.com/ava-labs/avalanchego" && target == TargetCoreth:
		// avalanchego cloning coreth: replace coreth in current go.mod
		modDir = originalDir
		relPath, err := filepath.Rel(originalDir, clonedPath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		replacements = []replacement{
			{old: "github.com/ava-labs/coreth", new: relPath},
		}

	default:
		// No replacement needed for this combination
		log.Debug("No go mod replace needed for this module/target combination")
		return nil
	}

	log.Debug("Determined replacements",
		zap.String("modDir", modDir),
		zap.Int("replacementCount", len(replacements)),
	)

	// Apply replacements
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	log.Debug("Changing to mod directory",
		zap.String("modDir", modDir),
	)
	if err := os.Chdir(modDir); err != nil {
		return fmt.Errorf("failed to change to %s: %w", modDir, err)
	}

	for _, repl := range replacements {
		// Ensure relative paths start with ./ for go mod edit
		replacePath := repl.new
		if !filepath.IsAbs(replacePath) && !strings.HasPrefix(replacePath, ".") {
			replacePath = "./" + replacePath
		}
		log.Debug("Applying go mod replace",
			zap.String("old", repl.old),
			zap.String("new", replacePath),
		)
		cmd := exec.Command("go", "mod", "edit", fmt.Sprintf("-replace=%s=%s", repl.old, replacePath))
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
		// Don't fail on go mod tidy errors - the replace might point to a path
		// that doesn't have all dependencies yet (e.g., in test scenarios)
		log.Warn("go mod tidy failed (this may be expected)",
			zap.Error(err),
		)
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
