// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"google.golang.org/protobuf/types/known/emptypb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ syncpb.SyncableDBServer = &SyncableDBServer{}

type SyncableDB interface {
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}

type SyncableDBServer struct {
	syncpb.UnsafeSyncableDBServer

	db SyncableDB
}

func (s *SyncableDBServer) GetMerkleRoot(ctx context.Context, _ *emptypb.Empty) (*syncpb.GetMerkleRootResponse, error) {
	root, err := s.db.GetMerkleRoot(ctx)
	if err != nil {
		return nil, err
	}
	return &syncpb.GetMerkleRootResponse{
		RootHash: root[:],
	}, nil // TODO return rpc error
}

func (s *SyncableDBServer) GetChangeProof(ctx context.Context, req *syncpb.GetChangeProofRequest) (*syncpb.GetChangeProofResponse, error) {
	startRootID, err := ids.ToID(req.StartRootHash)
	if err != nil {
		return nil, err
	}
	endRootID, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return nil, err
	}
	changeProof, err := s.db.GetChangeProof(ctx, startRootID, endRootID, req.StartKey, req.EndKey, int(req.KeyLimit))
	if err != nil {
		return nil, err
	}
	return &syncpb.GetChangeProofResponse{ // TODO fix
		Proof: &syncpb.ChangeProof{
			HadRootsInHistory: changeProof.HadRootsInHistory,
			StartProof:        &syncpb.Proof{},
			EndProof:          &syncpb.Proof{},
			KeyChanges:        []*syncpb.KeyChange{},
		},
	}, nil
}

func (s *SyncableDBServer) VerifyChangeProof(context.Context, *syncpb.VerifyChangeProofRequest) (*syncpb.VerifyChangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) CommitChangeProof(context.Context, *syncpb.CommitChangeProofRequest) (*syncpb.CommitChangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) GetProof(context.Context, *syncpb.GetProofRequest) (*syncpb.GetProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) GetRangeProof(context.Context, *syncpb.GetRangeProofRequest) (*syncpb.GetRangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) VerifyRangeProof(context.Context, *syncpb.VerifyRangeProofRequest) (*syncpb.VerifyRangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) CommitRangeProof(context.Context, *syncpb.CommitRangeProofRequest) (*syncpb.CommitRangeProofResponse, error) {
	return nil, errors.New("TODO")
}

type SyncableDBClient struct{}
