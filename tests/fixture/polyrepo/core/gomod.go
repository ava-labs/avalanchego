// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"os"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// ReadGoMod reads and parses a go.mod file
func ReadGoMod(log logging.Logger, path string) (*modfile.File, error) {
	log.Debug("reading go.mod file",
		zap.String("path", path),
	)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, stacktrace.Errorf("failed to read go.mod: %w", err)
	}

	log.Debug("parsing go.mod file",
		zap.Int("sizeBytes", len(data)),
	)

	modFile, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, stacktrace.Errorf("failed to parse go.mod: %w", err)
	}

	log.Debug("successfully parsed go.mod")

	return modFile, nil
}

// GetModulePath returns the module path from a go.mod file
func GetModulePath(log logging.Logger, goModPath string) (string, error) {
	log.Debug("getting module path from go.mod",
		zap.String("goModPath", goModPath),
	)

	modFile, err := ReadGoMod(log, goModPath)
	if err != nil {
		return "", err
	}

	if modFile.Module == nil {
		log.Debug("go.mod has no module declaration")
		return "", stacktrace.Errorf("go.mod has no module declaration")
	}

	modulePath := modFile.Module.Mod.Path
	log.Debug("found module path",
		zap.String("modulePath", modulePath),
	)

	return modulePath, nil
}

// AddReplaceDirective adds a replace directive to a go.mod file
func AddReplaceDirective(log logging.Logger, goModPath, oldPath, newPath string) error {
	log.Debug("adding replace directive",
		zap.String("goModPath", goModPath),
		zap.String("module", oldPath),
		zap.String("path", newPath),
	)

	data, err := os.ReadFile(goModPath)
	if err != nil {
		return stacktrace.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return stacktrace.Errorf("failed to parse go.mod: %w", err)
	}

	// Check if replace already exists
	log.Debug("checking for existing replace directive",
		zap.String("module", oldPath),
	)

	var existingReplace *modfile.Replace
	for _, replace := range modFile.Replace {
		if replace.Old.Path == oldPath {
			existingReplace = replace
			break
		}
	}

	if existingReplace != nil {
		log.Debug("found existing replace directive",
			zap.String("oldPath", existingReplace.New.Path),
			zap.String("newPath", newPath),
		)

		if existingReplace.New.Path == newPath {
			log.Debug("replace directive already correct, no changes needed")
			return nil
		}

		log.Debug("updating existing replace directive")
	} else {
		log.Debug("adding new replace directive")
	}

	// Add the replace directive
	err = modFile.AddReplace(oldPath, "", newPath, "")
	if err != nil {
		return stacktrace.Errorf("failed to add replace directive: %w", err)
	}

	// Format and write back
	log.Debug("formatting go.mod")
	formattedData, err := modFile.Format()
	if err != nil {
		return stacktrace.Errorf("failed to format go.mod: %w", err)
	}

	log.Debug("writing modified go.mod",
		zap.String("path", goModPath),
	)
	err = os.WriteFile(goModPath, formattedData, 0o600)
	if err != nil {
		return stacktrace.Errorf("failed to write go.mod: %w", err)
	}

	log.Debug("successfully added replace directive")
	return nil
}

// RemoveReplaceDirective removes a replace directive from a go.mod file
func RemoveReplaceDirective(log logging.Logger, goModPath, oldPath string) error {
	log.Debug("removing replace directive",
		zap.String("goModPath", goModPath),
		zap.String("module", oldPath),
	)

	data, err := os.ReadFile(goModPath)
	if err != nil {
		return stacktrace.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return stacktrace.Errorf("failed to parse go.mod: %w", err)
	}

	// Check if replace exists
	var foundReplace *modfile.Replace
	for _, replace := range modFile.Replace {
		if replace.Old.Path == oldPath {
			foundReplace = replace
			break
		}
	}

	if foundReplace != nil {
		log.Debug("found replace directive to remove",
			zap.String("module", oldPath),
			zap.String("path", foundReplace.New.Path),
		)
	} else {
		log.Debug("no replace directive found for module",
			zap.String("module", oldPath),
		)
	}

	// Remove the replace directive
	err = modFile.DropReplace(oldPath, "")
	if err != nil {
		return stacktrace.Errorf("failed to remove replace directive: %w", err)
	}

	// Format and write back
	log.Debug("formatting go.mod")
	formattedData, err := modFile.Format()
	if err != nil {
		return stacktrace.Errorf("failed to format go.mod: %w", err)
	}

	log.Debug("writing modified go.mod",
		zap.String("path", goModPath),
	)
	err = os.WriteFile(goModPath, formattedData, 0o600)
	if err != nil {
		return stacktrace.Errorf("failed to write go.mod: %w", err)
	}

	log.Debug("successfully removed replace directive")
	return nil
}

// GetDependencyVersion returns the version of a dependency from a go.mod file
func GetDependencyVersion(log logging.Logger, goModPath, modulePath string) (string, error) {
	log.Debug("looking up dependency version",
		zap.String("goModPath", goModPath),
		zap.String("module", modulePath),
	)

	modFile, err := ReadGoMod(log, goModPath)
	if err != nil {
		return "", err
	}

	log.Debug("parsed go.mod, searching for dependency",
		zap.Int("requireCount", len(modFile.Require)),
	)

	for _, req := range modFile.Require {
		if req.Mod.Path == modulePath {
			log.Debug("found dependency",
				zap.String("module", modulePath),
				zap.String("version", req.Mod.Version),
			)
			return req.Mod.Version, nil
		}
	}

	log.Debug("dependency not found in go.mod",
		zap.String("module", modulePath),
	)

	return "", stacktrace.Errorf("dependency %s not found in go.mod", modulePath)
}

// ConvertVersionToGitRef converts a Go module version to a git ref.
// For pseudo-versions (e.g., v1.13.6-0.20251007213349-63cc1a166a56),
// it extracts and returns the commit hash (e.g., 63cc1a166a56).
// For regular versions (e.g., v0.13.8), it returns the version as-is.
func ConvertVersionToGitRef(log logging.Logger, version string) (string, error) {
	log.Debug("converting version to git ref",
		zap.String("version", version),
	)

	isPseudo := module.IsPseudoVersion(version)
	log.Debug("checked if version is pseudo-version",
		zap.Bool("isPseudo", isPseudo),
	)

	if isPseudo {
		log.Debug("extracting commit hash from pseudo-version",
			zap.String("pseudoVersion", version),
		)

		// Parse pseudo-version format: v0.0.0-yyyymmddhhmmss-abcdefabcdef
		// Split to visualize parts for debugging
		parts := strings.Split(version, "-")
		log.Debug("split pseudo-version into parts",
			zap.Int("partCount", len(parts)),
			zap.Strings("parts", parts),
		)

		commitHash, err := module.PseudoVersionRev(version)
		if err != nil {
			return "", stacktrace.Errorf("failed to extract commit hash from pseudo-version: %w", err)
		}

		log.Debug("extracted commit hash",
			zap.String("commitHash", commitHash),
		)

		return commitHash, nil
	}

	log.Debug("version is not pseudo-version, returning as-is",
		zap.String("gitRef", version),
	)

	return version, nil
}
