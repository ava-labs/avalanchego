// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// ResetRepos removes replace directives for specified repos (or all if none specified)
func ResetRepos(log logging.Logger, goModPath string, repoNames []string) error {
	log.Debug("resetting repositories",
		zap.String("goModPath", goModPath),
		zap.Strings("repos", repoNames),
	)

	// If no repos specified, remove all polyrepo-related replaces
	if len(repoNames) == 0 {
		log.Debug("no repos specified, resetting all known repositories")
		repoNames = []string{"avalanchego", "coreth", "firewood"}
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

		log.Debug("removing replace directive for module",
			zap.String("module", config.GoModule),
		)

		// Remove the replace directive
		err = RemoveReplaceDirective(log, goModPath, config.GoModule)
		if err != nil {
			// Ignore error if replace doesn't exist
			log.Debug("ignoring error (replace may not exist)",
				zap.String("module", config.GoModule),
				zap.Error(err),
			)
			continue
		}
	}

	log.Debug("reset complete")
	return nil
}
