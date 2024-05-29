// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import "strings"

func AppendNamespace(prefix, suffix string) string {
	switch {
	case len(prefix) == 0:
		return suffix
	case len(suffix) == 0:
		return prefix
	default:
		return strings.Join([]string{prefix, suffix}, "_")
	}
}
