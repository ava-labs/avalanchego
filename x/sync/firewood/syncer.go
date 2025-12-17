// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var (
	_ xsync.DB[*RangeProof, struct{}] = (*database)(nil)

	defaultSimultaneousWorkLimit = 8
)

// database wraps a Firewood FFI database to implement the xsync.DB interface.
type database struct {
	db *ffi.Database
}

type Config struct {
	SimultaneousWorkLimit int                   // optional, defaults to 8
	Log                   logging.Logger        // optional
	StateSyncNodes        []ids.NodeID          // optional
	Registerer            prometheus.Registerer // optional
}

func NewSyncer(config Config, db *ffi.Database, targetRoot ids.ID, rangeProofClient *p2p.Client, changeProofClient *p2p.Client) (*xsync.Syncer[*RangeProof, struct{}], error) {
	r := config.Registerer
	if r == nil {
		r = prometheus.NewRegistry()
	}
	logger := config.Log
	if logger == nil {
		logger = logging.NoLog{}
	}
	workLimit := config.SimultaneousWorkLimit
	if workLimit <= 0 {
		workLimit = defaultSimultaneousWorkLimit
	}
	return xsync.NewSyncer(
		&database{db: db},
		xsync.Config[*RangeProof, struct{}]{
			RangeProofMarshaler:   rangeProofMarshaler{},
			ChangeProofMarshaler:  dummyMarshaler{},
			EmptyRoot:             ids.ID(types.EmptyRootHash),
			RangeProofClient:      rangeProofClient,
			ChangeProofClient:     changeProofClient,
			SimultaneousWorkLimit: workLimit,
			Log:                   logger,
			TargetRoot:            targetRoot,
			StateSyncNodes:        config.StateSyncNodes,
		},
		r,
	)
}

func (db *database) GetMerkleRoot(context.Context) (ids.ID, error) {
	root, err := db.db.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

func (db *database) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.db.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return &RangeProof{
		rp: proof,
	}, nil
}

func (*database) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	// Extra data must be provided at commit time.
	// TODO: remove this once the FFI no longer requires it.
	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.rp.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

func (db *database) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	_, err := db.db.VerifyAndCommitRangeProof(proof.rp, start, end, ffi.Hash(proof.root), uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	nextKeyRange, err := proof.rp.FindNextKey()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	// No error indicates the range is complete.
	if nextKeyRange == nil {
		return maybe.Nothing[[]byte](), nil
	}

	nextKey := nextKeyRange.StartKey()
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	// TODO: This will eventually be handled by `FindNextKey`.
	if (end.HasValue() && bytes.Compare(nextKey, end.Value()) > 0) || (start.HasValue() && bytes.Equal(nextKey, start.Value())) {
		return maybe.Nothing[[]byte](), nil
	}

	return maybe.Some(nextKey), nil
}

// TODO: implement this method.
func (*database) GetChangeProof(context.Context, ids.ID, ids.ID, maybe.Maybe[[]byte], maybe.Maybe[[]byte], int) (struct{}, error) {
	return struct{}{}, errors.New("change proofs are not implemented")
}

// TODO: implement this method.
func (*database) VerifyChangeProof(context.Context, struct{}, maybe.Maybe[[]byte], maybe.Maybe[[]byte], ids.ID, int) error {
	return errors.New("change proofs are not implemented")
}

// TODO: implement this method.
func (*database) CommitChangeProof(context.Context, maybe.Maybe[[]byte], struct{}) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), errors.New("change proofs are not implemented")
}

func (db *database) Clear() error {
	// Prefix delete key of length 0.
	_, err := db.db.Update([][]byte{{}}, [][]byte{nil})
	return err
}
