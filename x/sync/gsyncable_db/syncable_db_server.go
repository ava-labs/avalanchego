// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsyncabledb

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/avalanchego/x/sync"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ pb.SyncableDBServer = (*SyncableDBServer)(nil)

func NewSyncableDBServer(db sync.SyncableDB) *SyncableDBServer {
	return &SyncableDBServer{db: db}
}

type SyncableDBServer struct {
	pb.UnsafeSyncableDBServer

	db sync.SyncableDB
}

func (s *SyncableDBServer) GetMerkleRoot(
	ctx context.Context,
	_ *emptypb.Empty,
) (*pb.GetMerkleRootResponse, error) {
	root, err := s.db.GetMerkleRoot(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.GetMerkleRootResponse{
		RootHash: root[:],
	}, nil
}

func (s *SyncableDBServer) GetChangeProof(
	ctx context.Context,
	req *pb.GetChangeProofRequest,
) (*pb.ChangeProof, error) {
	startRootID, err := ids.ToID(req.StartRootHash)
	if err != nil {
		return nil, err
	}
	endRootID, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return nil, err
	}
	changeProof, err := s.db.GetChangeProof(
		ctx,
		startRootID,
		endRootID,
		req.StartKey,
		req.EndKey,
		int(req.KeyLimit),
	)
	if err != nil {
		return nil, err
	}
	return changeProof.ToProto(), nil
}

func (s *SyncableDBServer) VerifyChangeProof(
	ctx context.Context,
	req *pb.VerifyChangeProofRequest,
) (*pb.VerifyChangeProofResponse, error) {
	var proof merkledb.ChangeProof
	if err := proof.UnmarshalProto(req.Proof); err != nil {
		return nil, err
	}

	rootID, err := ids.ToID(req.ExpectedRootHash)
	if err != nil {
		return nil, err
	}

	// TODO there's probably a better way to do this.
	var errString string
	if err := s.db.VerifyChangeProof(ctx, &proof, req.StartKey, req.EndKey, rootID); err != nil {
		errString = err.Error()
	}
	return &pb.VerifyChangeProofResponse{
		Error: errString,
	}, nil
}

func (s *SyncableDBServer) CommitChangeProof(
	ctx context.Context,
	req *pb.CommitChangeProofRequest,
) (*emptypb.Empty, error) {
	var proof merkledb.ChangeProof
	if err := proof.UnmarshalProto(req.Proof); err != nil {
		return nil, err
	}

	err := s.db.CommitChangeProof(ctx, &proof)
	return &emptypb.Empty{}, err
}

func (s *SyncableDBServer) GetProof(
	ctx context.Context,
	req *pb.GetProofRequest,
) (*pb.GetProofResponse, error) {
	proof, err := s.db.GetProof(ctx, req.Key)
	if err != nil {
		return nil, err
	}

	return &pb.GetProofResponse{
		Proof: proof.ToProto(),
	}, nil
}

func (s *SyncableDBServer) GetRangeProof(
	ctx context.Context,
	req *pb.GetRangeProofRequest,
) (*pb.GetRangeProofResponse, error) {
	rootID, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}

	proof, err := s.db.GetRangeProofAtRoot(ctx, rootID, req.StartKey, req.EndKey, int(req.KeyLimit))
	if err != nil {
		return nil, err
	}

	protoProof := &pb.GetRangeProofResponse{
		Proof: &pb.RangeProof{
			Start:     make([]*pb.ProofNode, len(proof.StartProof)),
			End:       make([]*pb.ProofNode, len(proof.EndProof)),
			KeyValues: make([]*pb.KeyValue, len(proof.KeyValues)),
		},
	}
	for i, node := range proof.StartProof {
		protoProof.Proof.Start[i] = node.ToProto()
	}
	for i, node := range proof.EndProof {
		protoProof.Proof.End[i] = node.ToProto()
	}
	for i, kv := range proof.KeyValues {
		protoProof.Proof.KeyValues[i] = &pb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return protoProof, nil
}

func (s *SyncableDBServer) CommitRangeProof(
	ctx context.Context,
	req *pb.CommitRangeProofRequest,
) (*emptypb.Empty, error) {
	var proof merkledb.RangeProof
	if err := proof.UnmarshalProto(req.RangeProof); err != nil {
		return nil, err
	}

	err := s.db.CommitRangeProof(ctx, req.StartKey, &proof)
	return &emptypb.Empty{}, err
}
