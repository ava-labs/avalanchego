// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	pvmblock "github.com/ava-labs/avalanchego/vms/proposervm/block"
	psigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

// TestByzantineGossip checks that a tx that a Byzantine peer gossips to one
// honest node goes through the real p2p stack into the mempool, and that
// every honest node then accepts it.
func TestByzantineGossip(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, _, key := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	b, err := connectByzantine(ctx, nodes[0].StakingAddress())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, b.close(context.WithoutCancel(ctx)))
	})

	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(key), primary.WalletConfig{})
	require.NoError(t, err)

	utx, err := wallet.Builder().NewBaseTx(
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

	tx, err := psigner.SignUnsigned(ctx, wallet.Signer(), utx)
	require.NoError(t, err)

	require.NoError(t, b.gossipTx(ctx, tx))
	for _, n := range nodes {
		require.NoError(t, platformvm.NewClient(n.APIURI()).AwaitTxAccepted(ctx, tx.ID(), 100*time.Millisecond), n.ID)
	}
}

// TestByzantineValidatorGetsBlock checks that a peer with a validator's
// staking key connects to an honest node and fetches the preferred proposervm
// block over p2p.
func TestByzantineValidatorGetsBlock(t *testing.T) {
	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, byzVdrs, funded := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	b, err := connectValidator(ctx, nodes[0].StakingAddress(), byzVdrs[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, b.close(context.WithoutCancel(ctx)))
	})

	// Issue a tx so that the P-Chain builds and accepts a block to fetch.
	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(funded), primary.WalletConfig{})
	require.NoError(t, err)
	tx, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: wallet.Builder().Context().AVAXAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{funded.Address()}}},
		}},
		common.WithContext(ctx),
	)
	require.NoError(t, err)
	require.NoError(t, platformvm.NewClient(nodes[0].APIURI()).AwaitTxAccepted(ctx, tx.ID(), 100*time.Millisecond))

	// The last block that node 0 accepted on the P-Chain, as a proposervm ID.
	var blkID ids.ID
	require.Eventually(t, func() bool {
		accepted := nodes[0].Accepted(constants.PlatformChainID)
		if len(accepted) == 0 {
			return false
		}
		blkID = accepted[len(accepted)-1]
		return true
	}, 10*time.Second, 100*time.Millisecond)

	blockBytes, err := b.getBlock(ctx, constants.PlatformChainID, blkID)
	require.NoError(t, err)

	blk, err := pvmblock.Parse(blockBytes, constants.PlatformChainID)
	require.NoError(t, err)
	require.Equal(t, blkID, blk.ID())
}

// TestByzantineProposesBlock checks that a Byzantine validator can build a
// valid proposervm block in its own slot, send it to an honest node, and have
// the honest nodes accept it.
func TestByzantineProposesBlock(t *testing.T) {
	// The build-and-sign path works (see buildProposerBlock), but asserting
	// acceptance races the honest proposers: with ProposerMinBlockDelay 0 the
	// honest chain advances every ~1s, so by the time the Byzantine
	// validator's slot arrives the block builds on a stale parent. Landing
	// this needs the proxy to hold honest traffic while the Byzantine block is
	// injected, which is the next step.
	t.Skip("acceptance races honest proposers; needs proxy hold")

	p := &proxy{}
	t.Cleanup(p.Close)
	nodes, byzVdrs, funded := startNodes(t, keptTempDir(t), 1, p)

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	key := byzVdrs[0]
	b, err := connectValidator(ctx, nodes[0].StakingAddress(), key)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, b.close(context.WithoutCancel(ctx)))
	})

	// Issue a tx so the P-Chain accepts a block to build on.
	wallet, err := primary.MakePWallet(ctx, nodes[0].APIURI(), secp256k1fx.NewKeychain(funded), primary.WalletConfig{})
	require.NoError(t, err)
	seed, err := wallet.IssueBaseTx(
		[]*avax.TransferableOutput{{
			Asset: avax.Asset{ID: wallet.Builder().Context().AVAXAssetID},
			Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{funded.Address()}}},
		}},
		common.WithContext(ctx),
	)
	require.NoError(t, err)
	require.NoError(t, platformvm.NewClient(nodes[0].APIURI()).AwaitTxAccepted(ctx, seed.ID(), 100*time.Millisecond))

	var parentID ids.ID
	require.Eventually(t, func() bool {
		a := nodes[0].Accepted(constants.PlatformChainID)
		if len(a) == 0 {
			return false
		}
		parentID = a[len(a)-1]
		return true
	}, 10*time.Second, 100*time.Millisecond)

	parentBytes, err := b.getBlock(ctx, constants.PlatformChainID, parentID)
	require.NoError(t, err)
	parentPVM, err := pvmblock.Parse(parentBytes, constants.PlatformChainID)
	require.NoError(t, err)
	innerParent, err := platform.ParseBlock(platform.Codec, parentPVM.Block())
	require.NoError(t, err)

	// An empty time-advancing standard block on top of the parent is valid.
	parentTime := innerParent.(interface{ Timestamp() time.Time }).Timestamp()
	inner, err := platform.NewBanffStandardBlock(
		parentTime.Add(time.Second),
		innerParent.ID(),
		innerParent.Height()+1,
		nil,
	)
	require.NoError(t, err)

	var vdrs []ids.NodeID
	for _, n := range nodes {
		vdrs = append(vdrs, n.ID)
	}
	vdrs = append(vdrs, key.nodeID)

	blockBytes, err := key.buildProposerBlock(ctx, constants.PlatformChainID, parentBytes, inner, vdrs)
	require.NoError(t, err)

	blk, err := pvmblock.Parse(blockBytes, constants.PlatformChainID)
	require.NoError(t, err)
	require.NoError(t, b.proposeBlock(ctx, constants.PlatformChainID, blockBytes))

	// Every honest node accepts the Byzantine validator's block.
	require.Eventually(t, func() bool {
		for _, n := range nodes {
			if !slices.Contains(n.Accepted(constants.PlatformChainID), blk.ID()) {
				return false
			}
		}
		return true
	}, 30*time.Second, 200*time.Millisecond)
}
