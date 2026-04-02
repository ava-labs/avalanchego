// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import "strings"

const (
	NamespaceSeparatorByte = '_'
	NamespaceSeparator     = string(NamespaceSeparatorByte)
)

func AppendNamespace(prefix, suffix string) string {
	switch {
	case len(prefix) == 0:
		return suffix
	case len(suffix) == 0:
		return prefix
	default:
		return strings.Join([]string{prefix, suffix}, NamespaceSeparator)
	}
}
