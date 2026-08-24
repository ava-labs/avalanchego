// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
			// A chain whose head is still the genesis block has no history to
			// misread, so allow switching it to Firewood; genesis state is
			// re-committed by SetupGenesisBlock because the Firewood database
			// does not have the genesis root. This covers tmpnet, which
			// initializes a chain once with default config before restarting
			// nodes with the real chain config.
			headHash := rawdb.ReadHeadHeaderHash(db)
			if number := rawdb.ReadHeaderNumber(db, headHash); number == nil || *number == 0 {
				return FirewoodScheme, nil
			}
			return "", errStateSchemeConflict
		}
		// If no conflicting scheme is found, is valid.
		return FirewoodScheme, nil
	}

	// Check for valid eth scheme
	return rawdb.ParseStateScheme(provided, db)
}
