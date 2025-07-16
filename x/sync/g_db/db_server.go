// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gdb

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/ava-labs/avalanchego/x/sync"

	pb "github.com/ava-labs/avalanchego/buf/proto/pb/sync"
)

var _ pb.DBServer = (*DBServer)(nil)

func NewDBServer(db sync.DB) *DBServer {
	return &DBServer{
		db: db,
	}
}

type DBServer struct {
	pb.UnsafeDBServer

	db sync.DB
}

func (s *DBServer) GetMerkleRoot(
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

func (s *DBServer) GetChangeProof(
	ctx context.Context,
	req *pb.GetChangeProofRequest,
) (*pb.GetChangeProofResponse, error) {
	startRootID, err := ids.ToID(req.StartRootHash)
	if err != nil {
		return nil, err
	}
	endRootID, err := ids.ToID(req.EndRootHash)
	if err != nil {
		return nil, err
	}
	start := maybe.Nothing[[]byte]()
	if req.StartKey != nil && !req.StartKey.IsNothing {
		start = maybe.Some(req.StartKey.Value)
	}
	end := maybe.Nothing[[]byte]()
	if req.EndKey != nil && !req.EndKey.IsNothing {
		end = maybe.Some(req.EndKey.Value)
	}

	changeProof, err := s.db.GetChangeProof(
		ctx,
		startRootID,
		endRootID,
		start,
		end,
		int(req.KeyLimit),
	)
	if err != nil {
		if !errors.Is(err, merkledb.ErrInsufficientHistory) {
			return nil, err
		}
		return &pb.GetChangeProofResponse{
			Response: &pb.GetChangeProofResponse_RootNotPresent{
				RootNotPresent: true,
			},
		}, nil
	}

	return &pb.GetChangeProofResponse{
		Response: &pb.GetChangeProofResponse_ChangeProof{
			ChangeProof: changeProof.ToProto(),
		},
	}, nil
}

func (s *DBServer) VerifyChangeProof(
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
	startKey := maybe.Nothing[[]byte]()
	if req.StartKey != nil && !req.StartKey.IsNothing {
		startKey = maybe.Some(req.StartKey.Value)
	}
	endKey := maybe.Nothing[[]byte]()
	if req.EndKey != nil && !req.EndKey.IsNothing {
		endKey = maybe.Some(req.EndKey.Value)
	}

	// TODO there's probably a better way to do this.
	var errString string
	if err := s.db.VerifyChangeProof(ctx, &proof, startKey, endKey, rootID); err != nil {
		errString = err.Error()
	}
	return &pb.VerifyChangeProofResponse{
		Error: errString,
	}, nil
}

func (s *DBServer) CommitChangeProof(
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

func (s *DBServer) GetProof(
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

func (s *DBServer) GetRangeProof(
	ctx context.Context,
	req *pb.GetRangeProofRequest,
) (*pb.GetRangeProofResponse, error) {
	rootID, err := ids.ToID(req.RootHash)
	if err != nil {
		return nil, err
	}
	start := maybe.Nothing[[]byte]()
	if req.StartKey != nil && !req.StartKey.IsNothing {
		start = maybe.Some(req.StartKey.Value)
	}
	end := maybe.Nothing[[]byte]()
	if req.EndKey != nil && !req.EndKey.IsNothing {
		end = maybe.Some(req.EndKey.Value)
	}
	proof, err := s.db.GetRangeProofAtRoot(ctx, rootID, start, end, int(req.KeyLimit))
	if err != nil {
		return nil, err
	}

	protoProof := &pb.GetRangeProofResponse{
		Proof: &pb.RangeProof{
			StartProof: make([]*pb.ProofNode, len(proof.StartProof)),
			EndProof:   make([]*pb.ProofNode, len(proof.EndProof)),
			KeyValues:  make([]*pb.KeyValue, len(proof.KeyChanges)),
		},
	}
	for i, node := range proof.StartProof {
		protoProof.Proof.StartProof[i] = node.ToProto()
	}
	for i, node := range proof.EndProof {
		protoProof.Proof.EndProof[i] = node.ToProto()
	}
	for i, kv := range proof.KeyChanges {
		protoProof.Proof.KeyValues[i] = &pb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value.Value(),
		}
	}

	return protoProof, nil
}

func (s *DBServer) CommitRangeProof(
	ctx context.Context,
	req *pb.CommitRangeProofRequest,
) (*emptypb.Empty, error) {
	var proof merkledb.RangeProof
	if err := proof.UnmarshalProto(req.RangeProof); err != nil {
		return nil, err
	}

	start := maybe.Nothing[[]byte]()
	if req.StartKey != nil && !req.StartKey.IsNothing {
		start = maybe.Some(req.StartKey.Value)
	}

	end := maybe.Nothing[[]byte]()
	if req.EndKey != nil && !req.EndKey.IsNothing {
		end = maybe.Some(req.EndKey.Value)
	}

	err := s.db.CommitRangeProof(ctx, start, end, &proof)
	return &emptypb.Empty{}, err
}

func (s *DBServer) Clear(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.db.Clear()
}
