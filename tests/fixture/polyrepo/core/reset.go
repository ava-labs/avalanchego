// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// ErrGoModNotFound is returned when go.mod is not found in the specified directory
var ErrGoModNotFound = errors.New("go.mod not found in current directory")

// Reset removes replace directives from go.mod for specified repositories.
// If repoNames is empty, removes replace directives for all polyrepo-managed repositories.
// Returns error if go.mod doesn't exist in baseDir.
func Reset(log logging.Logger, baseDir string, repoNames []string) error {
	// Validate go.mod exists in baseDir
	goModPath := filepath.Join(baseDir, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		if os.IsNotExist(err) {
			return ErrGoModNotFound
		}
		return fmt.Errorf("failed to stat go.mod: %w", err)
	}

	log.Debug("resetting repositories",
		zap.String("baseDir", baseDir),
		zap.String("goModPath", goModPath),
		zap.Strings("repos", repoNames),
	)

	// If no repos specified, remove all polyrepo-related replaces
	if len(repoNames) == 0 {
		log.Debug("no repos specified, resetting all known repositories")
		repoNames = []string{"avalanchego", "firewood"}
	}

	log.Debug("removing replace directives",
		zap.Strings("repoNames", repoNames),
	)

	// Remove replace directives for each repo
	for _, repoName := range repoNames {
		log.Debug("processing repository",
			zap.String("repoName", repoName),
		)

		config, err := GetRepoConfig(repoName)
		if err != nil {
			return err
		}

		// Try to remove replace directive for the primary module name
		log.Debug("removing replace directive for module",
			zap.String("module", config.GoModule),
		)
		err = RemoveReplaceDirective(log, goModPath, config.GoModule)
		if err != nil {
			// Ignore error if replace doesn't exist
			log.Debug("ignoring error (replace may not exist)",
				zap.String("module", config.GoModule),
				zap.Error(err),
			)
		}

		// If the repo has an InternalGoModule (like firewood), also try to remove that
		if config.InternalGoModule != "" && config.InternalGoModule != config.GoModule {
			log.Debug("removing replace directive for internal module",
				zap.String("module", config.InternalGoModule),
			)
			err = RemoveReplaceDirective(log, goModPath, config.InternalGoModule)
			if err != nil {
				// Ignore error if replace doesn't exist
				log.Debug("ignoring error (replace may not exist)",
					zap.String("module", config.InternalGoModule),
					zap.Error(err),
				)
			}
		}
	}

	log.Debug("reset complete")
	return nil
}
