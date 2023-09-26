// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

type apiFetcher struct {
	clients map[ids.NodeID]Client
}

func NewAPIFetcher(clients map[ids.NodeID]Client) *apiFetcher {
	return &apiFetcher{
		clients: clients,
	}
}

func (f *apiFetcher) FetchWarpSignature(ctx context.Context, nodeID ids.NodeID, unsignedWarpMessage *avalancheWarp.UnsignedMessage) (*bls.Signature, error) {
	client, ok := f.clients[nodeID]
	if !ok {
		return nil, fmt.Errorf("no warp client for nodeID: %s", nodeID)
	}

	signatureBytes, err := client.GetSignature(ctx, unsignedWarpMessage.ID())
	if err != nil {
		return nil, err
	}

	signature, err := bls.SignatureFromBytes(signatureBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature from client %s: %w", nodeID, err)
	}
	return signature, nil
}
