// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var _ xsync.DB[*RangeProof, struct{}] = (*evmDB)(nil)

type CodeQueue interface {
	AddCode(context.Context, []common.Hash) error
}

type evmDB struct {
	db        *database
	codeQueue CodeQueue
}

func NewEVM(
	config Config,
	db *ffi.Database,
	codeQueue CodeQueue,
	targetRoot ids.ID,
	rangeProofClient *p2p.Client,
	changeProofClient *p2p.Client,
) (*xsync.Syncer[*RangeProof, struct{}], error) {
	return newWithDB(
		config,
		&evmDB{
			db:        &database{db},
			codeQueue: codeQueue,
		},
		targetRoot,
		rangeProofClient,
		changeProofClient,
	)
}

func (e *evmDB) CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	// First enqueue any code hashes found in the proof.
	// If an error occurs here, we don't want to commit the proof to the database, because the
	// code hashes will never be requested.
	var codeHashes []common.Hash //nolint:prealloc // we don't know how many there will be
	for h, err := range proof.rp.CodeHashes() {
		if err != nil {
			return maybe.Nothing[[]byte](), err
		}
		codeHashes = append(codeHashes, common.Hash(h))
	}
	if err := e.codeQueue.AddCode(ctx, codeHashes); err != nil {
		return maybe.Nothing[[]byte](), err
	}
	nextKey, err := e.db.CommitRangeProof(ctx, start, end, proof)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return nextKey, nil
}

func (*evmDB) CommitChangeProof(context.Context, maybe.Maybe[[]byte], struct{}) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), errors.New("change proof code hashes not implemented")
}

func (e *evmDB) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	return e.db.GetMerkleRoot(ctx)
}

func (e *evmDB) Clear() error {
	return e.db.Clear()
}

func (e *evmDB) GetChangeProof(ctx context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (struct{}, error) {
	return e.db.GetChangeProof(ctx, startRootID, endRootID, start, end, maxLength)
}

func (e *evmDB) GetRangeProofAtRoot(ctx context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	return e.db.GetRangeProofAtRoot(ctx, rootID, start, end, maxLength)
}

func (e *evmDB) VerifyChangeProof(ctx context.Context, proof struct{}, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return e.db.VerifyChangeProof(ctx, proof, start, end, expectedEndRootID, maxLength)
}

func (e *evmDB) VerifyRangeProof(ctx context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	return e.db.VerifyRangeProof(ctx, proof, start, end, expectedEndRootID, maxLength)
}
