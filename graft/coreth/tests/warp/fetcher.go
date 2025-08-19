// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpBackend "github.com/ava-labs/coreth/warp"
)

type apiFetcher struct {
	clients map[ids.NodeID]warpBackend.Client
}

func NewAPIFetcher(clients map[ids.NodeID]warpBackend.Client) *apiFetcher {
	return &apiFetcher{
		clients: clients,
	}
}

func (f *apiFetcher) GetSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
	client, ok := f.clients[nodeID]
	if !ok {
		return nil, fmt.Errorf("no warp client for nodeID: %s", nodeID)
	}
	var signatureBytes []byte
	parsedPayload, err := payload.Parse(unsignedWarpMessage.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to parse unsigned message payload: %w", err)
	}
	switch p := parsedPayload.(type) {
	case *payload.AddressedCall:
		signatureBytes, err = client.GetMessageSignature(ctx, unsignedWarpMessage.ID())
	case *payload.Hash:
		signatureBytes, err = client.GetBlockSignature(ctx, p.Hash)
	}
	if err != nil {
		return nil, err
	}

	signature, err := bls.SignatureFromBytes(signatureBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature from client %s: %w", nodeID, err)
	}
	return signature, nil
}
