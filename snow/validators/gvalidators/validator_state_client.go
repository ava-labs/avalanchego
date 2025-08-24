// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gvalidators

import (
	"context"
	"errors"
	"maps"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
)

const validatorSetsCacheSize = 128

var (
	_                             validators.State = (*Client)(nil)
	errFailedPublicKeyDeserialize                  = errors.New("couldn't deserialize public key")
)

type Client struct {
	client pb.ValidatorStateClient

	// Maps caches for each subnet.
	// Key: Subnet ID
	// Value: cache mapping height -> validator set map
	caches map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]
}

func NewClient(client pb.ValidatorStateClient) *Client {
	return &Client{
		client: client,
		caches: make(map[ids.ID]cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput]),
	}
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

func (c *Client) GetValidatorSet(
	ctx context.Context,
	height uint64,
	subnetID ids.ID,
) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	validatorSetsCache := c.getValidatorSetCache(subnetID)

	if validatorSet, ok := validatorSetsCache.Get(height); ok {
		return maps.Clone(validatorSet), nil
	}

	resp, err := c.client.GetValidatorSet(ctx, &pb.GetValidatorSetRequest{
		Height:   height,
		SubnetId: subnetID[:],
	})
	if err != nil {
		return nil, err
	}

	vdrs := make(map[ids.NodeID]*validators.GetValidatorOutput, len(resp.Validators))
	for _, validator := range resp.Validators {
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
		vdrs[nodeID] = &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: publicKey,
			Weight:    validator.Weight,
		}
	}

	// cache the validator set
	validatorSetsCache.Put(height, maps.Clone(vdrs))

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
		return nil, 0, err
	}

	vdrs := make(map[ids.ID]*validators.GetCurrentValidatorOutput, len(resp.Validators))
	for _, validator := range resp.Validators {
		nodeID, err := ids.ToNodeID(validator.NodeId)
		if err != nil {
			return nil, 0, err
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
				return nil, 0, errFailedPublicKeyDeserialize
			}
		}
		validationID, err := ids.ToID(validator.ValidationId)
		if err != nil {
			return nil, 0, err
		}

		vdrs[validationID] = &validators.GetCurrentValidatorOutput{
			ValidationID:  validationID,
			NodeID:        nodeID,
			PublicKey:     publicKey,
			Weight:        validator.Weight,
			StartTime:     validator.StartTime,
			MinNonce:      validator.MinNonce,
			IsActive:      validator.IsActive,
			IsL1Validator: validator.IsL1Validator,
		}
	}
	return vdrs, resp.GetCurrentHeight(), nil
}

func (c *Client) getValidatorSetCache(subnetID ids.ID) cache.Cacher[uint64, map[ids.NodeID]*validators.GetValidatorOutput] {
	// Don't cache the primary network
	if subnetID != constants.PrimaryNetworkID {
		return &cache.Empty[uint64, map[ids.NodeID]*validators.GetValidatorOutput]{}
	}

	validatorSetsCache, exists := c.caches[subnetID]
	if exists {
		return validatorSetsCache
	}

	validatorSetsCache = lru.NewCache[uint64, map[ids.NodeID]*validators.GetValidatorOutput](validatorSetsCacheSize)
	c.caches[subnetID] = validatorSetsCache
	return validatorSetsCache
}
