// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"bytes"
	"context"
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var (
	_ sync.DB[*RangeProof, *ChangeProof] = (*database)(nil)

	defaultSimultaneousWorkLimit = 8
)

// database wraps a Firewood [ffi.Database] to implement the xsync.DB interface.
type database struct {
	db *ffi.Database
}

type Config struct {
	SimultaneousWorkLimit int
	Log                   logging.Logger
	StateSyncNodes        []ids.NodeID
	Registerer            prometheus.Registerer
}

func New(config Config, db *ffi.Database, targetRoot ids.ID, proofClient *p2p.Client) (*sync.Syncer[*RangeProof, *ChangeProof], error) {
	return newWithDB(
		config,
		&database{db: db},
		targetRoot,
		proofClient,
	)
}

func newWithDB(config Config, db sync.DB[*RangeProof, *ChangeProof], targetRoot ids.ID, proofClient *p2p.Client) (*sync.Syncer[*RangeProof, *ChangeProof], error) {
	if config.Registerer == nil {
		config.Registerer = prometheus.NewRegistry()
	}
	if config.Log == nil {
		config.Log = logging.NoLog{}
	}
	if config.SimultaneousWorkLimit == 0 {
		config.SimultaneousWorkLimit = defaultSimultaneousWorkLimit
	}
	return sync.NewSyncer(
		db,
		sync.Config[*RangeProof, *ChangeProof]{
			RangeProofMarshaler:   rangeProofMarshaler{},
			ChangeProofMarshaler:  changeProofMarshaler{},
			EmptyRoot:             ids.ID(types.EmptyRootHash),
			ProofClient:           proofClient,
			SimultaneousWorkLimit: config.SimultaneousWorkLimit,
			Log:                   config.Log,
			TargetRoot:            targetRoot,
			StateSyncNodes:        config.StateSyncNodes,
		},
		config.Registerer,
	)
}

func (db *database) GetMerkleRoot(context.Context) (ids.ID, error) {
	return ids.ID(db.db.Root()), nil
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

func (db *database) GetChangeProof(_ context.Context, startRootID ids.ID, endRootID ids.ID, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], maxLength int) (*ChangeProof, error) {
	proof, err := db.db.ChangeProof(ffi.Hash(startRootID), ffi.Hash(endRootID), start, end, uint32(maxLength))
	switch {
	case errors.Is(err, ffi.ErrStartRevisionNotFound):
		return nil, sync.ErrInsufficientHistory
	case errors.Is(err, ffi.ErrEndRevisionNotFound):
		return nil, sync.ErrNoEndRoot
	case err != nil:
		return nil, err
	}
	return &ChangeProof{cp: proof}, nil
}

func (db *database) VerifyChangeProof(_ context.Context, proof *ChangeProof, start maybe.Maybe[[]byte], end maybe.Maybe[[]byte], expectedEndRootID ids.ID, maxLength int) error {
	proposal, err := db.db.VerifyChangeProof(proof.cp, ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
	if err != nil {
		return err
	}
	proof.proposal = proposal
	return nil
}

func (*database) CommitChangeProof(_ context.Context, end maybe.Maybe[[]byte], proof *ChangeProof) (maybe.Maybe[[]byte], error) {
	if proof.proposal == nil {
		return maybe.Nothing[[]byte](), errors.New("change proof was not verified before commit")
	}
	if _, err := proof.proposal.CommitWithRebase(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	nextKeyRange, err := proof.cp.FindNextKey(end)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	if nextKeyRange == nil {
		return maybe.Nothing[[]byte](), nil
	}

	nextKey := nextKeyRange.StartKey()
	if err := nextKeyRange.Free(); err != nil {
		return maybe.Nothing[[]byte](), err
	}

	if end.HasValue() && bytes.Compare(nextKey, end.Value()) > 0 {
		return maybe.Nothing[[]byte](), nil
	}
	return maybe.Some(nextKey), nil
}

func (db *database) Clear() error {
	// Prefix delete key of length 0.
	_, err := db.db.Update([]ffi.BatchOp{ffi.PrefixDelete([]byte{})})
	return err
}
