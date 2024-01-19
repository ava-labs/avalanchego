// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import "github.com/ava-labs/avalanchego/x/merkledb"

type DB interface {
	merkledb.Clearer
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}
