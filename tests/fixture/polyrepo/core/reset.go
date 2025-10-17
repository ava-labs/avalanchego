// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

// ResetRepos removes replace directives for specified repos (or all if none specified)
func ResetRepos(goModPath string, repoNames []string) error {
	// If no repos specified, remove all polyrepo-related replaces
	if len(repoNames) == 0 {
		repoNames = []string{"avalanchego", "coreth", "firewood"}
	}

	// Remove replace directives for each repo
	for _, repoName := range repoNames {
		config, err := GetRepoConfig(repoName)
		if err != nil {
			return err
		}

		// Remove the replace directive
		err = RemoveReplaceDirective(goModPath, config.GoModule)
		if err != nil {
			// Ignore error if replace doesn't exist
			continue
		}
	}

	return nil
}
