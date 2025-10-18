// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/polyrepo/core"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const goModFilename = "go.mod"

var logLevel string

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// Key for storing logger in context
type contextKey string

const loggerKey contextKey = "logger"

// getLogger retrieves the logger from the command context
func getLogger(cmd *cobra.Command) logging.Logger {
	return cmd.Context().Value(loggerKey).(logging.Logger)
}

var rootCmd = &cobra.Command{
	Use:   "polyrepo",
	Short: "Manage local development across avalanchego, coreth, and firewood repositories",
	Long: `Polyrepo is a tool for managing local development across multiple interdependent
repositories (avalanchego, coreth, firewood) by managing replace directives in go.mod files
and coordinating git operations.`,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// Parse log level from flag (flags are now parsed)
		var level logging.Level
		switch strings.ToLower(logLevel) {
		case "debug":
			level = logging.Debug
		case "info":
			level = logging.Info
		case "warn":
			level = logging.Warn
		case "error":
			level = logging.Error
		default:
			return fmt.Errorf("invalid log level: %s (valid options: debug, info, warn, error)", logLevel)
		}

		// Create logger
		logFactory := logging.NewFactory(logging.Config{
			DisplayLevel: level,
			LogLevel:     level,
		})
		log, err := logFactory.Make("polyrepo")
		if err != nil {
			return fmt.Errorf("error creating logger: %w", err)
		}

		// Store logger in context for command handlers
		ctx := context.WithValue(cmd.Context(), loggerKey, log)
		cmd.SetContext(ctx)

		return nil
	},
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
	RunE: func(cmd *cobra.Command, _ []string) error {
		log := getLogger(cmd)

		// Get current directory
		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		// Detect which repo we're currently in
		primaryRepo, err := core.DetectCurrentRepo(log, baseDir)
		if err != nil {
			return fmt.Errorf("failed to detect current repository: %w", err)
		}

		// Get path to go.mod based on primary repo
		goModPath := ""
		if primaryRepo != "" {
			config, err := core.GetRepoConfig(primaryRepo)
			if err == nil {
				goModPath = config.GoModPath
				if _, err := os.Stat(goModPath); err != nil {
					goModPath = ""
				}
			}
		} else {
			// Not in a known repo, check for go.mod in current directory
			if _, err := os.Stat(goModFilename); err == nil {
				goModPath = goModFilename
			}
		}

		// Display primary repository section
		if primaryRepo != "" {
			fmt.Printf("Primary Repository: %s\n", primaryRepo)

			// Get status for primary repo (isPrimary = true)
			status, err := core.GetRepoStatus(log, primaryRepo, baseDir, goModPath, true)
			if err != nil {
				log.Warn("failed to get status for primary repository",
					zap.String("repo", primaryRepo),
					zap.Error(err),
				)
			} else {
				fmt.Printf("  %s\n", core.FormatRepoStatus(status))
			}
			fmt.Println()
		} else {
			fmt.Println("Primary Repository: none (not in a known repository)")
			fmt.Println()
		}

		// Display other repositories section
		fmt.Println("Other Repositories:")
		repos := []string{"avalanchego", "coreth", "firewood"}
		for _, repoName := range repos {
			// Skip the primary repo since we already displayed it
			if repoName == primaryRepo {
				continue
			}

			// Get status for synced repo (isPrimary = false)
			status, err := core.GetRepoStatus(log, repoName, baseDir, goModPath, false)
			if err != nil {
				log.Warn("failed to get status for repository",
					zap.String("repo", repoName),
					zap.Error(err),
				)
				continue
			}

			fmt.Printf("  %s\n", core.FormatRepoStatus(status))
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

If no repositories are specified, syncs based on the current directory and go.mod:
- From avalanchego: syncs coreth and firewood at versions specified in go.mod
- From coreth: syncs avalanchego and firewood at versions specified in go.mod
- From firewood: syncs avalanchego and coreth at their default branches
- From unknown location: syncs all three at their default branches

Repositories will be cloned into the current directory with their repository names.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := getLogger(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		force, _ := cmd.Flags().GetBool("force")

		// Get current directory
		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		log.Info("starting sync command",
			zap.Strings("args", args),
			zap.Int("depth", depth),
			zap.Bool("force", force),
			zap.String("baseDir", baseDir),
		)

		// Get path to go.mod
		goModPath := goModFilename
		if _, err := os.Stat(goModPath); err != nil {
			return errors.New("go.mod not found in current directory")
		}

		// Determine which repos to sync
		type repoWithRef struct {
			name        string
			ref         string
			wasExplicit bool // true if ref was explicitly specified, false if discovered
		}
		var reposToSync []repoWithRef

		if len(args) > 0 {
			log.Info("parsing repository arguments from command line",
				zap.Int("count", len(args)),
			)

			// Detect current repo for determining default refs from go.mod
			currentRepo, err := core.DetectCurrentRepo(log, baseDir)
			if err != nil {
				return fmt.Errorf("failed to detect current repo: %w", err)
			}

			// Parse repo[@ref] format from args
			for _, arg := range args {
				repoName, ref, err := core.ParseRepoAndVersion(arg)
				if err != nil {
					return fmt.Errorf("invalid repo format %q: %w", arg, err)
				}

				wasExplicit := ref != ""

				// If no explicit ref provided, determine from go.mod
				if ref == "" {
					ref, err = core.GetDefaultRefForRepo(log, currentRepo, repoName, goModPath)
					if err != nil {
						return fmt.Errorf("failed to get default ref for %s: %w", repoName, err)
					}
				}

				reposToSync = append(reposToSync, repoWithRef{name: repoName, ref: ref, wasExplicit: wasExplicit})
			}
		} else {
			log.Info("no repositories specified, auto-detecting based on current directory")
			// Auto-detect based on current repo
			currentRepo, err := core.DetectCurrentRepo(log, baseDir)
			if err != nil {
				return fmt.Errorf("failed to detect current repo: %w", err)
			}

			log.Info("detected current repository",
				zap.String("currentRepo", currentRepo),
			)

			// Get repos to sync and determine default refs from go.mod
			repos := core.GetReposToSync(currentRepo)
			log.Info("determined repositories to sync",
				zap.Int("count", len(repos)),
				zap.Strings("repos", repos),
			)

			for _, repoName := range repos {
				// Determine the default ref for this repo
				ref, err := core.GetDefaultRefForRepo(log, currentRepo, repoName, goModPath)
				if err != nil {
					return fmt.Errorf("failed to get default ref for %s: %w", repoName, err)
				}
				reposToSync = append(reposToSync, repoWithRef{name: repoName, ref: ref, wasExplicit: false})
			}
		}

		// Sync each repository
		syncedRepoNames := make([]string, 0, len(reposToSync))
		for _, repo := range reposToSync {
			log.Info("syncing repository",
				zap.String("repo", repo.name),
				zap.String("ref", repo.ref),
			)

			config, err := core.GetRepoConfig(repo.name)
			if err != nil {
				return err
			}

			log.Info("repository configuration",
				zap.String("repo", repo.name),
				zap.String("gitRepo", config.GitRepo),
				zap.String("defaultBranch", config.DefaultBranch),
				zap.String("goModule", config.GoModule),
				zap.Bool("requiresNixBuild", config.RequiresNixBuild),
			)

			clonePath := core.GetRepoClonePath(repo.name, baseDir)

			// Use specified ref or default branch
			refToUse := repo.ref
			if refToUse == "" {
				refToUse = config.DefaultBranch
			}

			log.Info("syncing repository",
				zap.String("repo", repo.name),
				zap.String("ref", refToUse),
				zap.Bool("explicit", repo.wasExplicit),
				zap.Int("depth", depth),
				zap.String("url", config.GitRepo),
				zap.String("path", clonePath),
			)

			// Clone or update the repo
			err = core.CloneOrUpdateRepo(log, config.GitRepo, clonePath, refToUse, depth, force)
			if err != nil {
				return fmt.Errorf("failed to sync %s: %w", repo.name, err)
			}

			// Check if dirty (refuse unless forced)
			if !force {
				isDirty, err := core.IsRepoDirty(log, clonePath)
				if err == nil && isDirty {
					return core.ErrDirtyWorkingDir(clonePath)
				}
			}

			// Run nix build if required
			if config.RequiresNixBuild {
				log.Info("running nix build",
					zap.String("repo", repo.name),
					zap.String("path", core.GetNixBuildPath(clonePath, config.NixBuildPath)),
				)
				nixBuildPath := core.GetNixBuildPath(clonePath, config.NixBuildPath)
				err = core.RunNixBuild(log, nixBuildPath)
				if err != nil {
					return fmt.Errorf("failed to run nix build for %s: %w", repo.name, err)
				}
			}

			log.Info("successfully synced repository",
				zap.String("repo", repo.name),
				zap.String("path", clonePath),
				zap.String("ref", refToUse),
			)

			syncedRepoNames = append(syncedRepoNames, repo.name)
		}

		// Update all replace directives for the dependency graph
		// This ensures that both the primary repo and all synced repos
		// have replace directives for their locally cloned dependencies
		log.Info("updating replace directives for all repos")
		err = core.UpdateAllReplaceDirectives(log, baseDir, syncedRepoNames)
		if err != nil {
			return fmt.Errorf("failed to update replace directives: %w", err)
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
		log := getLogger(cmd)

		// Get path to go.mod
		goModPath := goModFilename
		if _, err := os.Stat(goModPath); err != nil {
			return errors.New("go.mod not found in current directory")
		}

		log.Info("resetting repositories",
			zap.Strings("repos", args),
		)

		err := core.ResetRepos(log, goModPath, args)
		if err != nil {
			return err
		}

		if len(args) == 0 {
			log.Info("removed all polyrepo replace directives")
		} else {
			log.Info("removed replace directives for repositories",
				zap.Strings("repos", args),
			)
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
		log := getLogger(cmd)

		version := ""
		if len(args) > 0 {
			version = args[0]
		}

		log.Info("updating avalanchego dependency",
			zap.String("version", version),
		)

		// Get path to go.mod
		goModPath := goModFilename
		if _, err := os.Stat(goModPath); err != nil {
			return errors.New("go.mod not found in current directory")
		}

		err := core.UpdateAvalanchego(log, goModPath, version)
		if err != nil {
			return err
		}

		log.Info("updated avalanchego dependency",
			zap.String("version", version),
		)
		return nil
	},
}

func init() {
	// Add flags for sync command
	syncCmd.Flags().IntP("depth", "d", 1, "Clone depth (0 for full clone, 1 for shallow, >1 for partial)")
	syncCmd.Flags().BoolP("force", "f", false, "Force sync even if directory is dirty or already exists")

	// Add persistent flag for log level (applies to all commands)
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Set log level (debug, info, warn, error)")
}
