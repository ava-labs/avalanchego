// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/database/rpcdb"
	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ syncpb.SyncableDBServer = (*SyncableDBServer)(nil)

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
	}, nil
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

func (*SyncableDBServer) VerifyChangeProof(context.Context, *syncpb.VerifyChangeProofRequest) (*syncpb.VerifyChangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (*SyncableDBServer) CommitChangeProof(context.Context, *syncpb.CommitChangeProofRequest) (*syncpb.CommitChangeProofResponse, error) {
	return nil, errors.New("TODO")
}

func (s *SyncableDBServer) GetProof(ctx context.Context, req *syncpb.GetProofRequest) (*syncpb.GetProofResponse, error) {
	proof, err := s.db.GetProof(ctx, req.Key)
	if err != nil {
		return nil, err
	}

	protoProof := &syncpb.GetProofResponse{
		Proof: &syncpb.Proof{
			Proof: make([]*syncpb.ProofNode, len(proof.Path)),
		},
	}
	for i, node := range proof.Path {
		protoProof.Proof.Proof[i] = node.ToProto()
	}

	return protoProof, nil
}

func (s *SyncableDBServer) GetRangeProof(ctx context.Context, req *syncpb.GetRangeProofRequest) (*syncpb.GetRangeProofResponse, error) {
	rootID, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}

	proof, err := s.db.GetRangeProofAtRoot(ctx, rootID, req.StartKey, req.EndKey, int(req.KeyLimit))
	if err != nil {
		return nil, err
	}

	protoProof := &syncpb.GetRangeProofResponse{
		Proof: &syncpb.RangeProof{
			Start: &syncpb.Proof{
				Proof: make([]*syncpb.ProofNode, len(proof.StartProof)),
			},
			End: &syncpb.Proof{
				Proof: make([]*syncpb.ProofNode, len(proof.EndProof)),
			},
			KeyValues: make([]*syncpb.KeyValue, len(proof.KeyValues)),
		},
	}
	for i, node := range proof.StartProof {
		protoProof.Proof.Start.Proof[i] = node.ToProto()
	}
	for i, node := range proof.EndProof {
		protoProof.Proof.End.Proof[i] = node.ToProto()
	}
	for i, kv := range proof.KeyValues {
		protoProof.Proof.KeyValues[i] = &syncpb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return protoProof, nil
}

func (s *SyncableDBServer) CommitRangeProof(ctx context.Context, req *syncpb.CommitRangeProofRequest) (*syncpb.CommitRangeProofResponse, error) {
	var proof merkledb.RangeProof
	if err := proof.UnmarshalProto(req.RangeProof); err != nil {
		return nil, err
	}

	err := s.db.CommitRangeProof(ctx, req.StartKey, &proof)
	return &syncpb.CommitRangeProofResponse{
		Error: rpcdb.ErrorToErrEnum[err],
	}, rpcdb.ErrorToRPCError(err)
}

type SyncableDBClient struct{}
