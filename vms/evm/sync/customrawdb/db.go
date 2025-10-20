// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"errors"

	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// FirewoodScheme is the scheme for the Firewood storage scheme.
const FirewoodScheme = "firewood"

// errStateSchemeConflict indicates the provided state scheme conflicts with
// what is on disk.
var errStateSchemeConflict = errors.New("state scheme conflict")

// ParseStateScheme parses the state scheme from the provided string.
func ParseStateScheme(provided string, db ethdb.Database) (string, error) {
	// Check for custom scheme
	if provided == FirewoodScheme {
		if diskScheme := rawdb.ReadStateScheme(db); diskScheme != "" {
			// Valid scheme on db mismatched
			return "", errStateSchemeConflict
		}
		// If no conflicting scheme is found, is valid.
		return FirewoodScheme, nil
	}

	// Check for valid eth scheme
	return rawdb.ParseStateScheme(provided, db)
}
