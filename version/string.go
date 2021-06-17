// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
)

func String(commit string) string {
	format := "%s [database=%s"
	args := []interface{}{
		Current,
		CurrentDatabase,
	}

	if commit != "" {
		format += ", commit=%s"
		args = append(args, commit)
	}
	format += "]\n"
	return fmt.Sprintf(format, args...)
}
