package firewood

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	xsync "github.com/ava-labs/avalanchego/x/sync"
	"github.com/ava-labs/firewood-go-ethhash/ffi"
)

var _ xsync.DB[*ffi.RangeProof, *ffi.ChangeProof] = (*DB)(nil)

type DB struct {
	fw *ffi.Database
}

func New(db *ffi.Database) *DB {
	return &DB{fw: db}
}

func (db *DB) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	root, err := db.fw.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

func (db *DB) CommitChangeProof(ctx context.Context, end maybe.Maybe[[]byte], proof *ffi.ChangeProof) (maybe.Maybe[[]byte], error) {
	// TODO: implement
	return maybe.Maybe[[]byte]{}, nil
}

func (db *DB) CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *ffi.RangeProof) (maybe.Maybe[[]byte], error) {
	/*
	result, err := db.fw.VerifyAndCommitRangeProof(proof, end, start, end, maxLength)
	if err != nil {
		return maybe.Maybe[[]byte]{}, err
	}
	return maybe.Some(result), nil
	return db.fw.CommitRangeProof(ctx, start, end, proof)
	*/
	return maybe.Maybe[[]byte]{}, nil
}

func (db *DB) GetChangeProof(ctx context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ffi.ChangeProof, error) {
	// return db.fw.ChangeProof(startRootID, endRootID, start, end, maxLength)
	return nil, nil
}

func (db *DB) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ffi.RangeProof, error) {
	return db.fw.RangeProof(maybe.Some(rootID[:]), start, end, uint32(maxLength))
}

func (db *DB) VerifyChangeProof(ctx context.Context, proof *ffi.ChangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return nil
}

func (db *DB) VerifyRangeProof(ctx context.Context, proof *ffi.RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return proof.Verify(expectedEndRootID[:], start, end, uint32(maxLength))
}

func (db *DB) Clear() error {
	// TODO: implement
	return nil
}
