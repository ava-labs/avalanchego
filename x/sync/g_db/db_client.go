// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

var _ sync.DB = (*DBClient)(nil)

func NewDBClient(client pb.DBClient) *DBClient {
	return &DBClient{client: client}
}

type DBClient struct {
	client pb.DBClient
}

func (c *DBClient) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	resp, err := c.client.GetMerkleRoot(ctx, &emptypb.Empty{})
	if err != nil {
		return ids.ID{}, err
	}
	return ids.ToID(resp.RootHash)
}

func (c *DBClient) GetChangeProof(
	ctx context.Context,
	startRootID ids.ID,
	endRootID ids.ID,
	startKey []byte,
	endKey []byte,
	keyLimit int,
) (*merkledb.ChangeProof, error) {
	resp, err := c.client.GetChangeProof(ctx, &pb.GetChangeProofRequest{
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

func (c *DBClient) VerifyChangeProof(
	ctx context.Context,
	proof *merkledb.ChangeProof,
	startKey []byte,
	endKey maybe.Maybe[[]byte],
	expectedRootID ids.ID,
) error {
	resp, err := c.client.VerifyChangeProof(ctx, &pb.VerifyChangeProofRequest{
		Proof:            proof.ToProto(),
		StartKey:         startKey,
		EndKey:           endKey.Value(),
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

func (c *DBClient) CommitChangeProof(ctx context.Context, proof *merkledb.ChangeProof) error {
	_, err := c.client.CommitChangeProof(ctx, &pb.CommitChangeProofRequest{
		Proof: proof.ToProto(),
	})
	return err
}

func (c *DBClient) GetProof(ctx context.Context, key []byte) (*merkledb.Proof, error) {
	resp, err := c.client.GetProof(ctx, &pb.GetProofRequest{
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

func (c *DBClient) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	startKey []byte,
	endKey []byte,
	keyLimit int,
) (*merkledb.RangeProof, error) {
	resp, err := c.client.GetRangeProof(ctx, &pb.GetRangeProofRequest{
		RootHash: rootID[:],
		StartKey: startKey,
		EndKey:   endKey,
		KeyLimit: uint32(keyLimit),
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

func (c *DBClient) CommitRangeProof(
	ctx context.Context,
	startKey []byte,
	proof *merkledb.RangeProof,
) error {
	_, err := c.client.CommitRangeProof(ctx, &pb.CommitRangeProofRequest{
		StartKey:   startKey,
		RangeProof: proof.ToProto(),
	})
	return err
}
