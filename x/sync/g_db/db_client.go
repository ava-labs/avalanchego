// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

var _ sync.DB[merkledb.RangeProof, merkledb.ChangeProof] = (*DBClient)(nil)

func NewDBClient(client pb.DBClient) *DBClient {
	return &DBClient{
		client: client,
	}
}

type DBClient struct {
	client pb.DBClient
}

func (c *DBClient) GetMerkleRoot(ctx context.Context) (ids.ID, error) {
	resp, err := c.client.GetMerkleRoot(ctx, &emptypb.Empty{})
	if err != nil {
		return ids.Empty, err
	}
	return ids.ToID(resp.RootHash)
}

func (c *DBClient) GetChangeProof(
	ctx context.Context,
	startRootID ids.ID,
	endRootID ids.ID,
	startKey maybe.Maybe[[]byte],
	endKey maybe.Maybe[[]byte],
	keyLimit int,
) (*merkledb.ChangeProof, error) {
	if endRootID == ids.Empty {
		return nil, merkledb.ErrEmptyProof
	}

	resp, err := c.client.GetChangeProof(ctx, &pb.GetChangeProofRequest{
		StartRootHash: startRootID[:],
		EndRootHash:   endRootID[:],
		StartKey: &pb.MaybeBytes{
			IsNothing: startKey.IsNothing(),
			Value:     startKey.Value(),
		},
		EndKey: &pb.MaybeBytes{
			IsNothing: endKey.IsNothing(),
			Value:     endKey.Value(),
		},
		KeyLimit: uint32(keyLimit),
	})
	if err != nil {
		return nil, err
	}

	// TODO handle merkledb.ErrInvalidMaxLength
	// TODO disambiguate between the root not being present due to
	// the end root not being present and the start root not being
	// present before the end root. i.e. ErrNoEndRoot vs ErrInsufficientHistory.
	if resp.GetRootNotPresent() {
		return nil, merkledb.ErrInsufficientHistory
	}

	var proof merkledb.ChangeProof
	if err := proof.UnmarshalProto(resp.GetChangeProof()); err != nil {
		return nil, err
	}
	return &proof, nil
}

func (c *DBClient) VerifyRangeProof(
	ctx context.Context,
	proof *merkledb.RangeProof,
	startKey maybe.Maybe[[]byte],
	endKey maybe.Maybe[[]byte],
	expectedEndrootID ids.ID,
	maxKeys int,
) error {
	resp, err := c.client.VerifyRangeProof(ctx, &pb.VerifyRangeProofRequest{
		Proof: proof.ToProto(),
		StartKey: &pb.MaybeBytes{
			Value:     startKey.Value(),
			IsNothing: startKey.IsNothing(),
		},
		EndKey: &pb.MaybeBytes{
			Value:     endKey.Value(),
			IsNothing: endKey.IsNothing(),
		},
		ExpectedRootHash: expectedEndrootID[:],
		MaxKeys:          int32(maxKeys),
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

func (c *DBClient) VerifyChangeProof(
	ctx context.Context,
	proof *merkledb.ChangeProof,
	startKey maybe.Maybe[[]byte],
	endKey maybe.Maybe[[]byte],
	expectedRootID ids.ID,
	maxKeys int,
) error {
	resp, err := c.client.VerifyChangeProof(ctx, &pb.VerifyChangeProofRequest{
		Proof: proof.ToProto(),
		StartKey: &pb.MaybeBytes{
			Value:     startKey.Value(),
			IsNothing: startKey.IsNothing(),
		},
		EndKey: &pb.MaybeBytes{
			Value:     endKey.Value(),
			IsNothing: endKey.IsNothing(),
		},
		ExpectedRootHash: expectedRootID[:],
		MaxKeys:          int32(maxKeys),
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

func (c *DBClient) CommitChangeProof(ctx context.Context, endKey maybe.Maybe[[]byte], proof *merkledb.ChangeProof) (maybe.Maybe[[]byte], error) {
	resp, err := c.client.CommitChangeProof(ctx, &pb.CommitChangeProofRequest{
		Proof: proof.ToProto(),
		EndKey: &pb.MaybeBytes{
			IsNothing: endKey.IsNothing(),
			Value:     endKey.Value(),
		},
	})
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	if resp.NextKey == nil || resp.NextKey.IsNothing {
		return maybe.Nothing[[]byte](), nil
	}
	return maybe.Some(resp.NextKey.Value), nil
}

func (c *DBClient) GetRangeProofAtRoot(
	ctx context.Context,
	rootID ids.ID,
	startKey maybe.Maybe[[]byte],
	endKey maybe.Maybe[[]byte],
	keyLimit int,
) (*merkledb.RangeProof, error) {
	if rootID == ids.Empty {
		return nil, merkledb.ErrEmptyProof
	}

	resp, err := c.client.GetRangeProof(ctx, &pb.GetRangeProofRequest{
		RootHash: rootID[:],
		StartKey: &pb.MaybeBytes{
			IsNothing: startKey.IsNothing(),
			Value:     startKey.Value(),
		},
		EndKey: &pb.MaybeBytes{
			IsNothing: endKey.IsNothing(),
			Value:     endKey.Value(),
		},
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
	startKey maybe.Maybe[[]byte],
	endKey maybe.Maybe[[]byte],
	proof *merkledb.RangeProof,
) (maybe.Maybe[[]byte], error) {
	resp, err := c.client.CommitRangeProof(ctx, &pb.CommitRangeProofRequest{
		StartKey: &pb.MaybeBytes{
			IsNothing: startKey.IsNothing(),
			Value:     startKey.Value(),
		},
		EndKey: &pb.MaybeBytes{
			IsNothing: endKey.IsNothing(),
			Value:     endKey.Value(),
		},
		RangeProof: proof.ToProto(),
	})
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}
	if resp.NextKey == nil || resp.NextKey.IsNothing {
		return maybe.Nothing[[]byte](), nil
	}
	return maybe.Some(resp.NextKey.Value), nil
}

func (c *DBClient) Clear() error {
	_, err := c.client.Clear(context.Background(), &emptypb.Empty{})
	return err
}
