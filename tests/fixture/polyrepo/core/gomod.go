// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"os"

	"golang.org/x/mod/modfile"
)

// ReadGoMod reads and parses a go.mod file
func ReadGoMod(path string) (*modfile.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse(path, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}

	return modFile, nil
}

// GetModulePath returns the module path from a go.mod file
func GetModulePath(goModPath string) (string, error) {
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		return "", err
	}

	if modFile.Module == nil {
		return "", fmt.Errorf("go.mod has no module declaration")
	}

	return modFile.Module.Mod.Path, nil
}

// AddReplaceDirective adds a replace directive to a go.mod file
func AddReplaceDirective(goModPath, oldPath, newPath string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	// Add the replace directive
	err = modFile.AddReplace(oldPath, "", newPath, "")
	if err != nil {
		return fmt.Errorf("failed to add replace directive: %w", err)
	}

	// Format and write back
	formattedData, err := modFile.Format()
	if err != nil {
		return fmt.Errorf("failed to format go.mod: %w", err)
	}

	err = os.WriteFile(goModPath, formattedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	return nil
}

// RemoveReplaceDirective removes a replace directive from a go.mod file
func RemoveReplaceDirective(goModPath, oldPath string) error {
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return fmt.Errorf("failed to read go.mod: %w", err)
	}

	modFile, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return fmt.Errorf("failed to parse go.mod: %w", err)
	}

	// Remove the replace directive
	err = modFile.DropReplace(oldPath, "")
	if err != nil {
		return fmt.Errorf("failed to remove replace directive: %w", err)
	}

	// Format and write back
	formattedData, err := modFile.Format()
	if err != nil {
		return fmt.Errorf("failed to format go.mod: %w", err)
	}

	err = os.WriteFile(goModPath, formattedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	return nil
}

// GetDependencyVersion returns the version of a dependency from a go.mod file
func GetDependencyVersion(goModPath, modulePath string) (string, error) {
	modFile, err := ReadGoMod(goModPath)
	if err != nil {
		return "", err
	}

	for _, req := range modFile.Require {
		if req.Mod.Path == modulePath {
			return req.Mod.Version, nil
		}
	}

	return "", fmt.Errorf("dependency %s not found in go.mod", modulePath)
}
