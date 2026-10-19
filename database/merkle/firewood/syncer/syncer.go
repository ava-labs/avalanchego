// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package syncer

import (
	"context"
	"errors"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/merkle/sync"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/peertracker"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

var (
	_ sync.DB[*RangeProof, struct{}] = (*database)(nil)

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

func New(
	config Config,
	db *ffi.Database,
	targetRoot ids.ID,
	proofClient *peertracker.Client,
) (*sync.Syncer[*RangeProof, struct{}], error) {
	return newWithDB(
		config,
		&database{db},
		targetRoot,
		proofClient,
	)
}

func newWithDB(
	config Config,
	db sync.DB[*RangeProof, struct{}],
	targetRoot ids.ID,
	proofClient *peertracker.Client,
) (*sync.Syncer[*RangeProof, struct{}], error) {
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
		sync.Config[*RangeProof, struct{}]{
			RangeProofMarshaler:   rangeProofMarshaler{db: db.(*database).db},
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
	vrp, err := proof.rp.Verify(ffi.Hash(expectedEndRootID), start, end, uint32(maxLength))
	if err != nil {
		return err
	}
	proof.vrp = vrp
	return nil
}

func (db *database) CommitRangeProof(_ context.Context, _, _ maybe.Maybe[[]byte], proof *RangeProof) (maybe.Maybe[[]byte], error) {
	_, err := proof.vrp.Commit()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	nextKeyRanges, err := proof.vrp.NextKeyRanges()
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	// No error indicates the range is complete.
	if len(nextKeyRanges) == 0 {
		return maybe.Nothing[[]byte](), nil
	}

	nextKey := nextKeyRanges[0].StartKey
	return maybe.Some(nextKey), nil
}

// TODO: Use change proofs to optimize syncing.
// Returning the sentinel error suggests to the server handler to serve a full range proof instead.
func (*database) GetChangeProof(context.Context, ids.ID, ids.ID, maybe.Maybe[[]byte], maybe.Maybe[[]byte], int) (struct{}, error) {
	return struct{}{}, sync.ErrInsufficientHistory
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
	_, err := db.db.Update([]ffi.BatchOp{ffi.PrefixDelete([]byte{})})
	return err
}
