package draftreview

import (
	"fmt"

	repoversion "github.com/ava-labs/avalanchego/version"
)

func VersionString() string {
	commit := repoversion.GitCommit
	if commit == "" {
		commit = "unknown"
	}
	return fmt.Sprintf("gh-pending-review commit=%s", commit)
}
