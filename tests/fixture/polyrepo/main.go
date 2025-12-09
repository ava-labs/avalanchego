// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/polyrepo/core"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	logLevel  string
	targetDir string
)

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
	Short: "Manage local development across avalanchego and firewood repositories",
	Long: `Polyrepo is a tool for managing local development across multiple interdependent
repositories (avalanchego, firewood) by managing replace directives in go.mod files
and coordinating git operations.

Note: coreth is now part of avalanchego (grafted at graft/coreth/) and is not managed separately.`,
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		// Change to target directory if specified
		if targetDir != "" {
			absPath, err := filepath.Abs(targetDir)
			if err != nil {
				return fmt.Errorf("failed to resolve target directory %q: %w", targetDir, err)
			}

			// Verify it's a directory
			info, err := os.Stat(absPath)
			if err != nil {
				return fmt.Errorf("failed to access target directory %q: %w", absPath, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("target path %q is not a directory", absPath)
			}

			// Change to the target directory
			if err := os.Chdir(absPath); err != nil {
				return fmt.Errorf("failed to change to target directory %q: %w", absPath, err)
			}
		}

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

		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		return core.Status(log, baseDir, os.Stdout)
	},
}

var syncCmd = &cobra.Command{
	Use:   "sync [repo[@ref]...]",
	Short: "Sync repositories for local development",
	Long: `Clone or update repositories and add replace directives to go.mod.

Repositories can be specified with an optional ref (branch, tag, or commit):
  sync firewood@main
  sync avalanchego@57a74c3a7fd7dcdda24f49a237bfa9fa69f26a85

If no repositories are specified, syncs based on the current directory and go.mod:
- From avalanchego: syncs firewood at version specified in graft/coreth/go.mod
- From firewood: syncs avalanchego at default branch (master)
- From unknown location: syncs both avalanchego and firewood

Repositories will be cloned into the current directory with their repository names.

Note: coreth is now part of avalanchego (grafted at graft/coreth/) and is not synced separately.
When syncing firewood from avalanchego, the replace directive is also added to graft/coreth/go.mod.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := getLogger(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		force, _ := cmd.Flags().GetBool("force")

		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		return core.Sync(log, baseDir, args, depth, force)
	},
}

var resetCmd = &cobra.Command{
	Use:   "reset [repos...]",
	Short: "Remove replace directives for specified repositories",
	Long: `Remove replace directives from go.mod for the specified repositories.

If no repositories are specified, removes replace directives for all polyrepo-managed
repositories (avalanchego, firewood).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		log := getLogger(cmd)

		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		err = core.Reset(log, baseDir, args)
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

		baseDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		err = core.UpdateAvalanchego(log, baseDir, version)
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

	// Add persistent flags (apply to all commands)
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Set log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().StringVar(&targetDir, "target-dir", "", "Target directory to operate in (defaults to current directory)")
}
