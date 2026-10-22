// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// TestNodes checks that the nodes are distinct and connected, and that they
// all accept a P-Chain tx in the same block. Nodes reach each other only
// through the proxy, so this also tests the proxy.
func TestNodes(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, _, key := startNodes(t, keptTempDir(t), 0, p)

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	nodeIDs := make(map[ids.NodeID]struct{})
	for _, n := range nodes {
		client := info.NewClient(n.APIURI())
		nodeID, _, err := client.GetNodeID(ctx)
		require.NoError(t, err)
		nodeIDs[nodeID] = struct{}{}

		peers, err := client.Peers(ctx, nil)
		require.NoError(t, err)
		require.Len(t, peers, len(nodes)-1, nodeID)
	}
	require.Len(t, nodeIDs, len(nodes))

	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(key), primary.WalletConfig{})
	require.NoError(t, err)

	tx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: wallet.Builder().Context().AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: units.Avax,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.Address()},
				},
			},
		}},
		common.WithContext(ctx),
	)
	require.NoError(t, err)

	for _, n := range nodes {
		require.NoError(t, platformvm.NewClient(n.APIURI()).AwaitTxAccepted(ctx, tx.ID(), 100*time.Millisecond), n.ID)
	}

	// Every node accepts the same P-Chain blocks, in the same order, as
	// consensus reports them.
	require.Eventually(t, func() bool {
		want := nodes[0].Accepted(constants.PlatformChainID)
		for _, n := range nodes {
			if len(want) == 0 || !slices.Equal(want, n.Accepted(constants.PlatformChainID)) {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond, "nodes accepted different P-Chain blocks")
}
