// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/tools/depctl/dep"
	"github.com/spf13/cobra"
)

var (
	depFlag     string
	pathFlag    string
	versionFlag string
	shallowFlag bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "depctl",
	Short: "Manage golang dependencies for avalanchego, firewood, and coreth",
	Long: `depctl is a tool to simplify dependency management across avalanchego-related repositories.

It provides commands to get dependency versions, update versions, and clone repositories
with automatic go.mod replace directives configured.`,
}

var getVersionCmd = &cobra.Command{
	Use:   "get-version",
	Short: "Get the version of a dependency from go.mod",
	Long: `Retrieves the version of the specified dependency from go.mod.
For tagged versions, returns the version as-is.
For pseudo-versions, extracts and returns the first 8 chars of the commit hash.`,
	Example: `  depctl get-version
  depctl get-version --dep=firewood
  depctl get-version --dep=coreth`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target := dep.ParseRepoTarget(depFlag)
		if !target.IsValid() {
			return fmt.Errorf("invalid repo target %q, must be one of: avalanchego, firewood, coreth", depFlag)
		}

		version, err := dep.GetVersion(target)
		if err != nil {
			return err
		}

		fmt.Println(version)
		return nil
	},
}

var updateVersionCmd = &cobra.Command{
	Use:   "update-version <version>",
	Short: "Update the version of a dependency in go.mod",
	Long: `Updates the version of the specified dependency in go.mod using go get.
If the target is avalanchego, also updates GitHub workflow files to use the full SHA.`,
	Example: `  depctl update-version v1.11.11
  depctl update-version v1.11.11 --dep=avalanchego
  depctl update-version mytag --dep=firewood`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := dep.ParseRepoTarget(depFlag)
		if !target.IsValid() {
			return fmt.Errorf("invalid repo target %q, must be one of: avalanchego, firewood, coreth", depFlag)
		}

		version := args[0]
		if err := dep.UpdateVersion(target, version); err != nil {
			return err
		}

		fmt.Printf("Successfully updated %s to %s\n", target, version)
		return nil
	},
}

var cloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Clone or update a repository and set up go mod replace directives",
	Long: `Clones or updates a repository to a specific version and automatically
configures go mod replace directives based on the current repository context.

If no version is provided, uses the version from the current go.mod file.
If no path is provided, defaults to the repository name (e.g., "avalanchego", "firewood", "coreth").`,
	Example: `  depctl clone
  depctl clone --dep=firewood --version=mytag
  depctl clone --dep=coreth --path=../coreth
  depctl clone --version=v1.11.11
  depctl clone --shallow --version=v1.11.11`,
	RunE: func(cmd *cobra.Command, args []string) error {
		target := dep.ParseRepoTarget(depFlag)
		if !target.IsValid() {
			return fmt.Errorf("invalid repo target %q, must be one of: avalanchego, firewood, coreth", depFlag)
		}

		opts := dep.CloneOptions{
			Target:  target,
			Path:    pathFlag,
			Version: versionFlag,
			Shallow: shallowFlag,
		}

		if err := dep.Clone(opts); err != nil {
			return err
		}

		// Determine what version was used
		version := versionFlag
		if version == "" {
			var err error
			version, err = dep.GetVersion(target)
			if err != nil {
				return fmt.Errorf("failed to get version: %w", err)
			}
		}

		// Determine what path was used
		path := pathFlag
		if path == "" {
			path = string(target)
		}

		fmt.Printf("Successfully cloned %s at version %s to %s\n", target, version, path)
		return nil
	},
}

func init() {
	// Add flags to get-version command
	getVersionCmd.Flags().StringVar(&depFlag, "dep", "", "Repository target (avalanchego, firewood, coreth)")

	// Add flags to update-version command
	updateVersionCmd.Flags().StringVar(&depFlag, "dep", "", "Repository target (avalanchego, firewood, coreth)")

	// Add flags to clone command
	cloneCmd.Flags().StringVar(&depFlag, "dep", "", "Repository target (avalanchego, firewood, coreth)")
	cloneCmd.Flags().StringVar(&pathFlag, "path", "", "Clone path (defaults to repository name)")
	cloneCmd.Flags().StringVar(&versionFlag, "version", "", "Version to clone (defaults to version in go.mod)")
	cloneCmd.Flags().BoolVar(&shallowFlag, "shallow", false, "Perform a shallow clone (--depth 1)")

	// Add commands to root
	rootCmd.AddCommand(getVersionCmd)
	rootCmd.AddCommand(updateVersionCmd)
	rootCmd.AddCommand(cloneCmd)
}
