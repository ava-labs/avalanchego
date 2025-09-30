// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

var (
	_                             validators.State = (*Client)(nil)
	errFailedPublicKeyDeserialize                  = errors.New("couldn't deserialize public key")
)

type Client struct {
	client pb.ValidatorStateClient
}

func NewClient(client pb.ValidatorStateClient) *Client {
	return &Client{client: client}
}

func (c *Client) GetMinimumHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetMinimumHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetCurrentHeight(ctx context.Context) (uint64, error) {
	resp, err := c.client.GetCurrentHeight(ctx, &emptypb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *Client) GetSubnetID(ctx context.Context, chainID ids.ID) (ids.ID, error) {
	resp, err := c.client.GetSubnetID(ctx, &pb.GetSubnetIDRequest{
		ChainId: chainID[:],
	})
	if err != nil {
		return ids.Empty, err
	}
	return ids.ToID(resp.SubnetId)
}

func (c *Client) GetWarpValidatorSets(
	ctx context.Context,
	height uint64,
) (map[ids.ID]*validators.WarpSet, error) {
	resp, err := c.client.GetWarpValidatorSets(
		ctx,
		&pb.GetWarpValidatorSetsRequest{
			Height: height,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get all validator sets: %w", err)
	}

	validatorSets := make(map[ids.ID]*validators.WarpSet, len(resp.ValidatorSets))
	for _, validatorSet := range resp.ValidatorSets {
		subnetID, err := ids.ToID(validatorSet.GetSubnetId())
		if err != nil {
			return nil, fmt.Errorf("failed to parse subnet ID %s: %w", subnetID, err)
		}

		vdrs, err := warpValidatorsFromProto(validatorSet.GetValidators())
		if err != nil {
			return nil, fmt.Errorf("failed to parse warp validators: %w", err)
		}
		validatorSets[subnetID] = &validators.WarpSet{
			Validators:  vdrs,
			TotalWeight: validatorSet.GetTotalWeight(),
		}
	}

	return validatorSets, nil
}

func (c *Client) GetWarpValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (*validators.WarpSet, error) {
	resp, err := c.client.GetWarpValidatorSet(
		ctx,
		&pb.GetWarpValidatorSetRequest{
			Height:   height,
			SubnetId: subnetID[:],
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get all validator sets: %w", err)
	}

	vdrs, err := warpValidatorsFromProto(resp.GetValidators())
	if err != nil {
		return nil, fmt.Errorf("failed to parse warp validators: %w", err)
	}
	return &validators.WarpSet{
		Validators:  vdrs,
		TotalWeight: resp.GetTotalWeight(),
	}, nil
}

func (c *Client) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	resp, err := c.client.GetValidatorSet(ctx, &pb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set for subnet %s at height %d: %w", subnetID.String(), height, err)
	}

	vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, len(resp.Validators))
	for _, validator := range resp.Validators {
		vdr, err := validatorOutputFromProto(validator)
		if err != nil {
			return nil, fmt.Errorf("failed to parse validator: %w", err)
		}
		vdrs[vdr.NodeID] = vdr
	}
	return vdrs, nil
}

func (c *Client) GetCurrentValidatorSet(
	ctx context.Context,
	subnetID ids.ID,
) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	resp, err := c.client.GetCurrentValidatorSet(ctx, &pb.GetCurrentValidatorSetRequest{
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get current validator set for subnet %s: %w", subnetID.String(), err)
	}

	vdrs := make(map[ids.ID]*validators.GetCurrentValidatorOutput, len(resp.Validators))
	for _, validator := range resp.Validators {
		vdr, err := validatorOutputFromProto(validator)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse validator: %w", err)
		}
		validationID, err := ids.ToID(validator.ValidationId)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to parse validation ID %s: %w", validationID.String(), err)
		}

		vdrs[validationID] = &validators.GetCurrentValidatorOutput{
			ValidationID:  validationID,
			NodeID:        vdr.NodeID,
			PublicKey:     vdr.PublicKey,
			Weight:        validator.Weight,
			StartTime:     validator.StartTime,
			MinNonce:      validator.MinNonce,
			IsActive:      validator.IsActive,
			IsL1Validator: validator.IsL1Validator,
		}
	}
	return vdrs, resp.GetCurrentHeight(), nil
}

func validatorOutputFromProto(validator *pb.Validator) (*validators.GetValidatorOutput, error) {
	nodeID, err := ids.ToNodeID(validator.NodeId)
	if err != nil {
		return nil, err
	}

	var publicKey *bls.PublicKey
	if len(validator.PublicKey) > 0 {
		// PublicKeyFromValidUncompressedBytes is used rather than
		// PublicKeyFromCompressedBytes because it is significantly faster
		// due to the avoidance of decompression and key re-verification. We
		// can safely assume that the BLS Public Keys are verified before
		// being added to the P-Chain and served by the gRPC server.
		publicKey = bls.PublicKeyFromValidUncompressedBytes(validator.PublicKey)
		if publicKey == nil {
			return nil, errFailedPublicKeyDeserialize
		}
	}

	return &validators.GetValidatorOutput{
		NodeID:    nodeID,
		PublicKey: publicKey,
		Weight:    validator.Weight,
	}, nil
}

func warpValidatorsFromProto(proto []*pb.WarpValidator) ([]*validators.Warp, error) {
	vdrs := make([]*validators.Warp, len(proto))
	for i, vdr := range proto {
		pkBytes := vdr.GetPublicKey()
		nodeIDsBytes := vdr.GetNodeIds()
		nodeIDs := make([]ids.NodeID, len(nodeIDsBytes))
		for j, nodeIDBytes := range nodeIDsBytes {
			nodeID, err := ids.ToNodeID(nodeIDBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to parse node ID %s: %w", nodeID, err)
			}
			nodeIDs[j] = nodeID
		}
		vdrs[i] = &validators.Warp{
			PublicKey:      bls.PublicKeyFromValidUncompressedBytes(pkBytes),
			PublicKeyBytes: pkBytes,
			Weight:         vdr.GetWeight(),
			NodeIDs:        nodeIDs,
		}
	}
	return vdrs, nil
}
