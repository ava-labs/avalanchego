package sync

import "github.com/ava-labs/avalanchego/x/merkledb"

type SyncableDB interface {
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}
