// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var (
	_ syncpb.SyncableDBServer = (*SyncableDBServer)(nil)
	_ SyncableDB              = (*SyncableDBClient)(nil)
)

type SyncableDB interface {
	merkledb.MerkleRootGetter
	merkledb.ProofGetter
	merkledb.ChangeProofer
	merkledb.RangeProofer
}

func NewSyncableDBServer(db SyncableDB) *SyncableDBServer {
	return &SyncableDBServer{db: db}
}

type SyncableDBServer struct {
	syncpb.UnsafeSyncableDBServer

	db SyncableDB
}

func (s *SyncableDBServer) GetMerkleRoot(
	ctx context.Context,
	_ *emptypb.Empty,
) (*syncpb.GetMerkleRootResponse, error) {
	root, err := s.db.GetMerkleRoot(ctx)
	if err != nil {
		return nil, err
	}
	return &syncpb.GetMerkleRootResponse{
		RootHash: root[:],
	}, nil
}

func (s *SyncableDBServer) GetChangeProof(
	ctx context.Context,
	req *syncpb.GetChangeProofRequest,
) (*syncpb.ChangeProof, error) {
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
	req *syncpb.VerifyChangeProofRequest,
) (*syncpb.VerifyChangeProofResponse, error) {
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
	return &syncpb.VerifyChangeProofResponse{
		Error: errString,
	}, nil
}

func (s *SyncableDBServer) CommitChangeProof(
	ctx context.Context,
	req *syncpb.CommitChangeProofRequest,
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
	req *syncpb.GetProofRequest,
) (*syncpb.GetProofResponse, error) {
	proof, err := s.db.GetProof(ctx, req.Key)
	if err != nil {
		return nil, err
	}

	return &syncpb.GetProofResponse{
		Proof: proof.ToProto(),
	}, nil
}

func (s *SyncableDBServer) GetRangeProof(
	ctx context.Context,
	req *syncpb.GetRangeProofRequest,
) (*syncpb.GetRangeProofResponse, error) {
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
			Start:     make([]*syncpb.ProofNode, len(proof.StartProof)),
			End:       make([]*syncpb.ProofNode, len(proof.EndProof)),
			KeyValues: make([]*syncpb.KeyValue, len(proof.KeyValues)),
		},
	}
	for i, node := range proof.StartProof {
		protoProof.Proof.Start[i] = node.ToProto()
	}
	for i, node := range proof.EndProof {
		protoProof.Proof.End[i] = node.ToProto()
	}
	for i, kv := range proof.KeyValues {
		protoProof.Proof.KeyValues[i] = &syncpb.KeyValue{
			Key:   kv.Key,
			Value: kv.Value,
		}
	}

	return protoProof, nil
}

func (s *SyncableDBServer) CommitRangeProof(
	ctx context.Context,
	req *syncpb.CommitRangeProofRequest,
) (*emptypb.Empty, error) {
	var proof merkledb.RangeProof
	if err := proof.UnmarshalProto(req.RangeProof); err != nil {
		return nil, err
	}

	err := s.db.CommitRangeProof(ctx, req.StartKey, &proof)
	return &emptypb.Empty{}, err
}

func NewSyncableDBClient(client syncpb.SyncableDBClient) *SyncableDBClient {
	return &SyncableDBClient{client: client}
}

type SyncableDBClient struct {
	client syncpb.SyncableDBClient
}

func (c *SyncableDBClient) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	resp, err := c.client.GetMerkleRoot(ctx, &emptypb.Empty{})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.RootHash)
}

func (c *SyncableDBClient) GetChangeProof(
	ctx context.Context,
	startRootID ids.ID,
	endRootID ids.ID,
	startKey []byte,
	endKey []byte,
	keyLimit int,
) (*merkledb.ChangeProof, error) {
	resp, err := c.client.GetChangeProof(ctx, &syncpb.GetChangeProofRequest{
		StartRootHash: startRootID[:],
		EndRootHash:   endRootID[:],
		StartKey:      startKey,
		EndKey:        endKey,
		KeyLimit:      uint32(keyLimit),
	})
	if err != nil {
		return nil, err
	}
	var proof merkledb.ChangeProof
	if err := proof.UnmarshalProto(resp); err != nil {
		return nil, err
	}
	return &proof, nil
}

func (c *SyncableDBClient) VerifyChangeProof(
	ctx context.Context,
	proof *merkledb.ChangeProof,
	startKey []byte,
	endKey []byte,
	expectedRootID ids.ID,
) error {
	resp, err := c.client.VerifyChangeProof(ctx, &syncpb.VerifyChangeProofRequest{
		Proof:            proof.ToProto(),
		StartKey:         startKey,
		EndKey:           endKey,
		ExpectedRootHash: expectedRootID[:],
	})
	if err != nil {
		return err
	}

	// TODO there's probably a better way to do this.
	if len(resp.Error) == 0 {
		return nil
	}
	return errors.New(resp.Error)
}

func (c *SyncableDBClient) CommitChangeProof(ctx context.Context, proof *merkledb.ChangeProof) error {
	_, err := c.client.CommitChangeProof(ctx, &syncpb.CommitChangeProofRequest{
		Proof: proof.ToProto(),
	})
	return err
}

func (c *SyncableDBClient) GetProof(ctx context.Context, key []byte) (*merkledb.Proof, error) {
	resp, err := c.client.GetProof(ctx, &syncpb.GetProofRequest{
		Key: key,
	})
	if err != nil {
		return nil, err
	}

	var proof merkledb.Proof
	if err := proof.UnmarshalProto(resp.Proof); err != nil {
		return nil, err
	}
	return &proof, nil
}

func (c *SyncableDBClient) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	startKey []byte,
	endKey []byte,
	keyLimit int,
) (*merkledb.RangeProof, error) {
	resp, err := c.client.GetRangeProof(ctx, &syncpb.GetRangeProofRequest{
		RootHash: rootID[:],
		StartKey: startKey,
		EndKey:   endKey,
		KeyLimit: uint32(keyLimit),
		// BytesLimit: c.maxMessageSize, TODO remove
	})
	if err != nil {
		return nil, err
	}

	var proof merkledb.RangeProof
	if err := proof.UnmarshalProto(resp.Proof); err != nil {
		return nil, err
	}
	return &proof, nil
}

func (c *SyncableDBClient) CommitRangeProof(
	ctx context.Context,
	startKey []byte,
	proof *merkledb.RangeProof,
) error {
	_, err := c.client.CommitRangeProof(ctx, &syncpb.CommitRangeProofRequest{
		StartKey:   startKey,
		RangeProof: proof.ToProto(),
	})
	return err
}
