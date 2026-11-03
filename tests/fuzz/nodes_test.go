// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
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
// through the proxy, so this also tests the proxy. It runs in a synctest
// bubble, so it also checks that the nodes leave no goroutine behind.
func TestNodes(t *testing.T) {
	inBubble(t, checkNodes)
}

// inBubble runs f in a synctest bubble, where time moves only when every
// goroutine waits, so the network must hold no goroutine that a socket
// blocks.
func inBubble(t *testing.T, f func(t *testing.T)) {
	synctest.Test(t, func(t *testing.T) {
		// Runs after the nodes stop. A closed leveldb drains its memory pool
		// for up to a second, and time stops once the bubble's root
		// goroutine returns.
		t.Cleanup(func() {
			time.Sleep(time.Second)
		})
		f(t)
	})
}

func checkNodes(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	root := keptTempDir(t)
	nodes, _, key := startNodes(t, root, 0, p)

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

	// Each node logs its events to the harness, which writes them under the
	// node's data dir. A node writes an event after it handles the input, so
	// the line can trail the API.
	for _, n := range nodes {
		var logged []byte
		require.Eventually(t, func() bool {
			b, err := n.flushEvents()
			logged = append(logged, b...)
			return err == nil && bytes.Contains(logged, []byte(tx.ID().String()))
		}, 10*time.Second, 50*time.Millisecond, "node %s does not name tx %s", n.ID, tx.ID())

		b, err := os.ReadFile(filepath.Join(root, n.ID.String(), "logs", "events.log"))
		require.NoError(t, err)
		require.Equal(t, logged, b)
	}
}
