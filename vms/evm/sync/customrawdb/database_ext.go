// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"fmt"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// ParseStateSchemeExt parses the state scheme from the provided string.
func ParseStateSchemeExt(provided string, disk ethdb.Database) (string, error) {
	// Check for custom scheme
	if provided == FirewoodScheme {
		if diskScheme := rawdb.ReadStateScheme(disk); diskScheme != "" {
			// Valid scheme on disk mismatched
			return "", fmt.Errorf("state scheme %s already set on disk, cannot use Firewood", diskScheme)
		}
		// If no conflicting scheme is found, is valid.
		return FirewoodScheme, nil
	}

	// Check for valid eth scheme
	return rawdb.ParseStateScheme(provided, disk)
}
