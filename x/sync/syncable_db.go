package sync

import "github.com/ava-labs/avalanchego/x/merkledb"

type SyncableDB interface {
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofGetter
	merkledb.ChangeProofVerifier
	merkledb.ChangeProofCommitter
	merkledb.RangeProofGetter
	merkledb.RangeProofCommitter
}
