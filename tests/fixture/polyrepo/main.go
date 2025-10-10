// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/polyrepo/core"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "polyrepo",
	Short: "Manage local development across avalanchego, coreth, and firewood repositories",
	Long: `Polyrepo is a tool for managing local development across multiple interdependent
repositories (avalanchego, coreth, firewood) by managing replace directives in go.mod files
and coordinating git operations.`,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(resetCmd)
	rootCmd.AddCommand(updateAvalanchegoCmd)
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all polyrepo repositories",
	Long: `Display the status of all polyrepo-managed repositories including:
- Whether each repository is cloned
- Current branch or commit
- Whether working directory is dirty
- Whether replace directives are active`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get current directory
		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Get path to go.mod if it exists
		goModPath := ""
		if _, err := os.Stat("go.mod"); err == nil {
			goModPath = "go.mod"
		}

		// Get status for all repos
		repos := []string{"avalanchego", "coreth", "firewood"}
		for _, repoName := range repos {
			status, err := core.GetRepoStatus(repoName, baseDir, goModPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get status for %s: %v\n", repoName, err)
				continue
			}

			fmt.Println(core.FormatRepoStatus(status))
		}

		return nil
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync [repo[@ref]...]",
	Short: "Sync repositories for local development",
	Long: `Clone or update repositories and add replace directives to go.mod.

Repositories can be specified with an optional ref (branch, tag, or commit):
  sync firewood@main
  sync coreth@v0.13.8
  sync avalanchego@57a74c3a7fd7dcdda24f49a237bfa9fa69f26a85

If no repositories are specified, syncs based on the current directory:
- From avalanchego: syncs coreth and firewood
- From coreth: syncs avalanchego and firewood
- From firewood: syncs avalanchego and coreth
- From unknown location: syncs all three

Repositories will be cloned into the current directory with their repository names.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		depth, _ := cmd.Flags().GetInt("depth")
		force, _ := cmd.Flags().GetBool("force")

		// Get current directory
		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Determine which repos to sync
		type repoWithRef struct {
			name string
			ref  string
		}
		var reposToSync []repoWithRef

		if len(args) > 0 {
			// Parse repo[@ref] format from args
			for _, arg := range args {
				repoName, ref, err := core.ParseRepoAndVersion(arg)
				if err != nil {
					return fmt.Errorf("invalid repo format %q: %w", arg, err)
				}
				reposToSync = append(reposToSync, repoWithRef{name: repoName, ref: ref})
			}
		} else {
			// Auto-detect based on current repo
			currentRepo, err := core.DetectCurrentRepo(baseDir)
			if err != nil {
				return fmt.Errorf("failed to detect current repo: %w", err)
			}
			// Get repos to sync and use default branches
			repos := core.GetReposToSync(currentRepo)
			for _, repoName := range repos {
				reposToSync = append(reposToSync, repoWithRef{name: repoName, ref: ""})
			}
		}

		// Get path to go.mod
		goModPath := "go.mod"
		if _, err := os.Stat(goModPath); err != nil {
			return fmt.Errorf("go.mod not found in current directory")
		}

		// Sync each repository
		for _, repo := range reposToSync {
			if repo.ref != "" {
				fmt.Printf("Syncing %s@%s...\n", repo.name, repo.ref)
			} else {
				fmt.Printf("Syncing %s...\n", repo.name)
			}

			config, err := core.GetRepoConfig(repo.name)
			if err != nil {
				return err
			}

			clonePath := core.GetRepoClonePath(repo.name, baseDir)

			// Use specified ref or default branch
			refToUse := repo.ref
			if refToUse == "" {
				refToUse = config.DefaultBranch
			}

			// Clone or update the repo
			err = core.CloneOrUpdateRepo(config.GitRepo, clonePath, refToUse, depth, force)
			if err != nil {
				return fmt.Errorf("failed to sync %s: %w", repo.name, err)
			}

			// Check if dirty (refuse unless forced)
			if !force {
				isDirty, err := core.IsRepoDirty(clonePath)
				if err == nil && isDirty {
					return core.ErrDirtyWorkingDir(clonePath)
				}
			}

			// Run nix build if required
			if config.RequiresNixBuild {
				fmt.Printf("Running nix build for %s...\n", repo.name)
				nixBuildPath := core.GetNixBuildPath(clonePath, config.NixBuildPath)
				err = core.RunNixBuild(nixBuildPath)
				if err != nil {
					return fmt.Errorf("failed to run nix build for %s: %w", repo.name, err)
				}
			}

			// Add replace directive
			replacePath := core.GetRepoClonePath(repo.name, baseDir)
			if config.ModuleReplacementPath != "." {
				replacePath = replacePath + "/" + config.ModuleReplacementPath
			}

			err = core.AddReplaceDirective(goModPath, config.GoModule, replacePath)
			if err != nil {
				return fmt.Errorf("failed to add replace directive for %s: %w", repo.name, err)
			}

			fmt.Printf("Successfully synced %s\n", repo.name)
		}

		return nil
	},
}

var resetCmd = &cobra.Command{
	Use:   "reset [repos...]",
	Short: "Remove replace directives for specified repositories",
	Long: `Remove replace directives from go.mod for the specified repositories.

If no repositories are specified, removes replace directives for all polyrepo-managed
repositories (avalanchego, coreth, firewood).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Get path to go.mod
		goModPath := "go.mod"
		if _, err := os.Stat(goModPath); err != nil {
			return fmt.Errorf("go.mod not found in current directory")
		}

		err := core.ResetRepos(goModPath, args)
		if err != nil {
			return err
		}

		if len(args) == 0 {
			fmt.Println("Removed all polyrepo replace directives")
		} else {
			fmt.Printf("Removed replace directives for: %v\n", args)
		}

		return nil
	},
}

var updateAvalanchegoCmd = &cobra.Command{
	Use:   "update-avalanchego [version]",
	Short: "Update the avalanchego dependency version",
	Long: `Update the avalanchego dependency to the specified version in go.mod.

If no version is specified, uses the current version from go.mod.

This command cannot be run from the avalanchego repository itself.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		version := ""
		if len(args) > 0 {
			version = args[0]
		}

		// Get path to go.mod
		goModPath := "go.mod"
		if _, err := os.Stat(goModPath); err != nil {
			return fmt.Errorf("go.mod not found in current directory")
		}

		err := core.UpdateAvalanchego(goModPath, version)
		if err != nil {
			return err
		}

		fmt.Printf("Updated avalanchego to version %s\n", version)
		return nil
	},
}

func init() {
	// Add flags for sync command
	syncCmd.Flags().IntP("depth", "d", 1, "Clone depth (0 for full clone, 1 for shallow, >1 for partial)")
	syncCmd.Flags().BoolP("force", "f", false, "Force sync even if directory is dirty or already exists")
}
