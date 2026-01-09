// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

type CodeQueue interface {
	AddCode(context.Context, []common.Hash) error
}

type evmDB struct {
	*database
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
			database:  &database{db},
			codeQueue: codeQueue,
		},
		targetRoot,
		rangeProofClient,
		changeProofClient,
	)
}

func (e *evmDB) CommitRangeProof(ctx context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	nextKey, err := e.database.CommitRangeProof(ctx, start, end, proof)
	if err != nil {
		return nextKey, err
	}
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
	return nextKey, nil
}
