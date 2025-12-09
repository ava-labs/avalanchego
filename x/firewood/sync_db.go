// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"bytes"
	"context"
	"errors"
	"sync"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"

	xsync "github.com/ava-labs/avalanchego/x/sync"
)

var _ xsync.DB[*RangeProof, *ChangeProof] = (*db)(nil)

// db wraps a Firewood FFI database to implement the xsync.DB interface.
type db struct {
	db   *ffi.Database
	lock sync.Mutex // TODO: remove this lock once FFI is thread-safe
}

type Config struct {
	RangeProofClient      *p2p.Client
	ChangeProofClient     *p2p.Client
	SimultaneousWorkLimit int
	Log                   logging.Logger
	TargetRoot            ids.ID
	StateSyncNodes        []ids.NodeID
}

func NewSyncer(fw *ffi.Database, config Config, register prometheus.Registerer) (*xsync.Syncer[*RangeProof, *ChangeProof], error) {
	return xsync.NewSyncer(
		&db{db: fw},
		xsync.Config[*RangeProof, *ChangeProof]{
			RangeProofMarshaler:   RangeProofMarshaler{},
			ChangeProofMarshaler:  ChangeProofMarshaler{},
			EmptyRoot:             ids.ID(types.EmptyRootHash),
			RangeProofClient:      config.RangeProofClient,
			ChangeProofClient:     config.ChangeProofClient,
			SimultaneousWorkLimit: config.SimultaneousWorkLimit,
			Log:                   config.Log,
			TargetRoot:            config.TargetRoot,
			StateSyncNodes:        config.StateSyncNodes,
		},
		register,
	)
}

func (db *db) GetMerkleRoot(context.Context) (ids.ID, error) {
	root, err := db.db.Root()
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ID(root), nil
}

func (db *db) GetRangeProofAtRoot(_ context.Context, rootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*RangeProof, error) {
	proof, err := db.db.RangeProof(ffi.Hash(rootID), start, end, uint32(maxLength))
	if err != nil {
		return nil, err
	}

	return &RangeProof{
		rp: proof,
	}, nil
}

func (*db) VerifyRangeProof(_ context.Context, proof *RangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	// Extra data must be provided at commit time.
	// TODO: remove this once the FFI no longer requires it.
	proof.root = expectedEndRootID
	proof.maxLength = maxLength

	return proof.rp.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
}

func (db *db) CommitRangeProof(_ context.Context, start, end maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	db.lock.Lock()
	defer db.lock.Unlock()
	_, err := db.db.VerifyAndCommitRangeProof(proof.rp, start, end, ffi.Hash(proof.root), uint32(proof.maxLength))
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	nextKeyRange, err := proof.rp.FindNextKey()
	if err != nil || nextKeyRange == nil {
		// No error indicates the range is complete.
		return maybe.Nothing[[]byte](), err
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

//nolint:revive // TODO: implement this method.
func (db *db) GetChangeProof(_ context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	return nil, errors.New("change proofs are not implemented")
}

//nolint:revive // TODO: implement this method.
func (db *db) VerifyChangeProof(_ context.Context, proof *ChangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], endRoot ids.ID, maxLength int) error {
	return errors.New("change proofs are not implemented")
}

//nolint:revive // TODO: implement this method.
func (db *db) CommitChangeProof(_ context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (maybe.Maybe[[]byte], error) {
	return maybe.Nothing[[]byte](), errors.New("change proofs are not implemented")
}

func (db *db) Clear() error {
	db.lock.Lock()
	defer db.lock.Unlock()
	// Prefix delete key of length 0.
	_, err := db.db.Update([][]byte{{}}, [][]byte{nil})
	return err
}
